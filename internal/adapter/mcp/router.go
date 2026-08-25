package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/kgatilin/wyrd/internal/serve"
)

// HandleRequest routes one JSON-RPC message against state and returns the
// response to send back, or nil when req is a notification — JSON-RPC 2.0
// forbids answering one.
//
// This is the whole MCP method surface (initialize, the initialized
// acknowledgement, tools/list, tools/call, ping) with no transport in it, so
// the stdio transport and the Streamable HTTP endpoint answer identically by
// construction rather than by two switches kept in step by hand.
//
// diag, when non-nil, receives one line per tool-level failure; the stdio
// transport points it at stderr.
func HandleRequest(ctx context.Context, state *serve.State, req Request, diag io.Writer) *Response {
	// Notifications (no id) require no response per JSON-RPC 2.0.
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		if isNotification {
			return nil
		}
		return respond(NewResponse(req.ID, newInitializeResult(clientProtocolVersion(req.Params))))

	case "notifications/initialized", "initialized":
		// Client acknowledgement — no response required.
		return nil

	case "tools/list":
		if isNotification {
			return nil
		}
		return respond(NewResponse(req.ID, map[string]any{
			"tools": ToolDefinitions(),
		}))

	case "tools/call":
		if isNotification {
			return nil
		}
		return toolsCallResponse(ctx, state, req, diag)

	case "ping":
		if isNotification {
			return nil
		}
		return respond(NewResponse(req.ID, map[string]any{}))

	default:
		if isNotification {
			return nil
		}
		return respond(NewErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method %q not supported", req.Method)))
	}
}

// respond returns a pointer to resp. HandleRequest answers with a pointer so
// a caller can tell "nothing to send" (a notification) from an empty message.
func respond(resp Response) *Response { return &resp }

// toolsCallParams is the shape of tools/call params per the MCP spec.
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolsCallResponse extracts name+arguments from the request, invokes the
// tool, and builds the response. Tool-level failures (package not found)
// arrive as a ToolResult with IsError set; only protocol-level ones (unknown
// tool, undecodable arguments) become JSON-RPC errors.
func toolsCallResponse(ctx context.Context, state *serve.State, req Request, diag io.Writer) *Response {
	var params toolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return respond(NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid params: %v", err)))
		}
	}
	if params.Name == "" {
		return respond(NewErrorResponse(req.ID, ErrInvalidParams, "missing tool name"))
	}

	result, rpcErr := DispatchContext(ctx, state, params.Name, params.Arguments)
	if rpcErr != nil {
		if diag != nil {
			fmt.Fprintf(diag, "mcp: tool %q error: %s\n", params.Name, rpcErr.Message)
		}
		return respond(Response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcErr})
	}
	return respond(NewResponse(req.ID, result))
}
