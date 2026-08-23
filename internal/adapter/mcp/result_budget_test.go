package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClampToolResult_UnderCeilingPassesThrough(t *testing.T) {
	in := ToolResult{Content: []ToolResultContent{{Type: "text", Text: `{"ok":true}`}}}
	out := clampToolResult("get_package", in)
	if out.IsError {
		t.Fatalf("small result must pass through, got IsError: %+v", out)
	}
	if out.Content[0].Text != `{"ok":true}` {
		t.Errorf("payload altered: %q", out.Content[0].Text)
	}
}

func TestClampToolResult_OverCeilingReplacedWithEnvelope(t *testing.T) {
	big := strings.Repeat("x", maxResultBytes+1)
	in := ToolResult{Content: []ToolResultContent{{Type: "text", Text: big}}}
	out := clampToolResult("get_package", in)

	if !out.IsError {
		t.Fatal("oversize result must be marked IsError")
	}
	if len(out.Content) != 1 || len(out.Content[0].Text) > maxResultBytes {
		t.Fatalf("envelope itself must be within the ceiling, got %d bytes", len(out.Content[0].Text))
	}
	var env oversizeEnvelope
	if err := json.Unmarshal([]byte(out.Content[0].Text), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	if env.Error != "result_too_large" {
		t.Errorf("error = %q, want result_too_large", env.Error)
	}
	if env.Tool != "get_package" {
		t.Errorf("tool = %q, want get_package", env.Tool)
	}
	if env.Bytes <= maxResultBytes || env.Limit != maxResultBytes {
		t.Errorf("bytes/limit wrong: bytes=%d limit=%d", env.Bytes, env.Limit)
	}
	if !strings.Contains(env.Message, "get_node") {
		t.Errorf("get_package hint should point at get_node: %q", env.Message)
	}
}

func TestClampToolResult_SumsMultipleContentBlocks(t *testing.T) {
	half := strings.Repeat("y", maxResultBytes/2+1)
	in := ToolResult{Content: []ToolResultContent{
		{Type: "text", Text: half},
		{Type: "text", Text: half},
	}}
	out := clampToolResult("search", in)
	if !out.IsError {
		t.Fatal("two blocks that together exceed the ceiling must be clamped")
	}
}

func TestClip(t *testing.T) {
	if got := clip("short", 100); got != "short" {
		t.Errorf("under-limit clip altered text: %q", got)
	}
	got := clip(strings.Repeat("a", 100), 10)
	if !strings.Contains(got, "[+90 bytes]") {
		t.Errorf("clip should note elided bytes: %q", got)
	}
	// Rune safety: cutting mid-multibyte-rune must not corrupt output.
	s := strings.Repeat("é", 50) // 2 bytes each
	got = clip(s, 11)
	if !json.Valid([]byte(`"` + got + `"`)) {
		t.Errorf("clip produced invalid UTF-8 around a rune boundary: %q", got)
	}
}

func TestSynopsis(t *testing.T) {
	// The rule itself is covered in internal/godoc; this pins that the
	// renderer's doc field goes through it rather than a width cut of its own.
	doc := "Dispatch routes a call to its handler.\nEverything after this is detail."
	if got, want := synopsis(doc, 200), "Dispatch routes a call to its handler."; got != want {
		t.Errorf("synopsis = %q, want %q", got, want)
	}
	if got := synopsis(strings.Repeat("word ", 60), 50); !strings.HasSuffix(got, " …") {
		t.Errorf("over-budget synopsis should be clipped and marked: %q", got)
	}
}

func TestCapMembers(t *testing.T) {
	if got := capMembers([]string{"a", "b"}, 5); len(got) != 2 {
		t.Errorf("under-limit capMembers altered slice: %v", got)
	}
	got := capMembers([]string{"a", "b", "c", "d"}, 2)
	if len(got) != 3 || !strings.Contains(got[2], "+2 more") {
		t.Errorf("capMembers should append overflow sentinel: %v", got)
	}
}
