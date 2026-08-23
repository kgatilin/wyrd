package events

import (
	"reflect"
	"sort"
)

// The event model's diff, computed over the wire shape rather than over the
// domain model.
//
// That is deliberate. The reader's question is "what changed on this picture",
// and the picture is this wire shape — so comparing it means a mark appears
// exactly when something the canvas draws is different, and never when a field
// moved that the canvas was never going to show. A second comparison over
// eventmodel.Model would have to be kept in step with every projection choice
// buildModelView makes, and the first time it drifted the canvas would paint a
// component as unchanged while showing different contents.
//
// The result is a union, not a delta: everything current, plus everything the
// base had and this branch does not, marked removed. A diff view that drops the
// removed half answers "what is here now", which the undiffed view already did.

// changeAdded and friends are the marks a reader sees. Same vocabulary as the
// review canvas (web/src/types.ts), because a reader moving between the two
// surfaces should not have to learn a second one.
const (
	changeAdded   = "added"
	changeRemoved = "removed"
	changeChanged = "changed"
)

// baseIdentity names the commit a view was compared against. Carried on the
// response so the canvas can label what it is showing without asking a second
// endpoint what the base was.
type baseIdentity struct {
	Ref string `json:"ref"`
	Rev string `json:"rev,omitempty"`
}

// diffModelView returns cur marked up against base.
//
// Components, kinds and flows each carry their own mark, and a component's
// slots carry theirs, because the three answer different questions: which
// boxes moved, which vocabulary moved, which wires moved, and — inside a box
// whose contents changed — which line of it.
func diffModelView(cur, base modelView) modelView {
	out := modelView{
		Components: diffComponents(cur.Components, base.Components),
		Flows:      diffFlows(cur.Flows, base.Flows),
		Kinds:      diffKinds(cur.Kinds, base.Kinds),
	}
	return out
}

func diffComponents(cur, base []componentView) []componentView {
	byID := make(map[string]componentView, len(base))
	for _, c := range base {
		byID[c.ID] = c
	}

	out := make([]componentView, 0, len(cur)+len(base))
	for _, c := range cur {
		prev, existed := byID[c.ID]
		if !existed {
			c.Diff = changeAdded
			out = append(out, c)
			continue
		}
		delete(byID, c.ID)
		if reflect.DeepEqual(c, prev) {
			out = append(out, c)
			continue
		}
		c.Diff = changeChanged
		c.Inputs = diffSlots(c.Inputs, prev.Inputs)
		c.Outputs = diffSlots(c.Outputs, prev.Outputs)
		c.StateEvents = diffSlots(c.StateEvents, prev.StateEvents)
		c.Was = componentFacts(prev)
		out = append(out, c)
	}

	// What the base had and this branch does not. Its own slots are all
	// removed by construction, so they are not marked one by one: a box drawn
	// as removed whose every line also says "removed" is the same fact four
	// times.
	for _, c := range base {
		if _, unclaimed := byID[c.ID]; !unclaimed {
			continue
		}
		c.Diff = changeRemoved
		out = append(out, c)
	}
	return sortComponentViews(out)
}

// diffSlots marks one of a component's three lists.
//
// Matching is by (kind, address) first and by kind alone second. A kind can
// legitimately appear twice in one list — a port family puts it on one address
// per instance — so the pair is the identity; but the whole reason a reader
// opens this panel is to see an address move, and matching on the pair alone
// would report that move as a removal beside an unrelated addition.
func diffSlots(cur, base []slotView) []slotView {
	claimed := make([]bool, len(base))

	// Exact pairs first, so an unchanged entry can never be consumed as the
	// "was" of a changed one that happens to share its kind.
	matched := make([]int, len(cur))
	for i := range matched {
		matched[i] = -1
	}
	for i, slot := range cur {
		for j, prev := range base {
			if !claimed[j] && prev.Kind == slot.Kind && prev.Pattern == slot.Pattern {
				matched[i], claimed[j] = j, true
				break
			}
		}
	}
	for i, slot := range cur {
		if matched[i] >= 0 {
			continue
		}
		for j, prev := range base {
			if !claimed[j] && prev.Kind == slot.Kind {
				matched[i], claimed[j] = j, true
				break
			}
		}
	}

	out := make([]slotView, 0, len(cur)+len(base))
	for i, slot := range cur {
		if matched[i] < 0 {
			slot.Diff = changeAdded
			out = append(out, slot)
			continue
		}
		prev := base[matched[i]]
		if reflect.DeepEqual(slot, prev) {
			out = append(out, slot)
			continue
		}
		slot.Diff = changeChanged
		was := prev
		out = append(out, slot)
		out[len(out)-1].Was = &was
	}
	for j, prev := range base {
		if claimed[j] {
			continue
		}
		prev.Diff = changeRemoved
		out = append(out, prev)
	}
	return out
}

func diffFlows(cur, base []flowView) []flowView {
	type key struct{ from, to, kind string }
	byKey := make(map[key]flowView, len(base))
	for _, f := range base {
		byKey[key{f.From, f.To, f.Kind}] = f
	}

	out := make([]flowView, 0, len(cur)+len(base))
	for _, f := range cur {
		k := key{f.From, f.To, f.Kind}
		prev, existed := byKey[k]
		if !existed {
			f.Diff = changeAdded
			out = append(out, f)
			continue
		}
		delete(byKey, k)
		if !reflect.DeepEqual(f, prev) {
			f.Diff = changeChanged
		}
		out = append(out, f)
	}
	for _, f := range base {
		if _, unclaimed := byKey[key{f.From, f.To, f.Kind}]; !unclaimed {
			continue
		}
		f.Diff = changeRemoved
		out = append(out, f)
	}
	return sortFlowViews(out)
}

func diffKinds(cur, base []kindView) []kindView {
	byName := make(map[string]kindView, len(base))
	for _, k := range base {
		byName[k.Name] = k
	}

	out := make([]kindView, 0, len(cur)+len(base))
	for _, k := range cur {
		prev, existed := byName[k.Name]
		if !existed {
			k.Diff = changeAdded
			out = append(out, k)
			continue
		}
		delete(byName, k.Name)
		if reflect.DeepEqual(k, prev) {
			out = append(out, k)
			continue
		}
		k.Diff = changeChanged
		k.Was = kindFacts(prev, k)
		out = append(out, k)
	}
	for _, k := range base {
		if _, unclaimed := byName[k.Name]; !unclaimed {
			continue
		}
		k.Diff = changeRemoved
		out = append(out, k)
	}
	return sortKindViews(out)
}

// componentFacts is the previous value of the scalar facts a component's panel
// prints. The three lists are not repeated here — their entries carry their own
// marks, which is a better answer than a second copy of the old list.
func componentFacts(prev componentView) *componentWasView {
	return &componentWasView{
		Owns:         prev.Owns,
		Description:  prev.Description,
		SourceFile:   prev.SourceFile,
		PartitionKey: prev.PartitionKey,
		HasState:     prev.HasState,
	}
}

// kindFacts is the previous value of a kind's identity on the wire.
//
// The payload schema is reported as a flag rather than repeated. It is the
// largest thing in the response by a wide margin, doubling it would cost more
// than every other field here put together, and a reader who wants the detail
// has the file diff — where a schema change is legible as a patch and not as
// two JSON blobs to compare by eye.
func kindFacts(prev, now kindView) *kindWasView {
	return &kindWasView{
		Pattern:       prev.Pattern,
		Subjects:      prev.Subjects,
		Delivery:      prev.Delivery,
		Health:        prev.Health,
		Class:         prev.Class,
		Owner:         prev.Owner,
		SchemaChanged: !reflect.DeepEqual(prev.Schema, now.Schema),
	}
}

// The three sorts restore the order buildModelView emits, which the marked-up
// union breaks by appending everything removed at the end. A renderer and a
// reader scrolling a list both want one order, not "current, then whatever is
// gone".

func sortComponentViews(v []componentView) []componentView {
	sort.SliceStable(v, func(i, j int) bool { return v[i].ID < v[j].ID })
	return v
}

func sortFlowViews(v []flowView) []flowView {
	sort.SliceStable(v, func(i, j int) bool {
		if v[i].From != v[j].From {
			return v[i].From < v[j].From
		}
		if v[i].To != v[j].To {
			return v[i].To < v[j].To
		}
		return v[i].Kind < v[j].Kind
	})
	return v
}

func sortKindViews(v []kindView) []kindView {
	sort.SliceStable(v, func(i, j int) bool { return v[i].Name < v[j].Name })
	return v
}
