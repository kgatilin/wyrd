package serve

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

// fakeHTTPTransport is a minimal HTTPTransport used to exercise the
// serve.json lifecycle without pulling in the real http adapter (which
// would create an import cycle in this package).
type fakeHTTPTransport struct {
	boundAddr string
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
