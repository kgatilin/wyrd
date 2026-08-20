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
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}
	billing.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issue"}}

	shipping := &eventmodel.Component{
		ID:   "shipping",
		Owns: "shipping",
	}
	shipping.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	audit := &eventmodel.Component{
		ID:   "audit",
		Owns: "audit",
	}
	audit.StateEvents = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	gateway := &eventmodel.Component{
		ID:   "gateway",
		Owns: "gateway",
	}
	gateway.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issue"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"shipping": shipping,
			"audit":    audit,
			"gateway":  gateway,
		},
	}

	g := eventmodel.BuildGraph(m)
	output := ToMermaid(g)

	// Basic structure checks.
	if !strings.HasPrefix(output, "flowchart LR\n") {
		t.Error("output should start with 'flowchart LR'")
	}

	for _, id := range []string{"billing", "shipping", "audit", "gateway"} {
		if !strings.Contains(output, id) {
			t.Errorf("output should contain the %s component", id)
		}
	}

	// A solid arrow means the kind triggers the target.
	if !strings.Contains(output, "billing -->|invoice.issued| shipping") {
		t.Errorf("an input edge should be a solid arrow:\n%s", output)
	}

	// A dashed arrow means the target only folds the kind into state.
	if !strings.Contains(output, "billing -.->|invoice.issued| audit") {
		t.Errorf("a state-event edge should be a dashed arrow:\n%s", output)
	}
}

// A component folding its own output draws no self-loop: it is the normal
// idiom, so drawing it would put a loop on nearly every node.
func TestToMermaidSkipsSelfFold(t *testing.T) {
	billing := &eventmodel.Component{ID: "billing", Owns: "billing"}
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}
	billing.StateEvents = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{"billing": billing},
	}

	output := ToMermaid(eventmodel.BuildGraph(m))
	if strings.Contains(output, "billing -->") || strings.Contains(output, "billing -.->") {
		t.Errorf("a component folding its own output must draw no edge:\n%s", output)
	}
}

// A component both triggered by a kind and folding it is drawn once, as the
// trigger — the stronger of the two relations.
func TestToMermaidPrefersTriggerOverFold(t *testing.T) {
	producer := &eventmodel.Component{ID: "producer", Owns: "producer"}
	producer.Outputs = []eventmodel.Slot{{Kind: "producer.thing.happened"}}

	consumer := &eventmodel.Component{ID: "consumer", Owns: "consumer"}
	consumer.Inputs = []eventmodel.Slot{{Kind: "producer.thing.happened"}}
	consumer.StateEvents = []eventmodel.Slot{{Kind: "producer.thing.happened"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"producer": producer,
			"consumer": consumer,
		},
	}

	output := ToMermaid(eventmodel.BuildGraph(m))
	if strings.Count(output, "producer.thing.happened") > 0 {
		t.Errorf("edge labels are shortened, got:\n%s", output)
	}
	if n := strings.Count(output, "|thing.happened|"); n != 1 {
		t.Errorf("want exactly 1 edge for the kind, got %d:\n%s", n, output)
	}
	if !strings.Contains(output, "producer -->|thing.happened| consumer") {
		t.Errorf("the trigger relation should win:\n%s", output)
	}
}

func TestToMermaidWithHealth(t *testing.T) {
	// An output nobody observes.
	billing := &eventmodel.Component{
		ID:   "billing",
		Owns: "billing",
	}
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.orphan.event"}}

	// A component observing a different kind, so billing.orphan.event stays orphan.
	shipping := &eventmodel.Component{
		ID:   "shipping",
		Owns: "shipping",
	}
	shipping.Inputs = []eventmodel.Slot{{Kind: "billing.other.event"}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"shipping": shipping,
		},
	}

	g := eventmodel.BuildGraph(m)
	output := ToMermaid(g)

	// The orphan kind should NOT appear as an edge (no observer). The mermaid
	// output only shows edges between components, so we verify it still
	// generates valid output.
	if !strings.HasPrefix(output, "flowchart LR\n") {
		t.Error("output should start with 'flowchart LR'")
	}
}

func TestToMermaidWithSubgraphs(t *testing.T) {
	// Create enough components with distinct namespaces to trigger subgraphs.
	a := &eventmodel.Component{ID: "a", Owns: "ns_a"}
	a.Outputs = []eventmodel.Slot{{Kind: "ns_a.event"}}

	b := &eventmodel.Component{ID: "b", Owns: "ns_b"}
	b.Inputs = []eventmodel.Slot{{Kind: "ns_a.event"}}
	b.Outputs = []eventmodel.Slot{{Kind: "ns_b.event"}}

	c := &eventmodel.Component{ID: "c", Owns: "ns_c"}
	c.Inputs = []eventmodel.Slot{{Kind: "ns_b.event"}}

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
	billing.Outputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	ledger := &eventmodel.Component{ID: "ledger", Owns: "ledger"}
	ledger.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

	shipping := &eventmodel.Component{ID: "shipping", Owns: "shipping"}
	shipping.Inputs = []eventmodel.Slot{{Kind: "billing.invoice.issued"}}

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
