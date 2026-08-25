// Package mcp implements the Model Context Protocol (MCP) surface of the
// wyrd daemon: the tools themselves, and the method router that answers a
// JSON-RPC 2.0 message against the in-memory serve.State model.
//
// Two transports carry that router. This package owns the stdio one
// (line-delimited JSON-RPC over stdin/stdout, plus a thin-client variant that
// forwards tool calls to a running daemon over HTTP); the Streamable HTTP
// endpoint lives in internal/adapter/http and calls HandleRequest for every
// message it receives, so neither transport holds a method switch of its own.
//
// Only the subset of MCP needed to advertise and dispatch tools is
// implemented: the initialize handshake, tools/list and tools/call. Tool
// results are [{type: "text", text: "..."}] content blocks.
package mcp

import (
	"encoding/json"
	"slices"
)

// JSON-RPC 2.0 message shapes. Requests may carry an id (client calls)
// or omit it (notifications). Responses always echo the request id.

// jsonRPCVersion is the protocol version string required on every
// JSON-RPC 2.0 message.
const jsonRPCVersion = "2.0"

// latestProtocolVersion is the newest MCP revision this server speaks, and
// the one an initialize that names no version (or one we don't know) is
// answered with. Streamable HTTP was introduced in 2025-03-26, so a session
// on that transport cannot honestly claim the 2024-11-05 revision.
const latestProtocolVersion = "2025-06-18"

// protocolVersions are the revisions we speak, newest first. A client's
// version is echoed when it appears here, which is what the spec asks for:
// the two sides settle on a revision both know rather than on ours.
var protocolVersions = []string{latestProtocolVersion, "2025-03-26", "2024-11-05"}

// negotiateProtocolVersion returns the revision to advertise in reply to an
// initialize that asked for clientVersion. An unknown or absent version gets
// the newest one we speak — clients validate the answer against their own
// supported set and fail the handshake on anything outside it, so answering
// with a version nobody released is worse than answering with ours.
func negotiateProtocolVersion(clientVersion string) string {
	if slices.Contains(protocolVersions, clientVersion) {
		return clientVersion
	}
	return latestProtocolVersion
}

// initializeParams is the part of the initialize params we read: the revision
// the client speaks.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// clientProtocolVersion pulls the requested revision out of raw initialize
// params. Malformed or absent params yield "", which negotiates to the
// latest revision rather than failing the handshake.
func clientProtocolVersion(rawParams json.RawMessage) string {
	if len(rawParams) == 0 {
		return ""
	}
	var params initializeParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return ""
	}
	return params.ProtocolVersion
}

// Request is an incoming JSON-RPC 2.0 request or notification. ID is
// kept as a RawMessage so we can echo it back verbatim (clients may use
// numbers or strings interchangeably).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response. Exactly one of Result / Error is
// set on the wire; Response uses pointers so omitempty elides the other.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the error object defined by JSON-RPC 2.0.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes we surface.
const (
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
)

// serverInfo is returned from initialize so clients know who they're
// talking to.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeResult is the payload returned for the initialize request.
// We advertise the tools capability (the bag is empty — MCP defines no
// required fields inside it yet).
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

// newInitializeResult builds the response for an initialize that asked for
// clientVersion.
func newInitializeResult(clientVersion string) initializeResult {
	return initializeResult{
		ProtocolVersion: negotiateProtocolVersion(clientVersion),
		Capabilities: map[string]any{
			"tools": map[string]any{},
		},
		ServerInfo: serverInfo{
			Name:    "wyrd",
			Version: "0.1.0",
		},
	}
}

// newResponse builds a successful response for id with result.
func NewResponse(id json.RawMessage, result interface{}) Response {
	return Response{JSONRPC: jsonRPCVersion, ID: id, Result: result}
}

// newErrorResponse builds an error response for id with code/message.
func NewErrorResponse(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
