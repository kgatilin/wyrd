package mcp

import (
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
