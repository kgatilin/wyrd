package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// fixturePackage builds an in-memory package with one of each major symbol
// kind, plus call/dependency edges that the digest must drop.
func fixturePackage() domain.PackageModel {
	return domain.PackageModel{
		Path:  "internal/demo",
		Name:  "demo",
		Layer: "service",
		Interfaces: []domain.InterfaceDef{{
			Name: "Service", IsExported: true, SourceFile: "svc.go",
			Span: domain.Span{StartLine: 10}, Doc: "Service does things.\nExtra detail.",
			Methods: []domain.MethodDef{{Name: "Do", IsExported: true}},
		}},
		Structs: []domain.StructDef{{
			Name: "Impl", IsExported: true, SourceFile: "svc.go",
			Fields:  []domain.FieldDef{{Name: "n", Type: domain.TypeRef{Name: "int"}}},
			Methods: []domain.MethodDef{{Name: "Do", IsExported: true, Calls: []domain.CallEdge{{}}}},
		}},
		Functions: []domain.FunctionDef{{
			Name: "New", IsExported: true, SourceFile: "svc.go",
			Returns: []domain.TypeRef{{Name: "Impl", IsPointer: true}},
			Doc:     "New builds an Impl.",
			Calls:   []domain.CallEdge{{}}, // must be dropped from the digest
		}},
		Dependencies: []domain.Dependency{{}}, // must be dropped from the digest
	}
}

func TestBuildPackageDigest_BodyFreeWithCounts(t *testing.T) {
	dg := buildPackageDigest(fixturePackage(), nil, 0, digestDefaultLimit)

	b, err := json.Marshal(dg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	// The bulk that blew the budget — call edges and the dependency list —
	// must not appear anywhere in the digest.
	for _, banned := range []string{"Calls", "Dependencies", "Dependency", "call_edge"} {
		if strings.Contains(s, banned) {
			t.Errorf("digest must not embed %q: %s", banned, s)
		}
	}
	if dg.Counts.Interfaces != 1 || dg.Counts.Structs != 1 || dg.Counts.Functions != 1 || dg.Counts.Total != 3 {
		t.Errorf("counts wrong: %+v", dg.Counts)
	}
	// The surface (signatures/synopses) must survive.
	if !strings.Contains(s, `"New()`) {
		t.Errorf("function signature missing: %s", s)
	}
	if !strings.Contains(s, "Service does things.") || strings.Contains(s, "Extra detail.") {
		t.Errorf("doc should be first-line only: %s", s)
	}
}

func TestBuildPackageDigest_Pagination(t *testing.T) {
	pkg := domain.PackageModel{Path: "p", Name: "p"}
	for i := range 5 {
		pkg.Functions = append(pkg.Functions, domain.FunctionDef{Name: fmt.Sprintf("F%d", i), IsExported: true})
	}

	first := buildPackageDigest(pkg, nil, 0, 2)
	if first.Pagination.Returned != 2 || !first.Pagination.Truncated || first.Pagination.NextOffset != 2 {
		t.Fatalf("first page wrong: %+v", first.Pagination)
	}
	if first.Counts.Total != 5 {
		t.Errorf("counts must reflect whole package, got %d", first.Counts.Total)
	}
	if first.Symbols[0].Name != "F0" || first.Symbols[1].Name != "F1" {
		t.Errorf("first page symbols wrong: %+v", first.Symbols)
	}

	mid := buildPackageDigest(pkg, nil, first.Pagination.NextOffset, 2)
	if mid.Symbols[0].Name != "F2" || mid.Symbols[1].Name != "F3" {
		t.Errorf("second page symbols wrong: %+v", mid.Symbols)
	}

	last := buildPackageDigest(pkg, nil, 4, 2)
	if last.Pagination.Returned != 1 || last.Pagination.Truncated {
		t.Errorf("last page should be complete: %+v", last.Pagination)
	}
	if last.Symbols[0].Name != "F4" {
		t.Errorf("last page symbol wrong: %+v", last.Symbols)
	}
}

func TestBuildPackageDigest_KindsFilter(t *testing.T) {
	dg := buildPackageDigest(fixturePackage(), []string{"func"}, 0, digestDefaultLimit)
	if len(dg.Symbols) != 1 || dg.Symbols[0].Kind != "func" {
		t.Errorf("kinds filter should keep only funcs: %+v", dg.Symbols)
	}
	// Counts still describe the whole package, not the filtered view.
	if dg.Counts.Total != 3 {
		t.Errorf("counts must stay full under a kinds filter, got %d", dg.Counts.Total)
	}
}

func TestBuildPackageIndex_NoSymbols(t *testing.T) {
	idx := buildPackageIndex(fixturePackage())
	if len(idx.Symbols) != 0 {
		t.Errorf("index mode must carry no symbols, got %d", len(idx.Symbols))
	}
	if idx.Pagination != nil {
		t.Errorf("index mode has no pagination")
	}
	if idx.Counts.Total != 3 {
		t.Errorf("index must carry the census, got %d", idx.Counts.Total)
	}
}

func TestBuildPackageDigest_ByteBudgetHoldsCeiling(t *testing.T) {
	// Many mid-sized symbols: the soft byte budget must cut the page before
	// the count limit, and the result must clear the hard ceiling.
	pkg := domain.PackageModel{Path: "p", Name: "p"}
	longDoc := strings.Repeat("d ", 120) // ~240 chars, clipped to digestDocBytes
	for i := range 2000 {
		pkg.Functions = append(pkg.Functions, domain.FunctionDef{
			Name: fmt.Sprintf("Func%04d", i), IsExported: true, Doc: longDoc,
		})
	}
	dg := buildPackageDigest(pkg, nil, 0, digestDefaultLimit)

	if !dg.Pagination.Truncated {
		t.Fatal("2000 symbols must truncate")
	}
	if dg.Pagination.Returned >= digestDefaultLimit {
		t.Errorf("byte budget should cut the page below the count limit, returned=%d", dg.Pagination.Returned)
	}
	b, _ := json.Marshal(dg)
	if len(b) > maxResultBytes {
		t.Errorf("digest page must clear the hard ceiling, got %d bytes", len(b))
	}
	if dg.Pagination.Returned == 0 {
		t.Error("page must always emit at least one symbol")
	}
}
