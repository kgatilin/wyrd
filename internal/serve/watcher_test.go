package serve

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx, handler) }()

	// Give the watcher a moment to be ready for events.
	time.Sleep(20 * time.Millisecond)

	// Produce a rapid burst of events well inside the debounce window.
	for i := 0; i < 5; i++ {
		p := filepath.Join(root, "f.txt")
		if err := os.WriteFile(p, []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatalf("writefile: %v", err)
		}
	}

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
