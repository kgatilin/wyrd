package retrieval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/lindex/bm25"
	"github.com/kgatilin/wyrd/internal/adapter/vindex/vecstore"
	"github.com/kgatilin/wyrd/internal/domain"
)

// testCorpus is a corpus over the real in-memory adapters, returned with them
// so a test can look at what the channels hold.
func testCorpus(emb Embedder) (*Corpus, *vecstore.Store, *bm25.Index) {
	vectors := vecstore.New("", Recipe(emb.ID()))
	lexical := bm25.New()
	return NewCorpus(emb, vectors, lexical, ""), vectors, lexical
}

func func1Node() Node {
	return Node{
		ID: "pkg.Func1", Kind: "func", Package: "pkg", Name: "Func1",
		Signature: "Func1() error", Doc: "Func1 does something",
		Span:       domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 20, StartLine: 1, EndLine: 1},
		Embeddable: true,
	}
}

func TestService_Index(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	corpus, vectors, lexical := testCorpus(&testEmbedder{dim: 64})
	svc := NewService(tmpDir, corpus)

	nodes := []Node{
		func1Node(),
		{ID: "pkg.Const1", Kind: "const", Package: "pkg", Name: "Const1", Signature: "Const1 = 42"},
	}
	if err := svc.Index(context.Background(), nodes); err != nil {
		t.Fatalf("Index: %v", err)
	}

	if vectors.Len() != 1 {
		t.Errorf("vectors = %d, want 1 (only the embeddable node)", vectors.Len())
	}
	if lexical.Len() != 2 {
		t.Errorf("lexical documents = %d, want 2 (every node)", lexical.Len())
	}
	if !svc.DenseAvailable() {
		t.Error("dense should be available")
	}
	if !svc.Embedded("pkg.Func1") || svc.Embedded("pkg.Const1") {
		t.Errorf("Embedded: Func1=%v Const1=%v, want true/false", svc.Embedded("pkg.Func1"), svc.Embedded("pkg.Const1"))
	}
}

func TestService_IndexSkipsUnchangedHashes(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	corpus, _, _ := testCorpus(emb)
	svc := NewService(tmpDir, corpus)
	nodes := []Node{func1Node()}

	if err := svc.Index(context.Background(), nodes); err != nil {
		t.Fatalf("Index 1: %v", err)
	}
	first := emb.calls()
	if err := svc.Index(context.Background(), nodes); err != nil {
		t.Fatalf("Index 2: %v", err)
	}
	if emb.calls() != first {
		t.Errorf("expected no new embed calls, got %d more", emb.calls()-first)
	}
}

func TestService_IndexRemovesDeletedNodes(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	corpus, _, lexical := testCorpus(&testEmbedder{dim: 64})
	svc := NewService(tmpDir, corpus)

	a := Node{ID: "pkg.A", Kind: "func", Package: "pkg", Name: "A", Signature: "A()", Embeddable: true,
		Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 10, StartLine: 1, EndLine: 1}}
	b := Node{ID: "pkg.B", Kind: "func", Package: "pkg", Name: "B", Signature: "Bravo()", Embeddable: true,
		Span: domain.Span{File: "pkg/code.go", StartByte: 10, EndByte: 20, StartLine: 2, EndLine: 2}}

	if err := svc.Index(context.Background(), []Node{a, b}); err != nil {
		t.Fatalf("Index 1: %v", err)
	}
	if lexical.Len() != 2 {
		t.Fatalf("lexical documents = %d, want 2", lexical.Len())
	}

	if err := svc.Index(context.Background(), []Node{a}); err != nil {
		t.Fatalf("Index 2: %v", err)
	}
	// B's text was held by this view alone, so the corpus drops it.
	if lexical.Len() != 1 {
		t.Errorf("lexical documents after removal = %d, want 1", lexical.Len())
	}
	if svc.Embedded("pkg.B") {
		t.Error("a removed node still reads as embedded")
	}
	got, err := svc.Search(context.Background(), "Bravo", SearchOptions{K: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range got.Hits {
		if h.ID == "pkg.B" {
			t.Error("removed node appeared in search results")
		}
	}
}

func TestService_GracefulDegradationOnEmbedderError(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	corpus, _, lexical := testCorpus(&failingEmbedder{})
	svc := NewService(tmpDir, corpus)

	if err := svc.Index(context.Background(), []Node{func1Node()}); err != nil {
		t.Fatalf("Index should not error on embed failure: %v", err)
	}
	if svc.DenseAvailable() {
		t.Error("dense should be unavailable after embedder failure")
	}
	if lexical.Len() != 1 {
		t.Errorf("expected 1 document in lexical index, got %d", lexical.Len())
	}
	svc.SetGraph(&Graph{NodesByID: map[string]Node{"pkg.Func1": func1Node()}})
	found, err := svc.Search(context.Background(), "Func1", SearchOptions{K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Hits) != 1 || found.Dense {
		t.Errorf("search = %+v, want one lexical hit", found)
	}
}

func TestService_UpdateThenEmbedPending(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	corpus, _, lexical := testCorpus(emb)
	svc := NewService(tmpDir, corpus)
	ctx := context.Background()

	a := Node{ID: "pkg.A", Kind: "func", Package: "pkg", Name: "A", Signature: "A()", Embeddable: true,
		Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 10, StartLine: 1, EndLine: 1}}
	b := Node{ID: "pkg.B", Kind: "func", Package: "pkg", Name: "B", Signature: "Bravo()", Embeddable: true,
		Span: domain.Span{File: "pkg/code.go", StartByte: 10, EndByte: 20, StartLine: 2, EndLine: 2}}
	if err := svc.Index(ctx, []Node{a, b}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Update A, remove B, add C.
	a2 := a
	a2.Signature = "A() error"
	c := Node{ID: "pkg.C", Kind: "func", Package: "pkg", Name: "C", Signature: "Charlie()", Embeddable: true,
		Span: domain.Span{File: "pkg/code.go", StartByte: 20, EndByte: 30, StartLine: 3, EndLine: 3}}
	calls := emb.calls()
	if err := svc.Update(ctx, []Node{a2, c}, []string{"pkg.B"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if emb.calls() != calls {
		t.Error("Update called the embedder; embedding belongs to EmbedPending")
	}
	if lexical.Len() != 2 {
		t.Errorf("lexical documents = %d, want 2 (A's new text and C)", lexical.Len())
	}
	svc.SetGraph(&Graph{NodesByID: map[string]Node{a2.ID: a2, c.ID: c}})
	if st := svc.IndexStatus(); st.Pending != 2 {
		t.Errorf("pending before EmbedPending = %d, want 2", st.Pending)
	}
	if err := svc.EmbedPending(ctx); err != nil {
		t.Fatalf("EmbedPending: %v", err)
	}
	if st := svc.IndexStatus(); st.Embedded != 2 || st.Pending != 0 || st.InProgress {
		t.Errorf("status after EmbedPending = %+v, want 2 embedded, none pending", st)
	}

	found, err := svc.Search(ctx, "Charlie", SearchOptions{K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Hits) == 0 || found.Hits[0].ID != "pkg.C" {
		t.Errorf("search for the added node = %+v", found.Hits)
	}
}

func TestService_SaveAndLoadLexical(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)
	path := filepath.Join(t.TempDir(), "lexical.bm25")

	emb := &testEmbedder{dim: 64}
	corpus := NewCorpus(emb, vecstore.New("", Recipe(emb.ID())), bm25.New(), path)
	svc := NewService(tmpDir, corpus)
	if err := svc.Index(context.Background(), []Node{func1Node()}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	lexical := bm25.New()
	if err := NewCorpus(emb, vecstore.New("", Recipe(emb.ID())), lexical, path).Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if lexical.Len() != 1 {
		t.Errorf("reloaded lexical documents = %d, want 1", lexical.Len())
	}
}

func TestService_NoDenseChannel(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestSource(t, tmpDir)

	lexical := bm25.New()
	svc := NewService(tmpDir, NewCorpus(nil, nil, lexical, ""))
	if err := svc.Index(context.Background(), []Node{func1Node()}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if svc.DenseAvailable() {
		t.Error("dense should be unavailable with no embedder")
	}
	if lexical.Len() != 1 {
		t.Errorf("expected 1 document, got %d", lexical.Len())
	}
	if svc.EmbedderID() != "" {
		t.Errorf("EmbedderID = %q, want empty", svc.EmbedderID())
	}
}

func TestHashNodesMatchesContentHash(t *testing.T) {
	root := t.TempDir()
	setupTestSource(t, root)
	nodes := []Node{
		func1Node(),
		{ID: "pkg.Func2", Kind: "func", Package: "pkg", Name: "Func2", Signature: "Func2()",
			Span: domain.Span{File: "pkg/code.go", StartByte: 30, EndByte: 50, StartLine: 5, EndLine: 7}},
		{ID: "pkg.Missing", Kind: "func", Package: "pkg", Name: "Missing", Signature: "Missing()",
			Span: domain.Span{File: "pkg/gone.go", StartByte: 0, EndByte: 5, StartLine: 1, EndLine: 1}},
		{ID: "pkg.Bad", Kind: "func", Package: "pkg", Name: "Bad", Signature: "Bad()",
			Span: domain.Span{File: "pkg/code.go", StartByte: 0, EndByte: 1 << 20, StartLine: 1, EndLine: 1}},
		{ID: "pkg.NoSpan", Kind: "const", Package: "pkg", Name: "NoSpan"},
	}
	got := hashNodes(nodes, root)
	for _, n := range nodes {
		want, err := ContentHash(n, root)
		if err != nil {
			t.Fatalf("ContentHash(%s): %v", n.ID, err)
		}
		if got[n.ID] != want {
			t.Errorf("%s: hashNodes = %s, ContentHash = %s", n.ID, got[n.ID], want)
		}
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

func (e *testEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *testEmbedder) Dim() int {
	return e.dim
}

func (e *testEmbedder) ID() string {
	return "test:embedder"
}

// countingEmbedder wraps an embedder and records what it was asked to embed.
// Safe for concurrent use.
type countingEmbedder struct {
	testEmbedder
	mu        sync.Mutex
	callCount int
	texts     map[string]int
}

func (e *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.callCount++
	if e.texts == nil {
		e.texts = make(map[string]int)
	}
	for _, t := range texts {
		e.texts[t]++
	}
	e.mu.Unlock()
	return e.testEmbedder.Embed(ctx, texts)
}

func (e *countingEmbedder) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.callCount
}

// embedded returns how many texts were embedded, and the most times any one
// text was.
func (e *countingEmbedder) embedded() (total, maxRepeat int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, n := range e.texts {
		total += n
		maxRepeat = max(maxRepeat, n)
	}
	return total, maxRepeat
}

// failingEmbedder always returns an error
type failingEmbedder struct{}

func (e *failingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("embedder unavailable")
}

func (e *failingEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return nil, errors.New("embedder unavailable")
}

func (e *failingEmbedder) Dim() int {
	return 64
}

func (e *failingEmbedder) ID() string {
	return "failing:model"
}
