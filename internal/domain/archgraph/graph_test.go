package archgraph_test

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/domain/archgraph"
)

// synthModule returns a small handwritten []domain.PackageModel that
// exercises every field projection has to round-trip: packages with
// interfaces, structs (fields + methods), functions (with calls),
// type defs, constants, variables, errors, dependencies, and
// implementations. Avoids the Go reader so the test isn't coupled
// to live source.
func synthModule() []domain.PackageModel {
	pkgA := "example.com/m/internal/svc"
	pkgB := "example.com/m/internal/repo"

	contextRef := domain.TypeRef{Name: "Context", Package: "context"}
	userRef := domain.TypeRef{Name: "User", Package: pkgA, IsPointer: true}
	errRef := domain.TypeRef{Name: "error"}

	userStruct := domain.StructDef{
		Name:       "User",
		IsExported: true,
		SourceFile: "user.go",
		Doc:        "User is the aggregate root.",
		Stereotype: domain.StereotypeAggregate,
		Fields: []domain.FieldDef{
			{Name: "ID", Type: domain.TypeRef{Name: "string"}, IsExported: true, Tag: `json:"id"`},
			{Name: "name", Type: domain.TypeRef{Name: "string"}},
		},
		Methods: []domain.MethodDef{
			{
				Name:       "Rename",
				IsExported: true,
				Params:     []domain.ParamDef{{Name: "n", Type: domain.TypeRef{Name: "string"}}},
				Returns:    []domain.TypeRef{errRef},
			},
		},
	}

	userService := domain.InterfaceDef{
		Name:       "UserService",
		IsExported: true,
		SourceFile: "service.go",
		Doc:        "UserService manages users.",
		Stereotype: domain.StereotypeService,
		Methods: []domain.MethodDef{
			{
				Name:       "Get",
				IsExported: true,
				Params: []domain.ParamDef{
					{Name: "ctx", Type: contextRef},
					{Name: "id", Type: domain.TypeRef{Name: "string"}},
				},
				Returns: []domain.TypeRef{userRef, errRef},
				Calls: []domain.CallEdge{
					{To: domain.SymbolRef{Package: pkgB, Symbol: "Repository.Find"}},
				},
			},
		},
	}

	newUserService := domain.FunctionDef{
		Name:       "NewUserService",
		IsExported: true,
		SourceFile: "factory.go",
		Doc:        "NewUserService is a constructor.",
		Stereotype: domain.StereotypeFactory,
		Params:     []domain.ParamDef{{Name: "r", Type: domain.TypeRef{Name: "Repository", Package: pkgB}}},
		Returns:    []domain.TypeRef{{Name: "UserService", Package: pkgA}},
		Calls: []domain.CallEdge{
			{To: domain.SymbolRef{Package: pkgA, Symbol: "userServiceImpl.init"}},
		},
	}

	status := domain.TypeDef{
		Name:           "Status",
		UnderlyingType: domain.TypeRef{Name: "string"},
		Constants:      []string{"StatusActive", "StatusInactive"},
		IsExported:     true,
		SourceFile:     "status.go",
		Stereotype:     domain.StereotypeEnum,
	}

	pkgAModel := domain.PackageModel{
		Path:       pkgA,
		Name:       "svc",
		Layer:      "application",
		Aggregate:  "User",
		Structs:    []domain.StructDef{userStruct},
		Interfaces: []domain.InterfaceDef{userService},
		Functions:  []domain.FunctionDef{newUserService},
		TypeDefs:   []domain.TypeDef{status},
		Constants: []domain.ConstDef{
			{Name: "DefaultPageSize", Type: domain.TypeRef{Name: "int"}, Value: "20", IsExported: true, SourceFile: "config.go"},
		},
		Variables: []domain.VarDef{
			{Name: "DefaultTimeout", Type: domain.TypeRef{Name: "Duration", Package: "time"}, IsExported: true, SourceFile: "config.go"},
		},
		Errors: []domain.ErrorDef{
			{Name: "ErrNotFound", Message: "user not found", IsExported: true, SourceFile: "errors.go"},
		},
		Dependencies: []domain.Dependency{
			{
				From:            domain.SymbolRef{Package: pkgA, File: "service.go", Symbol: "UserService"},
				To:              domain.SymbolRef{Package: pkgB, File: "repository.go", Symbol: "Repository"},
				Kind:            domain.DependencyUses,
				ThroughExported: true,
			},
		},
		Implementations: []domain.Implementation{
			{
				Concrete:  domain.SymbolRef{Package: pkgA, File: "service.go", Symbol: "userServiceImpl"},
				Interface: domain.SymbolRef{Package: pkgA, File: "service.go", Symbol: "UserService"},
				IsPointer: true,
			},
		},
	}

	pkgBModel := domain.PackageModel{
		Path:  pkgB,
		Name:  "repo",
		Layer: "adapter",
		Interfaces: []domain.InterfaceDef{
			{
				Name:       "Repository",
				IsExported: true,
				SourceFile: "repository.go",
				Stereotype: domain.StereotypeRepository,
				Methods: []domain.MethodDef{
					{
						Name:       "Find",
						IsExported: true,
						Params: []domain.ParamDef{
							{Name: "ctx", Type: contextRef},
							{Name: "id", Type: domain.TypeRef{Name: "string"}},
						},
						Returns: []domain.TypeRef{userRef, errRef},
					},
				},
			},
		},
	}

	return []domain.PackageModel{pkgAModel, pkgBModel}
}

// TestBuildGraph_FromFixture builds a graph from the live archai
// codebase via the Go reader and asserts non-zero counts for every
// major node kind. This is the closest thing to a "from a small Go
// module" fixture without inventing a new testdata tree.
func TestBuildGraph_FromFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("integration-style read of archai sources skipped in -short")
	}
	repoRoot := repoRootFromTest(t)
	reader := golang.NewReader()
	models, err := reader.Read(context.Background(), []string{filepath.Join(repoRoot, "internal", "domain", "...")})
	if err != nil {
		t.Fatalf("reader.Read: %v", err)
	}
	if len(models) == 0 {
		t.Fatalf("reader returned no packages")
	}

	g, err := archgraph.BuildGraph(models, &domain.Module{Path: "github.com/kgatilin/archai"})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	counts := map[archgraph.NodeKind]int{}
	for _, n := range g.Nodes {
		counts[n.Kind]++
	}

	for _, kind := range []archgraph.NodeKind{
		archgraph.NodeKindPackage,
		archgraph.NodeKindStruct,
		archgraph.NodeKindFunction,
		archgraph.NodeKindFile,
	} {
		if counts[kind] == 0 {
			t.Errorf("expected non-zero nodes of kind %s, got 0", kind)
		}
	}
	if g.ModuleID == "" {
		t.Errorf("expected module node id to be set when Module was passed")
	}
	if len(g.Edges) == 0 {
		t.Fatalf("expected edges, got 0")
	}
}

// TestProjectPackages_RoundTrip — BuildGraph then ProjectPackages
// reconstructs the input modulo deterministic sort order.
func TestProjectPackages_RoundTrip(t *testing.T) {
	in := synthModule()
	g, err := archgraph.BuildGraph(in, &domain.Module{Path: "example.com/m"})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	out := g.ProjectPackages()
	want := archgraph.NormalizePackages(in)

	if !reflect.DeepEqual(out, want) {
		t.Fatalf("round-trip mismatch.\nwant: %#v\ngot:  %#v", want, out)
	}
}

// TestGraphIDs_AreStable — building the same graph twice on the same
// input yields identical id sets and edge sets.
func TestGraphIDs_AreStable(t *testing.T) {
	in := synthModule()
	g1, err := archgraph.BuildGraph(in, &domain.Module{Path: "example.com/m"})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := archgraph.BuildGraph(in, &domain.Module{Path: "example.com/m"})
	if err != nil {
		t.Fatal(err)
	}

	if len(g1.Nodes) != len(g2.Nodes) {
		t.Fatalf("node count drift: %d vs %d", len(g1.Nodes), len(g2.Nodes))
	}
	for i := range g1.Nodes {
		if g1.Nodes[i].ID != g2.Nodes[i].ID {
			t.Fatalf("node id drift at %d: %q vs %q", i, g1.Nodes[i].ID, g2.Nodes[i].ID)
		}
		if g1.Nodes[i].Kind != g2.Nodes[i].Kind {
			t.Fatalf("node kind drift at %s: %s vs %s", g1.Nodes[i].ID, g1.Nodes[i].Kind, g2.Nodes[i].Kind)
		}
	}
	if len(g1.Edges) != len(g2.Edges) {
		t.Fatalf("edge count drift: %d vs %d", len(g1.Edges), len(g2.Edges))
	}
	for i := range g1.Edges {
		if g1.Edges[i].ID != g2.Edges[i].ID {
			t.Fatalf("edge id drift at %d: %q vs %q", i, g1.Edges[i].ID, g2.Edges[i].ID)
		}
	}
}

// TestContainmentEdges — package contains files; file contains
// types/functions; type contains methods/fields.
func TestContainmentEdges(t *testing.T) {
	in := synthModule()
	g, err := archgraph.BuildGraph(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	pkgID := archgraph.PackageID("example.com/m/internal/svc")
	fileID := archgraph.FileID("example.com/m/internal/svc", "user.go")
	structID := archgraph.TypeID("example.com/m/internal/svc", "User")
	methodID := archgraph.MethodID("example.com/m/internal/svc", "User", "Rename")
	fieldID := archgraph.FieldID("example.com/m/internal/svc", "User", "ID")

	if !hasEdge(g, pkgID, fileID, archgraph.EdgeKindContains) {
		t.Errorf("expected package->file contains edge")
	}
	if !hasEdge(g, fileID, structID, archgraph.EdgeKindContains) {
		t.Errorf("expected file->struct contains edge")
	}
	if !hasEdge(g, pkgID, structID, archgraph.EdgeKindContains) {
		t.Errorf("expected package->struct contains edge")
	}
	if !hasEdge(g, structID, methodID, archgraph.EdgeKindContains) {
		t.Errorf("expected struct->method contains edge")
	}
	if !hasEdge(g, structID, fieldID, archgraph.EdgeKindContains) {
		t.Errorf("expected struct->field contains edge")
	}
}

// TestDependencyEdges — verify uses, implements, and calls edges
// land in the graph as expected.
func TestDependencyEdges(t *testing.T) {
	in := synthModule()
	g, err := archgraph.BuildGraph(in, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Implements: userServiceImpl => UserService
	implFrom := archgraph.TypeID("example.com/m/internal/svc", "userServiceImpl")
	implTo := archgraph.TypeID("example.com/m/internal/svc", "UserService")
	if !hasEdge(g, implFrom, implTo, archgraph.EdgeKindImplements) {
		t.Errorf("expected implements edge userServiceImpl=>UserService")
	}

	// Uses: UserService -> Repository
	usesFrom := archgraph.TypeID("example.com/m/internal/svc", "UserService")
	usesTo := archgraph.TypeID("example.com/m/internal/repo", "Repository")
	if !hasEdge(g, usesFrom, usesTo, archgraph.EdgeKindUses) {
		t.Errorf("expected uses edge UserService->Repository")
	}

	// Calls: NewUserService -> userServiceImpl.init
	callFrom := archgraph.FunctionID("example.com/m/internal/svc", "NewUserService")
	hasCall := false
	for _, e := range g.EdgesFrom(callFrom) {
		if e.Kind == archgraph.EdgeKindCalls {
			hasCall = true
			break
		}
	}
	if !hasCall {
		t.Errorf("expected at least one calls edge from NewUserService")
	}
}

func hasEdge(g *archgraph.Graph, from, to string, kind archgraph.EdgeKind) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

// repoRootFromTest walks up from the test file location to find the
// archai repo root (containing go.mod). Returns the absolute path.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	// file is .../internal/domain/archgraph/graph_test.go;
	// repo root is three dirs up.
	dir := filepath.Dir(file)
	for i := 0; i < 3; i++ {
		dir = filepath.Dir(dir)
	}
	return dir
}
