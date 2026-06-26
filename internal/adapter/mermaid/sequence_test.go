package mermaid

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/sequence"
)

func ref(pkg, sym string) domain.SymbolRef {
	return domain.SymbolRef{Package: pkg, Symbol: sym}
}

func TestBuildSequenceSource_NilRoot(t *testing.T) {
	if got := BuildSequenceSource(nil); got != "" {
		t.Fatalf("expected empty string for nil root, got %q", got)
	}
}

func TestBuildSequenceSource_TypeLifelinesAndFlow(t *testing.T) {
	// Service.Generate calls overlay.Merge, then itself calls a helper.
	root := &sequence.Node{
		Symbol: ref("internal/service", "Service.Generate"),
		Children: []*sequence.Node{
			{
				Symbol: ref("internal/overlay", "Merge"),
				Count:  1,
			},
			{
				Symbol: ref("internal/service", "Service.validate"),
				Count:  2,
			},
		},
	}

	out := BuildSequenceSource(root)

	if !strings.HasPrefix(out, "sequenceDiagram\n") {
		t.Fatalf("expected mermaid header, got:\n%s", out)
	}
	// Method lifeline is the receiver type; package func is its own lifeline.
	wantParticipants := []string{
		"participant p0 as service.Service",
		"participant p1 as overlay.Merge",
	}
	for _, w := range wantParticipants {
		if !strings.Contains(out, w) {
			t.Errorf("missing participant %q in:\n%s", w, out)
		}
	}
	// A same-type call (Service.validate) is a self-message on p0 and
	// carries the multiplicity annotation.
	if !strings.Contains(out, "p0->>p1: Merge()") {
		t.Errorf("expected Generate->Merge message, got:\n%s", out)
	}
	if !strings.Contains(out, "p0->>p0: validate() ×2") {
		t.Errorf("expected self-message with ×2 multiplicity, got:\n%s", out)
	}
}

func TestBuildSequenceSource_ViaAndTerminals(t *testing.T) {
	root := &sequence.Node{
		Symbol: ref("pkg/svc", "Service.Generate"),
		Children: []*sequence.Node{
			{
				Symbol: ref("pkg/rd", "fileReader.Read"),
				Via:    "io.Reader",
				Count:  1,
			},
			{
				Symbol: ref("pkg/svc", "Service.Generate"),
				Cycle:  true,
				Count:  1,
			},
			{
				Symbol:     ref("pkg/x", "Deep.Work"),
				DepthLimit: true,
				Count:      1,
			},
		},
	}

	out := BuildSequenceSource(root)

	if !strings.Contains(out, "Read() [via Reader]") {
		t.Errorf("expected interface-dispatch annotation, got:\n%s", out)
	}
	if !strings.Contains(out, "Generate() (cycle)") {
		t.Errorf("expected cycle annotation, got:\n%s", out)
	}
	if !strings.Contains(out, "Work() (depth limit)") {
		t.Errorf("expected depth-limit annotation, got:\n%s", out)
	}
}
