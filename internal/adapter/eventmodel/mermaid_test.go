package eventmodel

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/eventmodel"
)

func TestToMermaid(t *testing.T) {
	// Build a simple event model graph.
	billing := &eventmodel.Component{
		ID:   "billing",
		Owns: "billing",
	}
	billing.Emits = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}
	billing.Receives = []eventmodel.Slot{{Kind: "billing.invoice.issue", Role: eventmodel.RoleAction}}

	shipping := &eventmodel.Component{
		ID:   "shipping",
		Owns: "shipping",
	}
	shipping.Receives = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}

	gateway := &eventmodel.Component{
		ID:   "gateway",
		Owns: "gateway",
	}
	gateway.Emits = []eventmodel.Slot{{Kind: "billing.invoice.issue", Role: eventmodel.RoleAction}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"shipping": shipping,
			"gateway":  gateway,
		},
	}

	g := eventmodel.BuildGraph(m)
	output := ToMermaid(g)

	// Basic structure checks.
	if !strings.HasPrefix(output, "flowchart LR\n") {
		t.Error("output should start with 'flowchart LR'")
	}

	// Should contain all components.
	if !strings.Contains(output, "billing") {
		t.Error("output should contain billing component")
	}
	if !strings.Contains(output, "shipping") {
		t.Error("output should contain shipping component")
	}
	if !strings.Contains(output, "gateway") {
		t.Error("output should contain gateway component")
	}

	// Should have edge from billing to shipping (fact).
	if !strings.Contains(output, "-->") {
		t.Error("output should contain solid arrow for fact edge")
	}

	// Should have edge from gateway to billing (action).
	if !strings.Contains(output, "-.->") {
		t.Error("output should contain dashed arrow for action edge")
	}
}

func TestToMermaidWithHealth(t *testing.T) {
	// Create an orphan fact.
	billing := &eventmodel.Component{
		ID:   "billing",
		Owns: "billing",
	}
	billing.Emits = []eventmodel.Slot{{Kind: "billing.orphan.event", Role: eventmodel.RoleFact}}

	// Create a consumer that receives a different event (so billing.orphan.event is orphan).
	shipping := &eventmodel.Component{
		ID:   "shipping",
		Owns: "shipping",
	}
	shipping.Receives = []eventmodel.Slot{{Kind: "billing.other.event", Role: eventmodel.RoleFact}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"shipping": shipping,
		},
	}

	g := eventmodel.BuildGraph(m)
	output := ToMermaid(g)

	// The orphan kind should NOT appear as an edge (no receiver).
	// The mermaid output only shows edges between components.
	// So we just verify it generates valid output.
	if !strings.HasPrefix(output, "flowchart LR\n") {
		t.Error("output should start with 'flowchart LR'")
	}
}

func TestToMermaidWithSubgraphs(t *testing.T) {
	// Create enough components with distinct namespaces to trigger subgraphs.
	a := &eventmodel.Component{ID: "a", Owns: "ns_a"}
	a.Emits = []eventmodel.Slot{{Kind: "ns_a.event", Role: eventmodel.RoleFact}}

	b := &eventmodel.Component{ID: "b", Owns: "ns_b"}
	b.Receives = []eventmodel.Slot{{Kind: "ns_a.event", Role: eventmodel.RoleFact}}
	b.Emits = []eventmodel.Slot{{Kind: "ns_b.event", Role: eventmodel.RoleFact}}

	c := &eventmodel.Component{ID: "c", Owns: "ns_c"}
	c.Receives = []eventmodel.Slot{{Kind: "ns_b.event", Role: eventmodel.RoleFact}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"a": a,
			"b": b,
			"c": c,
		},
	}

	g := eventmodel.BuildGraph(m)
	output := ToMermaid(g)

	// Should use subgraphs since there are 3 distinct namespaces.
	if !strings.Contains(output, "subgraph") {
		t.Error("output should contain subgraphs for 3+ namespaces")
	}
}

func TestShortKindName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"billing.invoice.issued", "invoice.issued"},
		{"billing.invoice", "billing.invoice"},
		{"billing", "billing"},
		{"a.b.c.d.e", "d.e"},
	}
	for _, tc := range cases {
		got := shortKindName(tc.input)
		if got != tc.want {
			t.Errorf("shortKindName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMermaidID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"billing", "billing"},
		{"billing.invoice", "billing_invoice"},
		{"billing-core", "billing_core"},
		{"billing:main", "billing_main"},
	}
	for _, tc := range cases {
		got := mermaidID(tc.input)
		if got != tc.want {
			t.Errorf("mermaidID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMermaidLabel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"billing", "billing"},
		{"billing.invoice", "billing.invoice"},
		{"has space", `"has space"`},
		{"", `""`},
	}
	for _, tc := range cases {
		got := mermaidLabel(tc.input)
		if got != tc.want {
			t.Errorf("mermaidLabel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSubgraphIDsDisjointFromNodeIDs(t *testing.T) {
	// Verify that subgraph IDs cannot collide with component node IDs.
	// This matters because Mermaid treats subgraph IDs as node IDs.

	// Create components where namespace equals component id (the collision case).
	billing := &eventmodel.Component{ID: "billing", Owns: "billing"}
	billing.Emits = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}

	ledger := &eventmodel.Component{ID: "ledger", Owns: "ledger"}
	ledger.Receives = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}

	shipping := &eventmodel.Component{ID: "shipping", Owns: "shipping"}
	shipping.Receives = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"ledger":   ledger,
			"shipping": shipping,
		},
	}

	g := eventmodel.BuildGraph(m)
	output := ToMermaid(g)

	// The subgraph IDs should be prefixed with "ns_" to avoid collision.
	// For billing namespace, the subgraph should be ns_billing, not billing.
	if strings.Contains(output, "subgraph billing[") {
		t.Error("subgraph id should be ns_billing, not billing (collision with node id)")
	}
	if !strings.Contains(output, "subgraph ns_billing[") {
		t.Error("subgraph id should be ns_billing")
	}

	// Verify component nodes are still just their raw ids (no ns_ prefix).
	if !strings.Contains(output, "        billing[billing]") {
		t.Error("component node should be billing[billing]")
	}
}
