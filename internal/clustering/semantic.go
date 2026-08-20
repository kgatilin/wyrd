package clustering

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// Vectors looks up an embedding by retrieval node id. Declared here, on the
// consumer side, so the analyses depend on the lookup and not on the retrieval
// service that happens to provide it.
type Vectors interface {
	Vector(id string) ([]float32, bool)
}

// SemanticNode is one selected symbol with the embedding that puts it on the
// semantic side of an analysis.
type SemanticNode struct {
	ArchmotifID string
	RetrievalID string
	Vec         []float32
}

// RetrievalID converts an archmotif node id to a retrieval index key. Archmotif
// ids carry a kind prefix (type:, fn:, method:, field:, pkg:, file:); retrieval
// ids are "pkg.Symbol" and, for a method, "pkg.Receiver.Method" — the same
// string archmotif prefixes with "method:". The kinds that map to "" are the
// ones retrieval has no node for.
func RetrievalID(amid string) string {
	switch {
	case strings.HasPrefix(amid, "type:"):
		return strings.TrimPrefix(amid, "type:")
	case strings.HasPrefix(amid, "fn:"):
		return strings.TrimPrefix(amid, "fn:")
	case strings.HasPrefix(amid, "method:"):
		return strings.TrimPrefix(amid, "method:")
	}
	// field:, pkg: and file: nodes are not indexed for retrieval.
	return ""
}

// SemanticNodes keeps the selected ids that carry an embedding, in the order
// given, and reports how many were dropped for want of one. Both sides of a
// two-way comparison run on the surviving set: comparing partitions over
// different node sets measures nothing.
func SemanticNodes(archmotifIDs []string, vectors Vectors) (nodes []SemanticNode, dropped int) {
	nodes = make([]SemanticNode, 0, len(archmotifIDs))
	for _, amid := range archmotifIDs {
		rid := RetrievalID(amid)
		if rid == "" {
			dropped++
			continue
		}
		vec, ok := vectors.Vector(rid)
		if !ok || len(vec) == 0 {
			dropped++
			continue
		}
		nodes = append(nodes, SemanticNode{ArchmotifID: amid, RetrievalID: rid, Vec: vec})
	}
	return nodes, dropped
}

// SemanticGraph builds a graph whose edges are embedding similarity: every node
// gets an edge to each of its knn most similar neighbours above minSim. It is
// the semantic counterpart of the dependency graph — clustering it groups code
// by what it is about rather than by what calls what. Returns the graph and the
// edge count.
//
// The similarity pass is O(n²) in the node count and there is no way around
// that for an exact kNN graph, so the constant is where the time goes. Three
// things keep it down: every vector is scaled to unit length once, which turns
// each cosine into a plain dot product; each unordered pair is measured once
// and offered to both endpoints, since similarity is symmetric; and each node
// keeps a bounded top-k list rather than every candidate followed by a sort.
func SemanticGraph(nodes []SemanticNode, knn int, minSim float64) (*archmotifimport.Graph, int, error) {
	b := archmotifimport.NewBuilder()

	// Parent package nodes must exist before a type or function can name one.
	pkgPaths := map[string]bool{}
	for _, n := range nodes {
		pkgPaths[PackagePathOf(n.ArchmotifID)] = true
	}
	sortedPkgs := make([]string, 0, len(pkgPaths))
	for p := range pkgPaths {
		sortedPkgs = append(sortedPkgs, p)
	}
	sort.Strings(sortedPkgs)
	for _, pkgPath := range sortedPkgs {
		if err := b.AddPackage("pkg:"+pkgPath, "", ""); err != nil {
			return nil, 0, fmt.Errorf("adding package %s: %w", pkgPath, err)
		}
	}

	for _, n := range nodes {
		pkgID := "pkg:" + PackagePathOf(n.ArchmotifID)
		if strings.HasPrefix(n.ArchmotifID, "fn:") {
			if err := b.AddFunction(n.ArchmotifID, pkgID); err != nil {
				return nil, 0, fmt.Errorf("adding node %s: %w", n.ArchmotifID, err)
			}
			continue
		}
		// type:, method: and field: all become type nodes; the semantic graph
		// only needs them to exist as endpoints.
		if err := b.AddType(n.ArchmotifID, pkgID, false, ""); err != nil {
			return nil, 0, fmt.Errorf("adding node %s: %w", n.ArchmotifID, err)
		}
	}

	edgeCount := 0
	for i, neighbours := range nearestNeighbours(nodes, knn, minSim) {
		for _, nb := range neighbours {
			// "references" is archmotifimport's kind for "these two are
			// related"; the underlying meaning here is semantic proximity.
			// The edge is directed, and the spectral view symmetrizes it.
			if err := b.AddDependency(nodes[i].ArchmotifID, nodes[nb.index].ArchmotifID, archmotifimport.DependencyReferences); err != nil {
				continue
			}
			edgeCount++
		}
	}

	g, err := b.Build()
	return g, edgeCount, err
}

// neighbour is one entry of a node's top-k list.
type neighbour struct {
	index int
	sim   float64
}

// nearestNeighbours returns each node's knn most similar neighbours, best
// first. Ties go to the lower node index, so the same node set always produces
// the same graph — a partition that reshuffled between two identical calls
// would be unreadable as a diff.
func nearestNeighbours(nodes []SemanticNode, knn int, minSim float64) [][]neighbour {
	n := len(nodes)
	out := make([][]neighbour, n)
	if n == 0 || knn < 1 {
		return out
	}
	for i := range out {
		out[i] = make([]neighbour, 0, knn)
	}

	unit := make([][]float32, n)
	for i, node := range nodes {
		unit[i] = unitVector(node.Vec)
	}

	// Each unordered pair once: sim(i,j) == sim(j,i), so measuring both is
	// half the work thrown away. Candidates reach a node in increasing index
	// order either way, which is what makes the tie-break deterministic.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			sim := dot(unit[i], unit[j])
			if sim < minSim {
				continue
			}
			out[i] = offer(out[i], neighbour{index: j, sim: sim}, knn)
			out[j] = offer(out[j], neighbour{index: i, sim: sim}, knn)
		}
	}
	return out
}

// offer inserts cand into a descending-by-similarity list capped at limit,
// keeping the incumbent on a tie (candidates arrive in increasing index order,
// so the incumbent is the lower index).
func offer(list []neighbour, cand neighbour, limit int) []neighbour {
	if len(list) == limit && cand.sim <= list[limit-1].sim {
		return list
	}
	pos := sort.Search(len(list), func(i int) bool { return list[i].sim < cand.sim })
	if len(list) < limit {
		list = append(list, neighbour{})
	}
	copy(list[pos+1:], list[pos:])
	list[pos] = cand
	return list
}

// unitVector scales v to length 1 so a cosine similarity becomes a dot product.
// A zero vector stays zero, which scores 0 against everything — the same answer
// the cosine gives when a norm is zero.
func unitVector(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return make([]float32, len(v))
	}
	inv := float32(1 / math.Sqrt(norm))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

// dot is the cosine similarity of two unit vectors. It accumulates in float64
// because an embedding runs to a thousand dimensions and a float32 sum would
// blur near-ties into an arbitrary order. Vectors of different lengths cannot
// be compared and score 0.
func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
