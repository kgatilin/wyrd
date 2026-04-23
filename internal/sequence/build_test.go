package sequence

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// fixture builds a set of PackageModels covering:
//   - a simple A → B → C chain (functions);
//   - a method calling another method on a different struct;
//   - a cycle A → B → A;
//   - interface dispatch with two implementations.
func fixture() []domain.PackageModel {
	// Simple chain: pkg/a.Alpha → pkg/b.Beta → pkg/c.Gamma.
	alpha := domain.FunctionDef{
		Name: "Alpha",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/b", Symbol: "Beta"}},
		},
	}
	beta := domain.FunctionDef{
		Name: "Beta",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/c", Symbol: "Gamma"}},
		},
	}
	gamma := domain.FunctionDef{Name: "Gamma"}

	// Cycle: pkg/c.Loop → pkg/c.Loop.
	loop := domain.FunctionDef{
		Name: "Loop",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/c", Symbol: "Loop"}},
		},
	}

	// Method chain + interface dispatch.
	// pkg/svc.Service.Generate calls pkg/rd.Reader.Read via interface
	// io.Reader, which has two impls: fileReader.Read and memReader.Read.
	service := domain.StructDef{
		Name: "Service",
		Methods: []domain.MethodDef{
			{
				Name: "Generate",
				Calls: []domain.CallEdge{
					{
						To:  domain.SymbolRef{Package: "pkg/rd", Symbol: "fileReader.Read"},
						Via: "io.Reader",
					},
					{
						To:  domain.SymbolRef{Package: "pkg/rd", Symbol: "memReader.Read"},
						Via: "io.Reader",
					},
				},
			},
		},
	}
	rdStruct1 := domain.StructDef{
		Name:    "fileReader",
		Methods: []domain.MethodDef{{Name: "Read"}},
	}
	rdStruct2 := domain.StructDef{
		Name:    "memReader",
		Methods: []domain.MethodDef{{Name: "Read"}},
	}

	return []domain.PackageModel{
		{Path: "pkg/a", Name: "a", Functions: []domain.FunctionDef{alpha}},
		{Path: "pkg/b", Name: "b", Functions: []domain.FunctionDef{beta}},
		{Path: "pkg/c", Name: "c", Functions: []domain.FunctionDef{gamma, loop}},
		{Path: "pkg/svc", Name: "svc", Structs: []domain.StructDef{service}},
		{Path: "pkg/rd", Name: "rd", Structs: []domain.StructDef{rdStruct1, rdStruct2}},
	}
}

func TestBuildSimpleChain(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	root := Build(models, start, 5)

	if root == nil || root.Symbol != start {
		t.Fatalf("root mismatch: %+v", root)
	}
	if len(root.Children) != 1 || root.Children[0].Symbol.Symbol != "Beta" {
		t.Fatalf("expected single Beta child, got %+v", root.Children)
	}
	beta := root.Children[0]
	if len(beta.Children) != 1 || beta.Children[0].Symbol.Symbol != "Gamma" {
		t.Fatalf("expected single Gamma grandchild, got %+v", beta.Children)
	}
	gamma := beta.Children[0]
	if len(gamma.Children) != 0 || gamma.Cycle || gamma.DepthLimit {
		t.Errorf("Gamma should be a clean leaf: %+v", gamma)
	}
}

func TestBuildDepthLimit(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/a", Symbol: "Alpha"}
	root := Build(models, start, 1)

	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child at depth 1, got %d", len(root.Children))
	}
	beta := root.Children[0]
	// With depth=1, Alpha→Beta is rendered, and Beta is called with
	// depth=0, so Beta should be marked DepthLimit (since Beta has
	// outgoing calls) and have no children.
	if !beta.DepthLimit {
		t.Errorf("expected Beta to be marked DepthLimit, got %+v", beta)
	}
	if len(beta.Children) != 0 {
		t.Errorf("DepthLimit node should have no children, got %d", len(beta.Children))
	}
}

func TestBuildCycleDetection(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/c", Symbol: "Loop"}
	root := Build(models, start, 5)

	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child (cycle), got %d", len(root.Children))
	}
	child := root.Children[0]
	if !child.Cycle {
		t.Errorf("expected cycle flag on recursive child, got %+v", child)
	}
	if len(child.Children) != 0 {
		t.Errorf("cycle leaf must have no children, got %d", len(child.Children))
	}
}

func TestBuildInterfaceDispatchFanOut(t *testing.T) {
	models := fixture()
	start := domain.SymbolRef{Package: "pkg/svc", Symbol: "Service.Generate"}
	root := Build(models, start, 5)

	if len(root.Children) != 2 {
		t.Fatalf("expected 2 interface impls as siblings, got %d", len(root.Children))
	}
	for _, c := range root.Children {
		if c.Via != "io.Reader" {
			t.Errorf("child %s should have Via=io.Reader, got %q", c.Symbol.Symbol, c.Via)
		}
	}
	syms := []string{root.Children[0].Symbol.Symbol, root.Children[1].Symbol.Symbol}
	if syms[0] != "fileReader.Read" || syms[1] != "memReader.Read" {
		t.Errorf("unexpected impl order: %v", syms)
	}
}

func TestBuildSiblingsNotFalseCycles(t *testing.T) {
	// Diamond: root calls helper twice. Neither call is a cycle.
	helper := domain.FunctionDef{Name: "Helper"}
	rootFn := domain.FunctionDef{
		Name: "Root",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/x", Symbol: "Helper"}},
			{To: domain.SymbolRef{Package: "pkg/x", Symbol: "Helper"}},
		},
	}
	models := []domain.PackageModel{
		{Path: "pkg/x", Name: "x", Functions: []domain.FunctionDef{rootFn, helper}},
	}
	start := domain.SymbolRef{Package: "pkg/x", Symbol: "Root"}
	root := Build(models, start, 5)

	// The addEdge dedup in the Go reader would normally collapse these,
	// but the builder itself must treat siblings independently.
	for _, c := range root.Children {
		if c.Cycle {
			t.Errorf("sibling should not be marked cycle: %+v", c)
		}
	}
}

func TestBuildNotFoundEdge(t *testing.T) {
	// A function whose callee is not in the loaded models.
	rootFn := domain.FunctionDef{
		Name: "Root",
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: "pkg/missing", Symbol: "Missing"}},
		},
	}
	models := []domain.PackageModel{
		{Path: "pkg/x", Name: "x", Functions: []domain.FunctionDef{rootFn}},
	}
	start := domain.SymbolRef{Package: "pkg/x", Symbol: "Root"}
	root := Build(models, start, 5)

	if len(root.Children) != 1 {
		t.Fatalf("expected one child, got %d", len(root.Children))
	}
	if !root.Children[0].NotFound {
		t.Errorf("expected NotFound flag on unresolved edge: %+v", root.Children[0])
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in      string
		wantPkg string
		wantSym string
		wantOK  bool
	}{
		{"internal/service.Service.Generate", "internal/service", "Service.Generate", true},
		{"internal/service.NewService", "internal/service", "NewService", true},
		{"pkg.Func", "pkg", "Func", true},
		{"pkg.Type.Method", "pkg", "Type.Method", true},
		{"nodot", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseTarget(tc.in)
		if ok != tc.wantOK {
			t.Errorf("ParseTarget(%q) ok=%v want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Package != tc.wantPkg || got.Symbol != tc.wantSym {
			t.Errorf("ParseTarget(%q) = %+v, want {%s %s}", tc.in, got, tc.wantPkg, tc.wantSym)
		}
	}
}
