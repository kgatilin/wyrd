package events

import (
	"reflect"
	"testing"

	"github.com/kgatilin/wyrd/internal/eventmodel"
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

// The branch-1052 shape: a commit adds one instance to a port family and
// nothing else. Every kind the family carries was declared before and is
// declared now, at the same addresses by the same components — so the only
// marks the reader should see are the new component, the new addresses, and
// the edges that reach it.
//
// Two derivations used to break that. The kind's owner was the first claimant
// of `channel` in sorted order, and the kind's address was the first
// declaration in the same order; a new instance sorting ahead of the old first
// moved both, on every kind in the namespace at once — including the ten that
// only the siblings declare and the new instance never mentions. Fifteen
// changed rows for a commit that changed nothing on the wire.
func TestDiffModelViewIgnoresAnInstanceJoiningAPortFamily(t *testing.T) {
	instance := func(name string, kinds ...string) *eventmodel.Component {
		c := &eventmodel.Component{ID: "channel/" + name, Owns: "channel", Source: eventmodel.SourceAsyncAPI}
		for _, kind := range kinds {
			c.Inputs = append(c.Inputs, eventmodel.Slot{
				Kind:    kind,
				Pattern: "app.{tenant}.channel." + name + ".thread.{thread}." + kind,
			})
		}
		return c
	}
	// One kind the whole family carries, and one only slack does.
	const shared, slackOnly = "channel.command.message.send", "channel.event.slack.delivery.failed"
	view := func(comps ...*eventmodel.Component) modelView {
		m := &eventmodel.Model{Components: make(map[string]*eventmodel.Component, len(comps))}
		for _, c := range comps {
			m.Components[c.ID] = c
		}
		return buildModelView(m, eventmodel.BuildGraph(m))
	}

	base := view(instance("runner", shared), instance("slack", shared, slackOnly))
	cur := view(instance("adminui", shared), instance("runner", shared), instance("slack", shared, slackOnly))

	got := diffModelView(cur, base)

	for _, c := range got.Components {
		want := ""
		if c.ID == "channel/adminui" {
			want = changeAdded
		}
		if c.Diff != want {
			t.Errorf("component %s = %q, want %q", c.ID, c.Diff, want)
		}
	}
	for _, k := range got.Kinds {
		switch k.Name {
		case slackOnly:
			// Nothing about it moved. Not its address, not who declares it.
			if k.Diff != "" {
				t.Errorf("kind %s = %q with was %+v, want no mark", k.Name, k.Diff, k.Was)
			}
		case shared:
			// It does travel one more address now, which is the one true
			// thing to report — and it is reported as an address arriving,
			// not as the kind changing identity.
			if k.Diff != changeChanged {
				t.Fatalf("kind %s = %q, want %q", k.Name, k.Diff, changeChanged)
			}
			if k.Was == nil || k.Was.Owner != k.Owner {
				t.Errorf("owner moved to %q from %+v, want the same owner", k.Owner, k.Was)
			}
			if k.Was.Pattern != k.Pattern {
				t.Errorf("address moved from %q to %q, want neither named", k.Was.Pattern, k.Pattern)
			}
			if len(k.Subjects) != len(k.Was.Subjects)+1 {
				t.Errorf("subjects = %v, want one more than %v", k.Subjects, k.Was.Subjects)
			}
		}
	}
}
