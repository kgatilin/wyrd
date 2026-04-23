// Package mcp implements a minimal Model Context Protocol (MCP) stdio
// transport for the archai daemon. The transport speaks line-based
// JSON-RPC 2.0 over stdin/stdout and exposes three read-only tools
// (extract, list_packages, get_package) backed by the in-memory
// serve.State model.
//
// Only the subset of MCP needed to advertise and dispatch tools is
// implemented: the initialize handshake, tools/list and tools/call.
// Extract/list_packages/get_package return tool results as
// [{type: "text", text: "<JSON payload>"}] content blocks.
package mcp

import (
	"encoding/json"
)

// JSON-RPC 2.0 message shapes. Requests may carry an id (client calls)
// or omit it (notifications). Responses always echo the request id.

// jsonRPCVersion is the protocol version string required on every
// JSON-RPC 2.0 message.
const jsonRPCVersion = "2.0"

// protocolVersion is the MCP protocol version we advertise during
// initialize. Clients that don't recognize the exact string typically
// negotiate down — for the current read-only tool set this is fine.
const protocolVersion = "2024-11-05"

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

// newInitializeResult builds the fixed response for initialize.
func newInitializeResult() initializeResult {
	return initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities: map[string]any{
			"tools": map[string]any{},
		},
		ServerInfo: serverInfo{
			Name:    "archai",
			Version: "0.1.0",
		},
	}
}

// newResponse builds a successful response for id with result.
func newResponse(id json.RawMessage, result interface{}) Response {
	return Response{JSONRPC: jsonRPCVersion, ID: id, Result: result}
}

// newErrorResponse builds an error response for id with code/message.
func newErrorResponse(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
