package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

// Options configures the Serve entry point.
type Options struct {
	// Root is the project root directory. Defaults to cwd when empty.
	Root string

	// MCPStdio enables the MCP stdio transport. When true, MCPServe is
	// invoked with the live State so the transport can answer tool
	// calls from its in-memory snapshot.
	MCPStdio bool

	// MCPServe is the MCP stdio entry point. Left as a callback so the
	// serve package avoids importing the mcp adapter (which depends on
	// serve). cmd/archai wires this to mcp.Serve.
	MCPServe func(ctx context.Context, state *State) error

	// HTTPAddr enables the HTTP transport on the given address
	// (e.g. ":8080"). Empty disables HTTP.
	HTTPAddr string

	// HTTPServerFactory, when non-nil, is invoked after the initial
	// model load and receives the live State. The returned transport
	// is started by Serve whenever HTTPAddr is set. Factory-style
	// wiring keeps the serve package free of an http dependency
	// (internal/adapter/http imports serve, so serve cannot import
	// back).
	HTTPServerFactory func(*State) (HTTPTransport, error)

	// MultiState, when non-nil, switches the daemon into multi-worktree
	// mode: instead of a single State, the transport (typically the
	// HTTP server constructed by HTTPServerFactory, which in this mode
	// receives nil) manages one State per discovered worktree via
	// MultiState. Per-worktree fsnotify watchers are installed lazily
	// as each State is loaded (one watcher per loaded worktree).
	MultiState *MultiState

	// Debug enables verbose per-event logging.
	Debug bool

	// LogOut is the writer for human-readable daemon output. Defaults
	// to os.Stderr when nil.
	LogOut io.Writer

	// Debounce overrides the event coalescing window. Zero uses the
	// default (200ms). Exposed mainly for tests.
	Debounce time.Duration

	// IdleTimeout, when non-zero, cancels the daemon after this much
	// wall-clock time passes without any HTTP request being handled.
	// Used by auto-started MCP daemons so they don't outlive their
	// clients. Zero disables the idle timer entirely (the default for
	// user-started `archai serve`).
	IdleTimeout time.Duration
}

// HTTPTransport is the minimal contract the serve daemon needs from an
// HTTP transport: bind to addr, invoke ready(boundAddr) once the
// listener is up, then serve until ctx is cancelled. Returns nil on a
// graceful shutdown. The ready callback is how callers learn the real
// bound address when addr uses port 0. Implemented by
// internal/adapter/http.Server.
type HTTPTransport interface {
	Serve(ctx context.Context, addr string, ready func(boundAddr string)) error
}

// ActivityAware is implemented by HTTP transports that expose a hook
// for observing each handled request. serve.Serve wires this to the
// idle-timeout monitor so IdleTimeout only fires after a real period
// of HTTP quiet. Transports that don't implement it simply won't
// participate in idle shutdown (and IdleTimeout becomes a no-op).
type ActivityAware interface {
	SetActivityObserver(func())
}

// Serve runs the daemon: it builds the in-memory model, starts the
// fsnotify watcher, wires stub transports, and blocks until ctx is
// cancelled. Callers are expected to bridge SIGINT/SIGTERM into ctx.
func Serve(ctx context.Context, opts Options) error {
	root := opts.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("serve: resolving cwd: %w", err)
		}
		root = cwd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("serve: resolving root %s: %w", root, err)
	}

	logOut := opts.LogOut
	if logOut == nil {
		logOut = os.Stderr
	}

	// In multi-worktree mode we defer per-worktree loads to the
	// MultiState; there is no shared State. The HTTP factory receives
	// nil because the http.Server handles multi dispatch itself.
	var state *State
	if opts.MultiState == nil {
		fmt.Fprintf(logOut, "serve: loading model from %s\n", absRoot)
		state = NewState(absRoot)
		if err := state.Load(ctx); err != nil {
			return err
		}
		snap := state.Snapshot()
		fmt.Fprintf(logOut, "serve: loaded %d package(s), overlay=%v, target=%q\n",
			len(snap.Packages), snap.Overlay != nil, snap.CurrentTarget)
	} else {
		names := opts.MultiState.Names()
		fmt.Fprintf(logOut, "serve: multi-worktree mode, %d worktree(s) discovered: %v\n",
			len(names), names)
		// Install a per-worktree fsnotify hook: each State gets its own
		// watcher the first time it is loaded. The watchers are closed
		// when Refresh drops a worktree or when the daemon shuts down.
		opts.MultiState.SetWatcherHook(multiWatcherHook(ctx, opts.Debounce, logOut, opts.Debug))
		defer func() { _ = opts.MultiState.Close() }()
	}

	// HTTP transport: start in a goroutine when both an address and a
	// factory are provided. If the caller set the address but didn't
	// wire a factory we keep the old stub log so operators aren't
	// silently ignored.
	//
	// When a transport comes up we record a serve.json for this
	// worktree so `archai where` / `archai list-daemons` can find us.
	// The record is removed on graceful shutdown below.
	// Derive a child context that either the caller's ctx or the
	// idle-timeout monitor can cancel. The monitor only runs when an
	// HTTP transport is started and IdleTimeout > 0.
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	// lastActivity holds the Unix-nano timestamp of the most recent
	// HTTP request. Seeded at start so the idle monitor measures from
	// daemon boot (not from Unix epoch).
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	touchActivity := func() { lastActivity.Store(time.Now().UnixNano()) }

	httpErrCh := make(chan error, 1)
	var httpStarted bool
	var serveRecorded bool
	wtName := worktree.Name(absRoot)
	if opts.HTTPAddr != "" {
		if opts.HTTPServerFactory != nil {
			srv, err := opts.HTTPServerFactory(state)
			if err != nil {
				return fmt.Errorf("serve: building HTTP transport: %w", err)
			}
			if aware, ok := srv.(ActivityAware); ok {
				aware.SetActivityObserver(touchActivity)
			}
			fmt.Fprintf(logOut, "serve: HTTP transport binding %s (worktree=%q)\n", opts.HTTPAddr, wtName)
			httpStarted = true
			ready := func(boundAddr string) {
				rec := worktree.ServeRecord{
					PID:       os.Getpid(),
					HTTPAddr:  boundAddr,
					StartedAt: time.Now().UTC().Format(time.RFC3339),
				}
				if err := worktree.WriteServe(absRoot, wtName, rec); err != nil {
					fmt.Fprintf(logOut, "serve: write serve.json: %v\n", err)
					return
				}
				serveRecorded = true
				fmt.Fprintf(logOut, "serve: HTTP transport listening on %s\n", boundAddr)
			}
			go func() {
				httpErrCh <- srv.Serve(runCtx, opts.HTTPAddr, ready)
			}()

			// Idle-timeout monitor. Only meaningful when HTTP is up —
			// otherwise there's nothing to count as activity.
			if opts.IdleTimeout > 0 {
				fmt.Fprintf(logOut, "serve: idle-timeout %s enabled\n", opts.IdleTimeout)
				go runIdleMonitor(runCtx, opts.IdleTimeout, &lastActivity, runCancel, logOut)
			}
		} else {
			fmt.Fprintf(logOut, "serve: HTTP transport requested on %s — no transport wired (stub)\n", opts.HTTPAddr)
		}
	}
	// Always remove serve.json on return so a killed process doesn't
	// leave a dangling record when the shutdown path is taken.
	defer func() {
		if serveRecorded {
			if err := worktree.RemoveServe(absRoot, wtName); err != nil {
				fmt.Fprintf(logOut, "serve: remove serve.json: %v\n", err)
			}
		}
	}()

	// The fsnotify watcher is only meaningful against a single State;
	// in multi mode we skip it and rely on manual refreshes (future:
	// per-worktree watchers keyed by MultiState entries).
	var handler EventHandler
	var watcher *Watcher
	if state != nil {
		w, err := NewWatcher(absRoot, opts.Debounce)
		if err != nil {
			return err
		}
		watcher = w
		defer func() { _ = watcher.Close() }()
		handler = buildHandler(runCtx, state, logOut, opts.Debug)
	}

	// When the MCP stdio transport is requested, it owns the process's
	// stdin/stdout and drives shutdown: we run the watcher loop in a
	// goroutine for the duration of the MCP session. Stdout is reserved
	// for JSON-RPC frames; diagnostics stay on stderr (logOut).
	if opts.MCPStdio {
		if opts.MCPServe == nil {
			return fmt.Errorf("serve: --mcp-stdio set but MCPServe is nil")
		}
		if watcher == nil {
			return fmt.Errorf("serve: --mcp-stdio is not compatible with multi-worktree mode")
		}

		childCtx, cancel := context.WithCancel(runCtx)
		defer cancel()

		watchErrCh := make(chan error, 1)
		go func() {
			fmt.Fprintln(logOut, "serve: watching for changes (MCP stdio mode)")
			watchErrCh <- watcher.Run(childCtx, handler)
		}()

		fmt.Fprintln(logOut, "serve: starting MCP stdio transport")
		mcpErr := opts.MCPServe(childCtx, state)
		cancel()

		// Drain the watcher goroutine so we don't leak it.
		<-watchErrCh

		if mcpErr != nil && !errors.Is(mcpErr, context.Canceled) {
			return mcpErr
		}
		fmt.Fprintln(logOut, "serve: shutdown complete")
		return nil
	}

	if watcher != nil {
		fmt.Fprintln(logOut, "serve: watching for changes (Ctrl-C to stop)")
		watchErr := watcher.Run(runCtx, handler)
		if watchErr != nil && !errors.Is(watchErr, context.Canceled) {
			return watchErr
		}
	} else {
		// Multi mode: per-worktree watchers are spun up lazily as each
		// State is loaded (see multiWatcherHook). We just block on
		// runCtx here so HTTP stays alive until Ctrl-C or the
		// idle-timeout monitor cancels; the deferred MultiState.Close
		// stops every registered watcher on exit.
		fmt.Fprintln(logOut, "serve: multi-worktree mode (Ctrl-C to stop)")
		<-runCtx.Done()
	}

	// Wait for the HTTP goroutine to unwind (if one was started) so we
	// don't return while the listener is still closing sockets.
	if httpStarted {
		if err := <-httpErrCh; err != nil {
			fmt.Fprintf(logOut, "serve: http transport: %v\n", err)
		}
	}

	fmt.Fprintln(logOut, "serve: shutdown complete")
	return nil
}

// multiWatcherHook returns a WatcherHook that installs one fsnotify
// watcher per loaded worktree State, reusing buildHandler so the
// event-dispatch logic matches single-mode exactly. The returned
// io.Closer cancels the watcher goroutine and closes the underlying
// fsnotify watcher; MultiState invokes it on Refresh-drop / Close.
func multiWatcherHook(parent context.Context, debounce time.Duration, logOut io.Writer, debug bool) WatcherHook {
	return func(_ context.Context, name string, state *State) (io.Closer, error) {
		w, err := NewWatcher(state.Root(), debounce)
		if err != nil {
			return nil, err
		}
		childCtx, cancel := context.WithCancel(parent)
		handler := buildHandler(childCtx, state, logOut, debug)
		done := make(chan struct{})
		go func() {
			defer close(done)
			if rerr := w.Run(childCtx, handler); rerr != nil && !errors.Is(rerr, context.Canceled) {
				fmt.Fprintf(logOut, "serve: watcher for %q: %v\n", name, rerr)
			}
		}()
		if debug {
			fmt.Fprintf(logOut, "serve: watcher started for worktree %q at %s\n", name, state.Root())
		}
		return &watcherCloser{cancel: cancel, watcher: w, done: done}, nil
	}
}

// watcherCloser bundles the goroutine cancellation and the fsnotify
// watcher's own Close so MultiState can release both with a single
// io.Closer handle.
type watcherCloser struct {
	cancel  context.CancelFunc
	watcher *Watcher
	done    chan struct{}
}

// Close cancels the watcher goroutine, waits for it to unwind, and
// closes the underlying fsnotify watcher. Idempotent.
func (c *watcherCloser) Close() error {
	if c == nil {
		return nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		<-c.done
	}
	if c.watcher != nil {
		return c.watcher.Close()
	}
	return nil
}

// runIdleMonitor polls lastActivity and cancels the daemon (via cancel)
// once idleTimeout has elapsed without any HTTP request. The poll
// cadence is min(idleTimeout/4, 1s) so short timeouts react quickly in
// tests while long production timeouts don't spin a tight loop.
func runIdleMonitor(ctx context.Context, idleTimeout time.Duration, lastActivity *atomic.Int64, cancel context.CancelFunc, logOut io.Writer) {
	poll := idleTimeout / 4
	if poll > time.Second {
		poll = time.Second
	}
	if poll < 10*time.Millisecond {
		poll = 10 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := time.Unix(0, lastActivity.Load())
			if time.Since(last) >= idleTimeout {
				fmt.Fprintf(logOut, "serve: idle-timeout %s elapsed (last activity %s ago) — shutting down\n",
					idleTimeout, time.Since(last).Round(time.Millisecond))
				cancel()
				return
			}
		}
	}
}

// buildHandler returns the EventHandler closure that dispatches a
// debounced batch of file events into the appropriate state reloads.
func buildHandler(ctx context.Context, state *State, logOut io.Writer, debug bool) EventHandler {
	root := state.Root()

	return func(paths []string) {
		if debug {
			fmt.Fprintf(logOut, "serve: batch %d event(s)\n", len(paths))
		}

		// Deduplicate owning-package reloads within a batch.
		pkgReloads := make(map[string]struct{})
		overlayDirty := false
		currentDirty := false

		for _, p := range paths {
			abs := p
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(root, p)
			}
			rel, err := filepath.Rel(root, abs)
			if err != nil {
				rel = abs
			}
			rel = filepath.ToSlash(rel)

			switch {
			case rel == "archai.yaml":
				overlayDirty = true
			case rel == ".arch/targets/CURRENT":
				// Legacy pre-M9 location. Retained for compatibility
				// so a manually-edited legacy pointer still triggers a
				// reload; the per-worktree equivalent is handled next.
				currentDirty = true
			case strings.HasPrefix(rel, ".arch/.worktree/") && strings.HasSuffix(rel, "/CURRENT"):
				currentDirty = true
			case strings.HasSuffix(abs, ".go"):
				if pkg := state.FindOwningPackage(abs); pkg != "" {
					pkgReloads[pkg] = struct{}{}
				}
			}
		}

		var reloaded []string
		for pkg := range pkgReloads {
			if err := state.ReloadPackage(ctx, pkg); err != nil {
				fmt.Fprintf(logOut, "serve: reload %s: %v\n", pkg, err)
				continue
			}
			reloaded = append(reloaded, pkg)
			if debug {
				fmt.Fprintf(logOut, "serve: reloaded package %s\n", pkg)
			}
		}
		if len(reloaded) > 0 {
			state.PublishPackageReload(reloaded)
		}

		if overlayDirty {
			if err := state.ReloadOverlay(ctx); err != nil {
				fmt.Fprintf(logOut, "serve: reload overlay: %v\n", err)
			} else {
				state.PublishOverlayReload()
				if debug {
					fmt.Fprintln(logOut, "serve: reloaded overlay")
				}
			}
		}

		if currentDirty {
			name := worktree.Name(root)
			id, _, err := worktree.ReadCurrent(root, name)
			if err != nil {
				fmt.Fprintf(logOut, "serve: read CURRENT: %v\n", err)
			} else if err := state.SwitchTarget(id); err != nil {
				fmt.Fprintf(logOut, "serve: switch target: %v\n", err)
			} else {
				state.PublishTargetSwitch(id)
				if debug {
					fmt.Fprintf(logOut, "serve: active target = %q\n", id)
				}
			}
		}
	}
}
