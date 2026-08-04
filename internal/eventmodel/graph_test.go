package eventmodel

import (
	"testing"
)

func TestBuildGraphNodes(t *testing.T) {
	// Build a model with components, kinds, folds, and vocab.
	billing := comp("billing", "billing")
	billing.Vocab["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	billing.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
	billing.Folds = []Fold{{Name: "open-invoices", Pattern: "billing.invoice.>"}}

	shipping := comp("shipping", "shipping")
	shipping.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

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
	if nodesByKind[NodeFold] != 1 {
		t.Errorf("want 1 fold node, got %d", nodesByKind[NodeFold])
	}
	if nodesByKind[NodeType] != 1 {
		t.Errorf("want 1 type node, got %d", nodesByKind[NodeType])
	}

	// Check component node attributes.
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
}

func TestBuildGraphEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	billing.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
	// Use a specific pattern to match only facts.
	billing.Folds = []Fold{{Name: "self-fold", Pattern: "billing.invoice.issued"}}

	ledger := comp("ledger", "ledger")
	ledger.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	gateway := comp("gateway", "gateway")
	gateway.Emits = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}

	m := model(billing, ledger, gateway)
	g := BuildGraph(m)

	// Count edges by kind.
	edgesByKind := make(map[EdgeKind]int)
	for _, e := range g.Edges {
		edgesByKind[e.Kind]++
	}

	// billing emits billing.invoice.issued, gateway emits billing.invoice.issue
	if edgesByKind[EdgeEmits] != 2 {
		t.Errorf("want 2 emits edges, got %d", edgesByKind[EdgeEmits])
	}
	// billing receives billing.invoice.issue, ledger receives billing.invoice.issued
	if edgesByKind[EdgeReceives] != 2 {
		t.Errorf("want 2 receives edges, got %d", edgesByKind[EdgeReceives])
	}
	// billing.invoice.issued feeds billing.self-fold (exact pattern match)
	if edgesByKind[EdgeFeeds] != 1 {
		t.Errorf("want 1 feeds edge, got %d", edgesByKind[EdgeFeeds])
	}
	// billing.self-fold held-by billing
	if edgesByKind[EdgeHeldBy] != 1 {
		t.Errorf("want 1 held-by edge, got %d", edgesByKind[EdgeHeldBy])
	}

	// Verify edge attributes.
	for _, e := range g.Edges {
		if e.Kind == EdgeEmits {
			if e.Attrs["role"] == nil {
				t.Errorf("emits edge %s->%s missing role attribute", e.From, e.To)
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
			name: "ok: fact with producer and consumer",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
					return c
				}(),
				func() *Component {
					c := comp("shipping", "shipping")
					c.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOK,
		},
		{
			name: "orphan: fact with producer but no consumer",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOrphan,
		},
		{
			name: "starved: receives but no producer",
			model: model(
				func() *Component {
					c := comp("shipping", "shipping")
					c.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthStarved,
		},
		{
			name: "ambiguous: action with multiple receivers",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Emits = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
					return c
				}(),
				func() *Component {
					c := comp("billing1", "billing1")
					c.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
					return c
				}(),
				func() *Component {
					c := comp("billing2", "billing2")
					c.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthAmbiguous,
		},
		{
			name: "ok: fact consumed by fold",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
					return c
				}(),
				func() *Component {
					c := comp("analytics", "analytics")
					c.Folds = []Fold{{Name: "invoices", Pattern: "billing.>"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
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
	billing.Vocab["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Vocab["Line"] = SchemaNode{Raw: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"product": map[string]any{"$ref": "#/vocab/Invoice"},
		},
	}}
	billing.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/vocab/Invoice"}},
	}}

	shipping := comp("shipping", "shipping")
	shipping.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

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
	billing.Vocab["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Vocab["Line"] = SchemaNode{Raw: map[string]any{"type": "object"}}

	m := model(billing)
	g := BuildGraph(m)

	// Should have 2 vocab edges: component -> type
	var vocabEdges int
	for _, e := range g.Edges {
		if e.Kind == EdgeVocab && e.From == componentID("billing") {
			vocabEdges++
		}
	}
	if vocabEdges != 2 {
		t.Errorf("want 2 vocab edges, got %d", vocabEdges)
	}
}
