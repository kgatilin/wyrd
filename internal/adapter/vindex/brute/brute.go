// Package brute provides an in-memory brute-force vector index with
// cosine similarity search. It persists vectors to JSON and supports
// per-node content hashes for incremental re-embedding.
package brute

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/kgatilin/archai/internal/retrieval/types"
)

// Index implements retrieval.VectorIndex with brute-force cosine search.
type Index struct {
	mu sync.RWMutex

	// embedderID is the identifier of the embedder that produced these vectors.
	// If the embedder changes, all vectors must be invalidated.
	embedderID string

	// dim is the expected dimension of all vectors.
	dim int

	// vectors maps node ID to its vector.
	vectors map[string][]float32

	// hashes maps node ID to its content hash at embedding time.
	hashes map[string]string
}

// New creates a new brute-force vector index for the given embedder.
func New(embedderID string, dim int) *Index {
	return &Index{
		embedderID: embedderID,
		dim:        dim,
		vectors:    make(map[string][]float32),
		hashes:     make(map[string]string),
	}
}

// Upsert inserts or updates the vector for the given ID.
func (idx *Index) Upsert(id string, vec []float32) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.vectors[id] = vec
}

// UpsertWithHash inserts or updates the vector and its content hash.
// This is used for freshness tracking - unchanged hashes skip re-embedding.
func (idx *Index) UpsertWithHash(id string, vec []float32, contentHash string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.vectors[id] = vec
	idx.hashes[id] = contentHash
}

// Remove deletes the vector for the given ID.
func (idx *Index) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.vectors, id)
	delete(idx.hashes, id)
}

// Scored is an alias for types.Scored.
type Scored = types.Scored

// Search returns the top-k results by cosine similarity (descending).
func (idx *Index) Search(query []float32, k int) []types.Scored {
	if k <= 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.vectors) == 0 {
		return nil
	}

	// Compute cosine similarity for all vectors
	type scored struct {
		id    string
		score float32
	}
	scores := make([]scored, 0, len(idx.vectors))
	for id, vec := range idx.vectors {
		sim := cosineSimilarity(query, vec)
		scores = append(scores, scored{id: id, score: sim})
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top-k
	if k > len(scores) {
		k = len(scores)
	}

	results := make([]types.Scored, k)
	for i := 0; i < k; i++ {
		results[i] = types.Scored{
			ID:    scores[i].id,
			Score: scores[i].score,
		}
	}
	return results
}

// Len returns the number of vectors in the index.
func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.vectors)
}

// GetHash returns the stored content hash for a node, or empty if not found.
func (idx *Index) GetHash(id string) string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.hashes[id]
}

// EmbedderID returns the embedder identifier this index was built with.
func (idx *Index) EmbedderID() string {
	return idx.embedderID
}

// IDs returns all node IDs in the index.
func (idx *Index) IDs() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ids := make([]string, 0, len(idx.vectors))
	for id := range idx.vectors {
		ids = append(ids, id)
	}
	return ids
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector is zero or lengths don't match.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// persistedIndex is the JSON structure for persistence.
type persistedIndex struct {
	EmbedderID string                `json:"embedder_id"`
	Dim        int                   `json:"dim"`
	Vectors    map[string]vectorData `json:"vectors"`
}

type vectorData struct {
	Hash string    `json:"hash"`
	Vec  []float32 `json:"vec"`
}

// Save persists the index to a JSON file.
func (idx *Index) Save(path string) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	persisted := persistedIndex{
		EmbedderID: idx.embedderID,
		Dim:        idx.dim,
		Vectors:    make(map[string]vectorData, len(idx.vectors)),
	}

	for id, vec := range idx.vectors {
		persisted.Vectors[id] = vectorData{
			Hash: idx.hashes[id],
			Vec:  vec,
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load restores the index from a JSON file. If the embedder ID doesn't match
// the current embedder, all vectors are discarded (model changed).
func (idx *Index) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No persisted index, start fresh
		}
		return err
	}

	var persisted persistedIndex
	if err := json.Unmarshal(data, &persisted); err != nil {
		return err
	}

	// If embedder changed, discard all vectors
	if persisted.EmbedderID != idx.embedderID {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.dim = persisted.Dim
	idx.vectors = make(map[string][]float32, len(persisted.Vectors))
	idx.hashes = make(map[string]string, len(persisted.Vectors))

	for id, vd := range persisted.Vectors {
		idx.vectors[id] = vd.Vec
		idx.hashes[id] = vd.Hash
	}

	return nil
}
