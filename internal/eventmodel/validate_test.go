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
			c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
			return model(c)
		}},
		{"emit fact outside owns", func() *Model {
			c := comp("billing", "billing")
			c.Outputs = []Slot{{Kind: "ledger.entry.posted"}}
			return model(c)
		}},
		{"emit fact with no owns", func() *Model {
			c := comp("gateway", "")
			c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
			return model(c)
		}},
		{"emit action in owns", func() *Model {
			c := comp("billing", "billing")
			c.Outputs = []Slot{{Kind: "billing.invoice.retry"}}
			// The observer is a separate component: a component never
			// receives its own emission (see self-receive-conflict).
			worker := comp("retry-worker", "retry-worker")
			worker.Inputs = []Slot{{Kind: "billing.invoice.retry"}}
			return model(c, worker)
		}},
		{"receive action outside owns", func() *Model {
			c := comp("billing", "billing")
			c.Inputs = []Slot{{Kind: "ledger.entry.post"}}
			return model(c)
		}},
		{"receive action with no owns", func() *Model {
			c := comp("gateway", "")
			c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
			return model(c)
		}},
		{"receive fact outside owns", func() *Model {
			billing := comp("billing", "billing")
			billing.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
			shipping := comp("shipping", "shipping")
			shipping.Inputs = []Slot{{Kind: "billing.invoice.issued"}}
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
	producer.Outputs = []Slot{{Kind: "orchestrator.task.run", Pattern: "svc.*.orchestrator.{task}.run"}}

	controllerA := comp("controller-a", "controller-a")
	controllerA.Inputs = []Slot{{Kind: "orchestrator.task.run", Pattern: "svc.*.orchestrator.{task}.run"}}

	controllerB := comp("controller-b", "controller-b")
	controllerB.Inputs = []Slot{{Kind: "orchestrator.task.run", Pattern: "svc.*.orchestrator.{task}.run"}}

	projection := comp("projection", "projection")
	projection.StateEvents = []Slot{{Kind: "orchestrator.task.run", Pattern: "svc.*.orchestrator.{task}.run"}}

	fs := Validate(model(producer, controllerA, controllerB, projection))
	for _, f := range fs {
		if f.Severity == SeverityError {
			t.Errorf("multi-observer choreography must not error, got %s: %s", f.Kind, f.Message)
		}
	}
}

// TestMultipleComponentsFoldOneKind: stateful observation is equally unbounded.
// A component folding its own output is the normal idiom, and another
// component folding the same kind is not competition.
func TestMultipleComponentsFoldOneKind(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}
	billing.StateEvents = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}

	analytics := comp("analytics", "analytics")
	analytics.StateEvents = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}

	fs := Validate(model(billing, analytics))
	if len(fs) != 0 {
		t.Errorf("two components folding one kind must be clean, got %+v", fs)
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

func TestStarvedInput(t *testing.T) {
	c := comp("billing", "billing")
	c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
	// Nothing outputs this kind, so the trigger never fires.

	fs := Validate(model(c))
	starved := findingsByKind(fs, KindStarvedInput)
	if len(starved) != 1 {
		t.Fatalf("want 1 starved input, got %d", len(starved))
	}
	if starved[0].Severity != SeverityWarning {
		t.Errorf("starved input should be warning, got %s", starved[0].Severity)
	}
}

func TestStarvedStateEvent(t *testing.T) {
	c := comp("billing", "billing")
	c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}
	// Nothing outputs this kind, so the projection tracks nothing.

	fs := Validate(model(c))
	if !hasKind(fs, KindStarvedStateEvent) {
		t.Error("want starved state-event finding")
	}
}

func TestStarvedStateEventSatisfied(t *testing.T) {
	c := comp("billing", "billing")
	c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}
	c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}

	fs := Validate(model(c))
	if hasKind(fs, KindStarvedStateEvent) {
		t.Error("state event should be satisfied by the component's own output")
	}
}

func TestOrphanEvent(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	// No consumer.

	fs := Validate(model(c))
	if !hasKind(fs, KindOrphanEvent) {
		t.Error("want orphan event finding")
	}
}

func TestOrphanEventConsumedByReceive(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued"}}

	shipping := comp("shipping", "shipping")
	shipping.Inputs = []Slot{{Kind: "billing.invoice.issued"}}

	fs := Validate(model(billing, shipping))
	if hasKind(fs, KindOrphanEvent) {
		t.Error("event should not be orphan when it is another component's input")
	}
}

func TestOrphanEventFoldedAsStateEvent(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued"}}

	analytics := comp("analytics", "analytics")
	analytics.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}

	fs := Validate(model(billing, analytics))
	if hasKind(fs, KindOrphanEvent) {
		t.Error("event should not be orphan when a component folds it into state")
	}
}

// TestOrphanEventFoldedByItsOwnProducer: the producer's own state_events entry
// counts as an observation. Folding the outcome you just appended is the
// idiom the third list exists for.
func TestOrphanEventFoldedByItsOwnProducer(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}

	if hasKind(Validate(model(c)), KindOrphanEvent) {
		t.Error("a component folding its own output is an observer of it")
	}
}

// TestBroadcastOutputWithNoObserverIsWarning: without the exclusive opt-in an
// unobserved output is a closure warning, never an error.
func TestBroadcastOutputWithNoObserverIsWarning(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{Kind: "ledger.entry.post"}}

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
	c.Outputs = []Slot{{Kind: "ledger.entry.post", Delivery: DeliveryExclusive}}

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
	billing.Outputs = []Slot{{Kind: "ledger.entry.post"}}

	// Exclusivity declared on the input side propagates to the kind.
	ledger1 := comp("ledger1", "ledger")
	ledger1.Inputs = []Slot{{Kind: "ledger.entry.post", Delivery: DeliveryExclusive}}

	ledger2 := comp("ledger2", "ledger2")
	ledger2.Inputs = []Slot{{Kind: "ledger.entry.post"}}

	fs := Validate(model(billing, ledger1, ledger2))
	found := findingsByKind(fs, KindExclusiveConflict)
	if len(found) != 1 {
		t.Fatalf("want 1 exclusive-conflict finding, got %d: %+v", len(found), fs)
	}
	if !strings.Contains(found[0].Message, "ledger1") || !strings.Contains(found[0].Message, "ledger2") {
		t.Errorf("conflict should name both consumers: %s", found[0].Message)
	}
}

func TestExclusiveSatisfied(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Outputs = []Slot{{Kind: "ledger.entry.post", Delivery: DeliveryExclusive}}

	ledger := comp("ledger", "ledger")
	ledger.Inputs = []Slot{{Kind: "ledger.entry.post", Delivery: DeliveryExclusive}}

	fs := Validate(model(billing, ledger))
	if hasKind(fs, KindExclusiveUnhandled) || hasKind(fs, KindExclusiveConflict) {
		t.Errorf("exclusive contract should be satisfied: %+v", fs)
	}
}

func TestUnresolvedLocalRef(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
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
	c.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/Invoice"}},
	}}

	fs := Validate(model(c))
	if hasKind(fs, KindUnresolvedRef) {
		t.Errorf("ref should resolve: %+v", fs)
	}
}

func TestUnresolvedCrossComponentRef(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
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
	billing.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
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
	c.Outputs = []Slot{{
		Kind: "billing.invoice.issued",
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
	c.Outputs = []Slot{{
		Kind: "billing.invoice.issued",
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
	c.Outputs = []Slot{{
		Kind: "billing.invoice.issued",
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
	c.Inputs = []Slot{{Kind: "billing.foo", Pattern: "svc.*.billing.{unclosed.>"}}
	c.Outputs = []Slot{{Kind: "billing.foo"}}

	fs := Validate(model(c))
	if !hasKind(fs, KindMalformedSlot) {
		t.Error("want malformed-slot finding for unclosed brace")
	}
}

func TestMalformedSlotEmptySlot(t *testing.T) {
	c := comp("billing", "billing")
	c.Inputs = []Slot{{Kind: "billing.foo", Pattern: "svc.*.billing.{}.invoice.>"}}
	c.Outputs = []Slot{{Kind: "billing.foo"}}

	fs := Validate(model(c))
	if !hasKind(fs, KindMalformedSlot) {
		t.Error("want malformed-slot finding for empty slot")
	}
}

// TestSubjectNotMatchedAgainstKinds is a regression guard: the subject pattern
// is a transport address, never matched against the kind alphabet. Here the
// pattern shares no segments with the kind and that is not a finding.
func TestSubjectNotMatchedAgainstKinds(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.wire.{account}.42.>"}}
	c.StateEvents = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.wire.{account}.42.>"}}

	fs := Validate(model(c))
	if len(fs) != 0 {
		t.Errorf("a subject unrelated to the kind name is not a finding, got %+v", fs)
	}
}

// TestOutputsAreNotInTheReadSet: an output-only pattern keyed differently from
// the read-set is not a partition mismatch. Appending an event does not
// subscribe the component to it, so it never has to address its state.
func TestOutputsAreNotInTheReadSet(t *testing.T) {
	c := comp("billing", "billing")
	c.Inputs = []Slot{{Kind: "billing.invoice.issue", Pattern: "svc.*.billing.{account}.invoice.issue"}}
	c.Outputs = []Slot{{Kind: "ledger.entry.post", Pattern: "svc.*.ledger.{entry}.post"}}

	if hasKind(Validate(model(c)), KindPartitionMismatch) {
		t.Error("an output pattern must not be held to the read-set's partition key")
	}
}

func TestFindingSeverities(t *testing.T) {
	// Verify that errors sort before warnings.
	c := comp("billing", "billing")
	// Unresolved $ref (error).
	c.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/Missing"}},
	}}
	// Starved receive (warning).
	c.Inputs = []Slot{{Kind: "billing.foo"}}

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
	c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	c.StateEvents = []Slot{
		{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"},
		{Kind: "billing.credit.applied", Pattern: "svc.*.billing.{region}.invoice.applied"},
	}
	c.PartitionKey = []string{"account"}

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
	c.Outputs = []Slot{{Kind: "warehouse.stock.adjusted"}}
	c.StateEvents = []Slot{
		{Kind: "warehouse.stock.adjusted", Pattern: "svc.*.warehouse.{region}.{sku}.stock.adjusted"},
		{Kind: "warehouse.stock.depleted", Pattern: "svc.*.warehouse.{sku}.{region}.stock.depleted"},
	}
	c.PartitionKey = []string{"region", "sku"}

	if !hasKind(Validate(model(c)), KindPartitionMismatch) {
		t.Error("same slot names in a different order are a different partition key")
	}
}

func TestPartitionKeyShared(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{
		{Kind: "billing.invoice.issued"},
		{Kind: "billing.credit.applied"},
	}
	c.StateEvents = []Slot{
		{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"},
		{Kind: "billing.credit.applied", Pattern: "svc.*.billing.{account}.credit.applied"},
	}
	c.PartitionKey = []string{"account"}

	if hasKind(Validate(model(c)), KindPartitionMismatch) {
		t.Error("subjects sharing one ordered key must not be flagged")
	}
}

// TestUnpartitionedSubjectIsExempt: a globally addressed event legitimately
// feeds a partitioned state, so a pattern with no {slot} at all is not held to
// the component's key.
func TestUnpartitionedSubjectIsExempt(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{
		{Kind: "billing.invoice.issued"},
		{Kind: "billing.rates.changed"},
	}
	c.StateEvents = []Slot{
		{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"},
		{Kind: "billing.rates.changed", Pattern: "svc.*.billing.rates.changed"},
	}
	c.PartitionKey = []string{"account"}

	if hasKind(Validate(model(c)), KindPartitionMismatch) {
		t.Error("a subject carrying no slots must not be flagged")
	}
}

func TestUnderspecifiedState(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}
	c.State = SchemaNode{Raw: map[string]any{"type": "object"}}

	fs := Validate(model(c))
	found := findingsByKind(fs, KindUnderspecifiedState)
	if len(found) != 1 {
		t.Fatalf("want 1 underspecified-state finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityWarning {
		t.Errorf("underspecified-state should be a warning, got %s", found[0].Severity)
	}
}

func TestSpecifiedState(t *testing.T) {
	byRef := comp("billing", "billing")
	byRef.Types["Open"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	byRef.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	byRef.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}
	byRef.State = SchemaNode{Raw: map[string]any{"$ref": "#/types/Open"}}

	inline := comp("shipping", "shipping")
	inline.Outputs = []Slot{{Kind: "shipping.package.shipped"}}
	inline.StateEvents = []Slot{{Kind: "shipping.package.shipped"}}
	inline.State = SchemaNode{Raw: map[string]any{
		"type":       "object",
		"properties": map[string]any{"Count": map[string]any{"type": "integer"}},
	}}

	if hasKind(Validate(model(byRef, inline)), KindUnderspecifiedState) {
		t.Error("a state with a $ref or properties must not be flagged")
	}
}

// TestAbsentStateIsNotAFinding: the state schema is optional. A component may
// fold nothing worth describing, and only a declared-but-empty schema is the
// placeholder the warning is about.
func TestAbsentStateIsNotAFinding(t *testing.T) {
	c := comp("billing", "billing")
	c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}

	if hasKind(Validate(model(c)), KindUnderspecifiedState) {
		t.Error("an absent state must not be reported as underspecified")
	}
}

// --- kind-pattern-conflict ----------------------------------------------------

// The subject pattern is where a kind lives on the wire, so two components
// cannot address the same kind differently: the subscriber of one will never
// see what the other appends.
func TestKindPatternConflictAcrossComponents(t *testing.T) {
	producer := comp("llm", "llm")
	producer.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}

	consumer := comp("router", "router")
	consumer.Inputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.router.{session}.message"}}

	fs := Validate(model(producer, consumer))
	found := findingsByKind(fs, KindPatternConflict)
	if len(found) != 1 {
		t.Fatalf("want 1 kind-pattern-conflict finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityError {
		t.Errorf("kind-pattern-conflict should be an error, got %s", found[0].Severity)
	}
	for _, want := range []string{"llm.message", "svc.*.llm.{session}.message", "svc.*.router.{session}.message", "llm:outputs", "router:inputs"} {
		if !strings.Contains(found[0].Message, want) {
			t.Errorf("message should mention %q: %s", want, found[0].Message)
		}
	}
}

// One component can disagree with itself across its own lists.
func TestKindPatternConflictWithinComponent(t *testing.T) {
	c := comp("llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}
	c.StateEvents = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{turn}.message"}}

	if !hasKind(Validate(model(c)), KindPatternConflict) {
		t.Error("want kind-pattern-conflict when one component declares two addresses")
	}
}

// Agreeing declarations are clean, however many sites repeat them.
func TestKindPatternConsistent(t *testing.T) {
	const pattern = "svc.*.llm.{session}.message"

	a := comp("llm", "llm")
	a.Outputs = []Slot{{Kind: "llm.message", Pattern: pattern}}

	b := comp("mirror", "mirror")
	b.Outputs = []Slot{{Kind: "llm.message", Pattern: pattern}}

	c := comp("router", "router")
	c.Inputs = []Slot{{Kind: "llm.message", Pattern: pattern}}

	d := comp("audit", "audit")
	d.StateEvents = []Slot{{Kind: "llm.message", Pattern: pattern}}

	if hasKind(Validate(model(a, b, c, d)), KindPatternConflict) {
		t.Error("agreeing declarations must not conflict")
	}
}

// A declaration that carries no pattern is not disagreeing with one that does.
func TestKindPatternOmittedIsNotAConflict(t *testing.T) {
	producer := comp("llm", "llm")
	producer.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}

	consumer := comp("router", "router")
	consumer.Inputs = []Slot{{Kind: "llm.message"}}

	if hasKind(Validate(model(producer, consumer)), KindPatternConflict) {
		t.Error("an omitted pattern must not be read as a second address")
	}
}

// Two conflicting kinds produce two findings, not one per declaration site.
func TestKindPatternConflictIsPerKind(t *testing.T) {
	c := comp("llm", "llm")
	c.Inputs = []Slot{
		{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"},
		{Kind: "llm.tool", Pattern: "svc.*.llm.{session}.tool"},
	}
	c.StateEvents = []Slot{
		{Kind: "llm.message", Pattern: "svc.*.llm.{turn}.message"},
		{Kind: "llm.tool", Pattern: "svc.*.llm.{turn}.tool"},
	}

	found := findingsByKind(Validate(model(c)), KindPatternConflict)
	if len(found) != 2 {
		t.Fatalf("want 1 finding per conflicting kind, got %d", len(found))
	}
	if found[0].Location != "llm.message" || found[1].Location != "llm.tool" {
		t.Errorf("findings should be sorted by kind, got %q and %q", found[0].Location, found[1].Location)
	}
}

// PatternOf resolves the canonical address deterministically, and is empty for
// a kind nobody addressed.
func TestPatternOf(t *testing.T) {
	producer := comp("llm", "llm")
	producer.Outputs = []Slot{
		{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"},
		{Kind: "llm.silent"},
	}

	m := model(producer)
	if got := PatternOf(m, "llm.message"); got != "svc.*.llm.{session}.message" {
		t.Errorf("PatternOf(llm.message) = %q", got)
	}
	if got := PatternOf(m, "llm.silent"); got != "" {
		t.Errorf("PatternOf(llm.silent) = %q, want empty", got)
	}
	if got := PatternOf(m, "llm.unknown"); got != "" {
		t.Errorf("PatternOf(llm.unknown) = %q, want empty", got)
	}
}

// --- self-input-conflict -----------------------------------------------------

// A component does not trigger itself: the output is already the record, and a
// matching inputs entry draws a self-loop that nothing at runtime corresponds
// to.
func TestSelfInputConflict(t *testing.T) {
	c := comp("controller-llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.failed"}}
	c.Inputs = []Slot{{Kind: "llm.failed"}}

	fs := Validate(model(c))
	found := findingsByKind(fs, KindSelfInputConflict)
	if len(found) != 1 {
		t.Fatalf("want 1 self-input-conflict finding, got %d: %+v", len(found), fs)
	}
	if found[0].Severity != SeverityError {
		t.Errorf("self-input-conflict should be an error, got %s", found[0].Severity)
	}
	if found[0].Component != "controller-llm" {
		t.Errorf("Component = %q, want controller-llm", found[0].Component)
	}
	if found[0].Location != "llm.failed" {
		t.Errorf("Location = %q, want llm.failed", found[0].Location)
	}
	// Component id, kind, both declaration sites, and the fix must be
	// recoverable from the message alone.
	for _, want := range []string{`"controller-llm"`, `"llm.failed"`, "input", "output", "state_events"} {
		if !strings.Contains(found[0].Message, want) {
			t.Errorf("message should mention %q: %s", want, found[0].Message)
		}
	}
	if _, ok := found[0].Related["outputs"]; !ok {
		t.Errorf("Related should carry the outputs positions: %v", found[0].Related)
	}
	if _, ok := found[0].Related["inputs"]; !ok {
		t.Errorf("Related should carry the inputs positions: %v", found[0].Related)
	}
}

// The prescribed fix, and the reason the rule can stay narrow: folding the
// component's own output is a state event, not an input.
func TestSelfStateEventIsAllowed(t *testing.T) {
	c := comp("controller-llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.failed", Pattern: "svc.*.llm.{session}.failed"}}
	c.StateEvents = []Slot{{Kind: "llm.failed", Pattern: "svc.*.llm.{session}.failed"}}
	c.State = SchemaNode{Raw: map[string]any{
		"type":       "object",
		"properties": map[string]any{"Count": map[string]any{"type": "integer"}},
	}}

	fs := Validate(model(c))
	if len(fs) != 0 {
		t.Errorf("folding own output should validate clean, got %+v", fs)
	}
}

// Other components take the producer's kind as an input freely, however many.
func TestOtherComponentsMayInputAnOutputKind(t *testing.T) {
	producer := comp("controller-llm", "llm")
	producer.Outputs = []Slot{{Kind: "llm.failed"}}

	b := comp("router", "router")
	b.Inputs = []Slot{{Kind: "llm.failed"}}

	c := comp("audit", "audit")
	c.Inputs = []Slot{{Kind: "llm.failed"}}

	fs := Validate(model(producer, b, c))
	if len(fs) != 0 {
		t.Errorf("independent observers should validate clean, got %+v", fs)
	}
}

// Outputting one kind and being triggered by a different one is the ordinary
// case.
func TestOutputAndInputDifferentKinds(t *testing.T) {
	c := comp("controller-llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.failed"}}
	c.Inputs = []Slot{{Kind: "router.request.dispatched"}}

	router := comp("router", "router")
	router.Outputs = []Slot{{Kind: "router.request.dispatched"}}
	router.Inputs = []Slot{{Kind: "llm.failed"}}

	if hasKind(Validate(model(c, router)), KindSelfInputConflict) {
		t.Error("distinct kinds must not conflict")
	}
}

// One finding per (component, kind) — not one per duplicated slot.
func TestSelfInputConflictIsPerKind(t *testing.T) {
	c := comp("controller-llm", "llm")
	c.Outputs = []Slot{
		{Kind: "llm.failed"},
		{Kind: "llm.done"},
	}
	c.Inputs = []Slot{
		{Kind: "llm.failed"},
		{Kind: "llm.failed"},
		{Kind: "llm.done"},
	}

	found := findingsByKind(Validate(model(c)), KindSelfInputConflict)
	if len(found) != 2 {
		t.Fatalf("want 1 finding per conflicting kind, got %d", len(found))
	}
	// Both inputs positions of the repeated kind are reported.
	for _, f := range found {
		if f.Location != "llm.failed" {
			continue
		}
		pos, ok := f.Related["inputs"].([]int)
		if !ok || len(pos) != 2 {
			t.Errorf("both inputs positions should be reported, got %v", f.Related["inputs"])
		}
	}
}

// Matching is on the exact kind: a similarly-named kind is a different kind.
func TestSelfInputConflictMatchesExactKind(t *testing.T) {
	c := comp("controller-llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.failed"}}
	c.Inputs = []Slot{{Kind: "llm.failed.retried"}}

	other := comp("supervisor", "supervisor")
	other.Outputs = []Slot{{Kind: "llm.failed.retried"}}
	other.Inputs = []Slot{{Kind: "llm.failed"}}

	if hasKind(Validate(model(c, other)), KindSelfInputConflict) {
		t.Error("prefix-related kinds are different kinds")
	}
}
