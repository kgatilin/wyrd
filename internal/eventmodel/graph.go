package eventmodel

import "sort"

// Graph is a bipartite event-model graph projected from a validated Model.
// Nodes are components, kinds, folds, and vocab types. Edges capture the
// event-driven relationships: emission, reception, fold feeding, payload
// typing, and schema references.
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
	//   component:<id>  kind:<name>  fold:<component>.<name>  type:<component>.<vocabName>
	ID string

	// Kind classifies the node.
	Kind NodeKind

	// Attrs holds node-specific attributes. Keys vary by Kind:
	//   component: owns, deprecated
	//   kind: producer_count, consumer_count, health, deprecated, role
	//   fold: pattern
	//   type: deprecated
	Attrs map[string]any
}

// NodeKind classifies event-graph nodes.
type NodeKind string

const (
	NodeComponent NodeKind = "component"
	NodeEventKind NodeKind = "kind"
	NodeFold      NodeKind = "fold"
	NodeType      NodeKind = "type"
)

// Edge is a single directed edge in the event graph.
type Edge struct {
	// From and To are node IDs.
	From, To string

	// Kind classifies the edge.
	Kind EdgeKind

	// Attrs holds edge-specific attributes. Keys vary by Kind:
	//   emits: role
	//   receives: role, exposure
	Attrs map[string]any
}

// EdgeKind classifies event-graph edges.
type EdgeKind string

const (
	EdgeEmits     EdgeKind = "emits"     // component --emits--> kind
	EdgeReceives  EdgeKind = "receives"  // kind --receives--> component
	EdgeFeeds     EdgeKind = "feeds"     // kind --feeds--> fold
	EdgeHeldBy    EdgeKind = "held-by"   // fold --held-by--> component
	EdgePayload   EdgeKind = "payload"   // kind --payload--> type
	EdgeRefs      EdgeKind = "refs"      // type --refs--> type
	EdgeVocab     EdgeKind = "vocab"     // component --vocab--> type (structural contains)
)

// Health classifies a kind's connectivity status.
type Health string

const (
	HealthOK        Health = "ok"
	HealthOrphan    Health = "orphan"    // fact with no consumer
	HealthStarved   Health = "starved"   // no producer
	HealthAmbiguous Health = "ambiguous" // action with multiple receivers
)

// BuildGraph projects a Model into a Graph. The model should be validated
// first; health attributes are derived from the same analysis Validate uses,
// factored through computeHealth.
func BuildGraph(m *Model) *Graph {
	g := &Graph{}

	// Build indexes for health computation (same as Validate).
	receiversOf := make(map[string][]string)
	emittersOf := make(map[string][]string)
	allEmittedFacts := make(map[string]struct{})

	for id, comp := range m.Components {
		for _, slot := range comp.Receives {
			receiversOf[slot.Kind] = append(receiversOf[slot.Kind], id)
		}
		for _, slot := range comp.Emits {
			emittersOf[slot.Kind] = append(emittersOf[slot.Kind], id)
			if slot.Role == RoleFact {
				allEmittedFacts[slot.Kind] = struct{}{}
			}
		}
	}

	// Collect all unique kind names and their roles.
	kindRoles := make(map[string]Role)
	for _, comp := range m.Components {
		for _, slot := range comp.Receives {
			kindRoles[slot.Kind] = slot.Role
		}
		for _, slot := range comp.Emits {
			kindRoles[slot.Kind] = slot.Role
		}
	}

	// Sort component IDs for deterministic output.
	compIDs := make([]string, 0, len(m.Components))
	for id := range m.Components {
		compIDs = append(compIDs, id)
	}
	sort.Strings(compIDs)

	// Component nodes.
	for _, id := range compIDs {
		comp := m.Components[id]
		attrs := make(map[string]any)
		if comp.Owns != "" {
			attrs["owns"] = comp.Owns
		}
		g.Nodes = append(g.Nodes, Node{
			ID:    componentID(id),
			Kind:  NodeComponent,
			Attrs: attrs,
		})
	}

	// Kind nodes with health, counts, and role.
	kindNames := make([]string, 0, len(kindRoles))
	for k := range kindRoles {
		kindNames = append(kindNames, k)
	}
	sort.Strings(kindNames)

	for _, kind := range kindNames {
		role := kindRoles[kind]
		health := computeKindHealth(kind, role, receiversOf, emittersOf, m)
		attrs := map[string]any{
			"producer_count": len(emittersOf[kind]),
			"consumer_count": len(receiversOf[kind]),
			"health":         string(health),
			"role":           string(role),
		}
		g.Nodes = append(g.Nodes, Node{
			ID:    kindID(kind),
			Kind:  NodeEventKind,
			Attrs: attrs,
		})
	}

	// Fold nodes.
	for _, compID := range compIDs {
		comp := m.Components[compID]
		foldNames := make([]string, 0, len(comp.Folds))
		for _, f := range comp.Folds {
			foldNames = append(foldNames, f.Name)
		}
		sort.Strings(foldNames)

		for _, fname := range foldNames {
			var fold Fold
			for _, f := range comp.Folds {
				if f.Name == fname {
					fold = f
					break
				}
			}
			attrs := map[string]any{"pattern": fold.Pattern}
			g.Nodes = append(g.Nodes, Node{
				ID:    foldID(compID, fold.Name),
				Kind:  NodeFold,
				Attrs: attrs,
			})
		}
	}

	// Type (vocab) nodes.
	for _, compID := range compIDs {
		comp := m.Components[compID]
		vocabNames := make([]string, 0, len(comp.Vocab))
		for name := range comp.Vocab {
			vocabNames = append(vocabNames, name)
		}
		sort.Strings(vocabNames)

		for _, name := range vocabNames {
			schema := comp.Vocab[name]
			attrs := make(map[string]any)
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

	// Edges: component --emits--> kind.
	for _, compID := range compIDs {
		comp := m.Components[compID]
		// Sort slots by kind for determinism.
		emitSlots := make([]Slot, len(comp.Emits))
		copy(emitSlots, comp.Emits)
		sort.Slice(emitSlots, func(i, j int) bool {
			return emitSlots[i].Kind < emitSlots[j].Kind
		})

		for _, slot := range emitSlots {
			g.Edges = append(g.Edges, Edge{
				From:  componentID(compID),
				To:    kindID(slot.Kind),
				Kind:  EdgeEmits,
				Attrs: map[string]any{"role": string(slot.Role)},
			})
		}
	}

	// Edges: kind --receives--> component.
	for _, compID := range compIDs {
		comp := m.Components[compID]
		recvSlots := make([]Slot, len(comp.Receives))
		copy(recvSlots, comp.Receives)
		sort.Slice(recvSlots, func(i, j int) bool {
			return recvSlots[i].Kind < recvSlots[j].Kind
		})

		for _, slot := range recvSlots {
			attrs := map[string]any{"role": string(slot.Role)}
			if len(slot.Exposure) > 0 {
				attrs["exposure"] = slot.Exposure
			}
			g.Edges = append(g.Edges, Edge{
				From:  kindID(slot.Kind),
				To:    componentID(compID),
				Kind:  EdgeReceives,
				Attrs: attrs,
			})
		}
	}

	// Edges: kind --feeds--> fold (pattern match).
	for _, compID := range compIDs {
		comp := m.Components[compID]
		for _, fold := range comp.Folds {
			for _, kind := range kindNames {
				if MatchPattern(fold.Pattern, kind) {
					g.Edges = append(g.Edges, Edge{
						From: kindID(kind),
						To:   foldID(compID, fold.Name),
						Kind: EdgeFeeds,
					})
				}
			}
		}
	}

	// Edges: fold --held-by--> component.
	for _, compID := range compIDs {
		comp := m.Components[compID]
		for _, fold := range comp.Folds {
			g.Edges = append(g.Edges, Edge{
				From: foldID(compID, fold.Name),
				To:   componentID(compID),
				Kind: EdgeHeldBy,
			})
		}
	}

	// Edges: component --vocab--> type (structural containment).
	for _, compID := range compIDs {
		comp := m.Components[compID]
		vocabNames := make([]string, 0, len(comp.Vocab))
		for name := range comp.Vocab {
			vocabNames = append(vocabNames, name)
		}
		sort.Strings(vocabNames)

		for _, name := range vocabNames {
			g.Edges = append(g.Edges, Edge{
				From: componentID(compID),
				To:   typeID(compID, name),
				Kind: EdgeVocab,
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

		for _, slot := range comp.Emits {
			processSlotSchema(slot)
		}
		for _, slot := range comp.Receives {
			processSlotSchema(slot)
		}

		// Type refs from vocab.
		for fromName, schema := range comp.Vocab {
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
func computeKindHealth(kind string, role Role, receiversOf, emittersOf map[string][]string, m *Model) Health {
	producers := len(emittersOf[kind])
	consumers := len(receiversOf[kind])

	// Check fold consumers too.
	foldConsumers := 0
	for _, comp := range m.Components {
		for _, fold := range comp.Folds {
			if MatchPattern(fold.Pattern, kind) {
				foldConsumers++
			}
		}
	}

	switch role {
	case RoleFact:
		if producers == 0 {
			return HealthStarved
		}
		if consumers == 0 && foldConsumers == 0 {
			return HealthOrphan
		}
	case RoleAction:
		if producers == 0 {
			return HealthStarved
		}
		if consumers == 0 {
			// Actions must resolve to exactly one receiver.
			return HealthStarved
		}
		if consumers > 1 {
			return HealthAmbiguous
		}
	}
	return HealthOK
}

// resolveRefToTypeID converts a $ref string to a graph type node ID.
func resolveRefToTypeID(ref, currentCompID string, m *Model) string {
	// Local ref: "#/vocab/Name"
	if name, ok := cutLocalVocabRef(ref); ok {
		if _, exists := m.Components[currentCompID].Vocab[name]; exists {
			return typeID(currentCompID, name)
		}
		return ""
	}

	// Cross-component ref: "component#/vocab/Name"
	compID, name, ok := parseCrossComponentRef(ref)
	if !ok {
		return ""
	}
	comp, exists := m.Components[compID]
	if !exists {
		return ""
	}
	if _, exists := comp.Vocab[name]; !exists {
		return ""
	}
	return typeID(compID, name)
}

// cutLocalVocabRef parses "#/vocab/Name" and returns the name.
func cutLocalVocabRef(ref string) (string, bool) {
	const prefix = "#/vocab/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):], true
	}
	return "", false
}

// parseCrossComponentRef parses "component#/vocab/Name".
func parseCrossComponentRef(ref string) (compID, name string, ok bool) {
	const marker = "#/vocab/"
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
func componentID(id string) string      { return "component:" + id }
func kindID(name string) string         { return "kind:" + name }
func foldID(comp, name string) string   { return "fold:" + comp + "." + name }
func typeID(comp, name string) string   { return "type:" + comp + "." + name }
