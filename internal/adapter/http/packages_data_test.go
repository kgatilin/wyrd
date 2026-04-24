package http

import (
	"reflect"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// testPkg is a compact constructor that builds a PackageModel with
// enough symbols to exercise the filter/summary paths without dragging
// the whole domain surface into every test case.
func testPkg(path, name, layer string, opts ...func(*domain.PackageModel)) domain.PackageModel {
	p := domain.PackageModel{Path: path, Name: name, Layer: layer}
	for _, o := range opts {
		o(&p)
	}
	return p
}

func withIface(name string, stereo domain.Stereotype) func(*domain.PackageModel) {
	return func(p *domain.PackageModel) {
		p.Interfaces = append(p.Interfaces, domain.InterfaceDef{
			Name: name, IsExported: true, Stereotype: stereo,
		})
	}
}

func withStruct(name string, exported bool) func(*domain.PackageModel) {
	return func(p *domain.PackageModel) {
		p.Structs = append(p.Structs, domain.StructDef{Name: name, IsExported: exported})
	}
}

func withFunc(name string, exported bool) func(*domain.PackageModel) {
	return func(p *domain.PackageModel) {
		p.Functions = append(p.Functions, domain.FunctionDef{Name: name, IsExported: exported})
	}
}

func TestMatchesFilter_Layer(t *testing.T) {
	pkg := testPkg("internal/a", "a", "domain")
	if !matchesFilter(pkg, packageFilter{Layer: "domain"}) {
		t.Fatal("expected match on layer=domain")
	}
	if matchesFilter(pkg, packageFilter{Layer: "service"}) {
		t.Fatal("expected no match on layer=service")
	}
	if !matchesFilter(pkg, packageFilter{}) {
		t.Fatal("empty filter must accept")
	}
}

func TestMatchesFilter_Stereotype(t *testing.T) {
	pkg := testPkg("x", "x", "", withIface("R", domain.StereotypeRepository))
	if !matchesFilter(pkg, packageFilter{Stereotype: "repository"}) {
		t.Fatal("expected match on stereotype=repository")
	}
	if matchesFilter(pkg, packageFilter{Stereotype: "service"}) {
		t.Fatal("expected no match on stereotype=service")
	}
}

func TestMatchesFilter_Search(t *testing.T) {
	pkg := testPkg("internal/reader", "reader", "",
		withIface("ModelReader", domain.StereotypeNone),
		withFunc("NewReader", true),
	)
	cases := []struct {
		name   string
		needle string
		want   bool
	}{
		{"path match", "internal/read", true},
		{"name match", "reader", true},
		{"symbol match", "ModelReader", true},
		{"case insensitive", "newreader", true},
		{"no match", "writer", false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := matchesFilter(pkg, packageFilter{Search: c.needle})
			if got != c.want {
				t.Fatalf("needle=%q got=%v want=%v", c.needle, got, c.want)
			}
		})
	}
}

func TestSummarize_Counts(t *testing.T) {
	pkg := testPkg("x", "x", "adapter",
		withIface("A", domain.StereotypeNone),
		withIface("B", domain.StereotypeService),
		withStruct("S1", true),
		withStruct("S2", false),
		withFunc("F", true),
	)
	s := pkgSummarize(pkg)
	if s.Interfaces != 2 || s.Structs != 2 || s.Functions != 1 {
		t.Fatalf("counts: %+v", s)
	}
	if s.SymbolCount != 5 {
		t.Fatalf("SymbolCount = %d, want 5", s.SymbolCount)
	}
	if !reflect.DeepEqual(s.Stereotypes, []string{"service"}) {
		t.Fatalf("Stereotypes = %v, want [service]", s.Stereotypes)
	}
	if s.Layer != "adapter" {
		t.Fatalf("Layer = %q", s.Layer)
	}
}

func TestBuildPackageSummaries_Sorted(t *testing.T) {
	pkgs := []domain.PackageModel{
		testPkg("zeta", "zeta", ""),
		testPkg("alpha", "alpha", ""),
		testPkg("mid", "mid", ""),
	}
	got := buildPackageSummaries(pkgs, packageFilter{})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Path != "alpha" || got[1].Path != "mid" || got[2].Path != "zeta" {
		t.Fatalf("order: %v %v %v", got[0].Path, got[1].Path, got[2].Path)
	}
}

func TestBuildPackageTree_Hierarchy(t *testing.T) {
	summaries := []packageSummary{
		{Path: ".", Name: "root"},
		{Path: "internal/a", Name: "a"},
		{Path: "internal/a/sub", Name: "sub"},
		{Path: "internal/b", Name: "b"},
	}
	tree := buildPackageTree(summaries)
	if len(tree) != 2 {
		t.Fatalf("top-level count = %d, want 2 (. and internal)", len(tree))
	}
	// Children are sorted by name: "." before "internal".
	if tree[0].Name != "." {
		t.Fatalf("tree[0].Name = %q, want .", tree[0].Name)
	}
	if tree[1].Name != "internal" {
		t.Fatalf("tree[1].Name = %q, want internal", tree[1].Name)
	}
	// "internal" has no package of its own.
	if tree[1].Package != nil {
		t.Fatal("intermediate dir must not carry a package")
	}
	// internal has two children (a, b).
	if len(tree[1].Children) != 2 {
		t.Fatalf("internal.Children = %d, want 2", len(tree[1].Children))
	}
	// internal/a has a sub.
	a := tree[1].Children[0]
	if a.Name != "a" {
		t.Fatalf("a.Name = %q", a.Name)
	}
	if a.Package == nil {
		t.Fatal("a must carry a package")
	}
	if len(a.Children) != 1 || a.Children[0].Name != "sub" {
		t.Fatalf("a.sub missing, got %v", a.Children)
	}
	if a.Children[0].FullPath != "internal/a/sub" {
		t.Fatalf("sub.FullPath = %q", a.Children[0].FullPath)
	}
}

func TestCollectLayerOptions_Sorted(t *testing.T) {
	pkgs := []domain.PackageModel{
		{Layer: "service"},
		{Layer: "domain"},
		{Layer: "service"},
		{Layer: ""}, // ignored
	}
	got := collectLayerOptions(pkgs)
	want := []string{"domain", "service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCollectStereotypeOptions_Sorted(t *testing.T) {
	pkgs := []domain.PackageModel{
		testPkg("a", "a", "", withIface("I", domain.StereotypeService)),
		testPkg("b", "b", "", withIface("R", domain.StereotypeRepository)),
		testPkg("c", "c", "", withIface("X", domain.StereotypeService)),
	}
	got := collectStereotypeOptions(pkgs)
	want := []string{"repository", "service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
