package retrieval

import "testing"

// rankingGraph builds a small directed graph for the diffusion-ranking tests:
//   A→B→C→A (3-cycle) and D→A (D points into the cycle).
func rankingGraph() *Graph {
	mkNode := func(id string) Node { return Node{ID: id, Kind: "func", Package: "pkg", Name: id} }
	return &Graph{
		Outgoing: map[string][]Edge{
			"A": {{From: "A", To: "B", Kind: EdgeCalls}},
			"B": {{From: "B", To: "C", Kind: EdgeCalls}},
			"C": {{From: "C", To: "A", Kind: EdgeCalls}},
			"D": {{From: "D", To: "A", Kind: EdgeCalls}},
		},
		Incoming: map[string][]Edge{
			"A": {{From: "C", To: "A", Kind: EdgeCalls}, {From: "D", To: "A", Kind: EdgeCalls}},
			"B": {{From: "A", To: "B", Kind: EdgeCalls}},
			"C": {{From: "B", To: "C", Kind: EdgeCalls}},
		},
		NodesByID: map[string]Node{"A": mkNode("A"), "B": mkNode("B"), "C": mkNode("C"), "D": mkNode("D")},
	}
}

func TestNeighborNodesCap(t *testing.T) {
	g := &Graph{
		Outgoing: map[string][]Edge{
			"A": {
				{From: "A", To: "B", Kind: EdgeCalls},
				{From: "A", To: "C", Kind: EdgeCalls},
				{From: "A", To: "D", Kind: EdgeCalls},
				{From: "A", To: "E", Kind: EdgeCalls},
			},
		},
		Incoming:  map[string][]Edge{},
		NodesByID: map[string]Node{},
	}
	got := g.NeighborNodes([]string{"A"}, 1, nil, 3)
	if len(got) != 3 {
		t.Errorf("capped neighbour set size = %d, want 3", len(got))
	}
	if !got["A"] {
		t.Errorf("seed A missing from capped set %v", got)
	}
	// Uncapped returns the full 1-hop set (A + B,C,D,E = 5).
	if full := g.NeighborNodes([]string{"A"}, 1, nil, 0); len(full) != 5 {
		t.Errorf("uncapped set size = %d, want 5", len(full))
	}
}

func TestRankByDiffusionUndirectedReachesInbound(t *testing.T) {
	g := rankingGraph()
	candidates := map[string]bool{"A": true, "B": true, "C": true, "D": true}
	ranked := rankByDiffusion(g, candidates, []string{"A"})

	if len(ranked) != 4 {
		t.Fatalf("ranked len = %d, want 4", len(ranked))
	}
	if ranked[0].Name != "A" {
		t.Errorf("top ranked = %q, want seed A", ranked[0].Name)
	}
	// Undirected diffusion must give D (inbound-only neighbour of A) positive mass.
	byName := map[string]float64{}
	for _, s := range ranked {
		byName[s.Name] = s.Score
	}
	if byName["D"] <= 0 {
		t.Errorf("D score = %v, want > 0 (undirected reaches inbound neighbour)", byName["D"])
	}
}

func TestBuildRankedSubgraphCarriesScoresAndEdges(t *testing.T) {
	g := rankingGraph()
	candidates := map[string]bool{"A": true, "B": true, "C": true, "D": true}
	ranked := rankByDiffusion(g, candidates, []string{"A"})

	var svc Service
	sub := svc.buildRankedSubgraph(g, ranked)

	if len(sub.Nodes) != 4 {
		t.Fatalf("subgraph nodes = %d, want 4", len(sub.Nodes))
	}
	// Nodes are emitted in ranked (score-desc) order, each carrying its score.
	if sub.Nodes[0].ID != "A" || sub.Nodes[0].Score <= 0 {
		t.Errorf("first node = %+v, want A with positive score", sub.Nodes[0])
	}
	// Induced edges among the kept nodes are present (the 4 calls edges).
	if len(sub.Edges) != 4 {
		t.Errorf("induced edges = %d, want 4", len(sub.Edges))
	}
}
