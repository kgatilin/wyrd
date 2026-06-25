package mcp

import (
	"strings"
	"testing"

	archmotifAdapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/domain"
)

// twoFlowClusters builds two triangles of functions (a.A1..A3, b.B1..B3) wired
// by usesType edges, joined by a single bridge a.A3 -> b.B1. usesType is one of
// the flow edge kinds the region walk uses.
func twoFlowClusters() []domain.PackageModel {
	dep := func(fromPkg, from, toPkg, to string) domain.Dependency {
		return domain.Dependency{
			From: domain.SymbolRef{Package: fromPkg, Symbol: from},
			To:   domain.SymbolRef{Package: toPkg, Symbol: to},
			Kind: domain.DependencyUses,
		}
	}
	return []domain.PackageModel{
		{
			Path: "a", Name: "a",
			Functions: []domain.FunctionDef{{Name: "A1"}, {Name: "A2"}, {Name: "A3"}},
			Dependencies: []domain.Dependency{
				dep("a", "A1", "a", "A2"), dep("a", "A2", "a", "A3"), dep("a", "A3", "a", "A1"),
				dep("a", "A3", "b", "B1"), // bridge
			},
		},
		{
			Path: "b", Name: "b",
			Functions: []domain.FunctionDef{{Name: "B1"}, {Name: "B2"}, {Name: "B3"}},
			Dependencies: []domain.Dependency{
				dep("b", "B1", "b", "B2"), dep("b", "B2", "b", "B3"), dep("b", "B3", "b", "B1"),
			},
		},
	}
}

// worktree adds a new function a.A4 wired into cluster A (A4 -> A1). The diff
// seeds A4 (new node) and the endpoints of the new edge (A4, A1); ACL should
// grow that to cluster A and leave cluster B out.
func twoFlowClustersChanged() []domain.PackageModel {
	models := twoFlowClusters()
	models[0].Functions = append(models[0].Functions, domain.FunctionDef{Name: "A4"})
	models[0].Dependencies = append(models[0].Dependencies, domain.Dependency{
		From: domain.SymbolRef{Package: "a", Symbol: "A4"},
		To:   domain.SymbolRef{Package: "a", Symbol: "A1"},
		Kind: domain.DependencyUses,
	})
	return models
}

func TestDiffRegionNodes_RegionIsChangedCluster(t *testing.T) {
	base := twoFlowClusters()
	wt := twoFlowClustersChanged()

	graph, err := archmotifAdapter.ToArchmotifGraph(wt, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}

	region, meta, emsg := diffRegionNodes(graph, base, wt)
	if emsg != "" {
		t.Fatalf("diffRegionNodes: %s", emsg)
	}
	if meta == nil || meta.SeedCount < 1 {
		t.Fatalf("expected a seeded region, got meta=%+v", meta)
	}

	inRegion := map[string]bool{}
	for _, id := range region {
		inRegion[id] = true
	}
	// The change's own cluster must be present.
	if !inRegion["fn:a.A4"] {
		t.Errorf("region missing the changed node fn:a.A4: %v", region)
	}
	if !inRegion["fn:a.A1"] {
		t.Errorf("region missing cluster-A node fn:a.A1: %v", region)
	}
	// Cluster B should be excluded by the conductance sweep (only the bridge
	// connects it).
	if inRegion["fn:b.B2"] || inRegion["fn:b.B3"] {
		t.Errorf("region leaked into cluster B: %v", region)
	}
	// No container nodes.
	for _, id := range region {
		if strings.HasPrefix(id, "pkg:") || strings.HasPrefix(id, "file:") {
			t.Errorf("region contains a container node: %s", id)
		}
	}
}

func TestDiffRegionNodes_NoChanges(t *testing.T) {
	base := twoFlowClusters()
	graph, err := archmotifAdapter.ToArchmotifGraph(base, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}
	_, _, emsg := diffRegionNodes(graph, base, base)
	if emsg == "" {
		t.Error("expected a 'no changes' message for identical base/worktree")
	}
}
