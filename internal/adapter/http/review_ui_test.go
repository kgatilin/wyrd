package http

import (
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/kgatilin/wyrd/internal/serve"
)

func testReviewUIFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><link rel="icon" type="image/svg+xml" href="/assets/favicon.svg"><script type="module" src="/assets/app.js"></script><link rel="stylesheet" href="/assets/app.css"><div id="root"></div>`),
		},
		"assets/app.js":      {Data: []byte(`console.log("review")`)},
		"assets/app.css":     {Data: []byte(`body{margin:0}`)},
		"assets/favicon.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"></svg>`)},
	}
}

func serveStateForHTTPTest(t *testing.T) *serve.State {
	t.Helper()
	return serve.NewState(t.TempDir())
}

func TestReviewUI_SingleModeRootRedirectsToReview(t *testing.T) {
	state := serveStateForHTTPTest(t)
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &nethttp.Client{CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error {
		return nethttp.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/review/" {
		t.Fatalf("Location = %q, want /review/", loc)
	}
}

func TestReviewUI_IndexRewritesViteAssetPaths(t *testing.T) {
	state := serveStateForHTTPTest(t)
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/review/")
	if err != nil {
		t.Fatalf("GET /review/: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	html := string(body)
	for _, want := range []string{`src="/review/assets/app.js"`, `href="/review/assets/app.css"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("index missing rewritten asset path %q: %s", want, html)
		}
	}
}

func TestReviewUI_AssetServedUnderReviewPrefix(t *testing.T) {
	state := serveStateForHTTPTest(t)
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/review/assets/app.js")
	if err != nil {
		t.Fatalf("GET asset: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "review") {
		t.Fatalf("unexpected asset body: %s", body)
	}
}

func TestReviewUI_MultiModeRootRedirectsToWorktreeReview(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &nethttp.Client{CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error {
		return nethttp.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/alpha/review/" {
		t.Fatalf("Location = %q, want /w/alpha/review/", loc)
	}

	req, _ := nethttp.NewRequest(nethttp.MethodGet, ts.URL+"/", nil)
	req.AddCookie(&nethttp.Cookie{Name: cookieName, Value: "beta"})
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET / with cookie: %v", err)
	}
	defer resp2.Body.Close()
	if loc := resp2.Header.Get("Location"); loc != "/w/beta/review/" {
		t.Fatalf("cookie Location = %q, want /w/beta/review/", loc)
	}
}

func TestReviewUI_MultiModeWorktreeReviewIndexUsesScopedAssets(t *testing.T) {
	srv, _, loadCount := buildMultiServer(t)
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/w/beta/review/")
	if err != nil {
		t.Fatalf("GET /w/beta/review/: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	html := string(body)
	for _, want := range []string{
		`src="/w/beta/review/assets/app.js"`,
		`href="/w/beta/review/assets/app.css"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("index missing scoped asset path %q: %s", want, html)
		}
	}
	if got := atomic.LoadInt64(loadCount); got != 0 {
		t.Fatalf("review index loaded worktree state %d time(s), want 0", got)
	}

	assetResp, err := nethttp.Get(ts.URL + "/w/beta/review/assets/app.js")
	if err != nil {
		t.Fatalf("GET /w/beta/review/assets/app.js: %v", err)
	}
	defer assetResp.Body.Close()
	if assetResp.StatusCode != nethttp.StatusOK {
		assetBody, _ := io.ReadAll(assetResp.Body)
		t.Fatalf("asset status = %d body=%s", assetResp.StatusCode, assetBody)
	}
	if got := atomic.LoadInt64(loadCount); got != 0 {
		t.Fatalf("review asset loaded worktree state %d time(s), want 0", got)
	}
}

func TestReviewUI_MultiModeWorktreeRootRedirectsToReview(t *testing.T) {
	srv, _, loadCount := buildMultiServer(t)
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &nethttp.Client{CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error {
		return nethttp.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/w/beta/")
	if err != nil {
		t.Fatalf("GET /w/beta/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/beta/review/" {
		t.Fatalf("Location = %q, want /w/beta/review/", loc)
	}
	if got := atomic.LoadInt64(loadCount); got != 0 {
		t.Fatalf("worktree review redirect loaded worktree state %d time(s), want 0", got)
	}
}

// The favicon only resolves because index.html spells it "/assets/favicon.svg"
// and handleReviewUIIndex rewrites that one absolute prefix. Any other path
// (say "/favicon.svg") escapes the rewrite and the /review/assets/ route, and
// the tab falls back to the blank page icon.
func TestReviewUI_FaviconIsScopedToWorktreeAndServed(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	srv.WithReviewUI(testReviewUIFS())
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/w/beta/review/")
	if err != nil {
		t.Fatalf("GET /w/beta/review/: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if want := `href="/w/beta/review/assets/favicon.svg"`; !strings.Contains(string(body), want) {
		t.Fatalf("index missing scoped favicon path %q: %s", want, body)
	}

	iconResp, err := nethttp.Get(ts.URL + "/w/beta/review/assets/favicon.svg")
	if err != nil {
		t.Fatalf("GET favicon: %v", err)
	}
	defer iconResp.Body.Close()
	iconBody, _ := io.ReadAll(iconResp.Body)
	if iconResp.StatusCode != nethttp.StatusOK {
		t.Fatalf("favicon status = %d body=%s", iconResp.StatusCode, iconBody)
	}
	if ct := iconResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Fatalf("favicon Content-Type = %q, want image/svg+xml", ct)
	}
}
