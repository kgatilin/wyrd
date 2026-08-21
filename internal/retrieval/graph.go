package retrieval

import (
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/domain"
)

// EdgeKind represents the type of edge in the code graph.
type EdgeKind string

const (
	EdgeUses       EdgeKind = "uses"
	EdgeReturns    EdgeKind = "returns"
	EdgeImplements EdgeKind = "implements"
	EdgeCalls      EdgeKind = "calls"

	// EdgeContains ties a type to the methods declared on it. It is
	// structural, not behavioural: the diffusion deliberately leaves it out
	// (DefaultParams gives it no weight, and diffusionEdges reads that as "no
	// edge"), or a type would inherit the relevance of everything its methods
	// touch and turn into a hub the damping then has to fight. Traversals that
	// take all edges — expand, the induced edge list of an answer — do walk it,
	// which is how the aggregate view survives moving calls onto the methods.
	EdgeContains EdgeKind = "contains"
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
	b := &edgeBuilder{g: g, seen: make(map[string]bool)}
	for _, model := range models {
		b.fromModel(model)
	}

	return nodes, g
}

// edgeBuilder accumulates the graph's edges. It carries the dedup set because
// the same relation is reported at more than one granularity — the Go reader
// emits a struct's method dependencies from both the struct and the method —
// and a duplicate is not harmless: diffusionEdges hands every copy to the
// diffusion, which sums their weights and would quietly double the strength of
// whichever relation happened to be reported twice.
type edgeBuilder struct {
	g    *Graph
	seen map[string]bool
}

// fromModel extracts edges from a single PackageModel.
func (b *edgeBuilder) fromModel(model domain.PackageModel) {
	// Dependencies (uses, returns, implements)
	for _, dep := range model.Dependencies {
		if dep.To.External {
			continue // Skip external dependencies
		}
		kind := dependencyKindToEdgeKind(dep.Kind)
		if kind == "" {
			continue
		}
		b.add(symbolRefToNodeID(dep.From), symbolRefToNodeID(dep.To), kind)
	}

	// Implementations
	for _, impl := range model.Implementations {
		if impl.Concrete.External || impl.Interface.External {
			continue
		}
		b.add(symbolRefToNodeID(impl.Concrete), symbolRefToNodeID(impl.Interface), EdgeImplements)
	}

	// Call edges from functions
	for _, fn := range model.Functions {
		fromID := nodeID(model.Path, fn.Name)
		for _, call := range fn.Calls {
			if call.To.External {
				continue
			}
			b.add(fromID, symbolRefToNodeID(call.To), EdgeCalls)
		}
	}

	// Call edges from struct methods. They originate at the method, not at the
	// receiver: a method is a node of its own, and attributing its calls to the
	// type answers "who calls this" with the wrong symbol. The containment edge
	// is what keeps the type reachable from them.
	for _, s := range model.Structs {
		structID := nodeID(model.Path, s.Name)
		for _, method := range s.Methods {
			methodID := nodeID(model.Path, s.Name+"."+method.Name)
			b.add(structID, methodID, EdgeContains)
			for _, call := range method.Calls {
				if call.To.External {
					continue
				}
				b.add(methodID, symbolRefToNodeID(call.To), EdgeCalls)
			}
		}
	}

	// Call edges from interface methods (if any). Interface methods have no
	// body and so no node; whatever the reader recorded on them folds onto the
	// interface, which is where such a relation belongs anyway.
	for _, iface := range model.Interfaces {
		ifaceID := nodeID(model.Path, iface.Name)
		for _, method := range iface.Methods {
			for _, call := range method.Calls {
				if call.To.External {
					continue
				}
				b.add(ifaceID, symbolRefToNodeID(call.To), EdgeCalls)
			}
		}
	}
}

// add records one edge, after resolving both endpoints onto nodes that exist
// and dropping the duplicates.
func (b *edgeBuilder) add(from, to string, kind EdgeKind) {
	from, to = b.resolve(from), b.resolve(to)
	if from == "" || to == "" || from == to {
		return
	}
	key := from + "|" + to + "|" + string(kind)
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	addEdge(b.g, from, to, kind)
}

// resolve maps an edge endpoint onto a node the graph actually holds.
//
// The reader reports some relations against a member — "Iface.Method uses T" —
// and not every member becomes a node (interface methods have no body to
// search). Such an endpoint folds onto its owner, which is the granularity the
// relation was also reported at, so the fold produces a duplicate rather than a
// new claim. An endpoint that resolves to nothing is dropped: an edge into a
// symbol the graph cannot name is a region slot spent on an answer no caller
// can read.
func (b *edgeBuilder) resolve(id string) string {
	if id == "" {
		return ""
	}
	if _, ok := b.g.NodesByID[id]; ok {
		return id
	}
	if dot := strings.LastIndex(id, "."); dot > 0 {
		owner := id[:dot]
		if _, ok := b.g.NodesByID[owner]; ok {
			return owner
		}
	}
	return ""
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
