package eventmodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/eventmodel"
)

// ToMermaid renders an event-model graph as a Mermaid flowchart diagram.
// Components are nodes, event kinds are shown on edges. Facts use solid
// arrows (-->) while actions use dashed arrows (-.->). Health issues are
// annotated: orphan kinds have "(orphan)" suffix, starved have "(starved)",
// ambiguous have "(ambiguous)".
//
// Components are grouped by their owned namespace when subgraphs would
// improve readability (3+ components with distinct namespaces).
func ToMermaid(g *eventmodel.Graph) string {
	var sb strings.Builder
	sb.WriteString("flowchart LR\n")

	// Collect components and their namespaces.
	components := make(map[string]string)  // id -> owns
	kindHealth := make(map[string]string)  // kind ID -> health
	kindRole := make(map[string]string)    // kind ID -> role

	for _, n := range g.Nodes {
		switch n.Kind {
		case eventmodel.NodeComponent:
			owns := ""
			if v, ok := n.Attrs["owns"].(string); ok {
				owns = v
			}
			// Strip "component:" prefix for display.
			id := strings.TrimPrefix(n.ID, "component:")
			components[id] = owns
		case eventmodel.NodeEventKind:
			if h, ok := n.Attrs["health"].(string); ok {
				kindHealth[n.ID] = h
			}
			if r, ok := n.Attrs["role"].(string); ok {
				kindRole[n.ID] = r
			}
		}
	}

	// Sort components for deterministic output.
	compIDs := make([]string, 0, len(components))
	for id := range components {
		compIDs = append(compIDs, id)
	}
	sort.Strings(compIDs)

	// Group components by namespace.
	byNamespace := make(map[string][]string)
	for _, id := range compIDs {
		ns := components[id]
		if ns == "" {
			ns = "__none__"
		}
		byNamespace[ns] = append(byNamespace[ns], id)
	}

	// Decide whether to use subgraphs (3+ distinct namespaces).
	useSubgraphs := len(byNamespace) >= 3

	// Emit component nodes, optionally grouped by namespace.
	if useSubgraphs {
		nsList := make([]string, 0, len(byNamespace))
		for ns := range byNamespace {
			nsList = append(nsList, ns)
		}
		sort.Strings(nsList)

		for _, ns := range nsList {
			ids := byNamespace[ns]
			sort.Strings(ids)
			label := ns
			if ns == "__none__" {
				label = "unowned"
			}
			fmt.Fprintf(&sb, "    subgraph %s[%s]\n", subgraphID(ns), mermaidLabel(label))
			for _, id := range ids {
				fmt.Fprintf(&sb, "        %s[%s]\n", mermaidID(id), mermaidLabel(id))
			}
			sb.WriteString("    end\n")
		}
	} else {
		for _, id := range compIDs {
			fmt.Fprintf(&sb, "    %s[%s]\n", mermaidID(id), mermaidLabel(id))
		}
	}

	// Build edge map: from component -> to component -> list of kinds.
	type edgeKey struct{ from, to string }
	type kindInfo struct {
		name   string
		role   string
		health string
	}
	edgeKinds := make(map[edgeKey][]kindInfo)

	// Process emits edges to find what each component produces.
	emitsByKind := make(map[string][]string)  // kind ID -> component IDs that emit it
	receivesByKind := make(map[string][]string) // kind ID -> component IDs that receive it

	for _, e := range g.Edges {
		switch e.Kind {
		case eventmodel.EdgeEmits:
			compID := strings.TrimPrefix(e.From, "component:")
			emitsByKind[e.To] = append(emitsByKind[e.To], compID)
		case eventmodel.EdgeReceives:
			compID := strings.TrimPrefix(e.To, "component:")
			receivesByKind[e.From] = append(receivesByKind[e.From], compID)
		}
	}

	// Create edges between emitters and receivers.
	for kindID, emitters := range emitsByKind {
		receivers := receivesByKind[kindID]
		kindName := strings.TrimPrefix(kindID, "kind:")
		role := kindRole[kindID]
		health := kindHealth[kindID]

		for _, from := range emitters {
			for _, to := range receivers {
				key := edgeKey{from, to}
				edgeKinds[key] = append(edgeKinds[key], kindInfo{kindName, role, health})
			}
		}
	}

	// Sort edges for determinism.
	edgeKeys := make([]edgeKey, 0, len(edgeKinds))
	for k := range edgeKinds {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Slice(edgeKeys, func(i, j int) bool {
		if edgeKeys[i].from != edgeKeys[j].from {
			return edgeKeys[i].from < edgeKeys[j].from
		}
		return edgeKeys[i].to < edgeKeys[j].to
	})

	// Emit edges.
	for _, key := range edgeKeys {
		kinds := edgeKinds[key]
		sort.Slice(kinds, func(i, j int) bool {
			return kinds[i].name < kinds[j].name
		})

		for _, k := range kinds {
			label := mermaidLabel(shortKindName(k.name))
			if k.health != "" && k.health != string(eventmodel.HealthOK) {
				label += " (" + k.health + ")"
			}

			fromID := mermaidID(key.from)
			toID := mermaidID(key.to)

			if k.role == string(eventmodel.RoleAction) {
				// Action: dashed arrow.
				fmt.Fprintf(&sb, "    %s -.->|%s| %s\n", fromID, label, toID)
			} else {
				// Fact: solid arrow.
				fmt.Fprintf(&sb, "    %s -->|%s| %s\n", fromID, label, toID)
			}
		}
	}

	return sb.String()
}

// shortKindName extracts the last two segments of a kind name for brevity.
// "billing.invoice.issued" -> "invoice.issued"
func shortKindName(kind string) string {
	parts := strings.Split(kind, ".")
	if len(parts) <= 2 {
		return kind
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// mermaidID creates a safe Mermaid node ID from a string.
func mermaidID(s string) string {
	// Replace characters that could cause issues.
	r := strings.NewReplacer(
		".", "_",
		"-", "_",
		":", "_",
		" ", "_",
	)
	return r.Replace(s)
}

// subgraphID creates a Mermaid subgraph ID that cannot collide with node IDs.
// Mermaid treats subgraph IDs as node IDs, so we prefix with "ns_" to ensure
// a subgraph named "billing" doesn't collide with a component node "billing".
func subgraphID(ns string) string {
	return "ns_" + mermaidID(ns)
}

// mermaidLabel quotes a label when it contains special characters.
func mermaidLabel(s string) string {
	if s == "" {
		return `""`
	}
	// If it contains special characters, quote it.
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		default:
			return `"` + strings.ReplaceAll(s, `"`, `#quot;`) + `"`
		}
	}
	return s
}
