package clustering

import (
	"encoding/json"
	"fmt"
	"testing"
)

// mcpResultBudget mirrors internal/adapter/mcp's hard ceiling on one tool
// result. It is restated here rather than imported because that ceiling belongs
// to the agent transport; the point of the test below is that this encoding
// clears it, not that this package is subject to it.
const mcpResultBudget = 256 * 1024

// syntheticResult builds a partition of nodes symbols across k clusters per
// side. The ids run about 45 characters, which is what this repository's own
// look like (`type:internal/adapter/mcp.latentDomainsArgs`).
func syntheticResult(nodes, k int) Result {
	ids := make([]string, nodes)
	structural := make([]int, nodes)
	semantic := make([]int, nodes)
	for i := range ids {
		ids[i] = fmt.Sprintf("type:internal/adapter/pkg%02d.ExportedName%d", i%40, i)
		structural[i] = i % k
		semantic[i] = (i / 7) % k
	}
	return Result{
		Nodes:      ids,
		NodeCount:  nodes,
		Structural: Partition{K: k, ClusterCount: k, DominantShare: 0.68, Modularity: 0.19, Labels: structural},
		Semantic:   Partition{K: k, ClusterCount: k, DominantShare: 0.16, Modularity: 0.72, Labels: semantic},
		Agreement:  Agreement{AMI: 0.002, NMI: 0.086, Verdict: VerdictGlued},
		Glue:       Glue{GlueCluster: 11, Note: "Semantics splits into balanced domains but structure collapses into one blob."},
	}
}

// memberDump is the encoding this one replaced: every cluster listing its own
// members, on both sides, so every node id is written twice.
func memberDump(r Result) ([]byte, error) {
	type cluster struct {
		ID      int      `json:"id"`
		Size    int      `json:"size"`
		Members []string `json:"members"`
	}
	side := func(p Partition) []cluster {
		out := make([]cluster, 0, len(p.ClusterIDs()))
		for _, id := range p.ClusterIDs() {
			members := r.Members(p, id)
			out = append(out, cluster{ID: id, Size: len(members), Members: members})
		}
		return out
	}
	return json.Marshal(map[string]any{
		"node_count": r.NodeCount,
		"structural": map[string]any{"k": r.Structural.K, "clusters": side(r.Structural)},
		"semantic":   map[string]any{"k": r.Semantic.K, "clusters": side(r.Semantic)},
		"agreement":  r.Agreement,
		"glue":       r.Glue,
	})
}

// The bug this encoding fixes: on a repository of a few thousand symbols the
// member dump ran past the agent transport's 256 KiB ceiling and the canvas got
// a refusal instead of a grid.
//
// The fix is that a node's id is written once and each side contributes an
// integer, so the payload no longer grows with the number of sides. The bound
// is therefore stated per node rather than in total: one id plus two small
// integers is about 54 bytes, and the dump's two ids plus their per-cluster
// scaffolding is about twice that. A regression that reintroduced a second copy
// of the ids would breach the per-node budget at any node count, which a total
// stated for one fixture size would not catch.
func TestResultEncoding_CostsOneIDPerNode(t *testing.T) {
	const nodes, k = 4000, 40
	// One id plus a label on each side, with room for a longer id than this
	// repository's; not room for a second id.
	const maxBytesPerNode = 64

	res := syntheticResult(nodes, k)

	compact, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	dump, err := memberDump(res)
	if err != nil {
		t.Fatalf("marshal member dump: %v", err)
	}

	if len(dump) <= mcpResultBudget {
		t.Fatalf("premise broken: the member dump is %d bytes at %d nodes, inside the %d-byte budget — pick a larger fixture",
			len(dump), nodes, mcpResultBudget)
	}

	perNode := float64(len(compact)) / float64(nodes)
	if perNode > maxBytesPerNode {
		t.Errorf("compact encoding costs %.1f bytes per node (%d total at %d nodes), want at most %d — the ids look like they are being carried twice again",
			perNode, len(compact), nodes, maxBytesPerNode)
	}
	// The whole saving, stated as a ratio so it holds at any size.
	if len(compact)*10 > len(dump)*6 {
		t.Errorf("compact encoding is %d bytes against the dump's %d (%.0f%%), want under 60%%",
			len(compact), len(dump), 100*float64(len(compact))/float64(len(dump)))
	}
	t.Logf("%d nodes: compact %d bytes (%.1f/node), member dump %d bytes (%.1f/node)",
		nodes, len(compact), perNode, len(dump), float64(len(dump))/float64(nodes))
}

// Members and ClusterIDs are how a caller recovers membership from the label
// encoding; nothing else may reconstruct it, or the two would drift.
func TestMembersFromLabels(t *testing.T) {
	res := Result{Nodes: []string{"fn:p.A", "fn:p.B", "fn:p.C", "fn:p.D"}}
	part := Partition{Labels: []int{2, 0, -1, 2}}

	if got := part.ClusterIDs(); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("ClusterIDs = %v, want [0 2]", got)
	}
	members := res.Members(part, 2)
	if len(members) != 2 || members[0] != "fn:p.A" || members[1] != "fn:p.D" {
		t.Fatalf("Members(2) = %v, want [fn:p.A fn:p.D]", members)
	}
	if got := res.Members(part, -1); got != nil {
		t.Fatalf("Members(-1) = %v, want nothing — unplaced is not a cluster", got)
	}
}

// A diff-scoped analysis with no base is a question the daemon cannot answer,
// and it must say so rather than silently falling back to repo scope.
func TestLatentDomains_DiffWithoutBase(t *testing.T) {
	_, err := LatentDomains(Input{
		Packages: twoFlowClusters(),
		Vectors:  stubVectors{},
		Selector: Selector{Diff: true},
	})
	if err == nil {
		t.Fatal("expected an error for a diff selector with no review base")
	}
}

func TestLatentDomains_NoVectors(t *testing.T) {
	if _, err := LatentDomains(Input{Packages: twoFlowClusters()}); err == nil {
		t.Fatal("expected an error when no vector lookup is supplied")
	}
}

type stubVectors struct{}

func (stubVectors) Vector(string) ([]float32, bool) { return nil, false }
