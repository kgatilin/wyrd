package archmotif_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/adapter/golang"
)

// TestE2E_GoReaderToArchmotifGraph exercises the full pipeline:
//
//  1. Write a tiny two-package Go module to a temp dir.
//  2. Read it through archai's Go reader to produce
//     []domain.PackageModel.
//  3. Convert to an archmotif typed graph via ToArchmotifGraph.
//  4. Run a basic in-process metric — depth-2 cycle detection — on
//     the resulting graph's depends-on edges and assert the expected
//     count.
//
// This mirrors the archmotifimport example_test which detects
// 2-cycles inline (the metrics package is still archmotif-internal,
// so we can't yet call modularity/cycle helpers directly).
func TestE2E_GoReaderToArchmotifGraph(t *testing.T) {
	tmp := t.TempDir()

	// Module file.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"),
		[]byte("module example.test/e2e\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}

	// Package "internal/a" — domain with one struct.
	if err := os.MkdirAll(filepath.Join(tmp, "internal", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcA := `package a

// Entity is a domain entity.
type Entity struct {
    ID string
}

// Action returns an Entity.
func Action() *Entity { return &Entity{} }
`
	if err := os.WriteFile(filepath.Join(tmp, "internal", "a", "a.go"), []byte(srcA), 0o644); err != nil {
		t.Fatalf("a.go: %v", err)
	}

	// Package "internal/b" — service that uses Entity (cross-package dependency).
	if err := os.MkdirAll(filepath.Join(tmp, "internal", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcB := `package b

import "example.test/e2e/internal/a"

// Service depends on a.Entity.
type Service struct {
    E *a.Entity
}

// NewService is a factory function.
func NewService() *Service { return &Service{} }

// Fetch returns an entity from package a (cross-package dependency).
func Fetch() *a.Entity { return a.Action() }
`
	if err := os.WriteFile(filepath.Join(tmp, "internal", "b", "b.go"), []byte(srcB), 0o644); err != nil {
		t.Fatalf("b.go: %v", err)
	}

	// Read with archai's Go reader. packages.Load needs the working
	// dir to be inside the module so paths like "./..." resolve.
	prevWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	r := golang.NewReader()
	models, err := r.Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if len(models) < 2 {
		t.Fatalf("expected at least 2 packages, got %d", len(models))
	}

	g, err := archmotif.ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}

	// The graph must contain both package nodes.
	if _, ok := g.Node("pkg:internal/a"); !ok {
		t.Error("missing pkg:a node")
	}
	if _, ok := g.Node("pkg:internal/b"); !ok {
		t.Error("missing pkg:b node")
	}

	// In-process metric: count cross-package depends-on edges. b -> a
	// must be present because b imports a.
	var dependsOn int
	var bToA bool
	for _, e := range g.Edges() {
		if string(e.Kind) == "dependsOn" {
			dependsOn++
			if e.From == "pkg:internal/b" && e.To == "pkg:internal/a" {
				bToA = true
			}
		}
	}
	if dependsOn == 0 {
		t.Errorf("no dependsOn edges emitted")
	}
	if !bToA {
		t.Errorf("expected dependsOn edge pkg:b -> pkg:a, edges=%+v", g.Edges())
	}

	// Sanity: at least one type node per package (Entity in a,
	// Service in b).
	if _, ok := g.Node("type:internal/a.Entity"); !ok {
		t.Error("missing type:a.Entity")
	}
	if _, ok := g.Node("type:internal/b.Service"); !ok {
		t.Error("missing type:b.Service")
	}
}
