package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/buildinfo"
)

// TestAPIVersion_GET checks the JSON shape and content of /api/version.
// The endpoint must return the {version, commit, go} keys, no others
// being a hard requirement, with the version reflecting whatever was
// wired via WithVersion at construction time.
func TestAPIVersion_GET(t *testing.T) {
	ts, _, _ := newAPITestServer(t)
	// The test server already has /api/version mounted via routes(). We
	// hit it directly and verify the GO key is non-empty (always set by
	// debug.ReadBuildInfo inside `go test`).

	resp, err := nethttp.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type: %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)

	var info buildinfo.Info
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, body)
	}
	if info.Version == "" {
		t.Fatalf("version empty: body=%s", body)
	}
	if info.Go == "" {
		t.Fatalf("go empty: body=%s", body)
	}

	// Stable shape: confirm the three keys are present in the raw JSON
	// so a future struct-tag rename trips the test.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, k := range []string{"version", "commit", "go"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing key %q in %s", k, body)
		}
	}
}

// TestAPIVersion_MethodNotAllowed checks POST returns 405 with an
// Allow: GET header so clients can probe the endpoint correctly.
func TestAPIVersion_MethodNotAllowed(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	req, err := nethttp.NewRequest(nethttp.MethodPost, ts.URL+"/api/version", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusMethodNotAllowed {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != nethttp.MethodGet {
		t.Fatalf("Allow header: %q", allow)
	}
}

// TestAPIVersion_WithVersionInjection ensures WithVersion overrides the
// resolver, which is the contract main.go relies on to surface the
// linker-injected build identity.
func TestAPIVersion_WithVersionInjection(t *testing.T) {
	ts, state, _ := newAPITestServer(t)
	_ = ts // ensure baseline server still constructs cleanly
	_ = state

	// Build a fresh server, inject a synthetic Info, and exercise the
	// handler directly via httptest.NewRecorder so we don't have to
	// rewire the listener.
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	want := buildinfo.Info{Version: "v9.9.9-test", Commit: "deadbeef", Go: "go-test"}
	srv.WithVersion(want)

	req := httptest.NewRequest(nethttp.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	srv.handleAPIVersion(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var got buildinfo.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, rec.Body.String())
	}
	if got != want {
		t.Fatalf("info = %+v, want %+v", got, want)
	}
}
