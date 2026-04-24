package http

import (
	"context"
	"io"
	nethttp "net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/worktree"
)

// TestServe_Lifecycle_WithServeJSON drives the full daemon lifecycle:
// start serve on :0, read the bound URL from serve.json, hit "/" (no
// /healthz route exists), then cancel and verify serve.json is gone.
func TestServe_Lifecycle_WithServeJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/m9lifecycle\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	opts := serve.Options{
		Root:     root,
		HTTPAddr: "127.0.0.1:0",
		HTTPServerFactory: func(state *serve.State) (serve.HTTPTransport, error) {
			return NewServer(state)
		},
		LogOut: io.Discard,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serve.Serve(ctx, opts) }()

	// Poll serve.json until it appears.
	name := worktree.Name(root)
	servePath := worktree.ServePath(root, name)

	var rec *worktree.ServeRecord
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := worktree.ReadServe(root, name)
		if err != nil {
			t.Fatalf("ReadServe: %v", err)
		}
		if r != nil {
			rec = r
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rec == nil {
		t.Fatalf("serve.json never appeared at %s", servePath)
	}

	// Hit the server using the discovered address.
	resp, err := nethttp.Get("http://" + rec.HTTPAddr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("GET /: status = %d, body=%s", resp.StatusCode, string(body))
	}

	// Shutdown and wait for Serve to return.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5s of cancel")
	}

	// serve.json must be removed.
	if _, err := os.Stat(servePath); !os.IsNotExist(err) {
		t.Errorf("serve.json still exists after shutdown: err=%v", err)
	}

	// And ListDaemons must return empty.
	daemons, err := worktree.ListDaemons(root)
	if err != nil {
		t.Fatalf("ListDaemons: %v", err)
	}
	if len(daemons) != 0 {
		t.Errorf("expected 0 live daemons, got %d: %+v", len(daemons), daemons)
	}
}
