package mcp

import (
	"fmt"
	"math"
	"testing"

	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

func TestNormalizedMutualInfo_Identical(t *testing.T) {
	a := []int{0, 0, 1, 1, 2, 2}
	b := []int{0, 0, 1, 1, 2, 2}
	if got := normalizedMutualInfo(a, b); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("identical partitions NMI = %v, want 1", got)
	}
}

func TestNormalizedMutualInfo_RelabelInvariant(t *testing.T) {
	// Same partition, different label names -> still 1.
	a := []int{0, 0, 1, 1, 2, 2}
	b := []int{5, 5, 9, 9, 7, 7}
	if got := normalizedMutualInfo(a, b); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("relabeled partition NMI = %v, want 1", got)
	}
}

func TestNormalizedMutualInfo_Independent(t *testing.T) {
	// b is one big cluster while a splits -> H(b)=0 -> NMI 0 (no shared info).
	a := []int{0, 1, 0, 1, 0, 1}
	b := []int{0, 0, 0, 0, 0, 0}
	if got := normalizedMutualInfo(a, b); got != 0 {
		t.Errorf("structure-vs-blob NMI = %v, want 0", got)
	}
}

func TestNormalizedMutualInfo_BothSingleCluster(t *testing.T) {
	a := []int{0, 0, 0}
	b := []int{0, 0, 0}
	if got := normalizedMutualInfo(a, b); got != 1 {
		t.Errorf("both-single-cluster NMI = %v, want 1", got)
	}
}

func TestNormalizedMutualInfo_PartialOverlap(t *testing.T) {
	// Partitions agree on 2/3 of the structure -> NMI strictly between 0 and 1.
	a := []int{0, 0, 1, 1, 2, 2}
	b := []int{0, 0, 1, 1, 1, 2}
	got := normalizedMutualInfo(a, b)
	if got <= 0 || got >= 1 {
		t.Errorf("partial-overlap NMI = %v, want in (0,1)", got)
	}
}

func TestDominantShare(t *testing.T) {
	clusters := []spectralcluster.Cluster{
		{ID: 0, Members: []string{"a", "b", "c", "d"}}, // 4
		{ID: 1, Members: []string{"e", "f"}},           // 2
		{ID: 2, Members: []string{"g"}},                // 1
	}
	if got := dominantShare(clusters); math.Abs(got-4.0/7.0) > 1e-9 {
		t.Errorf("dominant_share = %v, want %v", got, 4.0/7.0)
	}
	if got := dominantShare(nil); got != 0 {
		t.Errorf("empty dominant_share = %v, want 0", got)
	}
}

func TestLatentVerdict(t *testing.T) {
	// Aligned: high NMI regardless of shares.
	if v, _ := latentVerdict(0.7, 0.9, 0.4, 5); v != "aligned" {
		t.Errorf("high NMI verdict = %q, want aligned", v)
	}
	// Glued: low NMI, structure far more degenerate than semantics.
	if v, _ := latentVerdict(0.1, 0.55, 0.30, 6); v != "latent_domains_glued" {
		t.Errorf("glued verdict = %q, want latent_domains_glued", v)
	}
	// Diverging: low NMI but no dominant structural blob.
	if v, _ := latentVerdict(0.1, 0.35, 0.30, 6); v != "diverging" {
		t.Errorf("diverging verdict = %q, want diverging", v)
	}
}

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

func TestLabelMap(t *testing.T) {
	clusters := []spectralcluster.Cluster{
		{ID: 0, Members: []string{"a", "b"}},
		{ID: 1, Members: []string{"c"}},
	}
	m := labelMap(clusters)
	if m["a"] != 0 || m["b"] != 0 || m["c"] != 1 {
		t.Errorf("labelMap = %v, want a,b->0 c->1", m)
	}
	if _, ok := m["z"]; ok {
		t.Error("labelMap should not contain unknown node")
	}
}

func TestAdjustedMutualInfo_Identical(t *testing.T) {
	a := []int{0, 0, 1, 1, 2, 2}
	if got := adjustedMutualInfo(a, a); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("identical AMI = %v, want 1", got)
	}
}

func TestAdjustedMutualInfo_BlobIsZero(t *testing.T) {
	a := []int{0, 1, 0, 1, 0, 1}
	b := []int{0, 0, 0, 0, 0, 0} // one cluster -> no information
	if got := adjustedMutualInfo(a, b); got != 0 {
		t.Errorf("structure-vs-blob AMI = %v, want 0", got)
	}
}

// AMI corrects for chance, so on a partial-overlap case it sits strictly below
// the raw NMI — this is exactly why it doesn't inflate the verdict as K grows.
func TestAdjustedMutualInfo_BelowNMI(t *testing.T) {
	a := []int{0, 0, 1, 1, 2, 2}
	b := []int{0, 0, 1, 1, 1, 2}
	ami := adjustedMutualInfo(a, b)
	nmi := normalizedMutualInfo(a, b)
	if ami < 0 || ami > 1 {
		t.Errorf("AMI = %v, want in [0,1]", ami)
	}
	if ami >= nmi {
		t.Errorf("AMI (%v) should be < NMI (%v) — chance correction lowers it", ami, nmi)
	}
}

// TestBuildClusterMembers_DumpsFullMembershipUncapped pins the contract
// include_members exists for: the caller draws the partition, so every member
// of every cluster must come back, on both sides, with no sample and no
// truncation flag. The default path (buildClusterSummaries) is asserted next to
// it so the two cannot quietly converge — the sampling is what keeps the
// agent-facing verdict small, and the dump is what makes the grid drawable.
func TestBuildClusterMembers_DumpsFullMembershipUncapped(t *testing.T) {
	ids := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("fn:internal/pkg.%s%d", prefix, i)
		}
		return out
	}
	// Well past both caps, so a cap of either kind would show.
	big := spectralcluster.Cluster{ID: 0, Members: ids("big", clusterMembersFullLimit+50)}
	small := spectralcluster.Cluster{ID: 1, Members: ids("small", 3)}
	clusters := []spectralcluster.Cluster{big, small}

	full := buildClusterMembers(clusters)
	if len(full) != 2 {
		t.Fatalf("cluster count = %d, want 2", len(full))
	}
	for i, want := range []int{clusterMembersFullLimit + 50, 3} {
		got := full[i]
		if len(got.Members) != want {
			t.Errorf("cluster %d members = %d, want %d (uncapped)", got.ID, len(got.Members), want)
		}
		if got.Size != want {
			t.Errorf("cluster %d size = %d, want %d", got.ID, got.Size, want)
		}
		if got.Truncated {
			t.Errorf("cluster %d marked truncated; include_members must not truncate", got.ID)
		}
		if got.MembersSample != nil {
			t.Errorf("cluster %d carries a sample alongside the full dump", got.ID)
		}
	}
	if full[0].Members[0] != "fn:internal/pkg.big0" {
		t.Errorf("member ids rewritten: got %q", full[0].Members[0])
	}

	// Default stays sampled: this is a verdict lens unless asked otherwise.
	sampled := buildClusterSummaries(clusters)
	if !sampled[0].Truncated || len(sampled[0].MembersSample) != clusterMembersSample {
		t.Errorf("default path stopped sampling: %+v", sampled[0])
	}
}

// TestLatentDomainsSchema_DeclaresIncludeMembers keeps the wire contract and the
// advertised schema together: a caller discovers the flag from tools/list, so an
// argument the handler reads but the schema omits is invisible.
func TestLatentDomainsSchema_DeclaresIncludeMembers(t *testing.T) {
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
	prop, ok := props["include_members"].(map[string]any)
	if !ok {
		t.Fatal("latent_domains schema does not declare include_members")
	}
	if prop["type"] != "boolean" {
		t.Errorf("include_members type = %v, want boolean", prop["type"])
	}
	if desc, _ := prop["description"].(string); desc == "" {
		t.Error("include_members has no description")
	}
}
