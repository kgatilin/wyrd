package serve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounce is the window used to coalesce rapid file events into
// a single batch. 200ms is enough to swallow editor save bursts and
// git checkout storms without noticeably delaying single-change reloads.
const DefaultDebounce = 200 * time.Millisecond

// EventHandler is invoked for each flushed batch of file paths. The
// Watcher calls it serially; the handler itself is responsible for any
// further concurrency.
type EventHandler func(paths []string)

// Watcher wraps an fsnotify.Watcher with a debounced event channel.
// Events arriving within Debounce of one another are coalesced into a
// single batch and delivered to the handler.
type Watcher struct {
	inner    *fsnotify.Watcher
	root     string
	debounce time.Duration

	// skipDir returns true when the given directory should not be
	// walked into when adding watches. Tests inject one; production
	// code uses defaultSkipDir.
	skipDir func(name string) bool
}

// NewWatcher creates a fsnotify-backed Watcher rooted at root. The
// watcher recursively adds every directory under root that passes
// skipDir. Debounce defaults to DefaultDebounce when zero.
func NewWatcher(root string, debounce time.Duration) (*Watcher, error) {
	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("serve: creating fsnotify watcher: %w", err)
	}
	if debounce <= 0 {
		debounce = DefaultDebounce
	}
	w := &Watcher{
		inner:    inner,
		root:     root,
		debounce: debounce,
		skipDir:  defaultSkipDir,
	}
	if err := w.addTree(root); err != nil {
		_ = inner.Close()
		return nil, err
	}
	return w, nil
}

// Close stops the underlying fsnotify watcher. Safe to call multiple times.
func (w *Watcher) Close() error {
	if w == nil || w.inner == nil {
		return nil
	}
	return w.inner.Close()
}

// Run pumps events from fsnotify into debounced batches and invokes
// handler for each batch. It returns when ctx is cancelled or the
// fsnotify event channel is closed. Errors from the underlying watcher
// are reported via the error channel and logged to stderr; they are not
// fatal.
func (w *Watcher) Run(ctx context.Context, handler EventHandler) error {
	if handler == nil {
		return errors.New("serve: watcher handler is nil")
	}

	pending := make(map[string]struct{})
	var timer *time.Timer
	// tickC is nil while no debounce window is active; assigning to
	// timer.C makes the select wake up when the window elapses.
	var tickC <-chan time.Time

	flush := func() {
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		pending = make(map[string]struct{})
		handler(paths)
	}

	for {
		select {
		case <-ctx.Done():
			// Drain any pending batch before exiting so in-flight
			// edits are not silently lost.
			flush()
			return nil

		case ev, ok := <-w.inner.Events:
			if !ok {
				flush()
				return nil
			}
			// If the event is the creation of a new directory, start
			// watching it too so nested edits are picked up.
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					// Best-effort: errors here are non-fatal.
					_ = w.addTree(ev.Name)
				}
			}
			if w.shouldIgnore(ev.Name) {
				continue
			}
			pending[ev.Name] = struct{}{}
			if timer == nil {
				timer = time.NewTimer(w.debounce)
				tickC = timer.C
			} else {
				if !timer.Stop() {
					// Drain the expired timer so Reset cannot race.
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
				tickC = timer.C
			}

		case err, ok := <-w.inner.Errors:
			if !ok {
				flush()
				return nil
			}
			// Non-fatal: log and continue. Callers that need stricter
			// handling can wrap EventHandler.
			fmt.Fprintf(os.Stderr, "serve: watcher error: %v\n", err)

		case <-tickC:
			flush()
			tickC = nil
			timer = nil
		}
	}
}

// addTree recursively registers every directory under root (that isn't
// skipped) with the underlying watcher.
func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip dirs we can't read rather than aborting the whole walk.
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if w.skipDir(d.Name()) && path != root {
			return filepath.SkipDir
		}
		if err := w.inner.Add(path); err != nil {
			// If a single directory can't be watched (e.g. too many
			// inodes) log and keep going; the daemon degrades to
			// whatever subset it can monitor.
			fmt.Fprintf(os.Stderr, "serve: adding watch %s: %v\n", path, err)
		}
		return nil
	})
}

// shouldIgnore returns true for paths the daemon should never react to
// (vendored code, VCS metadata, target snapshots, editor swap files).
func (w *Watcher) shouldIgnore(path string) bool {
	base := filepath.Base(path)
	// Editor/VCS junk.
	if strings.HasPrefix(base, ".#") || strings.HasSuffix(base, "~") || strings.HasSuffix(base, ".swp") {
		return true
	}
	// Changes inside .arch/targets are generated; reacting would
	// loop the daemon against its own writes.
	if strings.Contains(filepath.ToSlash(path), "/.arch/targets/") {
		// ...except the CURRENT pointer, which callers care about.
		if base == "CURRENT" {
			return false
		}
		return true
	}
	return false
}

// defaultSkipDir returns true for directories the watcher should not
// descend into when adding watches. Keeping this list tight is
// important: each directory adds an inotify (or equivalent) watch.
func defaultSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".idea", ".vscode":
		return true
	}
	return false
}
