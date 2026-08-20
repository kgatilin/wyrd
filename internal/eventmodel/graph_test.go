package eventmodel

import (
	"testing"
)

func TestBuildGraphNodes(t *testing.T) {
	// Build a model with components, kinds, and types.
	billing := comp("billing", "billing")
	billing.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Inputs = []Slot{{Kind: "billing.invoice.issue", Pattern: "svc.*.billing.{account}.invoice.issue"}}
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}
	billing.StateEvents = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}
	billing.PartitionKey = []string{"account"}

	shipping := comp("shipping", "shipping")
	shipping.Inputs = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}

	m := model(billing, shipping)
	g := BuildGraph(m)

	// Check node counts.
	nodesByKind := make(map[NodeKind]int)
	for _, n := range g.Nodes {
		nodesByKind[n.Kind]++
	}

	if nodesByKind[NodeComponent] != 2 {
		t.Errorf("want 2 component nodes, got %d", nodesByKind[NodeComponent])
	}
	// 2 kinds: billing.invoice.issue and billing.invoice.issued
	if nodesByKind[NodeEventKind] != 2 {
		t.Errorf("want 2 kind nodes, got %d", nodesByKind[NodeEventKind])
	}
	if nodesByKind[NodeType] != 1 {
		t.Errorf("want 1 type node, got %d", nodesByKind[NodeType])
	}

	// Check component node attributes. The fold is not a node of its own, so
	// the read-set and its key ride on the component that holds them.
	var billingNode Node
	for _, n := range g.Nodes {
		if n.ID == "component:billing" {
			billingNode = n
			break
		}
	}
	if billingNode.ID == "" {
		t.Fatal("billing component node not found")
	}
	if billingNode.Attrs["owns"] != "billing" {
		t.Errorf("billing owns = %v, want billing", billingNode.Attrs["owns"])
	}
	if got := billingNode.Attrs["partition_arity"]; got != 1 {
		t.Errorf("billing partition_arity = %v, want 1", got)
	}
	// Read-set is inputs + state events; the output-only pattern is the same
	// subject as the state event here, so the deduplicated set is 2.
	subjects, _ := billingNode.Attrs["subjects"].([]string)
	if len(subjects) != 2 {
		t.Errorf("billing subjects = %v, want 2 entries", subjects)
	}
}

func TestBuildGraphEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	billing.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}

	ledger := comp("ledger", "ledger")
	ledger.Inputs = []Slot{{Kind: "billing.invoice.issued"}}

	gateway := comp("gateway", "gateway")
	gateway.Outputs = []Slot{{Kind: "billing.invoice.issue"}}

	m := model(billing, ledger, gateway)
	g := BuildGraph(m)

	edgesByKind := make(map[EdgeKind]int)
	for _, e := range g.Edges {
		edgesByKind[e.Kind]++
	}

	// billing outputs billing.invoice.issued, gateway outputs billing.invoice.issue
	if edgesByKind[EdgeOutput] != 2 {
		t.Errorf("want 2 output edges, got %d", edgesByKind[EdgeOutput])
	}
	// billing is triggered by billing.invoice.issue, ledger by billing.invoice.issued
	if edgesByKind[EdgeInput] != 2 {
		t.Errorf("want 2 input edges, got %d", edgesByKind[EdgeInput])
	}
	// billing folds its own output.
	if edgesByKind[EdgeStateEvent] != 1 {
		t.Errorf("want 1 state-event edge, got %d", edgesByKind[EdgeStateEvent])
	}

	// Direction: outputs point away from the component, the other two into it.
	for _, e := range g.Edges {
		switch e.Kind {
		case EdgeOutput:
			if e.From != componentID("billing") && e.From != componentID("gateway") {
				t.Errorf("output edge should start at a component, got %s", e.From)
			}
		case EdgeInput, EdgeStateEvent:
			if e.To != componentID("billing") && e.To != componentID("ledger") {
				t.Errorf("%s edge should end at a component, got %s", e.Kind, e.To)
			}
		}
	}
}

func TestBuildGraphHealth(t *testing.T) {
	tests := []struct {
		name       string
		model      *Model
		kind       string
		wantHealth Health
	}{
		{
			name: "ok: output with a consumer",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
				func() *Component {
					c := comp("shipping", "shipping")
					c.Inputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOK,
		},
		{
			name: "orphan: output nobody observes",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOrphan,
		},
		{
			name: "starved: input with no producer",
			model: model(
				func() *Component {
					c := comp("shipping", "shipping")
					c.Inputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthStarved,
		},
		{
			// Without the exclusive opt-in, many consumers is the healthy
			// event-sourced default, not an ambiguity.
			name: "ok: broadcast kind with multiple consumers",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Outputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("billing1", "billing1")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("billing2", "billing2")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthOK,
		},
		{
			name: "ambiguous: exclusive kind with multiple consumers",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Outputs = []Slot{{Kind: "billing.invoice.issue", Delivery: DeliveryExclusive}}
					return c
				}(),
				func() *Component {
					c := comp("billing1", "billing1")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("billing2", "billing2")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthAmbiguous,
		},
		{
			// Folding is observation: a kind nobody is triggered by, but that
			// some component keeps in state, is not an orphan.
			name: "ok: output observed only as a state event",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
				func() *Component {
					c := comp("analytics", "analytics")
					c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOK,
		},
		{
			// Exclusive cardinality counts inputs alone: any number of
			// components may fold the kind without competing to handle it.
			name: "ok: exclusive kind with one consumer and extra folders",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Outputs = []Slot{{Kind: "billing.invoice.issue", Delivery: DeliveryExclusive}}
					return c
				}(),
				func() *Component {
					c := comp("billing", "billing")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("audit", "audit")
					c.StateEvents = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := BuildGraph(tc.model)
			var found bool
			for _, n := range g.Nodes {
				if n.ID == kindID(tc.kind) {
					found = true
					gotHealth := Health(n.Attrs["health"].(string))
					if gotHealth != tc.wantHealth {
						t.Errorf("health = %q, want %q", gotHealth, tc.wantHealth)
					}
					break
				}
			}
			if !found {
				t.Errorf("kind node %q not found", tc.kind)
			}
		})
	}
}

func TestBuildGraphPayloadAndRefEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Types["Line"] = SchemaNode{Raw: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"product": map[string]any{"$ref": "#/types/Invoice"},
		},
	}}
	billing.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/Invoice"}},
	}}

	shipping := comp("shipping", "shipping")
	shipping.Inputs = []Slot{{Kind: "billing.invoice.issued"}}

	m := model(billing, shipping)
	g := BuildGraph(m)

	// Check for payload edge: kind --payload--> type
	var foundPayload bool
	for _, e := range g.Edges {
		if e.Kind == EdgePayload && e.From == kindID("billing.invoice.issued") {
			foundPayload = true
			if e.To != typeID("billing", "Invoice") {
				t.Errorf("payload edge to = %q, want %q", e.To, typeID("billing", "Invoice"))
			}
		}
	}
	if !foundPayload {
		t.Error("payload edge not found")
	}

	// Check for refs edge: type --refs--> type (Line refs Invoice)
	var foundRefs bool
	for _, e := range g.Edges {
		if e.Kind == EdgeRefs && e.From == typeID("billing", "Line") {
			foundRefs = true
			if e.To != typeID("billing", "Invoice") {
				t.Errorf("refs edge to = %q, want %q", e.To, typeID("billing", "Invoice"))
			}
		}
	}
	if !foundRefs {
		t.Error("refs edge not found")
	}
}

func TestBuildGraphVocabEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Types["Line"] = SchemaNode{Raw: map[string]any{"type": "object"}}

	m := model(billing)
	g := BuildGraph(m)

	// Should have 2 types edges: component -> type
	var typesEdges int
	for _, e := range g.Edges {
		if e.Kind == EdgeDefines && e.From == componentID("billing") {
			typesEdges++
		}
	}
	if typesEdges != 2 {
		t.Errorf("want 2 types edges, got %d", typesEdges)
	}
}

// TestBuildGraphPatternConflict: where declarations disagree about a kind's
// address the projection must pick deterministically and say so, not silently
// expose whichever pattern the map iteration happened to land on.
func TestBuildGraphPatternConflict(t *testing.T) {
	producer := comp("llm", "llm")
	producer.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}

	consumer := comp("router", "router")
	consumer.Inputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.router.{session}.message"}}

	for i := 0; i < 20; i++ {
		g := BuildGraph(model(producer, consumer))
		var node *Node
		for j := range g.Nodes {
			if g.Nodes[j].ID == kindID("llm.message") {
				node = &g.Nodes[j]
				break
			}
		}
		if node == nil {
			t.Fatal("kind node not found")
		}
		// "llm" sorts before "router", so its declaration is canonical.
		if node.Attrs["pattern"] != "svc.*.llm.{session}.message" {
			t.Fatalf("pattern = %v, want a stable llm-side pattern", node.Attrs["pattern"])
		}
		if node.Attrs["pattern_conflict"] != true {
			t.Fatalf("pattern_conflict should be set, attrs = %v", node.Attrs)
		}
	}
}

func TestBuildGraphNoPatternConflictFlagWhenConsistent(t *testing.T) {
	c := comp("llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}
	c.StateEvents = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}

	g := BuildGraph(model(c))
	for _, n := range g.Nodes {
		if n.ID == kindID("llm.message") {
			if _, ok := n.Attrs["pattern_conflict"]; ok {
				t.Errorf("pattern_conflict must be absent when declarations agree: %v", n.Attrs)
			}
			return
		}
	}
	t.Fatal("kind node not found")
}
