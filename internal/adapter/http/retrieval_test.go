package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/embed/noop"
	"github.com/kgatilin/archai/internal/adapter/lindex/bm25"
	"github.com/kgatilin/archai/internal/adapter/vindex/brute"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/retrieval"
	"github.com/kgatilin/archai/internal/serve"
)

// setupRetrievalTestServer creates a Server with a mock retrieval service for testing.
func setupRetrievalTestServer(t *testing.T) *Server {
	t.Helper()
	tmpDir := t.TempDir()

	// Create source files
	srcDir := filepath.Join(tmpDir, "internal", "handler")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(srcDir, "handler.go"),
		[]byte(`package handler

// Handler processes requests.
type Handler struct {
	name string
}

// NewHandler creates a handler.
func NewHandler() *Handler {
	return &Handler{name: "default"}
}
`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Build retrieval service manually with noop embedder
	emb := noop.New()
	vidx := brute.New(emb.ID(), emb.Dim())
	lidx := bm25.New()
	svc := retrieval.NewService(tmpDir, emb, vidx, lidx)

	// Build and index test models
	models := []domain.PackageModel{
		{
			Path: "internal/handler",
			Name: "handler",
			Structs: []domain.StructDef{
				{
					Name:       "Handler",
					Doc:        "Handler processes requests.",
					IsExported: true,
					SourceFile: "handler.go",
					Span:       domain.Span{File: "internal/handler/handler.go", StartByte: 17, EndByte: 80},
					Fields: []domain.FieldDef{
						{Name: "name", Type: domain.TypeRef{Name: "string"}},
					},
				},
			},
			Functions: []domain.FunctionDef{
				{
					Name:       "NewHandler",
					Doc:        "NewHandler creates a handler.",
					IsExported: true,
					SourceFile: "handler.go",
					Span:       domain.Span{File: "internal/handler/handler.go", StartByte: 82, EndByte: 180},
					Returns:    []domain.TypeRef{{Name: "Handler", IsPointer: true, Package: ""}},
				},
			},
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "internal/handler", Symbol: "NewHandler"},
					To:   domain.SymbolRef{Package: "internal/handler", Symbol: "Handler"},
					Kind: domain.DependencyReturns,
				},
			},
		},
	}

	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		t.Fatal(err)
	}

	// Create state with retrieval service
	state := serve.NewStateWithRetrieval(tmpDir, svc)

	server, err := NewServer(state)
	if err != nil {
		t.Fatal(err)
	}

	return server
}

func TestHandleAPISearch(t *testing.T) {
	server := setupRetrievalTestServer(t)

	body := `{"query": "Handler", "k": 10}`
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleAPISearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Results) == 0 {
		t.Error("expected at least one result")
	}

	// Verify dense flag is present in response
	// (should be true since we're using noop embedder)
	if !resp.Dense {
		t.Error("expected dense=true with noop embedder")
	}
}

func TestHandleAPISearchGraph(t *testing.T) {
	server := setupRetrievalTestServer(t)

	body := `{"query": "Handler", "k": 5, "hops": 1}`
	req := httptest.NewRequest(http.MethodPost, "/api/search_graph", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleAPISearchGraph(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp searchGraphResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Nodes) == 0 {
		t.Error("expected at least one node")
	}

	// Check that we got Handler
	nodeIDs := make(map[string]bool)
	for _, n := range resp.Nodes {
		nodeIDs[n.ID] = true
	}
	if !nodeIDs["internal/handler.Handler"] {
		t.Error("expected Handler node in subgraph")
	}
}

func TestHandleAPIExpand(t *testing.T) {
	server := setupRetrievalTestServer(t)

	body := `{"node_ids": ["internal/handler.Handler"], "hops": 1}`
	req := httptest.NewRequest(http.MethodPost, "/api/expand", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleAPIExpand(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp expandResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Nodes) == 0 {
		t.Error("expected at least one node")
	}

	// Handler should be in the result
	found := false
	for _, n := range resp.Nodes {
		if n.ID == "internal/handler.Handler" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Handler node in expansion")
	}
}

func TestHandleAPINode(t *testing.T) {
	server := setupRetrievalTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/node/internal/handler.Handler", nil)
	w := httptest.NewRecorder()

	server.handleAPINode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp retrieval.NodeDetail
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.NodeID != "internal/handler.Handler" {
		t.Errorf("expected node_id='internal/handler.Handler', got %q", resp.NodeID)
	}

	if resp.Kind != "class" {
		t.Errorf("expected kind='class', got %q", resp.Kind)
	}

	// Body should be populated from the source file
	if resp.Body == "" {
		t.Error("expected body to be non-empty")
	}
}

func TestHandleAPIRefresh(t *testing.T) {
	server := setupRetrievalTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	w := httptest.NewRecorder()

	server.handleAPIRefresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp refreshResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Note: reindexed may be 0 because the State.Snapshot().Packages is empty
	// (we only injected the retrieval service, not the packages).
	// This tests that the refresh endpoint works without crashing.
	// A full integration test would use State.Load() which populates packages.
	if resp.Reindexed < 0 {
		t.Error("expected reindexed >= 0")
	}
}

func TestHandleAPISearchMissingQuery(t *testing.T) {
	server := setupRetrievalTestServer(t)

	body := `{"k": 10}`
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleAPISearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleAPIExpandMissingNodeIDs(t *testing.T) {
	server := setupRetrievalTestServer(t)

	body := `{"hops": 1}`
	req := httptest.NewRequest(http.MethodPost, "/api/expand", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleAPIExpand(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleAPINodeNotFound(t *testing.T) {
	server := setupRetrievalTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/node/nonexistent.Symbol", nil)
	w := httptest.NewRecorder()

	server.handleAPINode(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
