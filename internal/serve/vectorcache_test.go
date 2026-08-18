package serve

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/vindex/vecstore"
	"github.com/kgatilin/archai/internal/retrieval"
)

// TestStateVectorCacheWarmsSiblingWorktree walks the whole point of the
// shared cache: a worktree that was indexed before the cache existed seeds it
// on the next load (the migration path), and a fresh worktree holding the
// same code then indexes without embedding anything.
//
// A Put happens exactly once per node that went through the embedder, so
// "zero Puts while the index fills up" is the observable form of "zero
// embedder calls" from outside the State, which builds its own embedder.
func TestStateVectorCacheWarmsSiblingWorktree(t *testing.T) {
	// The package TestMain disables retrieval; these tests are about the
	// retrieval wiring, so turn it back on with a deterministic embedder.
	// loadAndIndex waits for the indexing goroutine, so no background work
	// outlives the test.
	t.Setenv("ARCHAI_RETRIEVAL_DISABLE", "0")
	t.Setenv("ARCHAI_EMBED_PROVIDER", "noop")
	ctx := context.Background()

	// A worktree indexed with no shared cache at all: this is what every
	// existing checkout looks like before this feature.
	first := newFixture(t)
	loadAndIndex(t, ctx, first, nil)

	store := &recordingVectorCache{inner: vecstore.New(filepath.Join(t.TempDir(), "embeddings"))}

	// Reloading it with a cache seeds the cache from the persisted vectors.
	// Nothing is re-embedded (the content hashes are unchanged), so every
	// Put here comes from the seeding pass over the loaded index.
	loadAndIndex(t, ctx, first, store)
	if store.count().puts == 0 {
		t.Fatal("reloading an already-indexed worktree seeded nothing into the shared cache")
	}
	if store.count().saves == 0 {
		t.Error("State never persisted the shared cache")
	}
	if ns := store.namespaces(); len(ns) != 1 {
		t.Fatalf("namespaces = %v, want exactly one", ns)
	} else if want := retrieval.VectorCacheNamespace("noop"); ns[0] != want {
		t.Errorf("namespace = %q, want %q", ns[0], want)
	}

	// A sibling worktree with byte-identical sources: fully served from the
	// cache, so it publishes nothing back.
	store.reset()
	second := newFixture(t)
	st := loadAndIndex(t, ctx, second, store)

	got := store.count()
	if got.puts != 0 {
		t.Errorf("sibling worktree embedded %d node(s); want 0, all served from the cache", got.puts)
	}
	if got.hits == 0 {
		t.Error("sibling worktree never hit the shared cache")
	}
	if status := st.Retrieval().IndexStatus(); status.Embedded == 0 {
		t.Errorf("sibling worktree index is empty: %+v", status)
	}
}

func TestStateWithoutVectorCacheIndexes(t *testing.T) {
	// The package TestMain disables retrieval; these tests are about the
	// retrieval wiring, so turn it back on with a deterministic embedder.
	// loadAndIndex waits for the indexing goroutine, so no background work
	// outlives the test.
	t.Setenv("ARCHAI_RETRIEVAL_DISABLE", "0")
	t.Setenv("ARCHAI_EMBED_PROVIDER", "noop")

	st := loadAndIndex(t, context.Background(), newFixture(t), nil)
	if status := st.Retrieval().IndexStatus(); status.Embedded == 0 {
		t.Errorf("index is empty without a shared cache: %+v", status)
	}
}

// loadAndIndex loads a State over root and blocks until its retrieval
// indexing pass (and the follow-up saves) have finished.
func loadAndIndex(t *testing.T, ctx context.Context, root string, cache VectorCacheProvider) *State {
	t.Helper()

	opts := []StateOption{}
	if cache != nil {
		opts = append(opts, WithVectorCache(cache))
	}
	st := NewState(root, opts...)
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load(%s): %v", root, err)
	}
	ready := st.RetrievalReady()
	if ready == nil {
		t.Fatalf("retrieval disabled for %s", root)
	}
	<-ready
	return st
}

// recordingVectorCache counts what the States ask of a real vecstore.Store.
type recordingVectorCache struct {
	inner *vecstore.Store

	mu     sync.Mutex
	counts cacheCounts
	seen   []string
}

type cacheCounts struct {
	hits   int
	misses int
	puts   int
	saves  int
}

func (c *recordingVectorCache) Namespace(ns string) retrieval.VectorCache {
	c.mu.Lock()
	found := false
	for _, s := range c.seen {
		if s == ns {
			found = true
			break
		}
	}
	if !found {
		c.seen = append(c.seen, ns)
	}
	c.mu.Unlock()
	return &recordingNamespace{owner: c, inner: c.inner.Namespace(ns)}
}

func (c *recordingVectorCache) Save() error {
	c.mu.Lock()
	c.counts.saves++
	c.mu.Unlock()
	return c.inner.Save()
}

func (c *recordingVectorCache) count() cacheCounts {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts
}

func (c *recordingVectorCache) namespaces() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.seen...)
}

func (c *recordingVectorCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts = cacheCounts{}
}

type recordingNamespace struct {
	owner *recordingVectorCache
	inner retrieval.VectorCache
}

func (n *recordingNamespace) Get(contentHash string) ([]float32, bool) {
	vec, ok := n.inner.Get(contentHash)
	n.owner.mu.Lock()
	if ok {
		n.owner.counts.hits++
	} else {
		n.owner.counts.misses++
	}
	n.owner.mu.Unlock()
	return vec, ok
}

func (n *recordingNamespace) Put(contentHash string, vec []float32) {
	n.owner.mu.Lock()
	n.owner.counts.puts++
	n.owner.mu.Unlock()
	n.inner.Put(contentHash, vec)
}
