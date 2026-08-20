package clustering

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func TestRetrievalID(t *testing.T) {
	cases := []struct {
		amid string
		want string
	}{
		{"type:internal/domain.PackageModel", "internal/domain.PackageModel"},
		{"fn:internal/service.Generate", "internal/service.Generate"},
		{"method:internal/domain.State.Method", "internal/domain.State.Method"},
		{"field:internal/domain.State.Name", ""}, // fields not indexed
		{"pkg:internal/domain", ""},              // packages not indexed
		{"file:internal/domain/model.go", ""},    // files not indexed
	}
	for _, tc := range cases {
		if got := RetrievalID(tc.amid); got != tc.want {
			t.Errorf("RetrievalID(%q) = %q, want %q", tc.amid, got, tc.want)
		}
	}
}

// SemanticGraph must register a symbol's package before the symbol, or the
// builder rejects the node with "unknown packageID".
func TestSemanticGraph_RegistersPackages(t *testing.T) {
	nodes := []SemanticNode{
		{ArchmotifID: "type:internal/domain.PackageModel", RetrievalID: "internal/domain.PackageModel", Vec: []float32{1.0, 0.0, 0.0, 0.0}},
		{ArchmotifID: "type:internal/domain.InterfaceDef", RetrievalID: "internal/domain.InterfaceDef", Vec: []float32{0.9, 0.1, 0.0, 0.0}},
		{ArchmotifID: "fn:internal/service.Generate", RetrievalID: "internal/service.Generate", Vec: []float32{0.0, 1.0, 0.0, 0.0}},
		{ArchmotifID: "fn:internal/service.NewService", RetrievalID: "internal/service.NewService", Vec: []float32{0.0, 0.9, 0.1, 0.0}},
	}

	graph, edgeCount, err := SemanticGraph(nodes, 2, 0.0)
	if err != nil {
		t.Fatalf("SemanticGraph failed: %v", err)
	}
	if graph == nil {
		t.Fatal("graph is nil")
	}
	// 4 nodes × 2 neighbours each.
	if edgeCount != 8 {
		t.Errorf("expected 8 edges, got %d", edgeCount)
	}
	// 4 symbol nodes + the 2 package nodes registered ahead of them.
	if graph.NodeCount() != 6 {
		t.Errorf("expected 6 nodes (4 symbols + 2 packages), got %d", graph.NodeCount())
	}
	if graph.EdgeCount() == 0 {
		t.Error("graph has no edges — semantic edges were not added")
	}
}

// The pair-symmetric, bounded-top-k neighbour search must return exactly what a
// full sort per node would: the knn highest similarities, best first, ties to
// the lower index. This is the correctness half of the speed change.
func TestNearestNeighbours_MatchesFullSort(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	nodes := make([]SemanticNode, 40)
	for i := range nodes {
		vec := make([]float32, 12)
		for j := range vec {
			vec[j] = float32(rng.NormFloat64())
		}
		nodes[i] = SemanticNode{ArchmotifID: fmt.Sprintf("fn:p.N%d", i), Vec: vec}
	}

	for _, knn := range []int{1, 3, 8} {
		got := nearestNeighbours(nodes, knn, 0.0)
		want := referenceNeighbours(nodes, knn, 0.0)
		for i := range nodes {
			if len(got[i]) != len(want[i]) {
				t.Fatalf("knn=%d node %d: %d neighbours, want %d", knn, i, len(got[i]), len(want[i]))
			}
			for j := range got[i] {
				if got[i][j].index != want[i][j].index {
					t.Fatalf("knn=%d node %d rank %d: index %d, want %d", knn, i, j, got[i][j].index, want[i][j].index)
				}
				// Scaling to unit length up front rounds each component back to
				// float32, so the similarity lands a few ulps from the divide-
				// at-the-end cosine. The tolerance is that rounding, not slack.
				if math.Abs(got[i][j].sim-want[i][j].sim) > 1e-6 {
					t.Fatalf("knn=%d node %d rank %d: sim %v, want %v", knn, i, j, got[i][j].sim, want[i][j].sim)
				}
			}
		}
	}
}

// A minSim floor drops the pairs below it entirely, so a node can come back
// with fewer than knn neighbours.
func TestNearestNeighbours_HonoursMinSim(t *testing.T) {
	nodes := []SemanticNode{
		{ArchmotifID: "fn:p.A", Vec: []float32{1, 0}},
		{ArchmotifID: "fn:p.B", Vec: []float32{0.99, 0.14}},
		{ArchmotifID: "fn:p.C", Vec: []float32{0, 1}},
	}
	got := nearestNeighbours(nodes, 2, 0.5)
	if len(got[0]) != 1 || got[0][0].index != 1 {
		t.Fatalf("A's neighbours = %v, want only B above the floor", got[0])
	}
	if len(got[2]) != 0 {
		t.Fatalf("C's neighbours = %v, want none above the floor", got[2])
	}
}

// The kNN build is O(n²) in the node count whatever it does, so the constant is
// the whole story. These two benchmark the same answer computed the two ways —
// the bounded pair-symmetric search against the all-pairs-then-sort one — at
// the shape a real repository has (a thousand symbols, a 1024-dimension
// embedding).
func BenchmarkNearestNeighbours(b *testing.B) {
	nodes := benchNodes(1000, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nearestNeighbours(nodes, 8, 0)
	}
}

func BenchmarkNearestNeighboursAllPairsSort(b *testing.B) {
	nodes := benchNodes(1000, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		referenceNeighbours(nodes, 8, 0)
	}
}

func benchNodes(n, dim int) []SemanticNode {
	rng := rand.New(rand.NewSource(11))
	nodes := make([]SemanticNode, n)
	for i := range nodes {
		vec := make([]float32, dim)
		for j := range vec {
			vec[j] = float32(rng.NormFloat64())
		}
		nodes[i] = SemanticNode{ArchmotifID: fmt.Sprintf("fn:p.N%d", i), Vec: vec}
	}
	return nodes
}

// referenceNeighbours is the straightforward all-pairs-then-sort answer the
// bounded search must agree with.
func referenceNeighbours(nodes []SemanticNode, knn int, minSim float64) [][]neighbour {
	out := make([][]neighbour, len(nodes))
	for i := range nodes {
		var cands []neighbour
		for j := range nodes {
			if i == j {
				continue
			}
			sim := cosine(nodes[i].Vec, nodes[j].Vec)
			if sim < minSim {
				continue
			}
			cands = append(cands, neighbour{index: j, sim: sim})
		}
		// Stable ordering: similarity descending, index ascending on a tie.
		for a := 1; a < len(cands); a++ {
			for b := a; b > 0 && cands[b].sim > cands[b-1].sim; b-- {
				cands[b], cands[b-1] = cands[b-1], cands[b]
			}
		}
		if len(cands) > knn {
			cands = cands[:knn]
		}
		out[i] = cands
	}
	return out
}

// cosine is the textbook similarity the unit-vector dot product replaced.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dotAB, normA, normB float64
	for i := range a {
		dotAB += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotAB / (math.Sqrt(normA) * math.Sqrt(normB))
}
