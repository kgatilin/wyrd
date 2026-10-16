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

	// seen is the last (mtime, size) observed per file. It decides what an
	// attribute-only event means: a file whose content stamp did not move
	// was only inspected, one whose stamp moved was edited by something that
	// reports edits as attribute changes. Touched only by addTree and Run,
	// which never run concurrently.
	seen map[string]contentStamp
}

// contentStamp is what an edit changes and an inspection does not.
type contentStamp struct {
	mod  int64
	size int64
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
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	w := &Watcher{
		inner:    inner,
		root:     root,
		debounce: debounce,
		skipDir:  defaultSkipDir,
		seen:     make(map[string]contentStamp),
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
			// fsnotify's kqueue backend can name a path outside the tree.
			// A kevent still queued for a watch that a directory removal
			// has just dropped comes out as a Remove of "", and its
			// follow-up check for a replacement file cleans "" to "." and
			// reports a Create of ".": the process's working directory. A
			// launchd service runs in $HOME, so adding that tree walked all
			// of it and set off macOS privacy prompts for Downloads, Photos
			// and iCloud Drive. Nothing outside root is ours to react to.
			if !w.contains(ev.Name) {
				continue
			}
			// An attribute-only event is an edit only when the file's
			// content stamp moved. macOS kqueue reports NOTE_ATTRIB
			// (fsnotify Chmod) for files that were merely inspected —
			// `git diff` and `git status` raise it on every file that
			// differs from the index — and treating that as an edit closed
			// a loop through the review UI: the UI reads /api/gitdiff, git
			// trips NOTE_ATTRIB, the daemon re-parses and publishes
			// model-changed, and the UI reads the diff again. But a host
			// directory shared into a VM (colima's mountInotify) delivers
			// *every* host edit as a chmod in the guest, so dropping the
			// bare Chmod outright left a daemon in a container blind to
			// all edits.
			if ev.Op&^fsnotify.Chmod == 0 {
				if !w.stampMoved(ev.Name) {
					continue
				}
			} else {
				w.stampMoved(ev.Name)
			}
			// If the event is the creation of a new directory, start
			// watching it too so nested edits are picked up.
			// The walk's own skip rules apply to it: addTree exempts its
			// root, which here is the new directory itself.
			if ev.Op&fsnotify.Create != 0 && !w.skipDir(filepath.Base(ev.Name)) {
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

// contains reports whether path names root or something under it.
func (w *Watcher) contains(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(w.root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
			if info, err := d.Info(); err == nil {
				w.seen[path] = stampOf(info)
			}
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

// stampMoved records path's current content stamp and reports whether it
// differs from the last one seen. A path that is gone has no stamp to
// compare and counts as moved, which is the safe direction: a spurious event
// costs one identical re-parse, a dropped one a stale model. A directory's
// own attributes say nothing about the files in it.
func (w *Watcher) stampMoved(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		delete(w.seen, path)
		return true
	}
	if info.IsDir() {
		return false
	}
	next := stampOf(info)
	prev, ok := w.seen[path]
	w.seen[path] = next
	return !ok || prev != next
}

func stampOf(info os.FileInfo) contentStamp {
	return contentStamp{mod: info.ModTime().UnixNano(), size: info.Size()}
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
	if strings.HasPrefix(name, ".") {
		return name != ".arch" && name != ".wyrd"
	}
	if strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "testdata":
		return true
	}
	return false
}
