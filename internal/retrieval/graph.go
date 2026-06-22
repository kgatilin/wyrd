package retrieval

import (
	"sort"

	"github.com/kgatilin/archai/internal/domain"
)

// EdgeKind represents the type of edge in the code graph.
type EdgeKind string

const (
	EdgeUses       EdgeKind = "uses"
	EdgeReturns    EdgeKind = "returns"
	EdgeImplements EdgeKind = "implements"
	EdgeCalls      EdgeKind = "calls"
)

// Edge represents a directed relationship between two nodes in the code graph.
type Edge struct {
	From string   // Source node ID
	To   string   // Target node ID
	Kind EdgeKind // Edge type
}

// Graph holds the adjacency map for code navigation.
// Keys are node IDs, values are slices of edges originating from that node.
type Graph struct {
	// Outgoing maps each node ID to its outgoing edges.
	Outgoing map[string][]Edge

	// Incoming maps each node ID to its incoming edges (for reverse traversal).
	Incoming map[string][]Edge

	// NodesByID maps node ID to Node for quick lookup.
	NodesByID map[string]Node
}

// BuildGraph constructs both nodes and their edge adjacency from domain models.
// Node IDs follow the same scheme as BuildNodes: {PackagePath}.{SymbolName}.
func BuildGraph(models []domain.PackageModel) ([]Node, *Graph) {
	nodes := BuildNodes(models)

	g := &Graph{
		Outgoing:  make(map[string][]Edge),
		Incoming:  make(map[string][]Edge),
		NodesByID: make(map[string]Node),
	}

	// Index nodes by ID
	for _, n := range nodes {
		g.NodesByID[n.ID] = n
	}

	// Build edges from domain model
	for _, model := range models {
		buildEdgesFromModel(g, model)
	}

	return nodes, g
}

// buildEdgesFromModel extracts edges from a single PackageModel.
func buildEdgesFromModel(g *Graph, model domain.PackageModel) {
	// Dependencies (uses, returns, implements)
	for _, dep := range model.Dependencies {
		if dep.To.External {
			continue // Skip external dependencies
		}
		fromID := symbolRefToNodeID(dep.From)
		toID := symbolRefToNodeID(dep.To)
		if fromID == "" || toID == "" {
			continue
		}

		kind := dependencyKindToEdgeKind(dep.Kind)
		if kind == "" {
			continue
		}

		addEdge(g, fromID, toID, kind)
	}

	// Implementations
	for _, impl := range model.Implementations {
		if impl.Concrete.External || impl.Interface.External {
			continue
		}
		fromID := symbolRefToNodeID(impl.Concrete)
		toID := symbolRefToNodeID(impl.Interface)
		if fromID == "" || toID == "" {
			continue
		}
		addEdge(g, fromID, toID, EdgeImplements)
	}

	// Call edges from functions
	for _, fn := range model.Functions {
		fromID := nodeID(model.Path, fn.Name)
		for _, call := range fn.Calls {
			if call.To.External {
				continue
			}
			toID := symbolRefToNodeID(call.To)
			if toID == "" {
				continue
			}
			addEdge(g, fromID, toID, EdgeCalls)
		}
	}

	// Call edges from struct methods
	// For methods, edge originates from the struct (our nodes are symbol-level)
	for _, s := range model.Structs {
		structID := nodeID(model.Path, s.Name)
		for _, method := range s.Methods {
			for _, call := range method.Calls {
				if call.To.External {
					continue
				}
				toID := symbolRefToNodeID(call.To)
				if toID == "" {
					continue
				}
				addEdge(g, structID, toID, EdgeCalls)
			}
		}
	}

	// Call edges from interface methods (if any)
	for _, iface := range model.Interfaces {
		ifaceID := nodeID(model.Path, iface.Name)
		for _, method := range iface.Methods {
			for _, call := range method.Calls {
				if call.To.External {
					continue
				}
				toID := symbolRefToNodeID(call.To)
				if toID == "" {
					continue
				}
				addEdge(g, ifaceID, toID, EdgeCalls)
			}
		}
	}
}

// symbolRefToNodeID converts a SymbolRef to a node ID.
func symbolRefToNodeID(ref domain.SymbolRef) string {
	if ref.Package == "" || ref.Symbol == "" {
		return ""
	}
	return ref.Package + "." + ref.Symbol
}

// dependencyKindToEdgeKind converts domain.DependencyKind to EdgeKind.
func dependencyKindToEdgeKind(kind domain.DependencyKind) EdgeKind {
	switch kind {
	case domain.DependencyUses:
		return EdgeUses
	case domain.DependencyReturns:
		return EdgeReturns
	case domain.DependencyImplements:
		return EdgeImplements
	default:
		return ""
	}
}

// addEdge adds an edge to both outgoing and incoming maps.
func addEdge(g *Graph, from, to string, kind EdgeKind) {
	edge := Edge{From: from, To: to, Kind: kind}
	g.Outgoing[from] = append(g.Outgoing[from], edge)
	g.Incoming[to] = append(g.Incoming[to], edge)
}

// NeighborNodes returns the node IDs reachable from the given IDs within hops steps.
// If edgeKinds is non-empty, only edges of those kinds are traversed.
//
// maxNodes caps the size of the returned set: once that many nodes have been
// visited, expansion stops. 0 means uncapped. The cap guards against hub nodes
// whose 1-hop neighbourhood is effectively the whole graph; the frontier is
// sorted each step so the truncation is deterministic.
func (g *Graph) NeighborNodes(startIDs []string, hops int, edgeKinds []EdgeKind, maxNodes int) map[string]bool {
	if hops < 1 {
		hops = 1
	}

	kindSet := make(map[EdgeKind]bool)
	for _, k := range edgeKinds {
		kindSet[k] = true
	}
	filterByKind := len(kindSet) > 0

	visited := make(map[string]bool)
	for _, id := range startIDs {
		visited[id] = true
	}
	capped := func() bool { return maxNodes > 0 && len(visited) >= maxNodes }

	frontier := make([]string, len(startIDs))
	copy(frontier, startIDs)

	for step := 0; step < hops && len(frontier) > 0 && !capped(); step++ {
		sort.Strings(frontier) // deterministic order for which neighbours fill the budget
		var next []string
		for _, id := range frontier {
			if capped() {
				break
			}
			// Outgoing edges
			for _, edge := range g.Outgoing[id] {
				if capped() {
					break
				}
				if filterByKind && !kindSet[edge.Kind] {
					continue
				}
				if !visited[edge.To] {
					visited[edge.To] = true
					next = append(next, edge.To)
				}
			}
			// Incoming edges (bidirectional traversal)
			for _, edge := range g.Incoming[id] {
				if capped() {
					break
				}
				if filterByKind && !kindSet[edge.Kind] {
					continue
				}
				if !visited[edge.From] {
					visited[edge.From] = true
					next = append(next, edge.From)
				}
			}
		}
		frontier = next
	}

	return visited
}

// InducedEdges returns all edges whose both endpoints are in the given node set.
func (g *Graph) InducedEdges(nodeSet map[string]bool) []Edge {
	var edges []Edge
	seen := make(map[string]bool)

	for id := range nodeSet {
		for _, edge := range g.Outgoing[id] {
			if !nodeSet[edge.To] {
				continue
			}
			key := edge.From + "|" + edge.To + "|" + string(edge.Kind)
			if !seen[key] {
				seen[key] = true
				edges = append(edges, edge)
			}
		}
	}

	return edges
}
