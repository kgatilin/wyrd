package events

import (
	"reflect"
	"testing"
)

func TestDiffModelViewKeepsWhatTheBaseHadAndTheBranchDropped(t *testing.T) {
	cur := modelView{
		Components: []componentView{{ID: "ingress/router"}},
		Kinds:      []kindView{{Name: "ingress.event.routed"}},
		Flows:      []flowView{},
	}
	base := modelView{
		Components: []componentView{{ID: "ingress/router"}, {ID: "legacy/relay"}},
		Kinds:      []kindView{{Name: "ingress.event.routed"}, {Name: "legacy.event.relayed"}},
		Flows:      []flowView{{From: "legacy/relay", To: "ingress/router", Kind: "legacy.event.relayed"}},
	}

	got := diffModelView(cur, base)

	// The point of the whole feature: the map stays whole. A view narrowed to
	// what is current answers "what is here now", which the unmarked view did.
	if len(got.Components) != 2 {
		t.Fatalf("components = %d, want 2 (the current one and the dropped one)", len(got.Components))
	}
	if got.Components[0].ID != "ingress/router" || got.Components[0].Diff != "" {
		t.Errorf("unchanged component = %+v, want no mark", got.Components[0])
	}
	if got.Components[1].ID != "legacy/relay" || got.Components[1].Diff != changeRemoved {
		t.Errorf("dropped component = %+v, want removed", got.Components[1])
	}
	if len(got.Flows) != 1 || got.Flows[0].Diff != changeRemoved {
		t.Errorf("flows = %+v, want one removed edge", got.Flows)
	}
	if len(got.Kinds) != 2 || got.Kinds[1].Diff != changeRemoved {
		t.Errorf("kinds = %+v, want the dropped one marked removed", got.Kinds)
	}
}

// The branch-996 shape: an address moved. Matching entries on (kind, address)
// alone would report that as a removal beside an unrelated addition, and the
// one thing a reader opened the panel for — what it used to be — would be gone.
func TestDiffSlotsMatchesOnKindWhenTheAddressMoved(t *testing.T) {
	cur := []slotView{{Kind: "llm.event.thinking", Pattern: "app.{tenant}.llm.session.{session}.eventlog.event.thinking"}}
	base := []slotView{{Kind: "llm.event.thinking", Pattern: "app.{tenant}.llm.session.{session}.main.event.thinking"}}

	got := diffSlots(cur, base)

	if len(got) != 1 {
		t.Fatalf("slots = %+v, want one changed entry, not a removal and an addition", got)
	}
	if got[0].Diff != changeChanged {
		t.Errorf("diff = %q, want %q", got[0].Diff, changeChanged)
	}
	if got[0].Was == nil || got[0].Was.Pattern != base[0].Pattern {
		t.Errorf("was = %+v, want the previous address", got[0].Was)
	}
}

// A port family declares one kind on one address per instance. An entry that
// did not move must never be consumed as the "was" of a sibling that did.
func TestDiffSlotsPrefersAnExactPairOverASharedKind(t *testing.T) {
	const kind = "channel.command.message.send"
	steady := slotView{Kind: kind, Pattern: "app.channel.slack.command.message.send"}
	cur := []slotView{
		steady,
		{Kind: kind, Pattern: "app.channel.runner.v2.command.message.send"},
	}
	base := []slotView{
		{Kind: kind, Pattern: "app.channel.runner.command.message.send"},
		steady,
	}

	got := diffSlots(cur, base)

	if len(got) != 2 {
		t.Fatalf("slots = %+v, want two entries", got)
	}
	if got[0].Diff != "" {
		t.Errorf("unmoved entry = %+v, want no mark", got[0])
	}
	if got[1].Diff != changeChanged || got[1].Was == nil {
		t.Fatalf("moved entry = %+v, want changed with a previous value", got[1])
	}
	if got[1].Was.Pattern != base[0].Pattern {
		t.Errorf("was = %q, want the runner address it moved from", got[1].Was.Pattern)
	}
}

func TestDiffKindsReportsASchemaMoveAsAFlag(t *testing.T) {
	cur := []kindView{{Name: "k", Schema: map[string]any{"type": "object"}}}
	base := []kindView{{Name: "k", Schema: map[string]any{"type": "string"}}}

	got := diffKinds(cur, base)

	if len(got) != 1 || got[0].Diff != changeChanged {
		t.Fatalf("kinds = %+v, want one changed", got)
	}
	if got[0].Was == nil || !got[0].Was.SchemaChanged {
		t.Errorf("was = %+v, want schema_changed", got[0].Was)
	}
}

// An identical model must come back with no marks at all. A diff that reports
// everything as changed is worse than no diff: it hides the two things that did.
func TestDiffModelViewMarksNothingWhenNothingMoved(t *testing.T) {
	view := modelView{
		Components: []componentView{{
			ID:      "billing",
			Inputs:  []slotView{{Kind: "billing.command.issue", Pattern: "app.billing.command.issue"}},
			Outputs: []slotView{{Kind: "billing.event.issued", Pattern: "app.billing.event.issued"}},
		}},
		Kinds: []kindView{{Name: "billing.event.issued", Pattern: "app.billing.event.issued"}},
		Flows: []flowView{{From: "billing", To: "ledger", Kind: "billing.event.issued", Trigger: true}},
	}

	got := diffModelView(view, view)

	if !reflect.DeepEqual(got, view) {
		t.Errorf("diff of a model against itself = %+v, want it untouched", got)
	}
}

func TestDiffModelViewAgainstAnEmptyBaseMarksEverythingAdded(t *testing.T) {
	cur := modelView{
		Components: []componentView{{ID: "billing"}},
		Kinds:      []kindView{{Name: "billing.event.issued"}},
		Flows:      []flowView{{From: "billing", To: "ledger", Kind: "billing.event.issued"}},
	}

	got := diffModelView(cur, modelView{})

	if got.Components[0].Diff != changeAdded || got.Kinds[0].Diff != changeAdded || got.Flows[0].Diff != changeAdded {
		t.Errorf("against an empty base = %+v, want everything added", got)
	}
}
