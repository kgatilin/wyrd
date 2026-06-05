package http

import (
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kgatilin/archai/internal/serve"
)

func testReviewUIFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><script type="module" src="/assets/app.js"></script><link rel="stylesheet" href="/assets/app.css"><div id="root"></div>`),
		},
		"assets/app.js":  {Data: []byte(`console.log("review")`)},
		"assets/app.css": {Data: []byte(`body{margin:0}`)},
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
}

func TestReviewUI_MultiModeWorktreeRootRedirectsToReview(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
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
}
