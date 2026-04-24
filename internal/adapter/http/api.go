package http

import (
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"

	"github.com/kgatilin/archai/internal/adapter/mcp"
)

// registerAPIRoutes wires JSON endpoints that mirror every MCP tool.
// Each endpoint funnels into mcp.Dispatch so the MCP stdio client (M11
// thin-client mode) and any direct HTTP caller see identical schemas.
//
// All responses are JSON:
//   - On success, the body is the unwrapped tool payload (the JSON text
//     embedded in ToolResult.Content[0].Text) as application/json.
//   - On tool-level error (IsError=true), status 400 with the error
//     message as {"error": "<msg>"}.
//   - On RPC-level error (unknown tool, malformed args), status 400
//     with {"error": "<msg>", "code": <rpc-code>}.
//
// Read endpoints (GET):
//
//	GET  /api/mcp/packages            — list_packages
//	GET  /api/mcp/packages/{path}     — get_package
//	GET  /api/mcp/targets             — list_targets
//	GET  /api/mcp/diff?target=<id>    — diff (body returned as JSON)
//	GET  /api/mcp/extract?path=a&…    — extract (filtered)
//
// Write endpoints (POST, JSON body):
//
//	POST /api/mcp/targets/lock        — lock_target    ({id, description})
//	POST /api/mcp/targets/current     — set_current_target ({id})
//	POST /api/mcp/diff/apply          — apply_diff     ({patch_yaml, target})
//	POST /api/mcp/validate            — validate       ({target})
//
// A single generic endpoint is also exposed for forward-compatibility:
//
//	POST /api/mcp/tools/call          — {name, arguments} → ToolResult
//
// All routes live under /api/mcp/ so they coexist with M8's
// cytoscape-shaped /api/layers, /api/packages/<path>/graph and
// /api/diff graph endpoints.
func (s *Server) registerAPIRoutes(mux *nethttp.ServeMux) {
	mux.HandleFunc("/api/mcp/tools/call", s.handleAPIToolsCall)
	mux.HandleFunc("/api/mcp/extract", s.handleAPIExtract)
	mux.HandleFunc("/api/mcp/packages", s.handleAPIPackages)
	mux.HandleFunc("/api/mcp/packages/", s.handleAPIPackageGet)
	mux.HandleFunc("/api/mcp/targets", s.handleAPITargets)
	mux.HandleFunc("/api/mcp/targets/lock", s.handleAPITargetsLock)
	mux.HandleFunc("/api/mcp/targets/current", s.handleAPITargetsCurrent)
	mux.HandleFunc("/api/mcp/diff", s.handleAPIDiff)
	mux.HandleFunc("/api/mcp/diff/apply", s.handleAPIDiffApply)
	mux.HandleFunc("/api/mcp/validate", s.handleAPIValidate)
}

// writeAPIToolResult unwraps a ToolResult from mcp.Dispatch and writes
// a JSON response. The caller passes the tool name for diagnostic
// messages only.
func writeAPIToolResult(w nethttp.ResponseWriter, res mcp.ToolResult, rpcErr *mcp.RPCError) {
	if rpcErr != nil {
		writeJSONError(w, rpcErr.Code, rpcErr.Message)
		return
	}
	text := firstTextContent(res)
	if res.IsError {
		// Tool-level errors surface the message but keep a 400 status so
		// the thin client can raise this back to the MCP caller.
		writeJSONErrorText(w, nethttp.StatusBadRequest, text)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// ToolResult text is already a JSON document (per mcp.textResult) —
	// forward it verbatim so clients can unmarshal into the exact same
	// types they would see over stdio.
	if text == "" {
		_, _ = w.Write([]byte("null"))
		return
	}
	_, _ = io.WriteString(w, text)
}

// firstTextContent returns the Text of the first content block or "".
func firstTextContent(res mcp.ToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	return res.Content[0].Text
}

// writeJSONError writes {"error": msg, "code": code} with a 400 status.
// Used for RPC-level failures (unknown tool, invalid params).
func writeJSONError(w nethttp.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(nethttp.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": msg,
		"code":  code,
	})
}

// writeJSONErrorText writes {"error": msg} with the given status.
// Used for tool-level errors where we want to keep HTTP semantics
// close to "bad request" without propagating JSON-RPC codes.
func writeJSONErrorText(w nethttp.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

// handleAPIToolsCall is a single passthrough that accepts the raw MCP
// tools/call shape: {"name": "...", "arguments": {...}}. It returns
// the ToolResult directly (not unwrapped) so the caller can inspect
// IsError and Content blocks with the same types used over stdio.
//
// This endpoint is the fallback used by the thin-client MCP wrapper.
func (s *Server) handleAPIToolsCall(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONErrorText(w, nethttp.StatusBadRequest, "invalid body: "+err.Error())
			return
		}
	}
	if req.Name == "" {
		writeJSONErrorText(w, nethttp.StatusBadRequest, "missing tool name")
		return
	}
	res, rpcErr := mcp.Dispatch(s.state, req.Name, req.Arguments)
	if rpcErr != nil {
		writeJSONError(w, rpcErr.Code, rpcErr.Message)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(res)
}

// handleAPIExtract serves GET /api/extract. Repeated `path` query
// params pass through as the extract tool's `paths` argument. Returns
// the tool payload (a JSON array of PackageModel) verbatim.
func (s *Server) handleAPIExtract(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	paths := r.URL.Query()["path"]
	args := map[string]any{}
	if len(paths) > 0 {
		args["paths"] = paths
	}
	rawArgs, _ := json.Marshal(args)
	res, rpcErr := mcp.Dispatch(s.state, "extract", rawArgs)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPIPackages serves GET /api/packages → list_packages.
func (s *Server) handleAPIPackages(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	res, rpcErr := mcp.Dispatch(s.state, "list_packages", nil)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPIPackageGet serves GET /api/packages/{path} → get_package.
// Path segments after /api/packages/ are joined back into a
// module-relative package path.
func (s *Server) handleAPIPackageGet(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	const prefix = "/api/mcp/packages/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		nethttp.NotFound(w, r)
		return
	}
	pkgPath := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if pkgPath == "" {
		s.handleAPIPackages(w, r)
		return
	}
	args, _ := json.Marshal(map[string]string{"path": pkgPath})
	res, rpcErr := mcp.Dispatch(s.state, "get_package", args)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPITargets serves GET /api/targets → list_targets.
func (s *Server) handleAPITargets(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	res, rpcErr := mcp.Dispatch(s.state, "list_targets", nil)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPITargetsLock serves POST /api/targets/lock → lock_target.
// Body: {"id": "...", "description": "..."}.
func (s *Server) handleAPITargetsLock(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	raw, err := readJSONBody(r)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	res, rpcErr := mcp.Dispatch(s.state, "lock_target", raw)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPITargetsCurrent serves POST /api/targets/current → set_current_target.
// Body: {"id": "..."}.
func (s *Server) handleAPITargetsCurrent(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	raw, err := readJSONBody(r)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	res, rpcErr := mcp.Dispatch(s.state, "set_current_target", raw)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPIDiff serves GET /api/diff → diff. Accepts ?target=<id>.
func (s *Server) handleAPIDiff(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	args := map[string]any{}
	if t := r.URL.Query().Get("target"); t != "" {
		args["target"] = t
	}
	rawArgs, _ := json.Marshal(args)
	res, rpcErr := mcp.Dispatch(s.state, "diff", rawArgs)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPIDiffApply serves POST /api/diff/apply → apply_diff.
// Body: {"patch_yaml": "...", "target": "..."}.
func (s *Server) handleAPIDiffApply(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	raw, err := readJSONBody(r)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	res, rpcErr := mcp.Dispatch(s.state, "apply_diff", raw)
	writeAPIToolResult(w, res, rpcErr)
}

// handleAPIValidate serves POST /api/validate → validate.
// Body: {"target": "..."}. POST (not GET) because validate is used as
// a write-style RPC in a thin-client session — the shape simply mirrors
// the MCP tool with an optional target override.
func (s *Server) handleAPIValidate(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost && r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	var raw json.RawMessage
	if r.Method == nethttp.MethodPost {
		b, err := readJSONBody(r)
		if err != nil {
			writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
			return
		}
		raw = b
	} else if t := r.URL.Query().Get("target"); t != "" {
		raw, _ = json.Marshal(map[string]string{"target": t})
	}
	res, rpcErr := mcp.Dispatch(s.state, "validate", raw)
	writeAPIToolResult(w, res, rpcErr)
}

// readJSONBody returns the request body as a json.RawMessage. An empty
// body is allowed (returns nil). A malformed body (not a JSON object)
// surfaces as an error.
func readJSONBody(r *nethttp.Request) (json.RawMessage, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	b = trimBOM(b)
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil, nil
	}
	// Validate it parses as JSON so the downstream tool dispatch reports
	// a tool-level "invalid arguments" instead of a partial decode.
	var tmp any
	if err := json.Unmarshal([]byte(s), &tmp); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	return json.RawMessage(s), nil
}

// trimBOM strips a leading UTF-8 byte-order mark if present. Clients
// very rarely send one for JSON, but it costs almost nothing to be
// forgiving.
func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
