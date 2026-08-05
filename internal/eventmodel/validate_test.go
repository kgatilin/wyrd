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
		Types: make(map[string]SchemaNode),
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

// TestOwnershipIsNotProductionControl pins the corrected semantics: `owns`
// declares who defines a namespace's schemas, not who may emit into it or who
// may observe it. Every cell of the old role x ownership matrix is now legal.
func TestOwnershipIsNotProductionControl(t *testing.T) {
	cases := []struct {
		name  string
		build func() *Model
	}{
		{"emit fact in owns", func() *Model {
			c := comp("billing", "billing")
			c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
			return model(c)
		}},
		{"emit fact outside owns", func() *Model {
			c := comp("billing", "billing")
			c.Emits = []Slot{{Kind: "ledger.entry.posted", Role: RoleFact}}
			return model(c)
		}},
		{"emit fact with no owns", func() *Model {
			c := comp("gateway", "")
			c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
			return model(c)
		}},
		{"emit action in owns", func() *Model {
			c := comp("billing", "billing")
			c.Emits = []Slot{{Kind: "billing.invoice.retry", Role: RoleAction}}
			c.Receives = []Slot{{Kind: "billing.invoice.retry", Role: RoleAction}}
			return model(c)
		}},
		{"receive action outside owns", func() *Model {
			c := comp("billing", "billing")
			c.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}
			return model(c)
		}},
		{"receive action with no owns", func() *Model {
			c := comp("gateway", "")
			c.Receives = []Slot{{Kind: "billing.invoice.issue", Role: RoleAction}}
			return model(c)
		}},
		{"receive fact outside owns", func() *Model {
			billing := comp("billing", "billing")
			billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
			shipping := comp("shipping", "shipping")
			shipping.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
			return model(billing, shipping)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, f := range Validate(tc.build()) {
				if f.Severity == SeverityError {
					t.Errorf("ownership must not produce errors, got %s: %s", f.Kind, f.Message)
				}
			}
		})
	}
}

// TestBroadcastFanOutIsNotAFinding is the core regression guard: several
// components and several folds observing one durable event is the normal
// event-sourced case, not an ambiguity.
func TestBroadcastFanOutIsNotAFinding(t *testing.T) {
	producer := comp("orchestrator", "orchestrator")
	producer.Emits = []Slot{{Kind: "orchestrator.task.run", Role: RoleAction}}

	controllerA := comp("controller-a", "controller-a")
	controllerA.Receives = []Slot{{Kind: "orchestrator.task.run", Role: RoleAction}}

	controllerB := comp("controller-b", "controller-b")
	controllerB.Receives = []Slot{{Kind: "orchestrator.task.run", Role: RoleAction}}

	projection := comp("projection", "projection")
	projection.Folds = []Fold{{
		Name:     "projection.tasks",
		Subjects: []string{"svc.*.orchestrator.{task}.>"},
		Consumes: []string{"orchestrator.task.*"},
	}}

	fs := Validate(model(producer, controllerA, controllerB, projection))
	for _, f := range fs {
		if f.Severity == SeverityError {
			t.Errorf("multi-observer choreography must not error, got %s: %s", f.Kind, f.Message)
		}
	}
}

// TestMultipleFoldsConsumeOneKind: stateful observation is equally unbounded.
func TestMultipleFoldsConsumeOneKind(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	billing.Folds = []Fold{{
		Name:     "billing.open-invoices",
		Subjects: []string{"svc.*.billing.{account}.>"},
		Consumes: []string{"billing.invoice.*"},
	}}

	analytics := comp("analytics", "analytics")
	analytics.Folds = []Fold{{
		Name:     "analytics.revenue",
		Subjects: []string{"svc.*.analytics.{tenant}.>"},
		Consumes: []string{"billing.invoice.*"},
	}}

	fs := Validate(model(billing, analytics))
	if len(fs) != 0 {
		t.Errorf("two folds over one kind must be clean, got %+v", fs)
	}
}

func TestDuplicateOwner(t *testing.T) {
	a := comp("a", "billing")
	b := comp("b", "billing")

	fs := Validate(model(a, b))
	if !hasKind(fs, KindDuplicateOwner) {
		t.Error("want duplicate owner finding")
	}
}

func TestNestedOwnershipPrefix(t *testing.T) {
	// billing and billing.invoice are nested prefixes => error
	a := comp("a", "billing")
	b := comp("b", "billing.invoice")

	fs := Validate(model(a, b))
	dupes := findingsByKind(fs, KindDuplicateOwner)
	if len(dupes) != 1 {
		t.Fatalf("want 1 duplicate owner finding for nested prefix, got %d: %+v", len(dupes), dupes)
	}
	// The error should mention nesting.
	if dupes[0].Related["parent_prefix"] != "billing" {
		t.Errorf("expected parent_prefix=billing, got %v", dupes[0].Related["parent_prefix"])
	}
}

func TestOwnerOfLongestPrefixWins(t *testing.T) {
	broad := comp("broad", "billing")
	narrow := comp("narrow", "billing.invoice.credit")

	m := model(broad, narrow)
	cases := []struct{ kind, want string }{
		{"billing.invoice.issued", "broad"},
		{"billing.invoice.credit.applied", "narrow"},
		{"ledger.entry.posted", ""},
	}
	for _, tc := range cases {
		if got := OwnerOf(m, tc.kind); got != tc.want {
			t.Errorf("OwnerOf(%q) = %q, want %q", tc.kind, got, tc.want)
		}
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
	c.Folds = []Fold{{Name: "billing.test", Subjects: []string{"svc.*.billing.{account}.>"}, Consumes: []string{"billing.invoice.>"}}}
	// No emitted kinds matching the consumes.

	fs := Validate(model(c))
	if !hasKind(fs, KindStarvedFold) {
		t.Error("want starved fold finding")
	}
}

func TestStarvedFoldSatisfied(t *testing.T) {
	c := comp("billing", "billing")
	c.Folds = []Fold{{Name: "billing.test", Subjects: []string{"svc.*.billing.{account}.>"}, Consumes: []string{"billing.invoice.>"}}}
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	fs := Validate(model(c))
	if hasKind(fs, KindStarvedFold) {
		t.Error("fold should be satisfied by emitted kind")
	}
}

func TestOrphanEvent(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	// No consumer.

	fs := Validate(model(c))
	if !hasKind(fs, KindOrphanEvent) {
		t.Error("want orphan event finding")
	}
}

func TestOrphanEventConsumedByReceive(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	shipping := comp("shipping", "shipping")
	shipping.Receives = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	fs := Validate(model(billing, shipping))
	if hasKind(fs, KindOrphanEvent) {
		t.Error("event should not be orphan when consumed by receive")
	}
}

func TestOrphanEventConsumedByFold(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	analytics := comp("analytics", "analytics")
	analytics.Folds = []Fold{{Name: "analytics.invoices", Subjects: []string{"svc.*.analytics.>"}, Consumes: []string{"billing.>"}}}

	fs := Validate(model(billing, analytics))
	if hasKind(fs, KindOrphanEvent) {
		t.Error("event should not be orphan when consumed by fold")
	}
}

// TestBroadcastActionWithNoReceiverIsWarning: without the exclusive opt-in an
// unobserved action is a closure warning, exactly like an unobserved fact.
func TestBroadcastActionWithNoReceiverIsWarning(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	fs := Validate(model(c))
	orphans := findingsByKind(fs, KindOrphanEvent)
	if len(orphans) != 1 {
		t.Fatalf("want 1 orphan-event finding, got %d: %+v", len(orphans), fs)
	}
	if orphans[0].Severity != SeverityWarning {
		t.Errorf("orphan event should be a warning, got %s", orphans[0].Severity)
	}
	if hasKind(fs, KindExclusiveUnhandled) {
		t.Error("exclusive rules must not fire without the delivery opt-in")
	}
}

func TestExclusiveUnhandled(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction, Delivery: DeliveryExclusive}}

	fs := Validate(model(c))
	found := findingsByKind(fs, KindExclusiveUnhandled)
	if len(found) != 1 {
		t.Fatalf("want 1 exclusive-unhandled finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityError {
		t.Errorf("exclusive-unhandled should be an error, got %s", found[0].Severity)
	}
}

func TestExclusiveConflict(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	// Exclusivity declared on the receiving side propagates to the kind.
	ledger1 := comp("ledger1", "ledger")
	ledger1.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction, Delivery: DeliveryExclusive}}

	ledger2 := comp("ledger2", "ledger2")
	ledger2.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction}}

	fs := Validate(model(billing, ledger1, ledger2))
	found := findingsByKind(fs, KindExclusiveConflict)
	if len(found) != 1 {
		t.Fatalf("want 1 exclusive-conflict finding, got %d: %+v", len(found), fs)
	}
	if !strings.Contains(found[0].Message, "ledger1") || !strings.Contains(found[0].Message, "ledger2") {
		t.Errorf("conflict should name both receivers: %s", found[0].Message)
	}
}

func TestExclusiveSatisfied(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Emits = []Slot{{Kind: "ledger.entry.post", Role: RoleAction, Delivery: DeliveryExclusive}}

	ledger := comp("ledger", "ledger")
	ledger.Receives = []Slot{{Kind: "ledger.entry.post", Role: RoleAction, Delivery: DeliveryExclusive}}

	fs := Validate(model(billing, ledger))
	if hasKind(fs, KindExclusiveUnhandled) || hasKind(fs, KindExclusiveConflict) {
		t.Errorf("exclusive contract should be satisfied: %+v", fs)
	}
}

func TestUnresolvedLocalRef(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/DoesNotExist"}},
	}}

	fs := Validate(model(c))
	if !hasKind(fs, KindUnresolvedRef) {
		t.Error("want unresolved ref finding")
	}
}

func TestResolvedLocalRef(t *testing.T) {
	c := comp("billing", "billing")
	c.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/Invoice"}},
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
		Schema: SchemaNode{Raw: map[string]any{"$ref": "ledger#/types/Entry"}},
	}}

	ledger := comp("ledger", "ledger")
	// No Entry types.

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
		Schema: SchemaNode{Raw: map[string]any{"$ref": "ledger#/types/Entry"}},
	}}

	ledger := comp("ledger", "ledger")
	ledger.Types["Entry"] = SchemaNode{Raw: map[string]any{"type": "object"}}

	fs := Validate(model(billing, ledger))
	if hasKind(fs, KindUnresolvedRef) {
		t.Errorf("cross-component ref should resolve: %+v", fs)
	}
}

func TestRefCycle(t *testing.T) {
	// a -> b -> c -> a
	a := comp("a", "a")
	a.Types["X"] = SchemaNode{Raw: map[string]any{"$ref": "b#/types/Y"}}

	b := comp("b", "b")
	b.Types["Y"] = SchemaNode{Raw: map[string]any{"$ref": "c#/types/Z"}}

	c := comp("c", "c")
	c.Types["Z"] = SchemaNode{Raw: map[string]any{"$ref": "a#/types/X"}}

	fs := Validate(model(a, b, c))
	if !hasKind(fs, KindRefCycle) {
		t.Error("want ref cycle finding")
	}
}

func TestRefCycleSelf(t *testing.T) {
	// Self-reference within a component is fine (not a cross-component cycle).
	c := comp("a", "a")
	c.Types["X"] = SchemaNode{Raw: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"child": map[string]any{"$ref": "#/types/X"},
		},
	}}

	fs := Validate(model(c))
	if hasKind(fs, KindRefCycle) {
		t.Error("self-reference should not be a cross-component cycle")
	}
}

func TestNestedSchemaRefs(t *testing.T) {
	c := comp("billing", "billing")
	c.Types["Line"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{
		Kind: "billing.invoice.issued",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"lines": map[string]any{
					"type":  "array",
					"items": map[string]any{"$ref": "#/types/Line"},
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
	c.Types["V1"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Types["V2"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{
		Kind: "billing.invoice.issued",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"oneOf": []any{
				map[string]any{"$ref": "#/types/V1"},
				map[string]any{"$ref": "#/types/V2"},
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
	c.Types["V1"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	// V2 is missing.
	c.Emits = []Slot{{
		Kind: "billing.invoice.issued",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"oneOf": []any{
				map[string]any{"$ref": "#/types/V1"},
				map[string]any{"$ref": "#/types/V2"},
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

func TestMalformedSlotSyntax(t *testing.T) {
	c := comp("billing", "billing")
	c.Folds = []Fold{{
		Name:     "billing.bad-subject",
		Subjects: []string{"svc.*.billing.{unclosed.>"},
		Consumes: []string{"billing.*"},
	}}
	c.Emits = []Slot{{Kind: "billing.foo", Role: RoleFact}}

	fs := Validate(model(c))
	if !hasKind(fs, KindMalformedSlot) {
		t.Error("want malformed-slot finding for unclosed brace")
	}
}

func TestMalformedSlotEmptySlot(t *testing.T) {
	c := comp("billing", "billing")
	c.Folds = []Fold{{
		Name:     "billing.empty-slot",
		Subjects: []string{"svc.*.billing.{}.invoice.>"},
		Consumes: []string{"billing.*"},
	}}
	c.Emits = []Slot{{Kind: "billing.foo", Role: RoleFact}}

	fs := Validate(model(c))
	if !hasKind(fs, KindMalformedSlot) {
		t.Error("want malformed-slot finding for empty slot")
	}
}

func TestStarvedFoldPerEntry(t *testing.T) {
	// A fold with multiple consumes entries, where only one is starved.
	c := comp("billing", "billing")
	c.Folds = []Fold{{
		Name:     "billing.multi",
		Subjects: []string{"svc.*.billing.{account}.>"},
		Consumes: []string{"billing.invoice.*", "nonexistent.events.*"},
	}}
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	fs := Validate(model(c))
	starved := findingsByKind(fs, KindStarvedFold)
	if len(starved) != 1 {
		t.Fatalf("want 1 starved-fold finding for the unmatched consumes entry, got %d", len(starved))
	}
	if !strings.Contains(starved[0].Message, "nonexistent.events.*") {
		t.Errorf("starved-fold message should mention the unmatched consumes entry: %s", starved[0].Message)
	}
}

func TestSubjectNotMatchedAgainstKinds(t *testing.T) {
	// Regression test: the subject pattern should not be matched against kinds.
	// This fold's subject is a transport pattern that would match nothing in
	// the kind alphabet, but its consumes entry does match.
	c := comp("billing", "billing")
	c.Folds = []Fold{{
		Name:     "billing.transport-subject",
		Subjects: []string{"svc.*.billing.{account}.invoice.>"},
		Consumes: []string{"billing.invoice.*"},
	}}
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}

	fs := Validate(model(c))
	if hasKind(fs, KindStarvedFold) {
		t.Error("fold should NOT be starved: consumes matches the emitted kind")
	}
}

func TestFindingSeverities(t *testing.T) {
	// Verify that errors sort before warnings.
	c := comp("billing", "billing")
	// Unresolved $ref (error).
	c.Emits = []Slot{{
		Kind:   "billing.invoice.issued",
		Role:   RoleFact,
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/Missing"}},
	}}
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

func TestPartitionMismatch(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	c.Folds = []Fold{{
		Name: "billing.mixed",
		Subjects: []string{
			"svc.*.billing.{account}.invoice.>",
			"svc.*.billing.{region}.invoice.>",
		},
		PartitionKey: []string{"account"},
		Consumes:     []string{"billing.invoice.*"},
	}}

	fs := Validate(model(c))
	found := findingsByKind(fs, KindPartitionMismatch)
	if len(found) != 1 {
		t.Fatalf("want 1 partition-mismatch finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityError {
		t.Errorf("partition-mismatch should be an error, got %s", found[0].Severity)
	}
}

func TestPartitionMismatchOrderMatters(t *testing.T) {
	c := comp("warehouse", "warehouse")
	c.Emits = []Slot{{Kind: "warehouse.stock.adjusted", Role: RoleFact}}
	c.Folds = []Fold{{
		Name: "warehouse.levels",
		Subjects: []string{
			"svc.*.warehouse.{region}.{sku}.stock.>",
			"svc.*.warehouse.{sku}.{region}.stock.>",
		},
		PartitionKey: []string{"region", "sku"},
		Consumes:     []string{"warehouse.stock.*"},
	}}

	if !hasKind(Validate(model(c)), KindPartitionMismatch) {
		t.Error("same slot names in a different order are a different partition key")
	}
}

func TestPartitionKeyShared(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	c.Folds = []Fold{{
		Name: "billing.open",
		Subjects: []string{
			"svc.*.billing.{account}.invoice.>",
			"svc.*.billing.{account}.credit.>",
		},
		PartitionKey: []string{"account"},
		Consumes:     []string{"billing.invoice.*"},
	}}

	if hasKind(Validate(model(c)), KindPartitionMismatch) {
		t.Error("subjects sharing one ordered key must not be flagged")
	}
}

func TestUnderspecifiedFoldState(t *testing.T) {
	c := comp("billing", "billing")
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	c.Folds = []Fold{{
		Name:     "billing.placeholder",
		Subjects: []string{"svc.*.billing.{account}.>"},
		Consumes: []string{"billing.invoice.*"},
		State:    SchemaNode{Raw: map[string]any{"type": "object"}},
	}}

	fs := Validate(model(c))
	found := findingsByKind(fs, KindUnderspecifiedState)
	if len(found) != 1 {
		t.Fatalf("want 1 underspecified-state finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityWarning {
		t.Errorf("underspecified-state should be a warning, got %s", found[0].Severity)
	}
}

func TestSpecifiedFoldState(t *testing.T) {
	c := comp("billing", "billing")
	c.Types["Open"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Emits = []Slot{{Kind: "billing.invoice.issued", Role: RoleFact}}
	c.Folds = []Fold{
		{
			Name:     "billing.by-ref",
			Subjects: []string{"svc.*.billing.{account}.>"},
			Consumes: []string{"billing.invoice.*"},
			State:    SchemaNode{Raw: map[string]any{"$ref": "#/types/Open"}},
		},
		{
			Name:     "billing.inline",
			Subjects: []string{"svc.*.billing.{account}.>"},
			Consumes: []string{"billing.invoice.*"},
			State: SchemaNode{Raw: map[string]any{
				"type":       "object",
				"properties": map[string]any{"Count": map[string]any{"type": "integer"}},
			}},
		},
	}

	if hasKind(Validate(model(c)), KindUnderspecifiedState) {
		t.Error("a state with a $ref or properties must not be flagged")
	}
}

// TestKindRoleConflictAcrossComponents: role is a property of the kind, so two
// components cannot read the same kind differently.
func TestKindRoleConflictAcrossComponents(t *testing.T) {
	producer := comp("llm", "llm")
	producer.Emits = []Slot{{Kind: "llm.message", Role: RoleFact}}

	consumer := comp("router", "router")
	consumer.Receives = []Slot{{Kind: "llm.message", Role: RoleAction}}

	fs := Validate(model(producer, consumer))
	found := findingsByKind(fs, KindRoleConflict)
	if len(found) != 1 {
		t.Fatalf("want 1 kind-role-conflict finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityError {
		t.Errorf("kind-role-conflict should be an error, got %s", found[0].Severity)
	}
	for _, want := range []string{"llm.message", "action", "fact", "llm:emits", "router:receives"} {
		if !strings.Contains(found[0].Message, want) {
			t.Errorf("message should mention %q: %s", want, found[0].Message)
		}
	}
}

// TestKindRoleConflictWithinComponent: the common shape — one component both
// receives a kind as an action and emits it as a fact.
func TestKindRoleConflictWithinComponent(t *testing.T) {
	c := comp("llm", "llm")
	c.Receives = []Slot{{Kind: "llm.message", Role: RoleAction}}
	c.Emits = []Slot{{Kind: "llm.message", Role: RoleFact}}

	if !hasKind(Validate(model(c)), KindRoleConflict) {
		t.Error("want kind-role-conflict when one component declares both roles")
	}
}

// TestKindRoleConsistent: the same role on every side is clean, and repeating a
// kind across many producers and observers is not itself a conflict.
func TestKindRoleConsistent(t *testing.T) {
	a := comp("llm", "llm")
	a.Emits = []Slot{{Kind: "llm.message", Role: RoleFact}}

	b := comp("mirror", "mirror")
	b.Emits = []Slot{{Kind: "llm.message", Role: RoleFact}}

	c := comp("router", "router")
	c.Receives = []Slot{{Kind: "llm.message", Role: RoleFact}}

	d := comp("audit", "audit")
	d.Receives = []Slot{{Kind: "llm.message", Role: RoleFact}}

	if hasKind(Validate(model(a, b, c, d)), KindRoleConflict) {
		t.Error("agreeing declarations must not conflict")
	}
}

// TestKindRoleConflictIsPerKind: two conflicting kinds produce two findings,
// not one per declaration site.
func TestKindRoleConflictIsPerKind(t *testing.T) {
	c := comp("llm", "llm")
	c.Receives = []Slot{
		{Kind: "llm.message", Role: RoleAction},
		{Kind: "llm.tool", Role: RoleAction},
	}
	c.Emits = []Slot{
		{Kind: "llm.message", Role: RoleFact},
		{Kind: "llm.tool", Role: RoleFact},
	}

	found := findingsByKind(Validate(model(c)), KindRoleConflict)
	if len(found) != 2 {
		t.Fatalf("want 1 finding per conflicting kind, got %d", len(found))
	}
	if found[0].Location != "llm.message" || found[1].Location != "llm.tool" {
		t.Errorf("findings should be sorted by kind, got %q and %q", found[0].Location, found[1].Location)
	}
}

// TestPayloadVariantsDoNotChangeRole: alternative payload shapes (oneOf, a
// deprecated legacy branch) are schema evolution, not a role change.
func TestPayloadVariantsDoNotChangeRole(t *testing.T) {
	c := comp("llm", "llm")
	c.Types["Text"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	c.Types["Legacy"] = SchemaNode{Raw: map[string]any{"type": "string", "deprecated": true}}
	c.Emits = []Slot{{
		Kind: "llm.message",
		Role: RoleFact,
		Schema: SchemaNode{Raw: map[string]any{
			"oneOf": []any{
				map[string]any{"$ref": "#/types/Text"},
				map[string]any{"$ref": "#/types/Legacy"},
			},
		}},
	}}
	c.Receives = []Slot{{Kind: "llm.message", Role: RoleFact}}

	if hasKind(Validate(model(c)), KindRoleConflict) {
		t.Error("payload variants must not be read as a role change")
	}
}

// TestSplitKindsResolveRoleConflict documents the prescribed fix: separate
// kinds for the intent and the outcome.
func TestSplitKindsResolveRoleConflict(t *testing.T) {
	c := comp("llm", "llm")
	c.Receives = []Slot{{Kind: "llm.message.send", Role: RoleAction}}
	c.Emits = []Slot{
		{Kind: "llm.message.send", Role: RoleAction},
		{Kind: "llm.message.sent", Role: RoleFact},
	}
	c.Folds = []Fold{{
		Name:     "llm.transcript",
		Subjects: []string{"svc.*.llm.{session}.message.>"},
		Consumes: []string{"llm.message.*"},
		State: SchemaNode{Raw: map[string]any{
			"type":       "object",
			"properties": map[string]any{"Turns": map[string]any{"type": "integer"}},
		}},
	}}

	fs := Validate(model(c))
	if len(fs) != 0 {
		t.Errorf("split kinds should validate clean, got %+v", fs)
	}
}
