package brute

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndex_UpsertAndSearch(t *testing.T) {
	idx := New("test:model", 3)

	// Insert some vectors
	idx.Upsert("a", []float32{1, 0, 0})
	idx.Upsert("b", []float32{0, 1, 0})
	idx.Upsert("c", []float32{0.7, 0.7, 0})
	idx.Upsert("d", []float32{-1, 0, 0})

	if idx.Len() != 4 {
		t.Errorf("expected 4 vectors, got %d", idx.Len())
	}

	// Search for something similar to [1,0,0]
	results := idx.Search([]float32{1, 0, 0}, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// "a" should be first (exact match)
	if results[0].ID != "a" {
		t.Errorf("expected first result to be 'a', got %q", results[0].ID)
	}
	// Score should be 1.0 for exact match
	if results[0].Score < 0.999 {
		t.Errorf("expected score ~1.0, got %f", results[0].Score)
	}

	// "c" should be second (positive cosine)
	if results[1].ID != "c" {
		t.Errorf("expected second result to be 'c', got %q", results[1].ID)
	}
}

func TestIndex_SearchDescendingOrder(t *testing.T) {
	idx := New("test:model", 3)

	// Insert vectors with known similarities to query [1,0,0]
	idx.Upsert("perfect", []float32{1, 0, 0})       // similarity = 1.0
	idx.Upsert("good", []float32{0.9, 0.1, 0})      // similarity ~ 0.99
	idx.Upsert("ok", []float32{0.7, 0.7, 0})        // similarity ~ 0.71
	idx.Upsert("bad", []float32{0, 1, 0})           // similarity = 0.0
	idx.Upsert("opposite", []float32{-1, 0, 0})    // similarity = -1.0

	results := idx.Search([]float32{1, 0, 0}, 5)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// Verify descending order
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not in descending order: %f > %f at positions %d, %d",
				results[i].Score, results[i-1].Score, i-1, i)
		}
	}

	// First should be "perfect"
	if results[0].ID != "perfect" {
		t.Errorf("expected first to be 'perfect', got %q", results[0].ID)
	}

	// Last should be "opposite"
	if results[4].ID != "opposite" {
		t.Errorf("expected last to be 'opposite', got %q", results[4].ID)
	}
}

func TestIndex_Remove(t *testing.T) {
	idx := New("test:model", 3)

	idx.UpsertWithHash("a", []float32{1, 0, 0}, "hash-a")
	idx.UpsertWithHash("b", []float32{0, 1, 0}, "hash-b")

	if idx.Len() != 2 {
		t.Errorf("expected 2 vectors, got %d", idx.Len())
	}
	if idx.GetHash("a") != "hash-a" {
		t.Errorf("expected hash 'hash-a', got %q", idx.GetHash("a"))
	}

	idx.Remove("a")

	if idx.Len() != 1 {
		t.Errorf("expected 1 vector after remove, got %d", idx.Len())
	}
	if idx.GetHash("a") != "" {
		t.Errorf("expected empty hash after remove, got %q", idx.GetHash("a"))
	}

	// Search should not find removed vector
	results := idx.Search([]float32{1, 0, 0}, 10)
	for _, r := range results {
		if r.ID == "a" {
			t.Error("removed vector should not appear in search results")
		}
	}
}

func TestIndex_PersistenceRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.json")

	// Create and populate index
	idx1 := New("ollama:nomic", 3)
	idx1.UpsertWithHash("a", []float32{1, 0, 0}, "hash-a")
	idx1.UpsertWithHash("b", []float32{0, 1, 0}, "hash-b")

	if err := idx1.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load into new index with same embedder
	idx2 := New("ollama:nomic", 3)
	if err := idx2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if idx2.Len() != 2 {
		t.Errorf("expected 2 vectors after load, got %d", idx2.Len())
	}
	if idx2.GetHash("a") != "hash-a" {
		t.Errorf("expected hash 'hash-a' after load, got %q", idx2.GetHash("a"))
	}

	// Search should work
	results := idx2.Search([]float32{1, 0, 0}, 1)
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("search after load failed: got %v", results)
	}
}

func TestIndex_EmbedderIDChangeInvalidatesVectors(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.json")

	// Create and save with one embedder
	idx1 := New("ollama:nomic", 3)
	idx1.UpsertWithHash("a", []float32{1, 0, 0}, "hash-a")
	if err := idx1.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load with different embedder - should discard vectors
	idx2 := New("ollama:different-model", 3)
	if err := idx2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if idx2.Len() != 0 {
		t.Errorf("expected 0 vectors (embedder changed), got %d", idx2.Len())
	}
}

func TestIndex_LoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")

	idx := New("test:model", 3)
	if err := idx.Load(path); err != nil {
		t.Errorf("Load should not error on missing file: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("expected 0 vectors for missing file, got %d", idx.Len())
	}
}

func TestIndex_SearchEmptyIndex(t *testing.T) {
	idx := New("test:model", 3)
	results := idx.Search([]float32{1, 0, 0}, 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty index, got %d", len(results))
	}
}

func TestIndex_SearchKGreaterThanLen(t *testing.T) {
	idx := New("test:model", 3)
	idx.Upsert("a", []float32{1, 0, 0})
	idx.Upsert("b", []float32{0, 1, 0})

	results := idx.Search([]float32{1, 0, 0}, 100)
	if len(results) != 2 {
		t.Errorf("expected 2 results (index size), got %d", len(results))
	}
}

func TestIndex_IDs(t *testing.T) {
	idx := New("test:model", 3)
	idx.Upsert("a", []float32{1, 0, 0})
	idx.Upsert("b", []float32{0, 1, 0})
	idx.Upsert("c", []float32{0, 0, 1})

	ids := idx.IDs()
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}

	found := make(map[string]bool)
	for _, id := range ids {
		found[id] = true
	}
	for _, expected := range []string{"a", "b", "c"} {
		if !found[expected] {
			t.Errorf("missing ID %q", expected)
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"empty", []float32{}, []float32{}, 0.0},
		{"different length", []float32{1, 0}, []float32{1, 0, 0}, 0.0},
		{"zero vector", []float32{0, 0, 0}, []float32{1, 0, 0}, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if diff := got - tc.want; diff > 0.001 || diff < -0.001 {
				t.Errorf("cosineSimilarity(%v, %v) = %f, want %f", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIndex_AtomicSave(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "vectors.json")

	idx := New("test:model", 3)
	idx.Upsert("a", []float32{1, 0, 0})

	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Tmp file should be cleaned up
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temporary file should not exist after successful save")
	}

	// Actual file should exist
	if _, err := os.Stat(path); err != nil {
		t.Errorf("saved file should exist: %v", err)
	}
}
