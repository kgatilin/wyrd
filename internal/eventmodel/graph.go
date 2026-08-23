package eventmodel

import (
	"sort"
	"strings"
)

// Graph is a bipartite event-model graph projected from a validated Model.
// Nodes are components, kinds, and type definitions. Edges capture the
// event-driven relationships: the component's output and input ports, the
// state events it folds, payload typing, and schema references.
//
// There is no fold node. One component holds one state, so the fold is the
// component: its read-set, partition key and state shape ride as attributes
// rather than as a vertex that would sit alone next to every component.
//
// This is a pure data structure with no rendering concerns. Adapters
// (GraphML, Mermaid) consume it to produce output formats.
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// Node is a single vertex in the event graph.
type Node struct {
	// ID follows the scheme from design.md §3:
	//   component:<id>  kind:<name>  type:<component>.<typeName>
	ID string

	// Kind classifies the node.
	Kind NodeKind

	// Attrs holds node-specific attributes. Keys vary by Kind:
	//   component: owns, subjects, consumes, partition_key, partition_arity,
	//              has_state
	//   kind: producer_count, input_count, state_fold_count, health, pattern,
	//         partition_key, pattern_conflict, delivery
	//   type: component, deprecated
	Attrs map[string]any
}

// NodeKind classifies event-graph nodes.
type NodeKind string

const (
	NodeComponent NodeKind = "component"
	NodeEventKind NodeKind = "kind"
	NodeType      NodeKind = "type"
)

// Edge is a single directed edge in the event graph.
type Edge struct {
	// From and To are node IDs.
	From, To string

	// Kind classifies the edge.
	Kind EdgeKind

	// Attrs holds edge-specific attributes. Keys vary by Kind:
	//   input, output, state-event: exposure
	Attrs map[string]any
}

// EdgeKind classifies event-graph edges.
type EdgeKind string

const (
	// EdgeOutput and EdgeInput are the component's ports; EdgeStateEvent is
	// the third channel, an observation that updates state without triggering
	// the component. All three point the way the data flows.
	EdgeOutput     EdgeKind = "output"      // component --output--> kind
	EdgeInput      EdgeKind = "input"       // kind --input--> component
	EdgeStateEvent EdgeKind = "state-event" // kind --state-event--> component
	EdgePayload    EdgeKind = "payload"     // kind --payload--> type
	EdgeRefs       EdgeKind = "refs"        // type --refs--> type
	EdgeDefines    EdgeKind = "defines"     // component --defines--> type (structural contains)
)

// Health classifies a kind's connectivity status.
type Health string

const (
	HealthOK      Health = "ok"
	HealthOrphan  Health = "orphan"  // emitted but observed by nobody
	HealthStarved Health = "starved" // observed but emitted by nobody
	// HealthAmbiguous applies only to kinds declared `delivery: exclusive`
	// that are the input of more than one component. Multiple observers of an
	// ordinary broadcast event are normal and healthy.
	HealthAmbiguous Health = "ambiguous"
	// HealthUnwired is a kind with a producer and an observer in different
	// components whose declared addresses never match: the append lands
	// somewhere nobody in this set is listening.
	//
	// Its own state because the counting rules cannot see it. Orphan and
	// starved count declarations by kind *name*, while a flow only exists when
	// two ends agree on the *subject* — so a kind appended by one component and
	// taken as an input by another scored "ok" while the canvas drew no edge
	// between them, and nothing said why. That gap is the whole reason this
	// exists: a reader looking at empty space cannot tell a relation the model
	// does not have from one it has and could not draw.
	HealthUnwired Health = "unwired"
)

// BuildGraph projects a Model into a Graph. The model should be validated
// first; health attributes are derived from the same analysis Validate uses,
// factored through computeHealth.
func BuildGraph(m *Model) *Graph {
	g := &Graph{}

	// Build indexes for health computation (same as Validate).
	inputsOf := make(map[string][]string)
	foldersOf := make(map[string][]string)
	producersOf := make(map[string][]string)
	exclusiveKinds := make(map[string]struct{})
	patterns := make(map[string]string)

	for id, comp := range m.Components {
		index := func(slots []Slot, into map[string][]string) {
			for _, slot := range slots {
				into[slot.Kind] = append(into[slot.Kind], id)
				if slot.Delivery.IsExclusive() {
					exclusiveKinds[slot.Kind] = struct{}{}
				}
			}
		}
		index(comp.Inputs, inputsOf)
		index(comp.Outputs, producersOf)
		index(comp.StateEvents, foldersOf)
	}

	// A kind travels one subject pattern globally; where declarations disagree
	// (a kind-pattern-conflict error) the first in deterministic order wins and
	// the node is flagged, so the projection exposes the conflict instead of
	// silently picking.
	//
	// A kind declared by an imported AsyncAPI document is never flagged. A port
	// puts one kind on one address per instance by construction —
	// `tools.command.call` legitimately travels `tools.confluence.…` and
	// `tools.jira.…` — and a caller may bind coordinates the owner left open,
	// so disagreement between those declarations is the format working rather
	// than a wiring bug.
	imported := importedKinds(m)
	patternDecls := patternDeclarations(m)
	patternConflict := make(map[string]bool, len(patternDecls))
	// Every address a kind was declared at, so a reader of one kind is not
	// shown one address as if it were the only one. A port family is the case
	// that makes this necessary: the kind is one vocabulary entry travelling
	// one address per instance.
	subjects := make(map[string][]string, len(patternDecls))
	for kind, decls := range patternDecls {
		patterns[kind] = decls[0].Pattern
		subjects[kind] = distinctPatterns(decls)
		if imported[kind] {
			continue
		}
		for _, d := range decls[1:] {
			if d.Pattern != decls[0].Pattern {
				patternConflict[kind] = true
				break
			}
		}
	}

	// Partition coordinates as the declaring document stated them. An AsyncAPI
	// address is in wire coordinates, so its {slot} tokens include a tenant and
	// a port parameter, neither of which keys a fold; the document names the
	// ones that do, and that answer is not recoverable from the address alone.
	declaredPartition := declaredPartitions(m)

	compIDs := sortedComponentIDs(m)

	// Component nodes. The fold is not a node of its own: one component holds
	// one state, so its read-set, partition key and state shape are attributes
	// of the component that holds them.
	for _, id := range compIDs {
		comp := m.Components[id]
		attrs := map[string]any{
			"subjects":        comp.Subjects(),
			"consumes":        comp.Consumes(),
			"partition_key":   comp.PartitionKey,
			"partition_arity": len(comp.PartitionKey),
			"has_state":       !comp.State.IsZero(),
		}
		if comp.Owns != "" {
			attrs["owns"] = comp.Owns
		}
		g.Nodes = append(g.Nodes, Node{
			ID:    componentID(id),
			Kind:  NodeComponent,
			Attrs: attrs,
		})
	}

	// Kind nodes with health and counts. Every kind named anywhere gets a node,
	// including one only ever consumed — that is what makes starvation visible.
	kindSet := make(map[string]struct{})
	for _, index := range []map[string][]string{inputsOf, producersOf, foldersOf} {
		for kind := range index {
			kindSet[kind] = struct{}{}
		}
	}
	kindNames := make([]string, 0, len(kindSet))
	for k := range kindSet {
		kindNames = append(kindNames, k)
	}
	sort.Strings(kindNames)

	unwired := UnwiredKinds(m)

	for _, kind := range kindNames {
		_, exclusive := exclusiveKinds[kind]
		health := computeKindHealth(kind, inputsOf, foldersOf, producersOf, exclusive, unwired[kind])
		delivery := DeliveryBroadcast
		if exclusive {
			delivery = DeliveryExclusive
		}
		attrs := map[string]any{
			"producer_count":   len(producersOf[kind]),
			"input_count":      len(inputsOf[kind]),
			"state_fold_count": len(foldersOf[kind]),
			"health":           string(health),
			"delivery":         string(delivery),
		}
		if pattern := patterns[kind]; pattern != "" {
			attrs["pattern"] = pattern
			if all := subjects[kind]; len(all) > 1 {
				attrs["subjects"] = all
			}
			if part, ok := declaredPartition[kind]; ok {
				attrs["partition_key"] = part
			} else {
				attrs["partition_key"] = SlotTokens(pattern)
			}
		}
		if patternConflict[kind] {
			attrs["pattern_conflict"] = true
		}
		g.Nodes = append(g.Nodes, Node{
			ID:    kindID(kind),
			Kind:  NodeEventKind,
			Attrs: attrs,
		})
	}

	// Type-definition nodes.
	for _, compID := range compIDs {
		comp := m.Components[compID]
		typeNames := make([]string, 0, len(comp.Types))
		for name := range comp.Types {
			typeNames = append(typeNames, name)
		}
		sort.Strings(typeNames)

		for _, name := range typeNames {
			schema := comp.Types[name]
			attrs := map[string]any{
				"component": compID, // Stored explicitly so exporters don't parse the ID.
			}
			if schema.Deprecated() {
				attrs["deprecated"] = true
			}
			g.Nodes = append(g.Nodes, Node{
				ID:    typeID(compID, name),
				Kind:  NodeType,
				Attrs: attrs,
			})
		}
	}

	// Port edges. Outputs point away from the component, inputs and state
	// events point into it — the direction data actually travels.
	for _, compID := range compIDs {
		comp := m.Components[compID]

		emit := func(slots []Slot, edge EdgeKind, out bool) {
			sorted := make([]Slot, len(slots))
			copy(sorted, slots)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Kind < sorted[j].Kind })
			for _, slot := range sorted {
				attrs := map[string]any{}
				if len(slot.Exposure) > 0 {
					attrs["exposure"] = slot.Exposure
				}
				// The address this end declared. A kind node is one vertex for
				// a kind that a port family puts on one address per instance,
				// so the subject each end actually named is only recoverable
				// from the edge.
				if slot.Pattern != "" {
					attrs["pattern"] = slot.Pattern
				}
				if len(attrs) == 0 {
					attrs = nil
				}
				e := Edge{Kind: edge, Attrs: attrs}
				if out {
					e.From, e.To = componentID(compID), kindID(slot.Kind)
				} else {
					e.From, e.To = kindID(slot.Kind), componentID(compID)
				}
				g.Edges = append(g.Edges, e)
			}
		}

		emit(comp.Outputs, EdgeOutput, true)
		emit(comp.Inputs, EdgeInput, false)
		emit(comp.StateEvents, EdgeStateEvent, false)
	}

	// Edges: component --defines--> type (structural containment).
	for _, compID := range compIDs {
		comp := m.Components[compID]
		typeNames := make([]string, 0, len(comp.Types))
		for name := range comp.Types {
			typeNames = append(typeNames, name)
		}
		sort.Strings(typeNames)

		for _, name := range typeNames {
			g.Edges = append(g.Edges, Edge{
				From: componentID(compID),
				To:   typeID(compID, name),
				Kind: EdgeDefines,
			})
		}
	}

	// Edges: kind --payload--> type and type --refs--> type.
	// Walk schemas to find $refs.
	for _, compID := range compIDs {
		comp := m.Components[compID]

		// Payload edges from slots.
		processSlotSchema := func(slot Slot) {
			walkSchemaNode(slot.Schema, func(n SchemaNode) {
				ref := n.Ref()
				if ref == "" {
					return
				}
				targetID := resolveRefToTypeID(ref, compID, m)
				if targetID == "" {
					return
				}
				g.Edges = append(g.Edges, Edge{
					From: kindID(slot.Kind),
					To:   targetID,
					Kind: EdgePayload,
				})
			})
		}

		for _, slots := range [][]Slot{comp.Outputs, comp.Inputs, comp.StateEvents} {
			for _, slot := range slots {
				processSlotSchema(slot)
			}
		}

		// Type-to-type refs from the component's type definitions.
		for fromName, schema := range comp.Types {
			fromID := typeID(compID, fromName)
			walkSchemaNode(schema, func(n SchemaNode) {
				ref := n.Ref()
				if ref == "" {
					return
				}
				targetID := resolveRefToTypeID(ref, compID, m)
				if targetID == "" || targetID == fromID {
					return
				}
				g.Edges = append(g.Edges, Edge{
					From: fromID,
					To:   targetID,
					Kind: EdgeRefs,
				})
			})
		}
	}

	return g
}

// computeKindHealth determines the health status of an event kind using the
// same logic as Validate. This is the single source of truth for health
// classification.
//
// Observation is inputs plus state events: a kind nobody is triggered by, but
// which some component folds into state, is observed. Only an explicit
// `delivery: exclusive` declaration makes consumer cardinality a health signal,
// and it counts inputs alone — folding an exclusive kind competes with nobody.
//
// unwired comes from UnwiredKinds and is checked last: it asks whether the ends
// meet on the wire, which is only a question once there are ends on both sides.
func computeKindHealth(kind string, inputsOf, foldersOf, producersOf map[string][]string, exclusive, unwired bool) Health {
	producers := len(producersOf[kind])
	inputs := len(inputsOf[kind])
	folders := len(foldersOf[kind])

	if producers == 0 {
		return HealthStarved
	}
	if exclusive && inputs != 1 {
		return HealthAmbiguous
	}
	if inputs == 0 && folders == 0 {
		return HealthOrphan
	}
	if unwired {
		return HealthUnwired
	}
	return HealthOK
}

// kindEnd is one end of a kind: the component that declared it and the address
// that declaration named.
type kindEnd struct {
	component string
	pattern   string
}

// indexKindEnds indexes every declaration of every kind by the address it
// named. Counting by kind name is not enough for any question about whether an
// append reaches an observer — that is decided by the subject — and this is the
// index that keeps health and flows answering from the same facts.
func indexKindEnds(m *Model) (producers, observers map[string][]kindEnd) {
	producers = make(map[string][]kindEnd)
	observers = make(map[string][]kindEnd)
	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		for _, slot := range comp.Outputs {
			producers[slot.Kind] = append(producers[slot.Kind], kindEnd{id, slot.Pattern})
		}
		for _, slots := range [][]Slot{comp.Inputs, comp.StateEvents} {
			for _, slot := range slots {
				observers[slot.Kind] = append(observers[slot.Kind], kindEnd{id, slot.Pattern})
			}
		}
	}
	return producers, observers
}

// UnwiredKinds names every kind that is appended by one component and observed
// by another, yet has no producer/observer pair whose subjects match.
//
// Two guards keep it honest:
//
//   - A pair inside one component is skipped. Folding your own output is the
//     normal idiom and draws no flow, so a component that appends on one
//     address and folds on another is not evidence of a broken wire.
//   - A cross-component pair must exist by name first. A kind whose observers
//     are simply absent from this repository is starved or orphaned, and those
//     already have their own states; this one is for ends that are both here.
//
// It runs over the whole composed set, imported documents included. The rules
// Validate skips for an AsyncAPI document assume the native format's
// conventions; this one assumes only that two declarations state an address in
// the same alphabet, which every format wyrd reads does.
func UnwiredKinds(m *Model) map[string]bool {
	producers, observers := indexKindEnds(m)
	out := make(map[string]bool)
	for kind, appends := range producers {
		crossComponent, matched := false, false
		for _, app := range appends {
			for _, obs := range observers[kind] {
				if obs.component == app.component {
					continue
				}
				crossComponent = true
				if SubjectsMatch(app.pattern, obs.pattern) {
					matched = true
				}
			}
		}
		if crossComponent && !matched {
			out[kind] = true
		}
	}
	return out
}

// resolveRefToTypeID converts a $ref string to a graph type node ID.
func resolveRefToTypeID(ref, currentCompID string, m *Model) string {
	// Local ref: "#/types/Name"
	if name, ok := cutLocalTypeRef(ref); ok {
		if _, exists := m.Components[currentCompID].Types[name]; exists {
			return typeID(currentCompID, name)
		}
		return ""
	}

	// Cross-component ref: "component#/types/Name"
	compID, name, ok := parseCrossComponentRef(ref)
	if !ok {
		return ""
	}
	comp, exists := m.Components[compID]
	if !exists {
		return ""
	}
	if _, exists := comp.Types[name]; !exists {
		return ""
	}
	return typeID(compID, name)
}

// cutLocalTypeRef parses "#/types/Name" and returns the name.
func cutLocalTypeRef(ref string) (string, bool) {
	const prefix = typeRefMarker
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):], true
	}
	return "", false
}

// parseCrossComponentRef parses "component#/types/Name".
func parseCrossComponentRef(ref string) (compID, name string, ok bool) {
	const marker = typeRefMarker
	for i := 0; i < len(ref)-len(marker); i++ {
		if ref[i:i+len(marker)] == marker {
			if i == 0 {
				return "", "", false
			}
			return ref[:i], ref[i+len(marker):], true
		}
	}
	return "", "", false
}

// ID helpers for graph nodes.
// distinctPatterns lists the addresses a kind was declared at, once each, in
// the order the declarations were read.
func distinctPatterns(decls []patternDecl) []string {
	out := make([]string, 0, len(decls))
	seen := make(map[string]bool, len(decls))
	for _, d := range decls {
		if seen[d.Pattern] {
			continue
		}
		seen[d.Pattern] = true
		out = append(out, d.Pattern)
	}
	return out
}

// endpoint is one end of a port edge: the component that declared it and the
// subject it declared. Two ends of the same kind are only connected when their
// subjects match, so the subject travels with the component.
type endpoint struct {
	component string
	pattern   string
}

func edgePattern(e Edge) string {
	pattern, _ := e.Attrs["pattern"].(string)
	return pattern
}

func componentID(id string) string    { return "component:" + id }
func kindID(name string) string       { return "kind:" + name }
func typeID(comp, name string) string { return "type:" + comp + "." + name }

// The inverses, for consumers collapsing the graph back onto bare ids.
func trimComponentID(id string) string { return strings.TrimPrefix(id, "component:") }
func trimKindID(id string) string      { return strings.TrimPrefix(id, "kind:") }

// importedKinds names every kind declared by a component read from an AsyncAPI
// document. It gates the rules that assume the native format's conventions.
func importedKinds(m *Model) map[string]bool {
	out := make(map[string]bool)
	for _, comp := range m.Components {
		if !comp.IsAsyncAPI() {
			continue
		}
		for _, slots := range [][]Slot{comp.Inputs, comp.Outputs, comp.StateEvents} {
			for _, slot := range slots {
				out[slot.Kind] = true
			}
		}
	}
	return out
}

// declaredPartitions collects the partition coordinates a slot states outright,
// keyed by kind. Only imported declarations carry them: the native format
// derives the key from the pattern's {slot} tokens, which is the right rule for
// a service-rooted subject and the wrong one for a wire address.
//
// Where two declarations of one kind disagree, the first in deterministic order
// wins — the same rule the pattern itself follows.
func declaredPartitions(m *Model) map[string][]string {
	out := make(map[string][]string)
	for _, id := range sortedComponentIDs(m) {
		comp := m.Components[id]
		for _, slots := range [][]Slot{comp.Inputs, comp.Outputs, comp.StateEvents} {
			for _, slot := range slots {
				if _, seen := out[slot.Kind]; seen {
					continue
				}
				if part, ok := stringList(slot.Extra["partition"]); ok {
					out[slot.Kind] = part
				}
			}
		}
	}
	return out
}

// stringList reads a []string out of opaque Extra data, which arrives as
// []string from a typed decode and as []any from a YAML one.
func stringList(v any) ([]string, bool) {
	switch typed := v.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

// Append is one output declaration resolved against the model: one component,
// one kind, one address, and the observers that address actually reaches.
//
// It is the finest true statement the model can make about an append, and the
// one every coarser view is built from. Collapsing to the kind loses the
// address, and the address is what decides who hears it: a component that
// publishes one kind on two addresses — the port-family idiom, one address per
// instance — reaches a different observer from each. A view that pairs every
// address with every observer of the kind draws edges the model does not have,
// which is the whole reason this type exists rather than a name-keyed join.
type Append struct {
	// From is the component that appends; Kind is what it appends; Subject
	// is the address it named.
	From, Kind, Subject string
	// Health is the kind node's health, carried so a renderer can mark a
	// broken or unobserved append without a second lookup.
	Health string
	// Reaches is every observer this address reaches, sorted by component
	// then subject. Empty means the append reaches nobody in this model.
	Reaches []Reach
}

// Reach is one observer an append arrives at.
type Reach struct {
	// To is the observing component.
	To string
	// Trigger is true when the kind is an input of the target and drives it,
	// false when the target only folds it into state.
	Trigger bool
	// Subject is the address the observer declared, which is the address that
	// matched — not necessarily the one the producer named, since either side
	// may leave segments open.
	Subject string
}

// BuildAppends resolves every output declaration in the graph against the
// observers whose declared address it matches.
//
// A component observing its own output is skipped: folding the outcome you
// just appended is the normal idiom, so counting it would put nearly every
// component in a relation with itself and say nothing.
//
// The result is sorted by (From, Kind, Subject) so a renderer and a diff over
// one see the same order every time.
func BuildAppends(g *Graph) []Append {
	health := make(map[string]string)
	for _, n := range g.Nodes {
		if n.Kind != NodeEventKind {
			continue
		}
		if h, ok := n.Attrs["health"].(string); ok {
			health[n.ID] = h
		}
	}

	producers := make(map[string][]endpoint) // kind node id -> declaring ends
	triggers := make(map[string][]endpoint)
	folders := make(map[string][]endpoint)
	for _, e := range g.Edges {
		switch e.Kind {
		case EdgeOutput:
			producers[e.To] = append(producers[e.To], endpoint{trimComponentID(e.From), edgePattern(e)})
		case EdgeInput:
			triggers[e.From] = append(triggers[e.From], endpoint{trimComponentID(e.To), edgePattern(e)})
		case EdgeStateEvent:
			folders[e.From] = append(folders[e.From], endpoint{trimComponentID(e.To), edgePattern(e)})
		}
	}

	var appends []Append
	for kindNode, declared := range producers {
		kind := trimKindID(kindNode)

		// An unwired kind has ends on both sides and no matching pair among
		// them (see UnwiredKinds), so the subject test below would drop every
		// candidate and the pair would be reported as nothing at all. Resolve
		// them instead, carrying the kind's health: empty space where a reader
		// expects a relation is the least informative way to report a broken
		// wire, because it looks exactly like a relation the model never had.
		broken := health[kindNode] == string(HealthUnwired)
		connects := func(a, b string) bool { return broken || SubjectsMatch(a, b) }

		seen := make(map[endpoint]bool, len(declared))
		for _, out := range declared {
			if seen[out] {
				continue
			}
			seen[out] = true

			// Trigger beats fold for one (observer, address) pair: it is the
			// stronger relation, and a component that both takes a kind as an
			// input and folds it observes it once.
			reached := make(map[Reach]bool)
			for _, in := range triggers[kindNode] {
				if in.component == out.component || !connects(out.pattern, in.pattern) {
					continue
				}
				reached[Reach{To: in.component, Trigger: true, Subject: in.pattern}] = true
			}
			for _, fold := range folders[kindNode] {
				if fold.component == out.component || !connects(out.pattern, fold.pattern) {
					continue
				}
				r := Reach{To: fold.component, Trigger: false, Subject: fold.pattern}
				if !reached[Reach{To: r.To, Trigger: true, Subject: r.Subject}] {
					reached[r] = true
				}
			}

			app := Append{From: out.component, Kind: kind, Subject: out.pattern, Health: health[kindNode]}
			for r := range reached {
				app.Reaches = append(app.Reaches, r)
			}
			sort.Slice(app.Reaches, func(i, j int) bool {
				if app.Reaches[i].To != app.Reaches[j].To {
					return app.Reaches[i].To < app.Reaches[j].To
				}
				return app.Reaches[i].Subject < app.Reaches[j].Subject
			})
			appends = append(appends, app)
		}
	}

	sort.Slice(appends, func(i, j int) bool {
		if appends[i].From != appends[j].From {
			return appends[i].From < appends[j].From
		}
		if appends[i].Kind != appends[j].Kind {
			return appends[i].Kind < appends[j].Kind
		}
		return appends[i].Subject < appends[j].Subject
	})
	return appends
}

// Flow is one producer-to-observer relation, the bipartite graph collapsed onto
// the components. It is what a reader of an event model actually looks at: who
// appends a kind, and who it reaches.
type Flow struct {
	// From is the component that appends the kind; To is the one that
	// observes it. Both are bare component ids, not node ids.
	From, To string
	// Kind is the event kind travelling the edge.
	Kind string
	// Trigger distinguishes the two ways of observing: true when the kind is
	// an input of the target and drives it, false when the target only folds
	// it into state.
	Trigger bool
	// Health is the kind node's health, carried so a renderer can mark a
	// starved or ambiguous edge without a second lookup.
	Health string
}

// BuildFlows collapses a Graph onto component-to-component edges.
//
// One line per pair and kind, whatever the addresses: a picture wants one edge
// between two boxes, not one per port instance. A reader who needs to know
// which address carried it asks BuildAppends, which is what this collapses.
//
// A component that both takes a kind as an input and folds it appears once, as
// the trigger: it is the stronger relation, and drawing both would put two
// parallel edges between the same pair for one event. A component folding its
// own output draws nothing — it is the normal idiom, so the self-loop would sit
// on nearly every node and say nothing.
//
// The result is sorted by (From, To, Kind), so a renderer and a diff over one
// see the same order every time.
func BuildFlows(g *Graph) []Flow {
	type pair struct{ from, to, kind string }
	trigger := make(map[pair]bool)
	health := make(map[pair]string)
	for _, app := range BuildAppends(g) {
		for _, r := range app.Reaches {
			p := pair{app.From, r.To, app.Kind}
			trigger[p] = trigger[p] || r.Trigger
			health[p] = app.Health
		}
	}

	flows := make([]Flow, 0, len(trigger))
	for p, t := range trigger {
		flows = append(flows, Flow{From: p.from, To: p.to, Kind: p.kind, Trigger: t, Health: health[p]})
	}
	sort.Slice(flows, func(i, j int) bool {
		if flows[i].From != flows[j].From {
			return flows[i].From < flows[j].From
		}
		if flows[i].To != flows[j].To {
			return flows[i].To < flows[j].To
		}
		return flows[i].Kind < flows[j].Kind
	})
	return flows
}
