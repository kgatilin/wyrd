package http

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/url"
	"strings"

	"github.com/kgatilin/wyrd/internal/adapter/mcp"
	"github.com/kgatilin/wyrd/internal/plugin"
	"github.com/kgatilin/wyrd/internal/serve"
)

// mcpPath is the MCP endpoint's path within the routes a worktree owns: /mcp
// on a single-worktree daemon, /w/<name>/mcp on a repo-level one. It carries
// the Streamable HTTP transport, which is the only one a client that cannot
// spawn processes can use — an agent in a container reaching the daemon on
// the host, or any MCP client configured with a URL instead of a command.
const mcpPath = "/mcp"

// handleMCP serves the Streamable HTTP transport in its stateless form: one
// JSON-RPC message per POST, one JSON-RPC message back, no session id and no
// server-initiated stream. wyrd's tools are synchronous request/response, so
// there is nothing for an SSE stream to carry.
//
// The shape below is what the official Go SDK's StreamableClientTransport
// requires of a server, and the compatibility test in this package drives a
// real one against it:
//
//	POST with an id          → 200, Content-Type: application/json, one message
//	POST without an id       → 202 Accepted, empty body
//	GET (standalone stream)  → 405, which the client reads as "no stream here"
//	DELETE (end session)     → 405; we never hand out a session to end
func (s *Server) handleMCP(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.authorizeMCP(w, r) {
		return
	}
	if r.Method != nethttp.MethodPost {
		w.Header().Set("Allow", nethttp.MethodPost)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeMCPMessage(w, nethttp.StatusBadRequest,
			mcp.NewErrorResponse(nil, mcp.ErrParseError, "read body: "+err.Error()))
		return
	}
	var req mcp.Request
	if err := json.Unmarshal(trimBOM(body), &req); err != nil {
		// Not a JSON-RPC message at all: answer 400 with the parse error
		// rather than 200, so a misdirected caller sees an HTTP failure.
		writeMCPMessage(w, nethttp.StatusBadRequest,
			mcp.NewErrorResponse(nil, mcp.ErrParseError, fmt.Sprintf("parse error: %v", err)))
		return
	}

	state := s.mcpState(r)
	if resp, notReady := s.mcpNotReadyResponse(r, req, state); notReady {
		writeMCPMessage(w, nethttp.StatusOK, resp)
		return
	}

	// A plugin tool reads out of the worktree this session addresses, so it
	// gets that worktree's Host rather than the one bootstrap happened to hold.
	ctx := r.Context()
	if state != nil {
		ctx = plugin.ContextWithHost(ctx, serve.NewHost(state, nil))
	}

	resp := mcp.HandleRequest(ctx, state, req, nil)
	if resp == nil {
		// A notification. JSON-RPC forbids a response and the spec pins the
		// status: 202 with nothing in the body.
		w.WriteHeader(nethttp.StatusAccepted)
		return
	}
	writeMCPMessage(w, nethttp.StatusOK, *resp)
}

// handleMCPNoWorktree answers a repo-level daemon's bare /mcp. The read APIs
// redirect a worktree-less URL to the selected worktree; an MCP session must
// not be treated that way — it POSTs, so a 302 is either replayed as a GET or
// quietly binds the session to whichever worktree a cookie last named. Name
// the URLs that do work instead.
func (s *Server) handleMCPNoWorktree(w nethttp.ResponseWriter, r *nethttp.Request) {
	msg := "this daemon serves one MCP endpoint per worktree; use /w/{worktree}/mcp"
	if names := s.multi.Names(); len(names) > 0 {
		msg += " (worktrees: " + strings.Join(names, ", ") + ")"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

// mcpState returns the model that answers r's MCP call. Unlike stateFor it
// never falls back to another worktree: a session addressed to /w/<name>/mcp
// answers from <name>'s model or reports that <name> has none yet. It returns
// nil while a repo-level daemon is still parsing the worktree — initialize and
// tools/list need no model, and mcpNotReadyResponse covers the calls that do.
func (s *Server) mcpState(r *nethttp.Request) *serve.State {
	if s.state != nil {
		return s.state
	}
	if st, ok := r.Context().Value(ctxWorktreeState).(*serve.State); ok {
		return st
	}
	return nil
}

// mcpNotReadyResponse answers a tools/call addressed to a worktree whose
// model has not finished loading. The payload is the readiness record the
// thin-client transport and the `status` tool already return, so a client
// renders "still parsing" (or "failed") the same way on either transport
// instead of reading a bare "no state available".
//
// It reports false for every other method: the handshake and the tool
// listing are answered from a cold worktree exactly as from a warm one,
// which is what lets an agent attach while the parse is still running.
func (s *Server) mcpNotReadyResponse(r *nethttp.Request, req mcp.Request, state *serve.State) (mcp.Response, bool) {
	if state != nil || s.multi == nil || req.Method != "tools/call" || len(req.ID) == 0 {
		return mcp.Response{}, false
	}
	name := s.currentWorktree(r)
	body := loadingStatusBody()
	if retryAt, loadErr := s.multi.LoadError(name); loadErr != nil {
		body = failedStatusBody(name, loadErr, retryAt)
	}
	payload, _ := json.Marshal(body)
	result := mcp.ToolResult{Content: []mcp.ToolResultContent{{Type: "text", Text: string(payload)}}}
	return mcp.NewResponse(req.ID, result), true
}

// writeMCPMessage writes one JSON-RPC message as the whole response body.
// The Content-Type is not decoration: the Go SDK client dispatches on it and
// treats anything other than application/json or text/event-stream as an
// unsupported response, failing the call.
func writeMCPMessage(w nethttp.ResponseWriter, status int, resp mcp.Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// authorizeMCP applies the two checks that guard the endpoint and reports
// whether the request may proceed. It answers the request itself when it
// does not.
func (s *Server) authorizeMCP(w nethttp.ResponseWriter, r *nethttp.Request) bool {
	if !mcpOriginAllowed(r.Header.Get("Origin"), r.Host) {
		nethttp.Error(w, "forbidden origin", nethttp.StatusForbidden)
		return false
	}
	if s.authToken == "" {
		return true
	}
	if !bearerTokenMatches(r.Header.Get("Authorization"), s.authToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="wyrd"`)
		nethttp.Error(w, "unauthorized", nethttp.StatusUnauthorized)
		return false
	}
	return true
}

// bearerTokenMatches reports whether an Authorization header carries want.
// The comparison is constant-time so a wrong token cannot be recovered one
// byte at a time from response timings.
func bearerTokenMatches(header, want string) bool {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	got := strings.TrimSpace(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// mcpOriginAllowed reports whether a request carrying this Origin may drive
// the MCP endpoint. The spec requires a locally-bound server to validate it,
// because DNS rebinding lets a page on any site POST to http://127.0.0.1:<port>
// from the visitor's browser and reach every tool the daemon exposes.
//
// A request with no Origin is not from a page — browsers attach one to every
// cross-origin request — so it passes: that is how an MCP client reaches us.
// A page served by this daemon passes too; anything else is refused.
func mcpOriginAllowed(origin, host string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// "null" (a sandboxed frame or a file:// page) lands here.
		return false
	}
	if strings.EqualFold(u.Host, host) {
		return true
	}
	return isLoopbackHost(u.Hostname())
}

// isLoopbackHost reports whether hostname names this machine's loopback
// interface, at any port. A daemon bound to loopback is reachable from the
// browser only via such a name, so those origins are the ones a legitimate
// local page has.
func isLoopbackHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}
