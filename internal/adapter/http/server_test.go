package http

import (
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/serve"
)

// newTestServer spins up a Server backed by an empty serve.State
// rooted at t.TempDir(), with the review UI mounted, and wraps it in an
// httptest.Server. Callers are responsible for closing the returned
// httptest.Server.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	state := serve.NewState(t.TempDir())
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	return httptest.NewServer(mux)
}

func TestServer_Unknown404(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestServer_Serve_ReadyCallback verifies that Serve invokes the
// ready callback with the actual bound address when addr uses port 0.
func TestServer_Serve_ReadyCallback(t *testing.T) {
	state := serve.NewState(t.TempDir())
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithReviewUI(testReviewUIFS())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ctx, "127.0.0.1:0", func(addr string) { addrCh <- addr })
	}()

	select {
	case addr := <-addrCh:
		if !strings.HasPrefix(addr, "127.0.0.1:") {
			t.Fatalf("bound addr = %q, want 127.0.0.1:<port>", addr)
		}
		// Hit the server to confirm it's actually up. The root redirects
		// to the review UI, which the default client follows.
		resp, err := nethttp.Get("http://" + addr + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != nethttp.StatusOK {
			t.Fatalf("GET /: status = %d", resp.StatusCode)
		}
		if got := resp.Request.URL.Path; got != reviewUIPrefix {
			t.Fatalf("GET / landed on %q, want %q", got, reviewUIPrefix)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback not invoked within 2s")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return within 2s of cancel")
	}
}

// TestServer_Serve_ContextCancel verifies that Serve returns cleanly
// when its context is cancelled while listening.
func TestServer_Serve_ContextCancel(t *testing.T) {
	state := serve.NewState(t.TempDir())
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, "127.0.0.1:0", nil) }()

	// Give the server a moment to bind, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return within 2s of cancel")
	}
}
