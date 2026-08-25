package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/mcp"
	"github.com/kgatilin/wyrd/internal/serve"
)

// postMCP sends one JSON-RPC message to ts's MCP endpoint and returns the
// raw response. headers are applied to the request as-is.
func postMCP(t *testing.T, ts *httptest.Server, path, body string, headers map[string]string) *nethttp.Response {
	t.Helper()
	req, err := nethttp.NewRequest(nethttp.MethodPost, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// decodeMCP reads a JSON-RPC response body, failing the test on a non-200
// status or an error object.
func decodeMCP(t *testing.T, resp *nethttp.Response) mcp.Response {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var out mcp.Response
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, body)
	}
	if out.Error != nil {
		t.Fatalf("JSON-RPC error: %+v", out.Error)
	}
	return out
}

// resultMap re-marshals a decoded Result into a map so a test can read the
// fields it cares about.
func resultMap(t *testing.T, resp mcp.Response) map[string]any {
	t.Helper()
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return out
}

func TestMCPHTTP_Initialize_NegotiatesAndStaysStateless(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp := postMCP(t, ts, "/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	result := resultMap(t, decodeMCP(t, resp))

	if got := result["protocolVersion"]; got != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's own 2025-06-18", got)
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("missing tools capability: %v", result["capabilities"])
	}
	// No session id: the endpoint is stateless, and a client that is handed
	// one has to echo it back on every later request.
	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		t.Errorf("Mcp-Session-Id = %q, want the endpoint to hand out no session", got)
	}
}

func TestMCPHTTP_ToolsList_MatchesTheStdioToolSet(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp := decodeMCP(t, postMCP(t, ts, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil))
	raw, _ := json.Marshal(resultMap(t, resp)["tools"])
	var got []mcp.ToolDefinition
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode tools: %v", err)
	}

	want := mcp.ToolDefinitions()
	if len(got) != len(want) {
		t.Fatalf("tools/list returned %d tools, want %d (the same set stdio serves)", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Errorf("tool[%d] = %q, want %q", i, got[i].Name, want[i].Name)
		}
	}
}

func TestMCPHTTP_ToolsCall_ReturnsTheModel(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp := decodeMCP(t, postMCP(t, ts, "/mcp",
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_packages","arguments":{}}}`, nil))
	raw, _ := json.Marshal(resp.Result)
	var result mcp.ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	if result.IsError {
		t.Fatalf("list_packages reported an error: %+v", result.Content)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "alpha") {
		t.Errorf("list_packages content = %+v, want the fixture's alpha package", result.Content)
	}
}

// A notification has no id, so JSON-RPC forbids a response and the spec pins
// the status the client expects instead.
func TestMCPHTTP_Notification_Is202WithNoBody(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp := postMCP(t, ts, "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want it empty", body)
	}
}

// The client opens a standalone SSE stream after initialize and reads 405 as
// "this server has none" — any other answer either breaks the session or
// leaves the client waiting on a stream that never speaks.
func TestMCPHTTP_GetAndDelete_Are405(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	for _, method := range []string{nethttp.MethodGet, nethttp.MethodDelete} {
		req, _ := nethttp.NewRequest(method, ts.URL+"/mcp", nil)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s /mcp: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != nethttp.StatusMethodNotAllowed {
			t.Errorf("%s /mcp status = %d, want 405", method, resp.StatusCode)
		}
		if got := resp.Header.Get("Allow"); got != nethttp.MethodPost {
			t.Errorf("%s /mcp Allow = %q, want POST", method, got)
		}
	}
}

func TestMCPHTTP_UnparseableBody_Is400WithAParseError(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp := postMCP(t, ts, "/mcp", `{not json`, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, body)
	}
	var out mcp.Response
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, body)
	}
	if out.Error == nil || out.Error.Code != mcp.ErrParseError {
		t.Errorf("error = %+v, want a JSON-RPC parse error", out.Error)
	}
}

// newMCPAuthServer is newAPITestServer with a bearer token required.
func newMCPAuthServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	_, _, srv, _ := newAPIFixture(t)
	srv.WithAuthToken(token)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestMCPHTTP_BearerToken_GuardsTheEndpoint(t *testing.T) {
	const token = "s3cret-token"
	ts := newMCPAuthServer(t, token)
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`

	cases := []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"no header", nil, nethttp.StatusUnauthorized},
		{"wrong token", map[string]string{"Authorization": "Bearer nope"}, nethttp.StatusUnauthorized},
		{"wrong scheme", map[string]string{"Authorization": "Basic " + token}, nethttp.StatusUnauthorized},
		{"correct token", map[string]string{"Authorization": "Bearer " + token}, nethttp.StatusOK},
		{"scheme is case-insensitive", map[string]string{"Authorization": "bearer " + token}, nethttp.StatusOK},
	}
	for _, tc := range cases {
		resp := postMCP(t, ts, "/mcp", initialize, tc.header)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
}

// The unauthenticated answer has to name the scheme, or a client cannot tell
// a closed endpoint from a broken one.
func TestMCPHTTP_Unauthorized_NamesTheScheme(t *testing.T) {
	ts := newMCPAuthServer(t, "tok")
	resp := postMCP(t, ts, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)
	resp.Body.Close()
	if got := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
	}
}

// The tools stay reachable when no token is configured: that is the local
// default, and requiring one would break every existing loopback client.
func TestMCPHTTP_NoTokenConfigured_LeavesTheEndpointOpen(t *testing.T) {
	ts, _, _ := newAPITestServer(t)
	resp := postMCP(t, ts, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)
	resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// DNS rebinding: a page on any site can POST to a loopback daemon from the
// visitor's browser. The browser attaches its own Origin to that request, and
// that is the only thing separating it from a real client's.
func TestMCPHTTP_Origin_RejectsWhatIsNotLocal(t *testing.T) {
	ts, _, _ := newAPITestServer(t)
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`

	cases := []struct {
		origin string
		want   int
	}{
		{"", nethttp.StatusOK},
		{"http://127.0.0.1:1234", nethttp.StatusOK},
		{"http://localhost:5173", nethttp.StatusOK},
		{"http://[::1]:8080", nethttp.StatusOK},
		{"https://evil.example.com", nethttp.StatusForbidden},
		{"null", nethttp.StatusForbidden},
		{"http://127.0.0.1.evil.example.com", nethttp.StatusForbidden},
	}
	for _, tc := range cases {
		headers := map[string]string{}
		if tc.origin != "" {
			headers["Origin"] = tc.origin
		}
		resp := postMCP(t, ts, "/mcp", initialize, headers)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("Origin %q: status = %d, want %d", tc.origin, resp.StatusCode, tc.want)
		}
	}
}

// The page this daemon itself served is allowed to call it, whatever host it
// was reached on.
func TestMCPHTTP_Origin_AllowsThePageThisDaemonServed(t *testing.T) {
	ts, _, _ := newAPITestServer(t)
	req, _ := nethttp.NewRequest(nethttp.MethodPost, ts.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Host = "wyrd.internal:8080"
	req.Header.Set("Origin", "http://wyrd.internal:8080")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// One MCP endpoint per worktree, isolated the way /api/mcp/tools/call is:
// each worktree is loaded with a reader that tags its package, so a call to
// /w/<name>/mcp must come back carrying that worktree's tag alone.
func TestMCPHTTP_MultiMode_IsolatesWorktrees(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "1")
	primary := buildTwoWorktreeRepo(t)

	multi := serve.NewMultiState(primary, func(ctx context.Context, name, path string) (*serve.State, error) {
		st := serve.NewState(path, serve.WithReader(taggedReader{tag: name}))
		if err := st.Load(ctx); err != nil {
			return nil, err
		}
		return st, nil
	})
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	srv, err := NewMultiServer(multi)
	if err != nil {
		t.Fatalf("NewMultiServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// The handshake is answered from a cold worktree — that is the point of
	// not blocking on the parse.
	resultMap(t, decodeMCP(t, postMCP(t, ts, "/w/alpha/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)))

	// tools/call against a still-parsing worktree answers "loading"; a real
	// client retries, so poll until the model is there.
	call := func(worktree string) string {
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp := decodeMCP(t, postMCP(t, ts, "/w/"+worktree+"/mcp",
				`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_packages","arguments":{}}}`, nil))
			raw, _ := json.Marshal(resp.Result)
			if !strings.Contains(string(raw), `\"status\":\"loading\"`) {
				return string(raw)
			}
			if time.Now().After(deadline) {
				t.Fatalf("/w/%s/mcp still loading after deadline: %s", worktree, raw)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	if got := call("alpha"); !strings.Contains(got, "pkg/alpha") || strings.Contains(got, "pkg/beta") {
		t.Errorf("/w/alpha/mcp list_packages = %s, want pkg/alpha and not pkg/beta", got)
	}
	if got := call("beta"); !strings.Contains(got, "pkg/beta") || strings.Contains(got, "pkg/alpha") {
		t.Errorf("/w/beta/mcp list_packages = %s, want pkg/beta and not pkg/alpha", got)
	}
}

// A repo-level daemon has no single MCP endpoint to redirect to, and a 302 on
// a POST is not a redirect an MCP session survives. Say which URL works.
func TestMCPHTTP_MultiMode_BareEndpointNamesTheWorktreeURLs(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "1")
	primary := buildTwoWorktreeRepo(t)

	multi := serve.NewMultiState(primary, func(ctx context.Context, name, path string) (*serve.State, error) {
		return serve.NewState(path, serve.WithReader(taggedReader{tag: name})), nil
	})
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	srv, err := NewMultiServer(multi)
	if err != nil {
		t.Fatalf("NewMultiServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp := postMCP(t, ts, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "/w/{worktree}/mcp") {
		t.Errorf("body = %s, want it to name the per-worktree endpoint", body)
	}
	for _, name := range []string{"alpha", "beta"} {
		if !strings.Contains(string(body), name) {
			t.Errorf("body = %s, want it to list worktree %q", body, name)
		}
	}
}
