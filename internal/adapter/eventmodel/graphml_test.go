package eventmodel

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/eventmodel"
)

func TestToArchmotifGraph(t *testing.T) {
	// Build a simple event model graph.
	billing := &eventmodel.Component{
		ID:    "billing",
		Owns:  "billing",
		Types: map[string]eventmodel.SchemaNode{"Invoice": {Raw: map[string]any{"type": "object"}}},
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
	// Create an orphan fact (no consumer).
	billing := &eventmodel.Component{
		ID:   "billing",
		Owns: "billing",
	}
	billing.Emits = []eventmodel.Slot{{Kind: "billing.orphan.event", Role: eventmodel.RoleFact}}

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

// TestDottedFoldName verifies that fold names containing dots are handled
// correctly. The old string-splitting approach (LastIndex(".")) would parse
// "fold:billing.invoice.tracker" as component "billing.invoice" instead of
// "billing". The fix stores the component ID in the node's attributes.
func TestDottedFoldName(t *testing.T) {
	billing := &eventmodel.Component{
		ID:   "billing",
		Owns: "billing",
	}
	// Fold name contains a dot - this would break string-splitting.
	billing.Folds = []eventmodel.Fold{{Name: "invoice.tracker", Subjects: []string{"svc.*.billing.{account}.>"}, Consumes: []string{"billing.invoice.>"}}}
	billing.Emits = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}

	consumer := &eventmodel.Component{
		ID:   "consumer",
		Owns: "consumer",
	}
	consumer.Receives = []eventmodel.Slot{{Kind: "billing.invoice.issued", Role: eventmodel.RoleFact}}

	m := &eventmodel.Model{
		Components: map[string]*eventmodel.Component{
			"billing":  billing,
			"consumer": consumer,
		},
	}

	g := eventmodel.BuildGraph(m)

	// Verify the fold node has the correct component attribute.
	var foldNode eventmodel.Node
	for _, n := range g.Nodes {
		if n.Kind == eventmodel.NodeFold {
			foldNode = n
			break
		}
	}
	if foldNode.ID == "" {
		t.Fatal("fold node not found")
	}

	// The fold ID should be "fold:billing.invoice.tracker".
	expectedID := "fold:billing.invoice.tracker"
	if foldNode.ID != expectedID {
		t.Errorf("fold ID = %q, want %q", foldNode.ID, expectedID)
	}

	// The component attribute should be "billing" (not "billing.invoice").
	if foldNode.Attrs["component"] != "billing" {
		t.Errorf("fold component attr = %q, want %q", foldNode.Attrs["component"], "billing")
	}

	// Convert to archmotif - this would fail with the old approach.
	amg, err := ToArchmotifGraph(g)
	if err != nil {
		t.Fatalf("ToArchmotifGraph failed: %v", err)
	}
	if amg == nil {
		t.Fatal("returned graph is nil")
	}

	// Verify the fold is contained by the billing component.
	nodes := amg.Nodes()
	var foundFold bool
	for _, n := range nodes {
		if n.ID == expectedID {
			foundFold = true
			break
		}
	}
	if !foundFold {
		t.Error("fold node not found in archmotif graph")
	}
}
