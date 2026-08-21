package eventmodel

import (
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/eventmodel"
)

func TestToArchmotifGraph(t *testing.T) {
	// Build a simple event model graph.
	billing := &eventmodel.Component{
		ID:    "billing",
		Owns:  "billing",
		Types: map[string]eventmodel.SchemaNode{"Invoice": {Raw: map[string]any{"type": "object"}}},
	}
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}
	billing.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issue"}}
	billing.StateEvents = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	shipping := &eventmodel.Component{
		ID:   "shipping",
		Owns: "shipping",
	}
	shipping.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	gateway := &eventmodel.Component{
		ID:   "gateway",
		Owns: "gateway",
	}
	gateway.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issue"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"shipping": shipping,
			"gateway":  gateway,
		},
	}

	g := eventmodel.BuildGraph(m)
	amg, err := ToArchmotifGraph(g)
	if err != nil {
		t.Fatalf("ToArchmotifGraph failed: %v", err)
	}

	// Verify basic structure.
	if amg == nil {
		t.Fatal("returned graph is nil")
	}

	// Check that component nodes were created.
	nodes := amg.Nodes()
	var hasComponent, hasKind, hasVocabType bool
	for _, n := range nodes {
		if strings.HasPrefix(n.ID, "component:") {
			hasComponent = true
		}
		if strings.HasPrefix(n.ID, "kind:") {
			hasKind = true
		}
		if strings.HasPrefix(n.ID, "type:") {
			hasVocabType = true
		}
	}

	if !hasComponent {
		t.Error("expected component nodes in graph")
	}
	if !hasKind {
		t.Error("expected kind nodes in graph")
	}
	if !hasVocabType {
		t.Error("expected types type nodes in graph")
	}
}

func TestToArchmotifGraphWithHealth(t *testing.T) {
	// Create an orphan output (nobody observes it).
	billing := &eventmodel.Component{
		ID:   "billing",
		Owns: "billing",
	}
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.orphan.event"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{"billing": billing},
	}

	g := eventmodel.BuildGraph(m)

	// Verify health is set on the kind node.
	var kindNode eventmodel.Node
	for _, n := range g.Nodes {
		if n.ID == "kind:billing.orphan.event" {
			kindNode = n
			break
		}
	}
	if kindNode.ID == "" {
		t.Fatal("kind node not found")
	}
	if kindNode.Attrs["health"] != string(eventmodel.HealthOrphan) {
		t.Errorf("expected health=orphan, got %v", kindNode.Attrs["health"])
	}

	// Convert to archmotif.
	amg, err := ToArchmotifGraph(g)
	if err != nil {
		t.Fatalf("ToArchmotifGraph failed: %v", err)
	}
	if amg == nil {
		t.Fatal("returned graph is nil")
	}
}

func TestKindPackage(t *testing.T) {
	cases := []struct {
		kindID string
		want   string
	}{
		{"kind:billing.invoice.issued", "kinds:billing"},
		{"kind:billing", "kinds:billing"},
		{"kind:a.b.c.d", "kinds:a"},
	}
	for _, tc := range cases {
		got := kindPackage(tc.kindID)
		if got != tc.want {
			t.Errorf("kindPackage(%q) = %q, want %q", tc.kindID, got, tc.want)
		}
	}
}

// TestDottedComponentID verifies that a component id containing dots survives
// the round trip into the archmotif graph. A type node's id is
// "type:<component>.<name>", so parsing the owner back out by splitting on the
// last dot would read "billing.invoice" as the component of
// "type:billing.invoice.Line". The component is stored in the node's
// attributes instead, and the exporter reads it from there.
func TestDottedComponentID(t *testing.T) {
	billing := &eventmodel.Component{
		ID:    "billing.invoice",
		Owns:  "billing.invoice",
		Types: map[string]eventmodel.SchemaNode{"Line": {Raw: map[string]any{"type": "object"}}},
	}
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	consumer := &eventmodel.Component{ID: "consumer", Owns: "consumer"}
	consumer.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing.invoice": billing,
			"consumer":        consumer,
		},
	}

	g := eventmodel.BuildGraph(m)

	var typeNode eventmodel.Node
	for _, n := range g.Nodes {
		if n.Kind == eventmodel.NodeType {
			typeNode = n
			break
		}
	}
	if typeNode.ID == "" {
		t.Fatal("type node not found")
	}
	if typeNode.ID != "type:billing.invoice.Line" {
		t.Errorf("type ID = %q, want %q", typeNode.ID, "type:billing.invoice.Line")
	}
	if typeNode.Attrs["component"] != "billing.invoice" {
		t.Errorf("type component attr = %q, want %q", typeNode.Attrs["component"], "billing.invoice")
	}

	// Convert to archmotif - this would fail with the old approach.
	amg, err := ToArchmotifGraph(g)
	if err != nil {
		t.Fatalf("ToArchmotifGraph failed: %v", err)
	}

	var foundType bool
	for _, n := range amg.Nodes() {
		if n.ID == "type:billing.invoice.Line" {
			foundType = true
			break
		}
	}
	if !foundType {
		t.Error("type node not found in archmotif graph")
	}
}
