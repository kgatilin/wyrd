package http

import (
	"context"
	"strings"
	"testing"
)

func TestRenderD2_SimpleGraph(t *testing.T) {
	svg, err := renderD2(context.Background(), "a -> b")
	if err != nil {
		t.Fatalf("renderD2: %v", err)
	}
	s := string(svg)
	if !strings.Contains(s, "<svg") {
		t.Fatalf("output missing <svg tag: %q", truncate(s, 200))
	}
}

func TestRenderD2_Empty(t *testing.T) {
	_, err := renderD2(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty source, got nil")
	}
}

func TestRenderD2_InvalidSource(t *testing.T) {
	// Unclosed brace is a reliable parse error across d2 versions.
	_, err := renderD2(context.Background(), "a -> b {")
	if err == nil {
		t.Fatal("expected error for invalid source, got nil")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
