package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/overlay"
)

// imp builds a PackageModel whose only link to other packages is its import
// list (module-relative paths, the reader's form), with no symbol dependency.
func imp(rel, layer string, imports ...string) domain.PackageModel {
	return domain.PackageModel{Path: full(rel), Layer: layer, Imports: imports}
}

func TestCheckImportOnlyEdgeIsUnlisted(t *testing.T) {
	models := []domain.PackageModel{
		imp("b", "app"),
		imp("a", "app", "b"),
	}
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"c -> b"}})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Kind != kindUnlistedEdge || vs[0].From != "a" || vs[0].To != "b" {
		t.Fatalf("want one unlisted-edge a -> b, got %+v", vs)
	}
}

func TestCheckImportEdgeAllowed(t *testing.T) {
	models := []domain.PackageModel{
		imp("b", "app"),
		imp("a", "app", "b"),
	}
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"a -> b"}})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("want no violations, got %+v", vs)
	}
}

func TestCheckImportEdgeForbidden(t *testing.T) {
	models := []domain.PackageModel{
		imp("infra", "infrastructure"),
		imp("core", "domain", "infra"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Forbid:        []string{"@domain !-> @infrastructure"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Kind != kindForbiddenEdge || vs[0].From != "core" || vs[0].To != "infra" {
		t.Fatalf("want one forbidden-edge core -> infra, got %+v", vs)
	}
}

func TestCheckImportEdgesCountForReachability(t *testing.T) {
	models := []domain.PackageModel{
		imp("c", ""),
		imp("b", "", "c"),
		pkg("a", "", "b"), // a symbol dependency then an import: one path
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Reachability:  []string{"a !~> c"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Kind != kindReachability {
		t.Fatalf("want one reachability violation, got %+v", vs)
	}
	if got := vs[0].Path; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("path = %v, want [a b c]", got)
	}
}

func TestCheckImportAndDependencyMakeOneEdge(t *testing.T) {
	a := pkg("a", "", "b")
	a.Imports = []string{"b"}
	models := []domain.PackageModel{imp("b", ""), a}
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"c -> b"}})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want one violation for the single a -> b edge, got %+v", vs)
	}
}

func TestCheckImportOfUnparsedPackageIgnored(t *testing.T) {
	// A scan scoped to a subset of packages does not see the rest; an import
	// of a package outside the scan is not an edge, as for a dependency.
	models := []domain.PackageModel{imp("a", "", "elsewhere", "a")}
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"c -> b"}})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("want no violations, got %+v", vs)
	}
}

func TestParseImportsKind(t *testing.T) {
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"a -> b (imports, type)"}})
	if got := spec.Rules[0].Kinds; len(got) != 2 || got[0] != "imports" || got[1] != "type" {
		t.Fatalf("kinds = %v, want [imports type]", got)
	}
}

// TestCheckImportsFromSource runs the Go reader over a module in which each
// package reaches the target package in one way that leaves no type in a
// declaration, and checks that every one of them is a policy edge — and that
// an import made only by a _test.go file is not.
func TestCheckImportsFromSource(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	const module = "test.example/garden"
	write("go.mod", "module "+module+"\n\ngo 1.21\n")
	write("soil/soil.go", `package soil

// Depth is a constant.
const Depth = 4

// Kind returns the soil kind.
func Kind() string { return "loam" }

// Bed is reached through an inferred value.
type Bed struct{}

// NewBed builds a Bed.
func NewBed() *Bed { return &Bed{} }

// Water is called on an inferred *Bed.
func (b *Bed) Water() string { return "wet" }
`)
	write("blank/blank.go", `package blank

import _ "test.example/garden/soil"
`)
	write("call/call.go", `package call

import "test.example/garden/soil"

// Describe uses soil only through a call whose result type is inferred.
func Describe() string {
	k := soil.Kind()
	return k
}
`)
	write("constant/constant.go", `package constant

import "test.example/garden/soil"

// Twice is computed from another package's constant.
const Twice = soil.Depth * 2
`)
	write("variable/variable.go", `package variable

import "test.example/garden/soil"

// Kind is a package variable initialised from a call.
var Kind = soil.Kind()
`)
	write("method/method.go", `package method

import "test.example/garden/soil"

// Tend calls a method on a value whose type is inferred.
func Tend() string {
	b := soil.NewBed()
	return b.Water()
}
`)
	write("testonly/testonly.go", `package testonly

// Idle imports nothing.
func Idle() {}
`)
	write("testonly/testonly_test.go", `package testonly

import (
	"testing"

	"test.example/garden/soil"
)

func TestIdle(t *testing.T) { _ = soil.Kind() }
`)
	t.Chdir(root)

	models, err := golang.NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	cfg := &overlay.Config{Module: module}
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"nothing -> soil"}})
	vs, err := Check(spec, models, cfg)
	if err != nil {
		t.Fatal(err)
	}
	edges := map[string]bool{}
	for _, v := range vs {
		if v.Kind != kindUnlistedEdge {
			t.Errorf("unexpected violation kind: %+v", v)
		}
		edges[v.From+" -> "+v.To] = true
	}

	for _, tc := range []struct {
		name string
		from string
		want bool
	}{
		{"blank import", "blank", true},
		{"call only", "call", true},
		{"constant only", "constant", true},
		{"variable from a call", "variable", true},
		{"method call on an inferred value", "method", true},
		{"import from a _test.go file", "testonly", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := edges[tc.from+" -> soil"]; got != tc.want {
				t.Errorf("edge %s -> soil reported = %v, want %v (violations: %+v)", tc.from, got, tc.want, vs)
			}
		})
	}
	if len(vs) != 5 {
		t.Errorf("want exactly 5 violations, got %d: %+v", len(vs), vs)
	}
}
