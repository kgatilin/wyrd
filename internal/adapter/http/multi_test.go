package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/worktree"
)

func TestStripWorktreePrefix(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantRest string
	}{
		{"/w/foo/", "foo", "/"},
		{"/w/foo/layers", "foo", "/layers"},
		{"/w/foo-bar/packages/x/y", "foo-bar", "/packages/x/y"},
		{"/w/foo", "foo", "/"},
		{"/layers", "", "/layers"},
		{"/", "", "/"},
	}
	for _, c := range cases {
		gotName, gotRest := stripWorktreePrefix(c.in)
		if gotName != c.wantName || gotRest != c.wantRest {
			t.Errorf("stripWorktreePrefix(%q) = (%q, %q), want (%q, %q)",
				c.in, gotName, gotRest, c.wantName, c.wantRest)
		}
	}
}

func TestRewriteForWorktree(t *testing.T) {
	cases := []struct {
		redirect string
		name     string
		want     string
	}{
		{"/layers", "foo", "/w/foo/layers"},
		{"/", "foo", "/w/foo/"},
		{"", "foo", "/w/foo/"},
		{"/w/other/packages", "foo", "/w/foo/packages"},
		{"/w/other/", "foo", "/w/foo/"},
		{"http://evil.example/hack", "foo", "/w/foo/"},
		{"//evil.example/hack", "foo", "/w/foo/"},
		{"relative", "foo", "/w/foo/relative"},
	}
	for _, c := range cases {
		got := rewriteForWorktree(c.redirect, c.name)
		if got != c.want {
			t.Errorf("rewriteForWorktree(%q, %q) = %q, want %q", c.redirect, c.name, got, c.want)
		}
	}
}

// gitRun runs git -C root with args or t.Fatal()s.
func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// buildTwoWorktreeRepo creates a git repo with two worktrees named
// "alpha" (the primary checkout) and "beta" (a linked worktree on a
// feat/x branch). Returns the primary worktree path. The parent
// directory is reused so `git worktree add ../beta` resolves to a
// sibling of alpha.
func buildTwoWorktreeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	parent := t.TempDir()
	alpha := filepath.Join(parent, "alpha")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(alpha, "go.mod"),
		[]byte("module example.com/multi\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	gitRun(t, alpha, "init", "-q", "-b", "main")
	gitRun(t, alpha, "add", ".")
	gitRun(t, alpha, "commit", "-qm", "init")
	beta := filepath.Join(parent, "beta")
	gitRun(t, alpha, "worktree", "add", "-b", "feat/x", beta)
	return alpha
}

// buildMultiServer returns a Server in multi mode backed by a real
// two-worktree repo. The state loader is a stub that counts invocations
// so tests can assert lazy-load behaviour without running the Go reader.
func buildMultiServer(t *testing.T) (*Server, *serve.MultiState, *int64) {
	t.Helper()
	primary := buildTwoWorktreeRepo(t)

	var loadCount int64
	loader := func(ctx context.Context, name, path string) (*serve.State, error) {
		atomic.AddInt64(&loadCount, 1)
		return serve.NewState(path), nil
	}
	multi := serve.NewMultiState(primary, loader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Sanity: Refresh should have discovered both worktrees by their
	// directory basenames.
	names := multi.Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", names)
	}

	srv, err := NewMultiServer(multi)
	if err != nil {
		t.Fatalf("NewMultiServer: %v", err)
	}
	return srv, multi, &loadCount
}

func TestMultiServer_LegacyRedirect(t *testing.T) {
	srv, _, _ := buildMultiServer(t)

	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// No cookie -> redirects to first alphabetical worktree.
	client := &nethttp.Client{
		CheckRedirect: func(req *nethttp.Request, via []*nethttp.Request) error {
			return nethttp.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/layers")
	if err != nil {
		t.Fatalf("GET /layers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/w/alpha/layers" {
		t.Errorf("Location = %q, want /w/alpha/layers", loc)
	}

	// With cookie -> redirects to the chosen worktree.
	req, _ := nethttp.NewRequest(nethttp.MethodGet, ts.URL+"/layers", nil)
	req.AddCookie(&nethttp.Cookie{Name: cookieName, Value: "beta"})
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /layers with cookie: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("Location") != "/w/beta/layers" {
		t.Errorf("Location with cookie = %q", resp2.Header.Get("Location"))
	}
}

func TestMultiServer_WorktreeDispatch(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// /w/alpha/ should render the dashboard with status 200.
	resp, err := nethttp.Get(ts.URL + "/w/alpha/")
	if err != nil {
		t.Fatalf("GET /w/alpha/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Dashboard") {
		t.Errorf("dashboard body missing Dashboard title: %s", string(body))
	}
	// Nav links should be scoped to /w/alpha/.
	if !strings.Contains(string(body), `href="/w/alpha/layers"`) {
		t.Errorf("nav links not prefixed with worktree: %s", string(body))
	}
}

func TestMultiServer_UnknownWorktree(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/w/nope/layers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMultiServer_SelectSetsCookie(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &nethttp.Client{
		CheckRedirect: func(req *nethttp.Request, via []*nethttp.Request) error {
			return nethttp.ErrUseLastResponse
		},
	}
	form := url.Values{}
	form.Set("name", "beta")
	form.Set("redirect", "/layers")
	resp, err := client.PostForm(ts.URL+"/worktree/select", form)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/beta/layers" {
		t.Errorf("Location = %q, want /w/beta/layers", loc)
	}
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.Value == "beta" {
			found = true
		}
	}
	if !found {
		t.Errorf("cookie not set")
	}
}

func TestMultiServer_LazyLoadCached(t *testing.T) {
	srv, _, count := buildMultiServer(t)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	mustGet := func(path string) {
		resp, err := nethttp.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
	}
	if atomic.LoadInt64(count) != 0 {
		t.Fatalf("load count before hits = %d, want 0", *count)
	}
	mustGet("/w/alpha/layers")
	if atomic.LoadInt64(count) != 1 {
		t.Errorf("load count after first hit = %d, want 1", *count)
	}
	// Second hit to the same worktree must reuse the cached State.
	mustGet("/w/alpha/packages")
	if atomic.LoadInt64(count) != 1 {
		t.Errorf("load count after second hit = %d, want 1 (cache miss)", *count)
	}
	// Touching a different worktree triggers another load.
	mustGet("/w/beta/layers")
	if atomic.LoadInt64(count) != 2 {
		t.Errorf("load count after beta hit = %d, want 2", *count)
	}
}

func TestMultiServer_SelectUnknownWorktree(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	form := url.Values{}
	form.Set("name", "ghost")
	resp, err := nethttp.PostForm(ts.URL+"/worktree/select", form)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestMultiServer_SelectHTMXRedirect verifies HX-Request receives an
// HX-Redirect header so client-side HTMX swaps the whole content.
func TestMultiServer_SelectHTMXRedirect(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	form := url.Values{}
	form.Set("name", "alpha")
	form.Set("redirect", "/packages")
	req, _ := nethttp.NewRequest(nethttp.MethodPost, ts.URL+"/worktree/select",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if hr := resp.Header.Get("HX-Redirect"); hr != "/w/alpha/packages" {
		t.Errorf("HX-Redirect = %q, want /w/alpha/packages", hr)
	}
}

// TestSingleServer_NoMultiRoutes ensures the classic single-mode
// routes are unchanged — no /w/* handlers, and /layers returns 200
// without redirecting.
func TestSingleServer_NoMultiRoutes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/single\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	state := serve.NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/layers")
	if err != nil {
		t.Fatalf("GET /layers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("single mode /layers status = %d, want 200 (no redirect expected)", resp.StatusCode)
	}

	// /w/foo/ should 404 in single mode (no prefix routes installed).
	resp2, err := nethttp.Get(ts.URL + "/w/foo/")
	if err != nil {
		t.Fatalf("GET /w/foo/: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode == nethttp.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		if strings.Contains(string(body), "Dashboard") {
			t.Errorf("single mode served dashboard at /w/foo/: %s", body)
		}
	}
}

// TestDiscover_GitFallback exercises worktree.Discover when run
// outside a git repo (fallback returns a single synthetic entry).
func TestDiscover_GitFallback(t *testing.T) {
	root := t.TempDir()
	entries, err := worktree.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Path == "" {
		t.Errorf("entry path empty: %+v", entries[0])
	}
}

// TestDiscover_RealRepo creates a minimal git repo and ensures
// Discover returns its single worktree.
func TestDiscover_RealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("README: %v", err)
	}
	gitRun(t, root, "init", "-q", "-b", "main")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "init")

	entries, err := worktree.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(entries), entries)
	}
}
