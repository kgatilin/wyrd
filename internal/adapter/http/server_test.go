package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/serve"
)

// navPaths lists the routes that every nav page should answer with a
// 200 + fully-formed base layout. Kept as a var so it's trivial to
// extend as M7b-f add real content.
var navPaths = []string{"/", "/layers", "/packages", "/configs", "/targets", "/diff", "/search"}

// expectedNavLinks are substrings we expect to find on every rendered
// page — they are the canonical top-nav entries defined in handlers.go.
var expectedNavLinks = []string{
	`href="/"`,
	`href="/layers"`,
	`href="/packages"`,
	`href="/configs"`,
	`href="/targets"`,
	`href="/diff"`,
	`href="/search"`,
}

// newTestServer spins up a Server backed by an empty serve.State
// rooted at t.TempDir() and wraps it in an httptest.Server. Callers
// are responsible for closing the returned httptest.Server.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	state := serve.NewState(t.TempDir())
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	return httptest.NewServer(mux)
}

func TestServer_NavPages_Return200WithNav(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	for _, path := range navPaths {
		path := path
		t.Run(path, func(t *testing.T) {
			resp, err := ts.Client().Get(ts.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != nethttp.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200", path, resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			s := string(body)
			for _, link := range expectedNavLinks {
				if !strings.Contains(s, link) {
					t.Errorf("GET %s: body missing nav link %q", path, link)
				}
			}
			if !strings.Contains(s, `<main class="content">`) {
				t.Errorf("GET %s: body missing main content area", path)
			}
		})
	}
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

func TestServer_AssetsServed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// htmx.min.js and styles.css must both be reachable.
	for _, asset := range []string{"/assets/htmx.min.js", "/assets/styles.css"} {
		resp, err := ts.Client().Get(ts.URL + asset)
		if err != nil {
			t.Fatalf("GET %s: %v", asset, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != nethttp.StatusOK {
			t.Fatalf("GET %s: status = %d (%s)", asset, resp.StatusCode, string(body))
		}
		if len(body) == 0 {
			t.Fatalf("GET %s: empty body", asset)
		}
	}
}

func TestServer_RenderEndpoint_POSTForm(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().PostForm(ts.URL+"/render", url.Values{"d2": {"a -> b"}})
	if err != nil {
		t.Fatalf("POST /render: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<svg") {
		t.Fatalf("body does not contain <svg: %s", string(body))
	}
}

func TestServer_RenderEndpoint_MissingSource(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/render")
	if err != nil {
		t.Fatalf("GET /render: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
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
	go func() { errCh <- srv.Serve(ctx, "127.0.0.1:0") }()

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
