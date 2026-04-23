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
)

// Options configures the Serve entry point.
type Options struct {
	// Root is the project root directory. Defaults to cwd when empty.
	Root string

	// MCPStdio enables the MCP stdio transport. Currently a stub
	// (M5a only wires the flag so M5b can implement it).
	MCPStdio bool

	// HTTPAddr enables the HTTP transport on the given address
	// (e.g. ":8080"). Empty disables HTTP. Stub until M7a.
	HTTPAddr string

	// Debug enables verbose per-event logging.
	Debug bool

	// LogOut is the writer for human-readable daemon output. Defaults
	// to os.Stderr when nil.
	LogOut io.Writer

	// Debounce overrides the event coalescing window. Zero uses the
	// default (200ms). Exposed mainly for tests.
	Debounce time.Duration
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

	// Transport stubs. M5b / M7a will replace these with real
	// implementations; for now we log so operators know they were
	// requested.
	if opts.MCPStdio {
		fmt.Fprintln(logOut, "serve: MCP stdio transport requested — not implemented yet (stub)")
	}
	if opts.HTTPAddr != "" {
		fmt.Fprintf(logOut, "serve: HTTP transport requested on %s — not implemented yet (stub)\n", opts.HTTPAddr)
	}

	watcher, err := NewWatcher(absRoot, opts.Debounce)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	handler := buildHandler(ctx, state, logOut, opts.Debug)

	fmt.Fprintln(logOut, "serve: watching for changes (Ctrl-C to stop)")
	if err := watcher.Run(ctx, handler); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
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
			id, err := readCurrent(filepath.Join(root, ".arch", "targets", "CURRENT"))
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

// readCurrent reads the single-line CURRENT pointer. Missing file is
// treated as an empty id (no active target).
func readCurrent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
