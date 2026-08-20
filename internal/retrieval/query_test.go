package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/embed/noop"
	"github.com/kgatilin/archai/internal/adapter/lindex/bm25"
	"github.com/kgatilin/archai/internal/adapter/vindex/brute"
	"github.com/kgatilin/archai/internal/domain"
)

// TestExpandBFS tests graph expansion with hops and edge filtering.
func TestExpandBFS(t *testing.T) {
	// Build a test graph:
	//   A --calls--> B --uses--> C
	//   A --uses--> D
	//   E (isolated)
	graph := &Graph{
		Outgoing: map[string][]Edge{
			"A": {
				{From: "A", To: "B", Kind: EdgeCalls},
				{From: "A", To: "D", Kind: EdgeUses},
			},
			"B": {
				{From: "B", To: "C", Kind: EdgeUses},
			},
		},
		Incoming: map[string][]Edge{
			"B": {{From: "A", To: "B", Kind: EdgeCalls}},
			"C": {{From: "B", To: "C", Kind: EdgeUses}},
			"D": {{From: "A", To: "D", Kind: EdgeUses}},
		},
		NodesByID: map[string]Node{
			"A": {ID: "A", Kind: "func", Package: "pkg", Name: "A"},
			"B": {ID: "B", Kind: "func", Package: "pkg", Name: "B"},
			"C": {ID: "C", Kind: "class", Package: "pkg", Name: "C"},
			"D": {ID: "D", Kind: "iface", Package: "pkg", Name: "D"},
			"E": {ID: "E", Kind: "func", Package: "pkg", Name: "E"},
		},
	}

	t.Run("1 hop from A", func(t *testing.T) {
		nodes := graph.NeighborNodes([]string{"A"}, 1, nil, 0)
		// Should include A, B, D
		if !nodes["A"] || !nodes["B"] || !nodes["D"] {
			t.Errorf("expected A, B, D in 1-hop from A, got %v", nodes)
		}
		if nodes["C"] || nodes["E"] {
			t.Errorf("C and E should not be in 1-hop from A, got %v", nodes)
		}
	})

	t.Run("2 hops from A", func(t *testing.T) {
		nodes := graph.NeighborNodes([]string{"A"}, 2, nil, 0)
		// Should include A, B, D, C
		if !nodes["A"] || !nodes["B"] || !nodes["C"] || !nodes["D"] {
			t.Errorf("expected A, B, C, D in 2-hop from A, got %v", nodes)
		}
		if nodes["E"] {
			t.Errorf("E should not be in 2-hop from A, got %v", nodes)
		}
	})

	t.Run("1 hop from A with calls edge filter", func(t *testing.T) {
		nodes := graph.NeighborNodes([]string{"A"}, 1, []EdgeKind{EdgeCalls}, 0)
		// Should include A, B only (calls edge)
		if !nodes["A"] || !nodes["B"] {
			t.Errorf("expected A, B in 1-hop (calls only), got %v", nodes)
		}
		if nodes["D"] {
			t.Errorf("D should not be reachable via calls-only filter, got %v", nodes)
		}
	})

	t.Run("induced edges", func(t *testing.T) {
		nodeSet := map[string]bool{"A": true, "B": true, "D": true}
		edges := graph.InducedEdges(nodeSet)
		// Should have A->B (calls) and A->D (uses)
		if len(edges) != 2 {
			t.Errorf("expected 2 induced edges, got %d", len(edges))
		}
	})
}

// TestSearchDegradedPath tests search when dense is unavailable (BM25 only).
func TestSearchDegradedPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test source file
	srcDir := filepath.Join(tmpDir, "internal", "pkg")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(srcDir, "foo.go")
	if err := os.WriteFile(srcFile, []byte(`package pkg
// FooService handles foo operations.
type FooService struct {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create service WITHOUT a working embedder (nil embedder simulates unavailable)
	lidx := bm25.New()
	svc := NewService(tmpDir, nil, nil, lidx) // nil embedder and vindex

	// Build test model
	models := []domain.PackageModel{
		{
			Path: "internal/pkg",
			Name: "pkg",
			Structs: []domain.StructDef{
				{
					Name:       "FooService",
					Doc:        "FooService handles foo operations.",
					SourceFile: "foo.go",
					Span:       domain.Span{File: "internal/pkg/foo.go", StartByte: 0, EndByte: 70},
				},
			},
		},
	}

	// Index from models
	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		t.Fatalf("IndexFromModels failed: %v", err)
	}

	// Dense should not be available
	if svc.DenseAvailable() {
		t.Error("expected dense to be unavailable")
	}

	// Search should still work via BM25
	found, err := svc.Search(ctx, "FooService", SearchOptions{K: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if found.Dense {
		t.Error("expected Dense to be false")
	}

	if len(found.Hits) == 0 {
		t.Error("expected at least one result from BM25")
	}

	if len(found.Hits) > 0 && found.Hits[0].ID != "internal/pkg.FooService" {
		t.Errorf("expected FooService, got %s", found.Hits[0].ID)
	}

	// The lexical channel alone still carries the full mass, so a lone hit
	// gets all of it rather than the (1−β) share of a convex mix.
	if len(found.Hits) > 0 && (found.Hits[0].TextScore <= 0 || found.Hits[0].TextScore > 1) {
		t.Errorf("text score = %v, want a calibrated mass in (0,1]", found.Hits[0].TextScore)
	}
}

// TestSearchWithDenseAvailable tests search when both dense and BM25 are available.
func TestSearchWithDenseAvailable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test source file
	srcDir := filepath.Join(tmpDir, "internal", "svc")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(srcDir, "bar.go")
	if err := os.WriteFile(srcFile, []byte(`package svc
// BarHandler processes bar requests.
type BarHandler struct {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create service with noop embedder
	emb := noop.New()
	vidx := brute.New(emb.ID(), emb.Dim())
	lidx := bm25.New()
	svc := NewService(tmpDir, emb, vidx, lidx)

	// Build test model
	models := []domain.PackageModel{
		{
			Path: "internal/svc",
			Name: "svc",
			Structs: []domain.StructDef{
				{
					Name:       "BarHandler",
					Doc:        "BarHandler processes bar requests.",
					IsExported: true,
					SourceFile: "bar.go",
					Span:       domain.Span{File: "internal/svc/bar.go", StartByte: 0, EndByte: 80},
				},
			},
		},
	}

	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		t.Fatalf("IndexFromModels failed: %v", err)
	}

	// Dense should be available
	if !svc.DenseAvailable() {
		t.Error("expected dense to be available")
	}

	// Search should use both dense and BM25
	found, err := svc.Search(ctx, "BarHandler", SearchOptions{K: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if !found.Dense {
		t.Error("expected Dense to be true")
	}

	if len(found.Hits) == 0 {
		t.Error("expected at least one result")
	}

	if len(found.Hits) > 0 && found.Hits[0].ID != "internal/svc.BarHandler" {
		t.Errorf("expected BarHandler, got %s", found.Hits[0].ID)
	}
}

// TestNodeBodyReadViaSpan tests that Node reads body from file via Span.
func TestNodeBodyReadViaSpan(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test source file
	srcDir := filepath.Join(tmpDir, "internal", "test")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(srcDir, "code.go")
	content := `package test

// MyFunc does something.
func MyFunc() string {
	return "hello"
}
`
	if err := os.WriteFile(srcFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create service
	emb := noop.New()
	vidx := brute.New(emb.ID(), emb.Dim())
	lidx := bm25.New()
	svc := NewService(tmpDir, emb, vidx, lidx)

	// Build model with span pointing to function body
	funcStart := len("package test\n\n// MyFunc does something.\n")
	funcEnd := len(content)
	models := []domain.PackageModel{
		{
			Path: "internal/test",
			Name: "test",
			Functions: []domain.FunctionDef{
				{
					Name:       "MyFunc",
					Doc:        "MyFunc does something.",
					IsExported: true,
					SourceFile: "code.go",
					Span:       domain.Span{File: "internal/test/code.go", StartByte: funcStart, EndByte: funcEnd},
				},
			},
		},
	}

	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		t.Fatalf("IndexFromModels failed: %v", err)
	}

	// Get node detail
	detail, err := svc.Node(ctx, "internal/test.MyFunc")
	if err != nil {
		t.Fatalf("Node failed: %v", err)
	}

	if detail.NodeID == "" {
		t.Fatal("expected node to exist")
	}

	// Body should contain the function definition
	if detail.Body == "" {
		t.Error("expected body to be non-empty")
	}

	if detail.Body != content[funcStart:funcEnd] {
		t.Errorf("body mismatch\ngot: %q\nwant: %q", detail.Body, content[funcStart:funcEnd])
	}
}

// TestSearchFilters tests that filters work correctly.
func TestSearchFilters(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source files
	for _, dir := range []string{"internal/a", "internal/b"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(tmpDir, dir, "svc.go"),
			[]byte("package "+filepath.Base(dir)+"\n// Service impl\ntype Service struct{}\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	emb := noop.New()
	vidx := brute.New(emb.ID(), emb.Dim())
	lidx := bm25.New()
	svc := NewService(tmpDir, emb, vidx, lidx)

	models := []domain.PackageModel{
		{
			Path: "internal/a",
			Name: "a",
			Structs: []domain.StructDef{
				{Name: "Service", Doc: "Service impl", SourceFile: "svc.go",
					Span: domain.Span{File: "internal/a/svc.go", StartByte: 0, EndByte: 50}},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewService", Doc: "Factory", SourceFile: "svc.go",
					Span: domain.Span{File: "internal/a/svc.go", StartByte: 0, EndByte: 50}},
			},
		},
		{
			Path: "internal/b",
			Name: "b",
			Structs: []domain.StructDef{
				{Name: "Service", Doc: "Service impl", SourceFile: "svc.go",
					Span: domain.Span{File: "internal/b/svc.go", StartByte: 0, EndByte: 50}},
			},
		},
	}

	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		t.Fatal(err)
	}

	// Filters constrain what the query text may match — the seeds — and not
	// the region the diffusion grows around them, so every assertion here is
	// about the hits marked as seeds.
	t.Run("filter by package prefix", func(t *testing.T) {
		found, err := svc.Search(ctx, "Service", SearchOptions{K: 10, Filters: Filters{PackagePrefix: "internal/a"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range found.Hits {
			if !h.Seed {
				continue
			}
			if h.ID != "internal/a.Service" && h.ID != "internal/a.NewService" {
				t.Errorf("unexpected seed from package filter: %s", h.ID)
			}
		}
	})

	t.Run("filter by kind", func(t *testing.T) {
		found, err := svc.Search(ctx, "Service", SearchOptions{K: 10, Filters: Filters{Kinds: []string{"func"}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range found.Hits {
			if !h.Seed {
				continue
			}
			if h.Kind != "func" {
				t.Errorf("expected kind=func, got %s for %s", h.Kind, h.ID)
			}
		}
	})
}
