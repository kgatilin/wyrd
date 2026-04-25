package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/plugin"
)

// TestSetPluginTools_AppendsToToolDefinitions verifies the prefixed
// plugin tools show up alongside the built-in ones.
func TestSetPluginTools_AppendsToToolDefinitions(t *testing.T) {
	t.Cleanup(func() { SetPluginTools(nil) })
	SetPluginTools([]plugin.NamedMCPTool{{
		Plugin: "complexity",
		Tool: plugin.MCPTool{
			Name:        "scores",
			Description: "Per-package complexity scores",
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				return map[string]any{"ok": true}, nil
			},
		},
	}})

	defs := ToolDefinitions()
	want := plugin.PrefixedMCPName("complexity", "scores")
	found := false
	for _, d := range defs {
		if d.Name == want {
			found = true
			if d.Description != "Per-package complexity scores" {
				t.Errorf("description = %q", d.Description)
			}
			if d.InputSchema == nil {
				t.Errorf("InputSchema should default to {} object")
			}
		}
	}
	if !found {
		t.Errorf("ToolDefinitions missing %q; got %d defs", want, len(defs))
	}
}

// TestDispatch_PluginTool routes a tools/call to a plugin handler via
// the prefixed name.
func TestDispatch_PluginTool(t *testing.T) {
	t.Cleanup(func() { SetPluginTools(nil) })
	gotArgs := map[string]any{}
	SetPluginTools([]plugin.NamedMCPTool{{
		Plugin: "complexity",
		Tool: plugin.MCPTool{
			Name: "scores",
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				gotArgs = args
				return map[string]any{"score": 42}, nil
			},
		},
	}})

	raw := json.RawMessage(`{"package":"internal/foo"}`)
	res, rpcErr := Dispatch(nil, plugin.PrefixedMCPName("complexity", "scores"), raw)
	if rpcErr != nil {
		t.Fatalf("Dispatch RPCError: %+v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError result: %+v", res)
	}
	if got := gotArgs["package"]; got != "internal/foo" {
		t.Errorf("plugin handler saw args[package] = %v, want internal/foo", got)
	}
	if len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, `"score": 42`) {
		t.Errorf("result content = %+v", res.Content)
	}
}

// TestDispatch_UnknownPluginTool returns method-not-found rather than
// silently dispatching to the wrong handler.
func TestDispatch_UnknownPluginTool(t *testing.T) {
	t.Cleanup(func() { SetPluginTools(nil) })
	SetPluginTools(nil)
	_, rpcErr := Dispatch(nil, "plugin.unknown.thing", nil)
	if rpcErr == nil {
		t.Fatalf("expected RPCError for unknown plugin tool")
	}
	if rpcErr.Code != ErrMethodNotFound {
		t.Errorf("error code = %d, want %d", rpcErr.Code, ErrMethodNotFound)
	}
}
