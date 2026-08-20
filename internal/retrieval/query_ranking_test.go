package retrieval

import (
	"context"
	"fmt"
	"sort"
	"testing"
)

// stubLexical answers every query with the same fixed hit list, which is what
// lets these tests state the seed masses instead of reverse-engineering a BM25
// score that would produce them. Search softmaxes the scores it is given, so a
// score gap of 10 lands roughly 99% of the mass on the leading seed.
type stubLexical struct{ hits []Scored }

func (s stubLexical) Upsert(id, text string) {}
func (s stubLexical) Remove(id string)       {}
func (s stubLexical) Len() int               { return len(s.hits) }
func (s stubLexical) Search(query string, k int) []Scored {
	if k < len(s.hits) {
		return s.hits[:k]
	}
	return s.hits
}

// callGraph builds a graph of `calls` edges from a list of endpoint pairs. Node
// metadata is uniform: these tests are about which nodes the diffusion keeps,
// never about what they say.
func callGraph(pairs [][2]string) *Graph {
	g := &Graph{
		Outgoing:  map[string][]Edge{},
		Incoming:  map[string][]Edge{},
		NodesByID: map[string]Node{},
	}
	declare := func(id string) {
		if _, ok := g.NodesByID[id]; !ok {
			g.NodesByID[id] = Node{ID: id, Kind: "func", Package: "pkg", Name: id}
		}
	}
	for _, p := range pairs {
		declare(p[0])
		declare(p[1])
		addEdge(g, p[0], p[1], EdgeCalls)
	}
	return g
}

// cliquePairs returns every pair among ids — a maximally dense community.
func cliquePairs(ids ...string) [][2]string {
	var out [][2]string
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			out = append(out, [2]string{ids[i], ids[j]})
		}
	}
	return out
}

// graphParams is DefaultParams with the exact-name floor switched off, so the
// seed masses are exactly the softmax of the stub's scores and the assertions
// are about the diffusion rather than about fusion.
func graphParams() Params {
	p := DefaultParams()
	p.ExactNameBoost = 0
	return p
}

func graphService(pairs [][2]string, hits []Scored, p Params) *Service {
	return &Service{graph: callGraph(pairs), lindex: stubLexical{hits: hits}, params: p}
}

func searchRegion(t *testing.T, svc *Service, hops int) GraphSearchResult {
	t.Helper()
	got, _, err := svc.SearchGraph(context.Background(), "query", 10, hops)
	if err != nil {
		t.Fatalf("SearchGraph: %v", err)
	}
	return got
}

func regionIDs(res GraphSearchResult) []string {
	ids := make([]string, len(res.Nodes))
	for i, n := range res.Nodes {
		ids[i] = n.ID
	}
	return ids
}

func sortedRegionIDs(res GraphSearchResult) []string {
	ids := regionIDs(res)
	sort.Strings(ids)
	return ids
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// bridgedClusters: two 4-cliques joined by the single edge a1—b1.
func bridgedClusters() [][2]string {
	pairs := cliquePairs("a1", "a2", "a3", "a4")
	pairs = append(pairs, cliquePairs("b1", "b2", "b3", "b4")...)
	return append(pairs, [2]string{"a1", "b1"})
}

// TestSearchGraphSeedMassPicksSide is the point of weighting the seeds: both
// clusters are seeded, and the side that wins is the side holding the relevance
// mass. Uniform personalization — what the old ranking did — would leave the
// two symmetric halves indistinguishable.
func TestSearchGraphSeedMassPicksSide(t *testing.T) {
	cases := []struct {
		name  string
		hits  []Scored
		want  []string
		avoid []string
	}{
		{
			name:  "mass on cluster A",
			hits:  []Scored{{ID: "a1", Score: 10}, {ID: "b1", Score: 0}},
			want:  []string{"a1", "a2", "a3", "a4"},
			avoid: []string{"b2", "b3", "b4"},
		},
		{
			name:  "mass on cluster B",
			hits:  []Scored{{ID: "b1", Score: 10}, {ID: "a1", Score: 0}},
			want:  []string{"b1", "b2", "b3", "b4"},
			avoid: []string{"a2", "a3", "a4"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := searchRegion(t, graphService(bridgedClusters(), tc.hits, graphParams()), 5)

			if ids := sortedRegionIDs(got); !sameIDs(ids, tc.want) {
				t.Errorf("region = %v, want %v", ids, tc.want)
			}
			if got.SeedCount != 2 {
				t.Errorf("seed count = %d, want 2 (both hits seeded the diffusion)", got.SeedCount)
			}
			// One edge out of a 4-clique is about as separable as a region gets.
			if got.Conductance <= 0 || got.Conductance > 0.2 {
				t.Errorf("conductance = %v, want a small positive cut", got.Conductance)
			}
			if got.Truncated {
				t.Errorf("region of %d nodes reported as truncated", len(got.Nodes))
			}
		})
	}
}

// TestSearchGraphOrdersByMass checks the contract consumers rely on when they
// re-truncate the node list: the heaviest node comes first and mass never
// climbs again further down.
func TestSearchGraphOrdersByMass(t *testing.T) {
	hits := []Scored{{ID: "a1", Score: 10}, {ID: "b1", Score: 0}}
	got := searchRegion(t, graphService(bridgedClusters(), hits, graphParams()), 5)

	if len(got.Nodes) == 0 {
		t.Fatal("empty region")
	}
	if got.Nodes[0].ID != "a1" {
		t.Errorf("first node = %q, want the seed a1", got.Nodes[0].ID)
	}
	for i := 1; i < len(got.Nodes); i++ {
		if got.Nodes[i].Score > got.Nodes[i-1].Score {
			t.Errorf("mass rises at %d: %v after %v", i, got.Nodes[i], got.Nodes[i-1])
		}
	}
	// The induced edges of a 4-clique are all six of them.
	if len(got.Edges) != 6 {
		t.Errorf("induced edges = %d, want 6", len(got.Edges))
	}
}

// glueFixture: a dense community {a1..a5} whose members all also call `glue`,
// which in turn reaches into a sparse ring of low-degree nodes. glue is the
// highest-degree node in the graph and it is the only way out of the community
// — the shape the "shared helper on every query" complaint is about.
func glueFixture() [][2]string {
	community := []string{"a1", "a2", "a3", "a4", "a5"}
	pairs := cliquePairs(community...)
	for _, id := range community {
		pairs = append(pairs, [2]string{"glue", id})
	}
	const ring = 12
	for i := 0; i < ring; i++ {
		pairs = append(pairs, [2]string{fmt.Sprintf("c%d", i), fmt.Sprintf("c%d", (i+1)%ring)})
	}
	for i := 0; i < 4; i++ {
		pairs = append(pairs, [2]string{"glue", fmt.Sprintf("c%d", i*3)})
	}
	return pairs
}

// TestSearchGraphHubDampingDropsGlue is the knob aimed at the dominant noise
// source. Undamped, the glue node's five edges into the community outweigh its
// four edges out, so the cut is cheaper with it than without and it joins the
// region. Damping divides each edge by its endpoints' degrees, which costs the
// links into the dense community more than the links to the sparse ring, and
// the glue stops paying for itself.
func TestSearchGraphHubDampingDropsGlue(t *testing.T) {
	hits := []Scored{{ID: "a1", Score: 10}}
	community := []string{"a1", "a2", "a3", "a4", "a5"}

	undamped := graphParams()
	undamped.HubDamping = 0
	got := searchRegion(t, graphService(glueFixture(), hits, undamped), 5)
	if ids := sortedRegionIDs(got); !sameIDs(ids, append(append([]string{}, community...), "glue")) {
		t.Errorf("undamped region = %v, want the community plus glue", ids)
	}

	damped := graphParams()
	damped.HubDamping = 1
	got = searchRegion(t, graphService(glueFixture(), hits, damped), 5)
	if ids := sortedRegionIDs(got); !sameIDs(ids, community) {
		t.Errorf("damped region = %v, want the community without glue", ids)
	}
}

// hopsFixture: the seed dangles off a1, which belongs to a 4-clique and also
// bridges to a second one. The community the sweep finds therefore reaches two
// steps out from the seed.
func hopsFixture() [][2]string {
	pairs := [][2]string{{"s", "a1"}}
	pairs = append(pairs, cliquePairs("a1", "a2", "a3", "a4")...)
	pairs = append(pairs, [2]string{"a1", "b1"})
	return append(pairs, cliquePairs("b1", "b2", "b3", "b4")...)
}

// TestSearchGraphHopsCapsRadius pins the meaning of the hops argument. The
// sweep wants the whole clique behind a1; hops=1 hands back only what is one
// edge from the seed, and raising it releases the rest. Nothing in alpha or
// epsilon expresses a radius, so this bound has to be enforced separately.
func TestSearchGraphHopsCapsRadius(t *testing.T) {
	hits := []Scored{{ID: "s", Score: 10}}

	near := searchRegion(t, graphService(hopsFixture(), hits, graphParams()), 1)
	if ids := sortedRegionIDs(near); !sameIDs(ids, []string{"a1", "s"}) {
		t.Errorf("hops=1 region = %v, want only the seed and its direct neighbour", ids)
	}

	wide := searchRegion(t, graphService(hopsFixture(), hits, graphParams()), 2)
	if ids := sortedRegionIDs(wide); !sameIDs(ids, []string{"a1", "a2", "a3", "a4", "s"}) {
		t.Errorf("hops=2 region = %v, want the whole community", ids)
	}
	// The cut is reported for the region the sweep chose, so trimming the
	// radius does not rewrite it.
	if near.Conductance != wide.Conductance {
		t.Errorf("conductance changed with hops: %v vs %v", near.Conductance, wide.Conductance)
	}
}

// TestSearchGraphMaxNodesTruncates checks the payload cap keeps the top of the
// mass order and says so.
func TestSearchGraphMaxNodesTruncates(t *testing.T) {
	hits := []Scored{{ID: "a1", Score: 10}, {ID: "b1", Score: 0}}

	uncapped := graphParams()
	uncapped.MaxGraphNodes = 0
	full := searchRegion(t, graphService(bridgedClusters(), hits, uncapped), 5)
	if len(full.Nodes) < 3 {
		t.Fatalf("region of %d nodes is too small to test the cap", len(full.Nodes))
	}

	capped := graphParams()
	capped.MaxGraphNodes = 2
	got := searchRegion(t, graphService(bridgedClusters(), hits, capped), 5)

	if !got.Truncated {
		t.Error("Truncated = false, want true once the cap bites")
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("capped region = %d nodes, want 2", len(got.Nodes))
	}
	if ids, want := regionIDs(got), regionIDs(full)[:2]; !sameIDs(ids, want) {
		t.Errorf("capped region = %v, want the mass-order prefix %v", ids, want)
	}
	// The cut is a property of the region the sweep chose, not of the slice
	// that fits in the response.
	if got.Conductance != full.Conductance {
		t.Errorf("conductance changed under the cap: %v vs %v", got.Conductance, full.Conductance)
	}
}

// TestSearchGraphEdgeKindWeightsGateParticipation covers the rule that the
// weight map is the participation list: a kind it does not mention contributes
// no edge, so a graph made entirely of that kind diffuses nowhere.
func TestSearchGraphEdgeKindWeightsGateParticipation(t *testing.T) {
	pairs := cliquePairs("a1", "a2", "a3", "a4")
	hits := []Scored{{ID: "a1", Score: 10}}

	p := graphParams()
	delete(p.EdgeKindWeights, string(EdgeCalls))
	got := searchRegion(t, graphService(pairs, hits, p), 5)

	if ids := sortedRegionIDs(got); !sameIDs(ids, []string{"a1"}) {
		t.Errorf("region = %v, want the isolated seed once calls edges are out", ids)
	}
	if len(got.Edges) != 0 {
		t.Errorf("edges = %v, want none", got.Edges)
	}
}

func TestSearchGraphDegenerate(t *testing.T) {
	t.Run("no graph", func(t *testing.T) {
		svc := &Service{lindex: stubLexical{hits: []Scored{{ID: "a1", Score: 10}}}, params: graphParams()}
		got := searchRegion(t, svc, 1)
		if len(got.Nodes) != 0 || len(got.Edges) != 0 {
			t.Errorf("region = %+v, want empty", got.Subgraph)
		}
		if got.Nodes == nil || got.Edges == nil {
			t.Error("empty result must carry empty slices, not nil")
		}
	})

	t.Run("no hits", func(t *testing.T) {
		svc := graphService(bridgedClusters(), nil, graphParams())
		got := searchRegion(t, svc, 1)
		if len(got.Nodes) != 0 || len(got.Edges) != 0 {
			t.Errorf("region = %+v, want empty", got.Subgraph)
		}
		if got.SeedCount != 0 || got.Conductance != 0 || got.Truncated {
			t.Errorf("diagnostics = %+v, want zeroed", got)
		}
	})
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
