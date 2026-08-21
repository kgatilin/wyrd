package mcp

import (
	"fmt"
	"testing"

	"github.com/kgatilin/wyrd/internal/clustering"
)

func TestParseClusterK(t *testing.T) {
	cases := []struct {
		in      any
		want    int
		wantErr bool
	}{
		{nil, 0, false},
		{"auto", 0, false},
		{float64(5), 5, false}, // JSON numbers decode to float64
		{3, 3, false},
		{"bogus", 0, true},
		{float64(0), 0, true},
		{-2, 0, true},
	}
	for _, c := range cases {
		got, err := parseClusterK(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseClusterK(%v) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseClusterK(%v) unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseClusterK(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// clusterSummaries is what keeps this two-sided lens inside the result budget:
// it samples on both sides, always. A regression here is not a cosmetic one —
// the full 2×K dump is what used to come back as an oversize refusal.
func TestClusterSummaries_AlwaysSamples(t *testing.T) {
	const big = 400
	nodes := make([]string, big+3)
	labels := make([]int, len(nodes))
	for i := range nodes {
		nodes[i] = fmt.Sprintf("fn:internal/pkg.Sym%d", i)
		if i >= big {
			labels[i] = 1
		}
	}
	res := clustering.Result{Nodes: nodes}
	part := clustering.Partition{Labels: labels}

	got := clusterSummaries(res, part)
	if len(got) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(got))
	}
	if got[0].Size != big {
		t.Errorf("cluster 0 size = %d, want %d", got[0].Size, big)
	}
	if !got[0].Truncated || len(got[0].MembersSample) != clusterMembersSample || got[0].Members != nil {
		t.Errorf("a %d-member cluster came back unsampled: %+v", big, got[0])
	}
	// A cluster that fits is listed whole — sampling three of three would only
	// make the verdict harder to read.
	if got[1].Truncated || len(got[1].Members) != 3 {
		t.Errorf("small cluster = %+v, want 3 full members", got[1])
	}
}

// A node no side placed belongs to no cluster; it must not be swept into one.
func TestClusterSummaries_SkipsUnplacedNodes(t *testing.T) {
	res := clustering.Result{Nodes: []string{"fn:p.A", "fn:p.B", "fn:p.C"}}
	part := clustering.Partition{Labels: []int{0, -1, 1}}
	got := clusterSummaries(res, part)
	if len(got) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(got))
	}
	for _, info := range got {
		if info.Size != 1 || info.Members[0] == "fn:p.B" {
			t.Errorf("unplaced node landed in a cluster: %+v", info)
		}
	}
}

// include_members was removed: it lifted the membership cap on a lens that
// samples for a reason, so its success depended on the repository being small.
// The browser reads GET /api/archmotif/domains instead. A schema that still
// advertised the flag would send callers back to the failure.
func TestLatentDomainsSchema_HasNoIncludeMembers(t *testing.T) {
	var schema map[string]any
	for _, def := range builtinToolDefinitions() {
		if def.Name == "latent_domains" {
			schema, _ = def.InputSchema.(map[string]any)
		}
	}
	if schema == nil {
		t.Fatal("latent_domains is not registered in builtinToolDefinitions")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("latent_domains schema has no properties object")
	}
	if _, found := props["include_members"]; found {
		t.Error("latent_domains still advertises include_members")
	}
}
