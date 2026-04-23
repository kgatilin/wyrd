package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
)

// ToolDefinition describes a single MCP tool exposed via tools/list.
// InputSchema is a JSON Schema object; we keep it as an any so each
// tool can define its own shape without a shared struct.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ToolResultContent is one "content" item returned by tools/call. We
// only emit text blocks; binary/resource blocks are out of scope for
// the M5b read-only surface.
type ToolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult is the payload returned by tools/call. IsError is a hint
// for clients — JSON-RPC errors are used for protocol-level failures
// (unknown tool etc.) while tool-level errors (package not found) are
// reported via IsError + a text message so the model can see them.
type ToolResult struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// PackageSummary is the minimal record returned by list_packages. The
// summary is intentionally small so agents can decide which package to
// drill into via get_package without paying for the full payload.
type PackageSummary struct {
	Path           string `json:"path"`
	Name           string `json:"name"`
	Layer          string `json:"layer,omitempty"`
	InterfaceCount int    `json:"interface_count"`
	StructCount    int    `json:"struct_count"`
	FunctionCount  int    `json:"function_count"`
}

// ToolDefinitions returns the three tools we advertise. Kept as a
// function rather than a var so the JSON-Schema maps don't become
// mutable package state.
func ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "extract",
			Description: "Return the full extracted Go model (all packages) from the archai daemon's in-memory state. Optionally filter to specific package paths.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Module-relative package paths to include. When omitted or empty, all packages are returned.",
					},
				},
			},
		},
		{
			Name:        "list_packages",
			Description: "List packages known to the daemon with minimal summary fields (path, name, layer, counts).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_package",
			Description: "Return the full PackageModel for a single package identified by its module-relative path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Module-relative package path, e.g. internal/service.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// Dispatch routes a tools/call invocation to the matching handler.
// Unknown tool names surface as JSON-RPC method-not-found errors; the
// three known tools all return ToolResult (possibly with IsError=true
// when inputs are invalid).
func Dispatch(state *serve.State, name string, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	switch name {
	case "extract":
		return handleExtract(state, rawArgs)
	case "list_packages":
		return handleListPackages(state)
	case "get_package":
		return handleGetPackage(state, rawArgs)
	default:
		return ToolResult{}, &RPCError{
			Code:    ErrMethodNotFound,
			Message: fmt.Sprintf("unknown tool %q", name),
		}
	}
}

// extractArgs is the input schema for the extract tool.
type extractArgs struct {
	Paths []string `json:"paths"`
}

// handleExtract returns the (optionally filtered) package list. An
// empty Paths slice means "return everything"; an unknown path is not
// an error — it just contributes nothing.
func handleExtract(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args extractArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return ToolResult{}, &RPCError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("invalid arguments: %v", err),
			}
		}
	}

	snap := snapshotOrEmpty(state)
	out := snap.Packages
	if len(args.Paths) > 0 {
		want := make(map[string]struct{}, len(args.Paths))
		for _, p := range args.Paths {
			want[p] = struct{}{}
		}
		filtered := make([]domain.PackageModel, 0, len(args.Paths))
		for _, m := range snap.Packages {
			if _, ok := want[m.Path]; ok {
				filtered = append(filtered, m)
			}
		}
		out = filtered
	}
	if out == nil {
		out = []domain.PackageModel{}
	}
	return textResult(out)
}

// handleListPackages returns the minimal per-package summary.
func handleListPackages(state *serve.State) (ToolResult, *RPCError) {
	snap := snapshotOrEmpty(state)
	summaries := make([]PackageSummary, 0, len(snap.Packages))
	for _, m := range snap.Packages {
		summaries = append(summaries, PackageSummary{
			Path:           m.Path,
			Name:           m.Name,
			Layer:          m.Layer,
			InterfaceCount: len(m.Interfaces),
			StructCount:    len(m.Structs),
			FunctionCount:  len(m.Functions),
		})
	}
	return textResult(summaries)
}

// getPackageArgs is the input schema for the get_package tool.
type getPackageArgs struct {
	Path string `json:"path"`
}

// handleGetPackage returns a single PackageModel. Missing/empty paths
// and unknown packages come back as IsError=true tool results (not
// JSON-RPC errors) so the model can see the message and recover.
func handleGetPackage(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args getPackageArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return ToolResult{}, &RPCError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("invalid arguments: %v", err),
			}
		}
	}
	if args.Path == "" {
		return errorResult("missing required argument: path"), nil
	}

	snap := snapshotOrEmpty(state)
	for _, m := range snap.Packages {
		if m.Path == args.Path {
			return textResult(m)
		}
	}
	return errorResult(fmt.Sprintf("package %q not found", args.Path)), nil
}

// snapshotOrEmpty returns a Snapshot even when state is nil, so the
// tools can be unit-tested in isolation and the daemon can answer
// requests before Load completes.
func snapshotOrEmpty(state *serve.State) serve.Snapshot {
	if state == nil {
		return serve.Snapshot{Packages: []domain.PackageModel{}}
	}
	return state.Snapshot()
}

// textResult marshals payload as indented JSON and wraps it in a
// single text content block.
func textResult(payload any) (ToolResult, *RPCError) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ToolResult{}, &RPCError{
			Code:    ErrInternal,
			Message: fmt.Sprintf("marshalling result: %v", err),
		}
	}
	return ToolResult{
		Content: []ToolResultContent{{Type: "text", Text: string(data)}},
	}, nil
}

// errorResult wraps a human-readable message as an IsError=true tool
// result. Used for input validation failures the agent can surface.
func errorResult(msg string) ToolResult {
	return ToolResult{
		Content: []ToolResultContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
