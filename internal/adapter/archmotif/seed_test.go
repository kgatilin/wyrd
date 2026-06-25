package archmotif

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// baseModels / worktreeModels share a domain package (Order) and differ in
// internal/service: the worktree adds a function (Bar), adds a method
// (OrderService.Handle), and adds a dependency edge OrderService -> Order.
func baseModels() []domain.PackageModel {
	return []domain.PackageModel{
		{Path: "internal/domain", Name: "domain", Structs: []domain.StructDef{{Name: "Order", IsExported: true}}},
		{
			Path: "internal/service", Name: "service",
			Structs:   []domain.StructDef{{Name: "OrderService", IsExported: true}},
			Functions: []domain.FunctionDef{{Name: "Foo", IsExported: true}},
		},
	}
}

func worktreeModels() []domain.PackageModel {
	return []domain.PackageModel{
		{Path: "internal/domain", Name: "domain", Structs: []domain.StructDef{{Name: "Order", IsExported: true}}},
		{
			Path: "internal/service", Name: "service",
			Structs: []domain.StructDef{{
				Name:       "OrderService",
				IsExported: true,
				Methods:    []domain.MethodDef{{Name: "Handle", IsExported: true}},
			}},
			Functions: []domain.FunctionDef{
				{Name: "Foo", IsExported: true},
				{Name: "Bar", IsExported: true},
			},
			Dependencies: []domain.Dependency{{
				From: domain.SymbolRef{Package: "internal/service", Symbol: "OrderService"},
				To:   domain.SymbolRef{Package: "internal/domain", Symbol: "Order"},
				Kind: domain.DependencyUses,
			}},
		},
	}
}

func TestSeedIDsFromDiff_CapturesChangesAndEndpoints(t *testing.T) {
	seeds := SeedIDsFromDiff(baseModels(), worktreeModels())

	set := map[string]bool{}
	for _, id := range seeds {
		set[id] = true
	}

	// The diff works at struct granularity: an added method surfaces as a
	// KindStruct change on OrderService, so the struct node is seeded (ACL
	// reaches the method via contains), not a standalone method node.
	want := []string{
		"fn:internal/service.Bar",            // added function
		"type:internal/service.OrderService", // struct change (added method) + dep endpoint
		"type:internal/domain.Order",         // dep edge endpoint (to)
	}
	for _, id := range want {
		if !set[id] {
			t.Errorf("seed missing %q; got %v", id, seeds)
		}
	}
}

// TestSeedIDsFromDiff_AllResolveToGraphNodes is the invariant guard: every id
// SeedIDsFromDiff produces must exist as a node in the worktree's archmotif
// graph. If the diff Path scheme and the exporter id scheme ever drift, this
// fails instead of silently seeding ids that match nothing.
func TestSeedIDsFromDiff_AllResolveToGraphNodes(t *testing.T) {
	wt := worktreeModels()
	seeds := SeedIDsFromDiff(baseModels(), wt)
	if len(seeds) == 0 {
		t.Fatal("expected a non-empty seed for a non-empty diff")
	}

	g, err := ToArchmotifGraph(wt, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}
	for _, id := range seeds {
		if !g.HasNode(id) {
			t.Errorf("seed id %q is not a node in the worktree graph", id)
		}
	}
}

func TestSeedIDsFromDiff_EmptyWhenIdentical(t *testing.T) {
	if seeds := SeedIDsFromDiff(baseModels(), baseModels()); len(seeds) != 0 {
		t.Errorf("expected empty seed for identical models, got %v", seeds)
	}
}
