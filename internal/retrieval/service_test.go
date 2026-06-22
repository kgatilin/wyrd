package retrieval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestService_Index(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &testEmbedder{dim: 64}
	vidx := newTestVectorIndex(emb.ID())
	lidx := newTestLexicalIndex()

	svc := NewService(tmpDir, emb, vidx, lidx)

	nodes := []Node{
		{
			ID:         "pkg.Func1",
			Kind:       "func",
			Package:    "pkg",
			Name:       "Func1",
			Signature:  "Func1() error",
			Doc:        "Func1 does something",
			Span:       domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 20, StartLine: 1, EndLine: 1},
			Embeddable: true,
		},
		{
			ID:         "pkg.Const1",
			Kind:       "const",
			Package:    "pkg",
			Name:       "Const1",
			Signature:  "Const1 = 42",
			Embeddable: false, // Not embeddable
		},
	}

	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Vector index should have only embeddable node
	if vidx.Len() != 1 {
		t.Errorf("expected 1 vector, got %d", vidx.Len())
	}

	// Lexical index should have ALL nodes
	if lidx.Len() != 2 {
		t.Errorf("expected 2 documents, got %d", lidx.Len())
	}

	// Dense should be available
	if !svc.DenseAvailable() {
		t.Error("dense should be available")
	}
}

func TestService_IndexSkipsUnchangedHashes(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	vidx := newTestVectorIndex(emb.ID())
	lidx := newTestLexicalIndex()

	svc := NewService(tmpDir, emb, vidx, lidx)

	nodes := []Node{
		{
			ID:         "pkg.Func1",
			Kind:       "func",
			Package:    "pkg",
			Name:       "Func1",
			Signature:  "Func1() error",
			Span:       domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 20, StartLine: 1, EndLine: 1},
			Embeddable: true,
		},
	}

	// First index
	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index 1: %v", err)
	}

	firstCallCount := emb.callCount

	// Second index with same content - should skip embedding
	err = svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index 2: %v", err)
	}

	if emb.callCount != firstCallCount {
		t.Errorf("expected no new embed calls, got %d more", emb.callCount-firstCallCount)
	}
}

func TestService_IndexRemovesDeletedNodes(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &testEmbedder{dim: 64}
	vidx := newTestVectorIndex(emb.ID())
	lidx := newTestLexicalIndex()

	svc := NewService(tmpDir, emb, vidx, lidx)

	// Index two nodes
	nodes := []Node{
		{ID: "pkg.A", Kind: "func", Package: "pkg", Name: "A", Signature: "A()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 10, StartLine: 1, EndLine: 1}},
		{ID: "pkg.B", Kind: "func", Package: "pkg", Name: "B", Signature: "B()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 10, EndByte: 20, StartLine: 2, EndLine: 2}},
	}

	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index 1: %v", err)
	}

	if vidx.Len() != 2 {
		t.Errorf("expected 2 vectors, got %d", vidx.Len())
	}

	// Re-index with only one node (B deleted)
	nodes = []Node{
		{ID: "pkg.A", Kind: "func", Package: "pkg", Name: "A", Signature: "A()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 10, StartLine: 1, EndLine: 1}},
	}

	err = svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index 2: %v", err)
	}

	// B should be removed from vector index
	if vidx.Len() != 1 {
		t.Errorf("expected 1 vector after removal, got %d", vidx.Len())
	}

	// Search should not find B
	results := vidx.Search([]float32{1, 0}, 10)
	for _, r := range results {
		if r.ID == "pkg.B" {
			t.Error("removed node should not appear in search results")
		}
	}
}

func TestService_GracefulDegradationOnEmbedderError(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	// Use an embedder that always fails
	emb := &failingEmbedder{}
	vidx := newTestVectorIndex("failing:model")
	lidx := newTestLexicalIndex()

	svc := NewService(tmpDir, emb, vidx, lidx)

	nodes := []Node{
		{ID: "pkg.Func1", Kind: "func", Package: "pkg", Name: "Func1", Signature: "Func1()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 20, StartLine: 1, EndLine: 1}},
	}

	// Index should NOT error - it should gracefully disable dense
	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index should not error on embed failure: %v", err)
	}

	// Dense should be unavailable
	if svc.DenseAvailable() {
		t.Error("dense should be unavailable after embedder failure")
	}

	// But BM25 should still work
	if lidx.Len() != 1 {
		t.Errorf("expected 1 document in lexical index, got %d", lidx.Len())
	}

	results := lidx.Search("Func1", 10)
	if len(results) != 1 {
		t.Errorf("expected 1 lexical result, got %d", len(results))
	}
}

func TestService_Refresh(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &testEmbedder{dim: 64}
	vidx := newTestVectorIndex(emb.ID())
	lidx := newTestLexicalIndex()

	svc := NewService(tmpDir, emb, vidx, lidx)

	// Initial index
	nodes := []Node{
		{ID: "pkg.A", Kind: "func", Package: "pkg", Name: "A", Signature: "A()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 10, StartLine: 1, EndLine: 1}},
		{ID: "pkg.B", Kind: "func", Package: "pkg", Name: "B", Signature: "B()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 10, EndByte: 20, StartLine: 2, EndLine: 2}},
	}

	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Refresh: update A, remove B, add C
	changedNodes := []Node{
		{ID: "pkg.A", Kind: "func", Package: "pkg", Name: "A", Signature: "A() error", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 15, StartLine: 1, EndLine: 1}},
		{ID: "pkg.C", Kind: "func", Package: "pkg", Name: "C", Signature: "C()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 20, EndByte: 30, StartLine: 3, EndLine: 3}},
	}
	removedIDs := []string{"pkg.B"}

	err = svc.Refresh(context.Background(), changedNodes, removedIDs)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// B should be gone from lexical
	results := lidx.Search("B", 10)
	for _, r := range results {
		if r.ID == "pkg.B" {
			t.Error("B should be removed from lexical index")
		}
	}

	// C should be searchable
	results = lidx.Search("C", 10)
	found := false
	for _, r := range results {
		if r.ID == "pkg.C" {
			found = true
			break
		}
	}
	if !found {
		t.Error("C should be in lexical index")
	}
}

func TestService_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &testEmbedder{dim: 64}
	vidx := newTestVectorIndex(emb.ID())
	lidx := newTestLexicalIndex()

	svc := NewService(tmpDir, emb, vidx, lidx)

	nodes := []Node{
		{ID: "pkg.Func1", Kind: "func", Package: "pkg", Name: "Func1", Signature: "Func1()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 20, StartLine: 1, EndLine: 1}},
	}

	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Save should not error with test doubles
	err = svc.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load should not error with test doubles
	err = svc.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Note: Test doubles don't persist, so we verify the methods don't error.
	// Actual persistence is tested in adapter-level tests (brute_test.go, bm25_test.go).
}

func TestService_NilIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	// Service with no vector index
	lidx := newTestLexicalIndex()
	svc := NewService(tmpDir, nil, nil, lidx)

	nodes := []Node{
		{ID: "pkg.Func1", Kind: "func", Package: "pkg", Name: "Func1", Signature: "Func1()", Embeddable: true,
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 20, StartLine: 1, EndLine: 1}},
	}

	// Should not panic
	err := svc.Index(context.Background(), nodes)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Dense should be unavailable
	if svc.DenseAvailable() {
		t.Error("dense should be unavailable with nil embedder")
	}

	// But BM25 should work
	if lidx.Len() != 1 {
		t.Errorf("expected 1 document, got %d", lidx.Len())
	}
}

// Helper to set up test source files
func setupTestSource(t *testing.T, root string) {
	t.Helper()
	srcDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("creating src dir: %v", err)
	}
	srcContent := `func Func1() error {
	return nil
}

func Func2() {
	// another function
}`
	if err := os.WriteFile(filepath.Join(srcDir, "code.go"), []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
}

// testEmbedder is a deterministic embedder for testing.
type testEmbedder struct {
	dim int
}

func (e *testEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = make([]float32, e.dim)
		// Simple deterministic pattern based on text length
		for j := range results[i] {
			results[i][j] = float32(len(texts[i])+i+j) / 100.0
		}
	}
	return results, nil
}

func (e *testEmbedder) Dim() int {
	return e.dim
}

func (e *testEmbedder) ID() string {
	return "test:embedder"
}

// countingEmbedder wraps an embedder and counts calls
type countingEmbedder struct {
	testEmbedder
	callCount int
}

func (e *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.callCount++
	return e.testEmbedder.Embed(ctx, texts)
}

// failingEmbedder always returns an error
type failingEmbedder struct{}

func (e *failingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("embedder unavailable")
}

func (e *failingEmbedder) Dim() int {
	return 64
}

func (e *failingEmbedder) ID() string {
	return "failing:model"
}

// testVectorIndex is a simple in-memory vector index for testing.
type testVectorIndex struct {
	embedderID string
	vectors    map[string][]float32
	hashes     map[string]string
}

func newTestVectorIndex(embedderID string) *testVectorIndex {
	return &testVectorIndex{
		embedderID: embedderID,
		vectors:    make(map[string][]float32),
		hashes:     make(map[string]string),
	}
}

func (idx *testVectorIndex) Upsert(id string, vec []float32) {
	idx.vectors[id] = vec
}

func (idx *testVectorIndex) UpsertWithHash(id string, vec []float32, contentHash string) {
	idx.vectors[id] = vec
	idx.hashes[id] = contentHash
}

func (idx *testVectorIndex) Remove(id string) {
	delete(idx.vectors, id)
	delete(idx.hashes, id)
}

func (idx *testVectorIndex) Search(vec []float32, k int) []Scored {
	// Simple implementation - return all with dummy scores
	results := make([]Scored, 0, len(idx.vectors))
	for id := range idx.vectors {
		results = append(results, Scored{ID: id, Score: 1.0})
	}
	if k < len(results) {
		return results[:k]
	}
	return results
}

func (idx *testVectorIndex) Len() int {
	return len(idx.vectors)
}

func (idx *testVectorIndex) GetHash(id string) string {
	return idx.hashes[id]
}

func (idx *testVectorIndex) IDs() []string {
	ids := make([]string, 0, len(idx.vectors))
	for id := range idx.vectors {
		ids = append(ids, id)
	}
	return ids
}

func (idx *testVectorIndex) EmbedderID() string {
	return idx.embedderID
}

func (idx *testVectorIndex) Save(path string) error {
	return nil // Noop for tests
}

func (idx *testVectorIndex) Load(path string) error {
	return nil // Noop for tests
}

// testLexicalIndex is a simple in-memory lexical index for testing.
type testLexicalIndex struct {
	docs map[string]string
}

func newTestLexicalIndex() *testLexicalIndex {
	return &testLexicalIndex{
		docs: make(map[string]string),
	}
}

func (idx *testLexicalIndex) Upsert(id, text string) {
	idx.docs[id] = text
}

func (idx *testLexicalIndex) Remove(id string) {
	delete(idx.docs, id)
}

func (idx *testLexicalIndex) Search(query string, k int) []Scored {
	// Simple substring match
	results := make([]Scored, 0)
	for id, text := range idx.docs {
		if containsIgnoreCase(text, query) {
			results = append(results, Scored{ID: id, Score: 1.0})
		}
	}
	if k < len(results) {
		return results[:k]
	}
	return results
}

func (idx *testLexicalIndex) Len() int {
	return len(idx.docs)
}

func (idx *testLexicalIndex) Save(path string) error {
	return nil // Noop for tests
}

func (idx *testLexicalIndex) Load(path string) error {
	return nil // Noop for tests
}

func containsIgnoreCase(text, substr string) bool {
	return len(text) > 0 && len(substr) > 0 &&
		(text == substr ||
			(len(text) >= len(substr) &&
				(text[:len(substr)] == substr || containsIgnoreCase(text[1:], substr))))
}
