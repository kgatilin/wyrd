package serve

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

// fakeHTTPTransport is a minimal HTTPTransport used to exercise the
// serve.json lifecycle without pulling in the real http adapter (which
// would create an import cycle in this package).
type fakeHTTPTransport struct {
	boundAddr string

	// observer, when non-nil, is the activity observer installed via
	// SetActivityObserver. Guarded by mu so idle-timeout tests can
	// safely read it from a separate goroutine under -race.
	mu       sync.Mutex
	observer func()
}

// getObserver returns the currently installed activity observer, if
// any, under the lock. Used by tests to simulate HTTP traffic.
func (f *fakeHTTPTransport) getObserver() func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.observer
}

func (f *fakeHTTPTransport) Serve(ctx context.Context, addr string, ready func(boundAddr string)) error {
	// Simulate a port-0 bind by picking a synthetic addr.
	bound := f.boundAddr
	if bound == "" {
		bound = "127.0.0.1:12345"
	}
	if ready != nil {
		ready(bound)
	}
	<-ctx.Done()
	return nil
}

// SetActivityObserver implements ActivityAware so serve.Serve wires
// the idle-timeout monitor to this fake when IdleTimeout > 0.
func (f *fakeHTTPTransport) SetActivityObserver(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observer = fn
}

// TestServe_WritesAndRemovesServeJSON drives Serve with a fake HTTP
// transport and verifies that serve.json appears on startup and is
// removed on graceful shutdown.
func TestServe_WritesAndRemovesServeJSON(t *testing.T) {
	root := t.TempDir()
	// Seed a tiny go.mod so state.Load has something to chew on.
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/m9\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	fake := &fakeHTTPTransport{boundAddr: "127.0.0.1:55555"}
	opts := Options{
		Root:     root,
		HTTPAddr: ":0",
		HTTPServerFactory: func(*State) (HTTPTransport, error) {
			return fake, nil
		},
		LogOut: io.Discard,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()

	// Wait for serve.json to appear.
	name := worktree.Name(root)
	servePath := worktree.ServePath(root, name)
	deadline := time.Now().Add(2 * time.Second)
	var rec *worktree.ServeRecord
	for time.Now().Before(deadline) {
		r, err := worktree.ReadServe(root, name)
		if err != nil {
			t.Fatalf("ReadServe: %v", err)
		}
		if r != nil {
			rec = r
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec == nil {
		t.Fatalf("serve.json never appeared at %s", servePath)
	}
	if rec.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", rec.PID, os.Getpid())
	}
	if rec.HTTPAddr != "127.0.0.1:55555" {
		t.Errorf("HTTPAddr = %q, want %q", rec.HTTPAddr, "127.0.0.1:55555")
	}
	if rec.StartedAt == "" {
		t.Errorf("StartedAt empty, want RFC3339 timestamp")
	}

	// Trigger shutdown and wait for Serve to return.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return within 3s of cancel")
	}

	// serve.json must be gone.
	if _, err := os.Stat(servePath); !os.IsNotExist(err) {
		t.Errorf("serve.json still exists after shutdown: err=%v", err)
	}
}

// TestServe_IdleTimeoutShutsDownWithNoActivity verifies that Serve
// exits on its own when IdleTimeout elapses without the activity
// observer being ticked.
func TestServe_IdleTimeoutShutsDownWithNoActivity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/idle\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	fake := &fakeHTTPTransport{}
	opts := Options{
		Root:     root,
		HTTPAddr: "127.0.0.1:0",
		HTTPServerFactory: func(*State) (HTTPTransport, error) {
			return fake, nil
		},
		IdleTimeout: 200 * time.Millisecond,
		LogOut:      io.Discard,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not exit within 3s of idle-timeout (expected ~200ms)")
	}
}

// TestServe_IdleTimeoutResetsOnActivity verifies that pinging the
// activity observer keeps the daemon alive past the idle window.
func TestServe_IdleTimeoutResetsOnActivity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/idle2\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	fake := &fakeHTTPTransport{}
	opts := Options{
		Root:     root,
		HTTPAddr: "127.0.0.1:0",
		HTTPServerFactory: func(*State) (HTTPTransport, error) {
			return fake, nil
		},
		IdleTimeout: 300 * time.Millisecond,
		LogOut:      io.Discard,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()

	// Wait for the fake transport to be wired (observer installed).
	deadline := time.Now().Add(1 * time.Second)
	var observer func()
	for time.Now().Before(deadline) {
		if observer = fake.getObserver(); observer != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if observer == nil {
		t.Fatal("activity observer was never installed on the transport")
	}

	// Tick activity every 100ms for ~600ms — twice the idle window.
	// The daemon must still be alive when we stop ticking.
	tickDone := make(chan struct{})
	go func() {
		defer close(tickDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(600 * time.Millisecond)
		for {
			select {
			case <-deadline:
				return
			case <-ticker.C:
				observer()
			}
		}
	}()

	select {
	case err := <-done:
		t.Fatalf("Serve exited while activity was still flowing: err=%v", err)
	case <-tickDone:
	}

	// Stop ticking — now the daemon should exit via idle-timeout.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not exit within 2s of ticks stopping")
	}
}
