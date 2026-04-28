package d2

import (
	"context"
	"strings"
	"testing"
)

func TestRenderSVGSimpleGraph(t *testing.T) {
	svg, err := RenderSVG(context.Background(), "a -> b")
	if err != nil {
		t.Fatalf("RenderSVG: %v", err)
	}
	s := string(svg)
	if !strings.Contains(s, "<svg") {
		t.Fatalf("output missing <svg tag: %q", truncateRenderTest(s, 200))
	}
}

func TestRenderSVGEmpty(t *testing.T) {
	_, err := RenderSVG(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty source, got nil")
	}
}

func TestRenderSVGInvalidSource(t *testing.T) {
	_, err := RenderSVG(context.Background(), "a -> b {")
	if err == nil {
		t.Fatal("expected error for invalid source, got nil")
	}
}

func truncateRenderTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
