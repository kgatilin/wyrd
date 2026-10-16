package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/lindex/bm25"
	"github.com/kgatilin/wyrd/internal/adapter/vindex/vecstore"
	"github.com/kgatilin/wyrd/internal/domain"
)

func TestRecipeCarriesEmbedderAndRecipeVersion(t *testing.T) {
	if got, want := Recipe("ollama:qwen3-embedding:0.6b"), "ollama:qwen3-embedding:0.6b@r1"; got != want {
		t.Fatalf("Recipe = %q, want %q", got, want)
	}
	if Recipe("a") == Recipe("b") {
		t.Fatal("different embedders share a recipe")
	}
}

// worktree writes one source file into a fresh root and returns the root and
// the models of the package it declares: a worktree of the shared fixture
// repository.
func worktree(t *testing.T, src string, funcs ...string) (string, []domain.PackageModel) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "shop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shop.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	model := domain.PackageModel{Path: "internal/shop", Name: "shop"}
	for _, name := range funcs {
		start := indexOf(src, "func "+name+"(")
		if start < 0 {
			t.Fatalf("fixture has no func %s", name)
		}
		end := start + indexOf(src[start:], "\n}") + 2
		model.Functions = append(model.Functions, domain.FunctionDef{
			Name: name, IsExported: true, SourceFile: "shop.go",
			Span: domain.Span{File: "internal/shop/shop.go", StartByte: start, EndByte: end, StartLine: 1, EndLine: 1},
		})
	}
	return root, []domain.PackageModel{model}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

const sharedSrc = `package shop

func PlaceOrder() {
	reserveStock()
}

func CancelOrder() {
	releaseStock()
}
`

const branchSrc = sharedSrc + `
func RefundPayment() {
	issueRefund()
}
`

func TestCorpusTwoViewsEmbedASharedHashOnce(t *testing.T) {
	ctx := context.Background()
	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 16}}
	corpus, vectors, lexical := testCorpus(emb)

	mainRoot, mainModels := worktree(t, sharedSrc, "PlaceOrder", "CancelOrder")
	branchRoot, branchModels := worktree(t, branchSrc, "PlaceOrder", "CancelOrder", "RefundPayment")
	mainView := NewService(mainRoot, corpus)
	branchView := NewService(branchRoot, corpus)

	// Both views index concurrently: the corpus admits one pass at a time and
	// re-checks after the lock, so no text reaches the embedder twice.
	var wg sync.WaitGroup
	for _, job := range []struct {
		svc    *Service
		models []domain.PackageModel
	}{{mainView, mainModels}, {branchView, branchModels}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := job.svc.IndexFromModels(ctx, job.models); err != nil {
				t.Errorf("IndexFromModels: %v", err)
			}
		}()
	}
	wg.Wait()

	total, maxRepeat := emb.embedded()
	if maxRepeat != 1 {
		t.Errorf("a text was embedded %d times, want once", maxRepeat)
	}
	if total != 3 || vectors.Len() != 3 {
		t.Errorf("embedded %d texts, stored %d vectors; want 3 distinct texts across both views", total, vectors.Len())
	}
	if lexical.Len() != 3 {
		t.Errorf("lexical documents = %d, want 3: a shared text is one document", lexical.Len())
	}
}

func TestCorpusViewOnlyHashIsInvisibleToOtherViews(t *testing.T) {
	ctx := context.Background()
	corpus, _, _ := testCorpus(&testEmbedder{dim: 16})

	mainRoot, mainModels := worktree(t, sharedSrc, "PlaceOrder", "CancelOrder")
	branchRoot, branchModels := worktree(t, branchSrc, "PlaceOrder", "CancelOrder", "RefundPayment")
	mainView := NewService(mainRoot, corpus)
	branchView := NewService(branchRoot, corpus)
	if err := mainView.IndexFromModels(ctx, mainModels); err != nil {
		t.Fatal(err)
	}
	if err := branchView.IndexFromModels(ctx, branchModels); err != nil {
		t.Fatal(err)
	}

	onBranch, err := branchView.Search(ctx, "RefundPayment issueRefund", SearchOptions{K: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !hasHit(onBranch, "internal/shop.RefundPayment") {
		t.Fatalf("branch view cannot find its own symbol: %+v", onBranch.Hits)
	}

	onMain, err := mainView.Search(ctx, "RefundPayment issueRefund", SearchOptions{K: 5})
	if err != nil {
		t.Fatal(err)
	}
	if hasHit(onMain, "internal/shop.RefundPayment") {
		t.Errorf("main view answered with a symbol only the branch has: %+v", onMain.Hits)
	}

	// A shared symbol is found from either view, under that view's own id.
	for name, svc := range map[string]*Service{"main": mainView, "branch": branchView} {
		got, err := svc.Search(ctx, "PlaceOrder", SearchOptions{K: 5})
		if err != nil {
			t.Fatal(err)
		}
		if !hasHit(got, "internal/shop.PlaceOrder") {
			t.Errorf("%s view: shared symbol not found: %+v", name, got.Hits)
		}
	}

	vecs, err := mainView.Vectors(ctx, []string{"internal/shop.PlaceOrder", "internal/shop.RefundPayment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := vecs["internal/shop.PlaceOrder"]; !ok || len(vecs) != 1 {
		t.Errorf("main view vectors = %v keys, want PlaceOrder only", len(vecs))
	}
}

func TestCorpusReadinessIsPerView(t *testing.T) {
	ctx := context.Background()
	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 16}}
	corpus, _, _ := testCorpus(emb)

	branchRoot, branchModels := worktree(t, branchSrc, "PlaceOrder", "CancelOrder", "RefundPayment")
	branchView := NewService(branchRoot, corpus)
	if err := branchView.IndexFromModels(ctx, branchModels); err != nil {
		t.Fatal(err)
	}
	calls := emb.calls()

	// A cold view whose every hash is already in the corpus is ready once its
	// view is built, without an embedder call.
	mainRoot, mainModels := worktree(t, sharedSrc, "PlaceOrder", "CancelOrder")
	mainView := NewService(mainRoot, corpus)
	nodes, graph := BuildGraph(mainModels)
	mainView.SetGraph(graph)
	if err := mainView.Update(ctx, nodes, nil); err != nil {
		t.Fatal(err)
	}
	st := mainView.IndexStatus()
	if st.Embeddable != 2 || st.Embedded != 2 || st.Pending != 0 {
		t.Errorf("cold view over a warm corpus: %+v, want 2/2 embedded", st)
	}
	if err := mainView.EmbedPending(ctx); err != nil {
		t.Fatal(err)
	}
	if emb.calls() != calls {
		t.Errorf("embedder called %d more time(s) for a fully shared view", emb.calls()-calls)
	}
	if !mainView.DenseAvailable() {
		t.Error("dense unavailable on a fully shared view")
	}

	// The branch's readiness counts its own three nodes, not main's two.
	if st := branchView.IndexStatus(); st.Embeddable != 3 || st.Embedded != 3 {
		t.Errorf("branch view status = %+v, want 3/3", st)
	}
}

func TestCorpusReleasesTextWithItsLastView(t *testing.T) {
	ctx := context.Background()
	corpus, vectors, lexical := testCorpus(&testEmbedder{dim: 16})

	mainRoot, mainModels := worktree(t, sharedSrc, "PlaceOrder", "CancelOrder")
	branchRoot, branchModels := worktree(t, branchSrc, "PlaceOrder", "CancelOrder", "RefundPayment")
	mainView := NewService(mainRoot, corpus)
	branchView := NewService(branchRoot, corpus)
	if err := mainView.IndexFromModels(ctx, mainModels); err != nil {
		t.Fatal(err)
	}
	if err := branchView.IndexFromModels(ctx, branchModels); err != nil {
		t.Fatal(err)
	}

	// The branch drops everything: shared texts stay for main, the branch's
	// own text leaves the lexical index. Vectors are never evicted.
	if err := branchView.Index(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if lexical.Len() != 2 {
		t.Errorf("lexical documents = %d, want main's 2", lexical.Len())
	}
	if vectors.Len() != 3 {
		t.Errorf("vectors = %d, want all 3 kept", vectors.Len())
	}
	if got, _ := mainView.Search(ctx, "CancelOrder", SearchOptions{K: 5}); !hasHit(got, "internal/shop.CancelOrder") {
		t.Errorf("main lost a shared text when the branch released it: %+v", got.Hits)
	}
}

func TestCorpusSavesOnlyHeldDocumentsOncePerRepository(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	emb := &testEmbedder{dim: 16}
	lexPath := filepath.Join(dir, "lexical@r1.bm25")
	vecPath := vecstore.Path(dir, Recipe(emb.ID()))
	corpus := NewCorpus(emb, vecstore.New(vecPath, Recipe(emb.ID())), bm25.New(), lexPath)

	mainRoot, mainModels := worktree(t, sharedSrc, "PlaceOrder", "CancelOrder")
	branchRoot, branchModels := worktree(t, branchSrc, "PlaceOrder", "CancelOrder", "RefundPayment")
	mainView := NewService(mainRoot, corpus)
	branchView := NewService(branchRoot, corpus)
	if err := mainView.IndexFromModels(ctx, mainModels); err != nil {
		t.Fatal(err)
	}
	if err := branchView.IndexFromModels(ctx, branchModels); err != nil {
		t.Fatal(err)
	}
	if err := branchView.Index(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := mainView.Save(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("corpus directory holds %d files, want the lexical and the vector file", len(entries))
	}
	for _, root := range []string{mainRoot, branchRoot} {
		if _, err := os.Stat(filepath.Join(root, ".archai")); !os.IsNotExist(err) {
			t.Errorf("%s: a per-worktree cache directory was written", root)
		}
	}

	reloaded := bm25.New()
	if err := NewCorpus(emb, vecstore.New(vecPath, Recipe(emb.ID())), reloaded, lexPath).Load(); err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 2 {
		t.Errorf("persisted lexical documents = %d, want the 2 a live view holds", reloaded.Len())
	}
	if n := vecstore.New(vecPath, Recipe(emb.ID())).Len(); n != 3 {
		t.Errorf("persisted vectors = %d, want 3", n)
	}
}

func hasHit(r SearchResult, id string) bool {
	for _, h := range r.Hits {
		if h.ID == id && h.Seed {
			return true
		}
	}
	return false
}
