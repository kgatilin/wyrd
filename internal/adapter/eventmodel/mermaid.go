package eventmodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/eventmodel"
)

// ToMermaid renders an event-model graph as a Mermaid flowchart diagram.
// Components are nodes, event kinds are shown on edges. A solid arrow (-->)
// means the kind is an input of the target — it triggers it; a dashed arrow
// (-.->) means the target only folds the kind into its state. Health issues are
// annotated: orphan kinds have "(orphan)" suffix, starved have "(starved)",
// ambiguous have "(ambiguous)".
//
// A component folding its own output draws no self-loop. It is the normal
// idiom — record the outcome you just appended — so drawing it would put a
// loop on nearly every node and say nothing. The JSON graph keeps the edge.
//
// Components are grouped by their owned namespace when subgraphs would
// improve readability (3+ components with distinct namespaces).
func ToMermaid(g *eventmodel.Graph) string {
	var sb strings.Builder
	sb.WriteString("flowchart LR\n")

	// Collect components and their namespaces.
	components := make(map[string]string) // id -> owns
	kindHealth := make(map[string]string) // kind ID -> health

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
		name    string
		health  string
		trigger bool
	}
	edgeKinds := make(map[edgeKey][]kindInfo)

	outputsByKind := make(map[string][]string) // kind ID -> component IDs that append it
	inputsByKind := make(map[string][]string)  // kind ID -> component IDs it triggers
	stateByKind := make(map[string][]string)   // kind ID -> component IDs that fold it

	for _, e := range g.Edges {
		switch e.Kind {
		case eventmodel.EdgeOutput:
			compID := strings.TrimPrefix(e.From, "component:")
			outputsByKind[e.To] = append(outputsByKind[e.To], compID)
		case eventmodel.EdgeInput:
			compID := strings.TrimPrefix(e.To, "component:")
			inputsByKind[e.From] = append(inputsByKind[e.From], compID)
		case eventmodel.EdgeStateEvent:
			compID := strings.TrimPrefix(e.To, "component:")
			stateByKind[e.From] = append(stateByKind[e.From], compID)
		}
	}

	// Connect producers to observers. A component that both takes a kind as an
	// input and folds it is drawn once, as the trigger — the stronger relation.
	for kindID, producers := range outputsByKind {
		kindName := strings.TrimPrefix(kindID, "kind:")
		health := kindHealth[kindID]

		triggered := make(map[string]bool, len(inputsByKind[kindID]))
		for _, to := range inputsByKind[kindID] {
			triggered[to] = true
		}

		observers := make(map[string]bool, len(triggered)+len(stateByKind[kindID]))
		for to := range triggered {
			observers[to] = true
		}
		for _, to := range stateByKind[kindID] {
			observers[to] = true
		}

		for _, from := range producers {
			for to := range observers {
				if from == to {
					continue
				}
				key := edgeKey{from, to}
				edgeKinds[key] = append(edgeKinds[key], kindInfo{kindName, health, triggered[to]})
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

			if k.trigger {
				// Input: the kind drives the target.
				fmt.Fprintf(&sb, "    %s -->|%s| %s\n", fromID, label, toID)
			} else {
				// State event: the target only folds it.
				fmt.Fprintf(&sb, "    %s -.->|%s| %s\n", fromID, label, toID)
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
