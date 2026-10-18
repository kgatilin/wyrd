package serve

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// TestWatcherDebounceCoalesces verifies that several fsnotify events
// arriving within the debounce window are delivered as a single batch.
func TestWatcherDebounceCoalesces(t *testing.T) {
	root := t.TempDir()

	w, err := NewWatcher(root, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer func() { _ = w.Close() }()

	var (
		mu      sync.Mutex
		batches [][]string
	)
	done := make(chan struct{})

	handler := func(paths []string) {
		mu.Lock()
		batches = append(batches, append([]string(nil), paths...))
		mu.Unlock()
		// Signal on first batch so the test can proceed without
		// waiting for a second flush that should never arrive.
		select {
		case done <- struct{}{}:
		default:
		}
	}

	// The burst is written before Run starts consuming, so every event of
	// it is already queued (the kernel coalesces repeated writes to one
	// file while nothing reads them) when the loop sees the first one, and
	// the whole burst lands inside one debounce window however loaded the
	// machine is. Writing it while Run is draining made the test's outcome a
	// function of how long five WriteFile calls take: under a parallel test
	// run that exceeded the window, and the burst arrived as two batches.
	for i := 0; i < 5; i++ {
		p := filepath.Join(root, "f.txt")
		if err := os.WriteFile(p, []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatalf("writefile: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx, handler) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for debounce flush")
	}

	// Allow a cushion for any stray events to NOT produce a second batch.
	time.Sleep(150 * time.Millisecond)

	cancel()
	<-runErr

	mu.Lock()
	defer mu.Unlock()
	if len(batches) == 0 {
		t.Fatalf("expected at least one batch, got none")
	}
	// The 5 writes to the same file should coalesce into a single
	// batch containing that one path.
	if len(batches) > 1 {
		t.Fatalf("expected single coalesced batch, got %d batches: %v", len(batches), batches)
	}
	if len(batches[0]) != 1 {
		t.Fatalf("expected 1 unique path in batch, got %d: %v", len(batches[0]), batches[0])
	}
}

// TestWatcherIgnoresTargetsSubtree verifies that writes under
// .arch/targets/ (except CURRENT) do not produce batches.
func TestWatcherIgnoresTargetsSubtree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".arch", "targets", "v1"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w, err := NewWatcher(root, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer func() { _ = w.Close() }()

	var (
		mu      sync.Mutex
		batches int
	)
	handler := func(paths []string) {
		mu.Lock()
		batches++
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx, handler) }()

	time.Sleep(20 * time.Millisecond)
	// Write under .arch/targets/v1/ — should be ignored.
	if err := os.WriteFile(filepath.Join(root, ".arch", "targets", "v1", "meta.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}
	// Wait well past the debounce window.
	time.Sleep(120 * time.Millisecond)

	cancel()
	<-runErr

	mu.Lock()
	defer mu.Unlock()
	if batches != 0 {
		t.Fatalf("expected 0 batches for ignored subtree, got %d", batches)
	}
}

// TestWatcherReportsAttributeEventThatMovedContent verifies that a chmod on a
// file whose content stamp changed is an edit. A host directory shared into a
// VM (colima's mountInotify) delivers every host-side edit to the guest as a
// chmod, so a daemon in a container sees nothing else.
func TestWatcherReportsAttributeEventThatMovedContent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	w, err := NewWatcher(root, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer func() { _ = w.Close() }()

	// The edit lands without an event the watcher sees, as it does when the
	// write happens on the other side of a VM boundary; only the chmod the
	// share forwards afterwards reaches the watcher.
	w.inner.Remove(root)
	if err := os.WriteFile(file, []byte("package p\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}
	if err := w.inner.Add(root); err != nil {
		t.Fatalf("re-add watch: %v", err)
	}

	var (
		mu    sync.Mutex
		paths []string
	)
	handler := func(batch []string) {
		mu.Lock()
		paths = append(paths, batch...)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx, handler) }()

	time.Sleep(20 * time.Millisecond)
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	cancel()
	<-runErr

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != file {
		t.Fatalf("expected one batch naming %s, got %v", file, paths)
	}
}

// TestWatcherDropsEventsOutsideRoot verifies that an event naming a path
// outside the watched tree is neither delivered nor followed. fsnotify's
// kqueue backend reports a Remove of "" and a Create of "." when a directory
// is removed with kevents for its children still queued; following that
// Create walked the process's working directory.
func TestWatcherDropsEventsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	t.Chdir(outside)
	if err := os.Mkdir(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w, err := NewWatcher(root, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer func() { _ = w.Close() }()

	var (
		mu    sync.Mutex
		paths []string
	)
	handler := func(batch []string) {
		mu.Lock()
		paths = append(paths, batch...)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx, handler) }()

	for _, ev := range []fsnotify.Event{
		{Name: "", Op: fsnotify.Remove},
		{Name: ".", Op: fsnotify.Create},
		{Name: "sub", Op: fsnotify.Create},
		{Name: outside, Op: fsnotify.Create},
	} {
		w.inner.Events <- ev
	}
	time.Sleep(120 * time.Millisecond)

	cancel()
	<-runErr

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 0 {
		t.Fatalf("expected no batch for paths outside root, got %v", paths)
	}
	for _, p := range w.inner.WatchList() {
		if p != root {
			t.Fatalf("watch added outside root: %s (all: %v)", p, w.inner.WatchList())
		}
	}
}

// TestWatcherSkipsCreatedSkippedDirs verifies that a directory created
// under the root is watched only when the initial walk would have watched
// it: a hidden scratch directory a tool creates and removes must not be.
func TestWatcherSkipsCreatedSkippedDirs(t *testing.T) {
	root := t.TempDir()
	w, err := NewWatcher(root, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer func() { _ = w.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx, func([]string) {}) }()

	time.Sleep(20 * time.Millisecond)
	hidden := filepath.Join(root, ".scratch")
	visible := filepath.Join(root, "pkg")
	for _, d := range []string{hidden, visible} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	time.Sleep(120 * time.Millisecond)

	cancel()
	<-runErr

	watched := map[string]bool{}
	for _, p := range w.inner.WatchList() {
		watched[p] = true
	}
	if watched[hidden] {
		t.Fatalf("hidden directory %s was watched", hidden)
	}
	if !watched[visible] {
		t.Fatalf("new directory %s was not watched (all: %v)", visible, w.inner.WatchList())
	}
}
