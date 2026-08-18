package retrieval

import (
	"context"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestVectorCacheNamespaceCarriesEmbedderAndRecipe(t *testing.T) {
	ns := VectorCacheNamespace("ollama:qwen3-embedding:0.6b")
	if want := "ollama:qwen3-embedding:0.6b@r1"; ns != want {
		t.Fatalf("VectorCacheNamespace = %q, want %q", ns, want)
	}
	if VectorCacheNamespace("a") == VectorCacheNamespace("b") {
		t.Fatal("different embedders share a namespace")
	}
}

func TestServiceIndexSkipsEmbedderForCachedHashes(t *testing.T) {
	root := t.TempDir()
	setupTestSource(t, root)

	nodes := cacheTestNodes()
	cache := newFakeVectorCache()
	cached := make(map[string][]float32, len(nodes))
	for _, node := range nodes {
		hash, err := ContentHash(node, root)
		if err != nil {
			t.Fatalf("ContentHash(%s): %v", node.ID, err)
		}
		vec := []float32{float32(len(hash)), 1, 2}
		cached[node.ID] = vec
		cache.Put(hash, vec)
	}
	cache.puts = 0

	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	vidx := newTestVectorIndex(emb.ID())
	svc := NewService(root, emb, vidx, newTestLexicalIndex(), WithVectorCache(cache))

	if err := svc.Index(context.Background(), nodes); err != nil {
		t.Fatalf("Index: %v", err)
	}

	if emb.callCount != 0 {
		t.Errorf("embedder called %d time(s) with a fully warm cache, want 0", emb.callCount)
	}
	if cache.puts != 0 {
		t.Errorf("cache Put called %d time(s) on a full hit, want 0", cache.puts)
	}
	if vidx.Len() != len(nodes) {
		t.Fatalf("indexed %d vectors, want %d", vidx.Len(), len(nodes))
	}
	for _, node := range nodes {
		got, ok := vidx.Vector(node.ID)
		if !ok {
			t.Fatalf("%s missing from the index", node.ID)
		}
		if !sameVector(got, cached[node.ID]) {
			t.Errorf("%s indexed %v, want the cached %v", node.ID, got, cached[node.ID])
		}
	}
	if !svc.DenseAvailable() {
		t.Error("dense search unavailable after a fully cached index pass")
	}
}

func TestServiceIndexPublishesEmbeddedVectors(t *testing.T) {
	root := t.TempDir()
	setupTestSource(t, root)

	nodes := cacheTestNodes()
	cache := newFakeVectorCache()

	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	vidx := newTestVectorIndex(emb.ID())
	svc := NewService(root, emb, vidx, newTestLexicalIndex(), WithVectorCache(cache))

	if err := svc.Index(context.Background(), nodes); err != nil {
		t.Fatalf("Index: %v", err)
	}

	if emb.callCount == 0 {
		t.Fatal("embedder not called on a cold cache")
	}
	if cache.puts != len(nodes) {
		t.Errorf("cache Put called %d time(s), want %d (one per embedded node)", cache.puts, len(nodes))
	}
	for _, node := range nodes {
		hash, err := ContentHash(node, root)
		if err != nil {
			t.Fatalf("ContentHash(%s): %v", node.ID, err)
		}
		cachedVec, ok := cache.Get(hash)
		if !ok {
			t.Fatalf("%s not published to the cache", node.ID)
		}
		indexedVec, _ := vidx.Vector(node.ID)
		if !sameVector(cachedVec, indexedVec) {
			t.Errorf("%s cached %v but indexed %v", node.ID, cachedVec, indexedVec)
		}
	}

	// A second service over the same cache — a sibling worktree — pays
	// nothing for the same content.
	sibling := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	siblingSvc := NewService(root, sibling, newTestVectorIndex(sibling.ID()), newTestLexicalIndex(),
		WithVectorCache(cache))
	if err := siblingSvc.Index(context.Background(), nodes); err != nil {
		t.Fatalf("sibling Index: %v", err)
	}
	if sibling.callCount != 0 {
		t.Errorf("sibling embedder called %d time(s), want 0", sibling.callCount)
	}
}

func TestServiceLoadSeedsVectorCache(t *testing.T) {
	root := t.TempDir()

	cache := newFakeVectorCache()
	emb := &testEmbedder{dim: 64}
	vidx := newTestVectorIndex(emb.ID())
	// Stand in for a vectors.json restored by vindex.Load: vectors already
	// present in this worktree's index when Load runs.
	vidx.UpsertWithHash("pkg.A", []float32{1, 2, 3}, "hash-a")
	vidx.UpsertWithHash("pkg.B", []float32{4, 5, 6}, "hash-b")
	vidx.UpsertWithHash("pkg.Unhashed", []float32{7, 8, 9}, "")

	svc := NewService(root, emb, vidx, newTestLexicalIndex(), WithVectorCache(cache))
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	vec, ok := cache.Get("hash-a")
	if !ok {
		t.Fatal("hash-a not seeded into the cache")
	}
	if !sameVector(vec, []float32{1, 2, 3}) {
		t.Errorf("hash-a seeded as %v, want [1 2 3]", vec)
	}
	if _, ok := cache.Get("hash-b"); !ok {
		t.Error("hash-b not seeded into the cache")
	}
	if _, ok := cache.Get(""); ok {
		t.Error("a node without a content hash was seeded")
	}
}

func TestServiceWithoutVectorCacheStillEmbeds(t *testing.T) {
	root := t.TempDir()
	setupTestSource(t, root)

	emb := &countingEmbedder{testEmbedder: testEmbedder{dim: 64}}
	vidx := newTestVectorIndex(emb.ID())
	svc := NewService(root, emb, vidx, newTestLexicalIndex())

	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := svc.Index(context.Background(), cacheTestNodes()); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if emb.callCount == 0 {
		t.Error("no embedder call without a cache")
	}
	if vidx.Len() != len(cacheTestNodes()) {
		t.Errorf("indexed %d vectors, want %d", vidx.Len(), len(cacheTestNodes()))
	}
}

func cacheTestNodes() []Node {
	return []Node{
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
			ID:         "pkg.Func2",
			Kind:       "func",
			Package:    "pkg",
			Name:       "Func2",
			Signature:  "Func2()",
			Span:       domain.Span{File: "pkg/code.go", StartByte: 30, EndByte: 50, StartLine: 5, EndLine: 7},
			Embeddable: true,
		},
	}
}

func sameVector(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fakeVectorCache is an in-memory VectorCache that records how often the
// service publishes to it.
type fakeVectorCache struct {
	vectors map[string][]float32
	puts    int
}

func newFakeVectorCache() *fakeVectorCache {
	return &fakeVectorCache{vectors: make(map[string][]float32)}
}

func (c *fakeVectorCache) Get(contentHash string) ([]float32, bool) {
	if contentHash == "" {
		return nil, false
	}
	vec, ok := c.vectors[contentHash]
	return vec, ok
}

func (c *fakeVectorCache) Put(contentHash string, vec []float32) {
	if contentHash == "" || len(vec) == 0 {
		return
	}
	if _, ok := c.vectors[contentHash]; ok {
		return
	}
	c.vectors[contentHash] = vec
	c.puts++
}
