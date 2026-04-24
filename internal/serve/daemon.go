package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	// Debug enables verbose per-event logging.
	Debug bool

	// LogOut is the writer for human-readable daemon output. Defaults
	// to os.Stderr when nil.
	LogOut io.Writer

	// Debounce overrides the event coalescing window. Zero uses the
	// default (200ms). Exposed mainly for tests.
	Debounce time.Duration
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

	fmt.Fprintf(logOut, "serve: loading model from %s\n", absRoot)
	state := NewState(absRoot)
	if err := state.Load(ctx); err != nil {
		return err
	}
	snap := state.Snapshot()
	fmt.Fprintf(logOut, "serve: loaded %d package(s), overlay=%v, target=%q\n",
		len(snap.Packages), snap.Overlay != nil, snap.CurrentTarget)

	// HTTP transport: start in a goroutine when both an address and a
	// factory are provided. If the caller set the address but didn't
	// wire a factory we keep the old stub log so operators aren't
	// silently ignored.
	//
	// When a transport comes up we record a serve.json for this
	// worktree so `archai where` / `archai list-daemons` can find us.
	// The record is removed on graceful shutdown below.
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
				httpErrCh <- srv.Serve(ctx, opts.HTTPAddr, ready)
			}()
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

	watcher, err := NewWatcher(absRoot, opts.Debounce)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	handler := buildHandler(ctx, state, logOut, opts.Debug)

	// When the MCP stdio transport is requested, it owns the process's
	// stdin/stdout and drives shutdown: we run the watcher loop in a
	// goroutine for the duration of the MCP session. Stdout is reserved
	// for JSON-RPC frames; diagnostics stay on stderr (logOut).
	if opts.MCPStdio {
		if opts.MCPServe == nil {
			return fmt.Errorf("serve: --mcp-stdio set but MCPServe is nil")
		}

		childCtx, cancel := context.WithCancel(ctx)
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

	fmt.Fprintln(logOut, "serve: watching for changes (Ctrl-C to stop)")
	watchErr := watcher.Run(ctx, handler)
	if watchErr != nil && !errors.Is(watchErr, context.Canceled) {
		return watchErr
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

		for pkg := range pkgReloads {
			if err := state.ReloadPackage(ctx, pkg); err != nil {
				fmt.Fprintf(logOut, "serve: reload %s: %v\n", pkg, err)
				continue
			}
			if debug {
				fmt.Fprintf(logOut, "serve: reloaded package %s\n", pkg)
			}
		}

		if overlayDirty {
			if err := state.ReloadOverlay(ctx); err != nil {
				fmt.Fprintf(logOut, "serve: reload overlay: %v\n", err)
			} else if debug {
				fmt.Fprintln(logOut, "serve: reloaded overlay")
			}
		}

		if currentDirty {
			name := worktree.Name(root)
			id, _, err := worktree.ReadCurrent(root, name)
			if err != nil {
				fmt.Fprintf(logOut, "serve: read CURRENT: %v\n", err)
			} else if err := state.SwitchTarget(id); err != nil {
				fmt.Fprintf(logOut, "serve: switch target: %v\n", err)
			} else if debug {
				fmt.Fprintf(logOut, "serve: active target = %q\n", id)
			}
		}
	}
}
