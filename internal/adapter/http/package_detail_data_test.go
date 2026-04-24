package http

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

func TestParseTab(t *testing.T) {
	cases := map[string]packageDetailTab{
		"":             tabOverview,
		"overview":     tabOverview,
		"public":       tabPublicAPI,
		"internal":     tabInternal,
		"dependencies": tabDependencies,
		"configs":      tabConfigs,
		"bogus":        tabOverview,
	}
	for in, want := range cases {
		if got := parseTab(in); got != want {
			t.Errorf("parseTab(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildTabs_ActiveMarked(t *testing.T) {
	tabs := buildTabs(tabDependencies)
	if len(tabs) != 5 {
		t.Fatalf("len = %d, want 5", len(tabs))
	}
	foundActive := ""
	for _, tab := range tabs {
		if tab.Active {
			foundActive = tab.ID
		}
	}
	if foundActive != string(tabDependencies) {
		t.Fatalf("active = %q, want %q", foundActive, tabDependencies)
	}
}

func TestBuildOutbound_InternalAndExternal(t *testing.T) {
	src := domain.PackageModel{
		Path: "internal/a",
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{Package: "internal/a", Symbol: "X"},
				To:   domain.SymbolRef{Package: "internal/b", Symbol: "Y"},
			},
			{
				From: domain.SymbolRef{Package: "internal/a", Symbol: "X"},
				To:   domain.SymbolRef{Package: "internal/b", Symbol: "Z"},
			},
			{
				From: domain.SymbolRef{Package: "internal/a", Symbol: "X"},
				To:   domain.SymbolRef{Package: "context", Symbol: "Context", External: true},
			},
			// Same-package dep — should be filtered.
			{
				From: domain.SymbolRef{Package: "internal/a", Symbol: "X"},
				To:   domain.SymbolRef{Package: "internal/a", Symbol: "Y"},
			},
		},
	}
	all := []domain.PackageModel{src, {Path: "internal/b"}}
	got := buildOutbound(src, all)
	if len(got) != 2 {
		t.Fatalf("got %d groups, want 2", len(got))
	}
	// Internal comes first.
	if got[0].TargetPkg != "internal/b" {
		t.Fatalf("got[0] = %q", got[0].TargetPkg)
	}
	if got[0].InternalPath != "internal/b" {
		t.Fatalf("InternalPath = %q", got[0].InternalPath)
	}
	if len(got[0].Symbols) != 2 {
		t.Fatalf("symbols = %v", got[0].Symbols)
	}
	// External second.
	if got[1].TargetPkg != "context" || !got[1].External {
		t.Fatalf("got[1] = %+v", got[1])
	}
	if got[1].InternalPath != "" {
		t.Fatalf("external InternalPath = %q, want empty", got[1].InternalPath)
	}
}

func TestBuildInbound_SkipsSelf(t *testing.T) {
	target := domain.PackageModel{Path: "internal/t"}
	a := domain.PackageModel{
		Path: "internal/a",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/t", Symbol: "X"}},
			{To: domain.SymbolRef{Package: "internal/other", Symbol: "Y"}},
		},
	}
	b := domain.PackageModel{
		Path: "internal/b",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/t", Symbol: "Z"}},
		},
	}
	// A self-reference from target should be ignored.
	self := domain.PackageModel{
		Path: "internal/t",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/t", Symbol: "W"}},
		},
	}
	got := buildInbound(target, []domain.PackageModel{a, b, self})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].SourcePkg != "internal/a" || got[1].SourcePkg != "internal/b" {
		t.Fatalf("order: %v, %v", got[0].SourcePkg, got[1].SourcePkg)
	}
}

func TestBuildConfigTypes_MatchesPackage(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/config",
		Structs: []domain.StructDef{
			{Name: "App", Doc: "app config"},
			{Name: "Other"},
		},
	}
	cfg := &overlay.Config{
		Module: "github.com/example/app",
		Configs: []string{
			"github.com/example/app/internal/config.App",
			"github.com/example/app/internal/other.Foo", // different package
		},
	}
	got := buildConfigTypes(pkg, cfg, "github.com/example/app")
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Name != "App" || got[0].Doc != "app config" {
		t.Fatalf("entry = %+v", got[0])
	}
}

func TestSplitFQTypeName(t *testing.T) {
	cases := []struct {
		in      string
		pkg     string
		typeSym string
	}{
		{"a/b/c.Foo", "a/b/c", "Foo"},
		{"Foo", "", ""},
		{".Foo", "", ""},
		{"a/b/c.", "", ""},
	}
	for _, c := range cases {
		p, s := splitFQTypeName(c.in)
		if p != c.pkg || s != c.typeSym {
			t.Errorf("%q → (%q,%q), want (%q,%q)", c.in, p, s, c.pkg, c.typeSym)
		}
	}
}

func TestRelToModule(t *testing.T) {
	if got := relToModule("github.com/x/y/internal/a", "github.com/x/y"); got != "internal/a" {
		t.Fatalf("got %q", got)
	}
	if got := relToModule("github.com/x/y", "github.com/x/y"); got != "." {
		t.Fatalf("got %q", got)
	}
	if got := relToModule("other.com/z", "github.com/x/y"); got != "other.com/z" {
		t.Fatalf("got %q", got)
	}
}

func TestUnexportedFilters(t *testing.T) {
	pkg := domain.PackageModel{
		Interfaces: []domain.InterfaceDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
		Structs:    []domain.StructDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
		Functions:  []domain.FunctionDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
		TypeDefs:   []domain.TypeDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
		Constants:  []domain.ConstDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
		Variables:  []domain.VarDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
		Errors:     []domain.ErrorDef{{Name: "Pub", IsExported: true}, {Name: "priv"}},
	}
	if n := len(unexportedInterfaces(pkg.Interfaces)); n != 1 {
		t.Errorf("interfaces = %d", n)
	}
	if n := len(unexportedStructs(pkg.Structs)); n != 1 {
		t.Errorf("structs = %d", n)
	}
	if n := len(unexportedFunctions(pkg.Functions)); n != 1 {
		t.Errorf("functions = %d", n)
	}
	if n := len(unexportedTypeDefs(pkg.TypeDefs)); n != 1 {
		t.Errorf("typedefs = %d", n)
	}
	if n := len(unexportedConstants(pkg.Constants)); n != 1 {
		t.Errorf("constants = %d", n)
	}
	if n := len(unexportedVariables(pkg.Variables)); n != 1 {
		t.Errorf("variables = %d", n)
	}
	if n := len(unexportedErrors(pkg.Errors)); n != 1 {
		t.Errorf("errors = %d", n)
	}
}
