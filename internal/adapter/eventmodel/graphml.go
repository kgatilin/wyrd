// Package eventmodel provides adapters for exporting event-model graphs
// to external formats (GraphML, Mermaid). The adapters depend on the
// internal/eventmodel domain; the domain never depends on adapters.
package eventmodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/eventmodel"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// ToArchmotifGraph converts an event-model graph into an archmotif typed
// graph so that archmotif's existing lenses (components, trophic layers,
// spectral clustering) can analyze event flow choreography.
//
// Node mapping:
//   - component -> archmotif package node (role="component")
//   - kind -> archmotif type node (role="event_kind")
//   - type -> archmotif type node (role="type_def")
//
// Edge mapping:
//   - output -> DependencyDependsOn (component -> kind)
//   - input -> DependencyDependsOn (kind -> component)
//   - state-event -> DependencyDependsOn (kind -> component)
//   - payload -> DependencyUsesType (kind -> type)
//   - refs -> DependencyUsesType (type -> type)
//   - defines -> AddContains (type is contained by component)
func ToArchmotifGraph(g *eventmodel.Graph) (*archmotifimport.Graph, error) {
	b := archmotifimport.NewBuilder()
	createdPkgs := make(map[string]bool)

	// Sort nodes for deterministic output.
	nodes := make([]eventmodel.Node, len(g.Nodes))
	copy(nodes, g.Nodes)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	// Pass 1: Create package nodes first (components and synthetic kind packages).
	for _, n := range nodes {
		if n.Kind == eventmodel.NodeComponent {
			if err := b.AddPackage(n.ID, "", ""); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: component %s: %w", n.ID, err)
			}
			createdPkgs[n.ID] = true
		}
	}

	// Collect synthetic packages needed for kinds.
	for _, n := range nodes {
		if n.Kind == eventmodel.NodeEventKind {
			pkg := kindPackage(n.ID)
			if !createdPkgs[pkg] {
				if err := b.AddPackage(pkg, "", ""); err != nil {
					return nil, fmt.Errorf("eventmodel graphml: kind package %s: %w", pkg, err)
				}
				createdPkgs[pkg] = true
			}
		}
	}

	// Pass 2: Create type nodes (kinds, type definitions).
	for _, n := range nodes {
		switch n.Kind {
		case eventmodel.NodeComponent:
			// Already created above.
		case eventmodel.NodeEventKind:
			pkg := kindPackage(n.ID)
			role := "event_kind"
			if health, ok := n.Attrs["health"].(string); ok && health != string(eventmodel.HealthOK) {
				role = "event_kind_" + health
			}
			if err := b.AddType(n.ID, pkg, false, role); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: kind %s: %w", n.ID, err)
			}
		case eventmodel.NodeType:
			// Type definitions store their owning component in attributes.
			compID := componentID(n.Attrs["component"].(string))
			role := "type_def"
			if deprecated, ok := n.Attrs["deprecated"].(bool); ok && deprecated {
				role = "type_def_deprecated"
			}
			if err := b.AddType(n.ID, compID, false, role); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: type %s: %w", n.ID, err)
			}
		}
	}

	// Sort edges for deterministic output.
	edges := make([]eventmodel.Edge, len(g.Edges))
	copy(edges, g.Edges)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})

	// Pass 2: Create edges.
	for _, e := range edges {
		switch e.Kind {
		case eventmodel.EdgeOutput:
			// Component appends kind: a dependency edge.
			if err := b.AddDependency(e.From, e.To, archmotifimport.DependencyDependsOn); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: output %s->%s: %w", e.From, e.To, err)
			}
		case eventmodel.EdgeInput:
			// Kind triggers component: a dependency edge.
			if err := b.AddDependency(e.From, e.To, archmotifimport.DependencyDependsOn); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: input %s->%s: %w", e.From, e.To, err)
			}
		case eventmodel.EdgeStateEvent:
			// Kind folded into the component's state: a dependency edge.
			if err := b.AddDependency(e.From, e.To, archmotifimport.DependencyDependsOn); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: state-event %s->%s: %w", e.From, e.To, err)
			}
		case eventmodel.EdgePayload:
			// Kind uses type.
			if err := b.AddDependency(e.From, e.To, archmotifimport.DependencyUsesType); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: payload %s->%s: %w", e.From, e.To, err)
			}
		case eventmodel.EdgeRefs:
			// Type refs type.
			if err := b.AddDependency(e.From, e.To, archmotifimport.DependencyUsesType); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: refs %s->%s: %w", e.From, e.To, err)
			}
		case eventmodel.EdgeDefines:
			// Component contains its type definitions: structural containment.
			if err := b.AddContains(e.From, e.To); err != nil {
				return nil, fmt.Errorf("eventmodel graphml: defines %s->%s: %w", e.From, e.To, err)
			}
		}
	}

	return b.Build()
}

// kindPackage extracts a synthetic package ID for a kind node.
// "kind:billing.invoice.issued" -> "kinds:billing"
func kindPackage(kindID string) string {
	// Strip "kind:" prefix.
	name := strings.TrimPrefix(kindID, "kind:")
	// Take first segment as the namespace.
	if idx := strings.Index(name, "."); idx > 0 {
		return "kinds:" + name[:idx]
	}
	return "kinds:" + name
}

// componentID creates the graph node ID for a component.
func componentID(id string) string {
	return "component:" + id
}
