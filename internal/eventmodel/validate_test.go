package eventmodel

import (
	"strings"
	"testing"
)

// model builds a test Model from component specs.
func model(comps ...*Component) *Model {
	m := &Model{Components: make(map[string]*Component)}
	for _, c := range comps {
		m.Components[c.ID] = c
	}
	return m
}

// comp builds a minimal component with the given id and owns.
func comp(id, owns string) *Component {
	return &Component{
		ID:    id,
		Owns:  owns,
		Vocab: make(map[string]SchemaNode),
	}
}

func hasKind(fs []Finding, kind FindingKind) bool {
	for _, f := range fs {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func findingsByKind(fs []Finding, kind FindingKind) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// TestOwnershipMatrix tests all four cells of the role x ownership matrix.
func TestOwnershipMatrix(t *testing.T) {
	t.Run("emit fact in owns: ok", func(t *testing.T) {
		c := comp("billing", "billing")
		c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
		fs := Validate(model(c))
		if hasKind(fs, KindOwnershipViolation) {
			t.Errorf("unexpected ownership violation: %+v", fs)
		}
	})

	t.Run("emit fact outside owns: error", func(t *testing.T) {
		c := comp("billing", "billing")
		c.Emits = []Slot{{Kind: "ledger.entry.posted", Role: RoleFact}}
		fs := Validate(model(c))
		if !hasKind(fs, KindOwnershipViolation) {
			t.Error("want ownership violation for emitting fact outside owns")
		}
	})

	t.Run("emit fact with no owns: error", func(t *testing.T) {
		c := comp("gateway", "")
		c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
		fs := Validate(model(c))
		if !hasKind(fs, KindOwnershipViolation) {
			t.Error("want ownership violation for emitting fact without owns")
		}
	})

	t.Run("emit action in owns: ok (self-scheduling)", func(t *testing.T) {
		c := comp("billing", "billing")
		c.Emits = []Slot{{Kind: "billing.invoice.retry", Role: RoleAction}}
		c.Receives = []Slot{{Kind: "billing.invoice.retry", Role: RoleAction}}
		fs := Validate(model(c))
		if hasKind(fs, KindOwnershipViolation) {
			t.Errorf("unexpected ownership violation: %+v", fs)
		}
	})

	t.Run("emit action outside owns: ok (call-out)", func(t *testing.T) {
		billing := comp("billing", "billing")
		billing.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

		ledger := comp("ledger", "ledger")
		ledger.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

		fs := Validate(model(billing, ledger))
		if hasKind(fs, KindOwnershipViolation) {
			t.Errorf("unexpected ownership violation: %+v", fs)
		}
	})

	t.Run("receive action in owns: ok", func(t *testing.T) {
		c := comp("billing", "billing")
		c.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
		// Add an emitter to avoid starved receive warning.
		gateway := comp("gateway", "gateway")
		gateway.Emits = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
		fs := Validate(model(c, gateway))
		if hasKind(fs, KindOwnershipViolation) {
			t.Errorf("unexpected ownership violation: %+v", fs)
		}
	})

	t.Run("receive action outside owns: error", func(t *testing.T) {
		c := comp("billing", "billing")
		c.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}
		fs := Validate(model(c))
		if !hasKind(fs, KindOwnershipViolation) {
			t.Error("want ownership violation for receiving action outside owns")
		}
	})

	t.Run("receive action with no owns: error", func(t *testing.T) {
		c := comp("gateway", "")
		c.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
		fs := Validate(model(c))
		if !hasKind(fs, KindOwnershipViolation) {
			t.Error("want ownership violation for receiving action without owns")
		}
	})

	t.Run("receive fact in owns: ok (self-observation)", func(t *testing.T) {
		c := comp("billing", "billing")
		c.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
		c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
		fs := Validate(model(c))
		if hasKind(fs, KindOwnershipViolation) {
			t.Errorf("unexpected ownership violation: %+v", fs)
		}
	})

	t.Run("receive fact outside owns: ok (subscription)", func(t *testing.T) {
		billing := comp("billing", "billing")
		billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

		shipping := comp("shipping", "shipping")
		shipping.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

		fs := Validate(model(billing, shipping))
		if hasKind(fs, KindOwnershipViolation) {
			t.Errorf("unexpected ownership violation: %+v", fs)
		}
	})
}

func TestDuplicateOwner(t *testing.T) {
	a := comp("a", "billing")
	b := comp("b", "billing")

	fs := Validate(model(a, b))
	if !hasKind(fs, KindDuplicateOwner) {
		t.Error("want duplicate owner finding")
	}
}

func TestStarvedReceive(t *testing.T) {
	c := comp("billing", "billing")
	c.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
	// No emitter for this kind.

	fs := Validate(model(c))
	starved := findingsByKind(fs, KindStarvedReceive)
	if len(starved) != 1 {
		t.Fatalf("want 1 starved receive, got %d", len(starved))
	}
	if starved[0].Severity != SeverityWarning {
		t.Errorf("starved receive should be warning, got %s", starved[0].Severity)
	}
}

func TestStarvedFold(t *testing.T) {
	c := comp("billing", "billing")
	c.Folds = []Fold{{Name: "billing.test", Pattern: "billing.invoice.>"}}
	// No emitted kinds matching the pattern.

	fs := Validate(model(c))
	if !hasKind(fs, KindStarvedFold) {
		t.Error("want starved fold finding")
	}
}

func TestStarvedFoldSatisfied(t *testing.T) {
	c := comp("billing", "billing")
	c.Folds = []Fold{{Name: "billing.test", Pattern: "billing.invoice.>"}}
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	fs := Validate(model(c))
	if hasKind(fs, KindStarvedFold) {
		t.Error("fold should be satisfied by emitted kind")
	}
}

func TestOrphanFact(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	// No consumer.

	fs := Validate(model(c))
	if !hasKind(fs, KindOrphanFact) {
		t.Error("want orphan fact finding")
	}
}

func TestOrphanFactConsumedByReceive(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	shipping := comp("shipping", "shipping")
	shipping.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	fs := Validate(model(billing, shipping))
	if hasKind(fs, KindOrphanFact) {
		t.Error("fact should not be orphan when consumed by receive")
	}
}

func TestOrphanFactConsumedByFold(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	analytics := comp("analytics", "analytics")
	analytics.Folds = []Fold{{Name: "analytics.invoices", Pattern: "billing.>"}}

	fs := Validate(model(billing, analytics))
	if hasKind(fs, KindOrphanFact) {
		t.Error("fact should not be orphan when consumed by fold")
	}
}

func TestUnresolvedCall(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}
	// No receiver.

	fs := Validate(model(c))
	if !hasKind(fs, KindUnresolvedCall) {
		t.Error("want unresolved call finding")
	}
}

func TestAmbiguousCall(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	// Two receivers for the same action kind.
	ledger1 := comp("ledger1", "ledger")
	ledger1.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	ledger2 := comp("ledger2", "ledger2") // different owns to avoid duplicate owner
	ledger2.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	fs := Validate(model(billing, ledger1, ledger2))
	if !hasKind(fs, KindAmbiguousCall) {
		t.Error("want ambiguous call finding")
	}
}

func TestCallResolved(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	ledger := comp("ledger", "ledger")
	ledger.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	fs := Validate(model(billing, ledger))
	if hasKind(fs, KindUnresolvedCall) || hasKind(fs, KindAmbiguousCall) {
		t.Errorf("call should resolve: %+v", fs)
	}
}

func TestUnresolvedLocalRef(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/vocab/DoesNotExist"}},
	}}

	fs := Validate(model(c))
	if !hasKind(fs, KindUnresolvedRef) {
		t.Error("want unresolved ref finding")
	}
}

func TestResolvedLocalRef(t *testing.T) {
	c := comp("billing", "billing")
	c.Vocab["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/vocab/Invoice"}},
	}}

	fs := Validate(model(c))
	if hasKind(fs, KindUnresolvedRef) {
		t.Errorf("ref should resolve: %+v", fs)
	}
}

func TestUnresolvedCrossComponentRef(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "ledger#/vocab/Entry"}},
	}}

	ledger := comp("ledger", "ledger")
	// No Entry vocab.

	fs := Validate(model(billing, ledger))
	if !hasKind(fs, KindUnresolvedRef) {
		t.Error("want unresolved ref finding for cross-component ref")
	}
}

func TestResolvedCrossComponentRef(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "ledger#/vocab/Entry"}},
	}}

	ledger := comp("ledger", "ledger")
	ledger.Vocab["Entry"] = SchemaNode{Raw: map[string]any{"type": "object"}}

	fs := Validate(model(billing, ledger))
	if hasKind(fs, KindUnresolvedRef) {
		t.Errorf("cross-component ref should resolve: %+v", fs)
	}
}

func TestRefCycle(t *testing.T) {
	// a -> b -> c -> a
	a := comp("a", "a")
	a.Vocab["X"] = SchemaNode{Raw: map[string]any{"$ref": "b#/vocab/Y"}}

	b := comp("b", "b")
	b.Vocab["Y"] = SchemaNode{Raw: map[string]any{"$ref": "c#/vocab/Z"}}

	c := comp("c", "c")
	c.Vocab["Z"] = SchemaNode{Raw: map[string]any{"$ref": "a#/vocab/X"}}

	fs := Validate(model(a, b, c))
	if !hasKind(fs, KindRefCycle) {
		t.Error("want ref cycle finding")
	}
}

func TestRefCycleSelf(t *testing.T) {
	// Self-reference within a component is fine (not a cross-component cycle).
	c := comp("a", "a")
	c.Vocab["X"] = SchemaNode{Raw: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"child": map[string]any{"$ref": "#/vocab/X"},
		},
	}}

	fs := Validate(model(c))
	if hasKind(fs, KindRefCycle) {
		t.Error("self-reference should not be a cross-component cycle")
	}
}

func TestNestedSchemaRefs(t *testing.T) {
	c := comp("billing", "billing")
	c.Vocab["Line"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{
		Kind: "billing.invoice.issued",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"lines": map[string]any{
					"type":  "array",
					"items": map[string]any{"$ref": "#/vocab/Line"},
				},
			},
		}},
	}}

	fs := Validate(model(c))
	if hasKind(fs, KindUnresolvedRef) {
		t.Errorf("nested ref should resolve: %+v", fs)
	}
}

func TestOneOfRefs(t *testing.T) {
	c := comp("billing", "billing")
	c.Vocab["V1"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Vocab["V2"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{
		Kind: "billing.invoice.issued",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"oneOf": []any{
				map[string]any{"$ref": "#/vocab/V1"},
				map[string]any{"$ref": "#/vocab/V2"},
			},
		}},
	}}

	fs := Validate(model(c))
	if hasKind(fs, KindUnresolvedRef) {
		t.Errorf("oneOf refs should resolve: %+v", fs)
	}
}

func TestUnresolvedOneOfRef(t *testing.T) {
	c := comp("billing", "billing")
	c.Vocab["V1"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	// V2 is missing.
	c.Emits = []Slot{{
		Kind: "billing.invoice.issued",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"oneOf": []any{
				map[string]any{"$ref": "#/vocab/V1"},
				map[string]any{"$ref": "#/vocab/V2"},
			},
		}},
	}}

	fs := Validate(model(c))
	unresolvedRefs := findingsByKind(fs, KindUnresolvedRef)
	if len(unresolvedRefs) != 1 {
		t.Errorf("want 1 unresolved ref, got %d", len(unresolvedRefs))
	}
	if len(unresolvedRefs) > 0 && !strings.Contains(unresolvedRefs[0].Message, "V2") {
		t.Errorf("should mention V2: %s", unresolvedRefs[0].Message)
	}
}

func TestFindingSeverities(t *testing.T) {
	// Verify that errors sort before warnings.
	c := comp("billing", "billing")
	// Ownership violation (error).
	c.Emits = []Slot{{Kind: "ledger.entry.posted", Role: RoleFact}}
	// Starved receive (warning).
	c.Receives = []Slot{{Kind: "billing.foo", Role: RoleFact}}

	fs := Validate(model(c))
	if len(fs) < 2 {
		t.Fatalf("want at least 2 findings, got %d", len(fs))
	}
	// Errors should come first.
	if fs[0].Severity != SeverityError {
		t.Errorf("first finding should be error, got %s", fs[0].Severity)
	}
}
