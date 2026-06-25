package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kgatilin/archai/internal/domain"
)

// Service orchestrates the vector and lexical indexes for code search.
// It handles embedding, freshness tracking, and graceful degradation
// when the embedder is unavailable.
type Service struct {
	mu sync.RWMutex

	// root is the project root directory for content hash computation.
	root string

	// embedder generates dense vector embeddings.
	embedder Embedder

	// vindex is the dense vector index (may be nil if embedding failed).
	vindex vectorIndexWithHash

	// lindex is the BM25 lexical index.
	lindex LexicalIndex

	// graph holds the adjacency map for expand/neighbor operations.
	graph *Graph

	// denseAvailable tracks whether dense search is operational.
	// Set to false if the embedder fails during Index/Refresh.
	denseAvailable bool

	// vectorsPath is where the vector index is persisted.
	vectorsPath string

	// lexicalPath is where the lexical index is persisted.
	lexicalPath string

	// idxInProgress is true while Index/Refresh is running (notably the slow
	// dense-embedding pass). Read by IndexStatus so callers — the `status`
	// tool, the readiness gate on embedding-backed lenses — can report
	// "indexing in progress" instead of blocking until a client timeout.
	idxInProgress bool

	// idxStartedAt is when the current (or last) indexing pass began.
	idxStartedAt time.Time
}

// IndexStatus is a cheap, non-blocking snapshot of the retrieval index's
// readiness: whether an indexing pass is running, how many embeddable nodes
// have vectors so far, and whether dense search is operational. It does no
// heavy work, so the `status` tool answers instantly even mid-indexing.
type IndexStatus struct {
	InProgress     bool      `json:"in_progress"`
	DenseAvailable bool      `json:"dense_available"`
	Embedded       int       `json:"embedded"`   // embeddable nodes with a vector now
	Embeddable     int       `json:"embeddable"` // nodes that should carry a vector
	Pending        int       `json:"pending"`    // embeddable not yet embedded
	StartedAt      time.Time `json:"started_at,omitempty"`
}

// IndexStatus returns a snapshot of indexing progress. Counts are computed
// on-demand from the live indexes, so Embedded climbs toward Embeddable as the
// dense pass streams vectors in.
func (s *Service) IndexStatus() IndexStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := IndexStatus{
		InProgress:     s.idxInProgress,
		DenseAvailable: s.denseAvailable,
		StartedAt:      s.idxStartedAt,
	}
	if s.vindex != nil {
		st.Embedded = len(s.vindex.IDs())
	}
	if s.graph != nil {
		for _, n := range s.graph.NodesByID {
			if n.Embeddable {
				st.Embeddable++
			}
		}
	}
	if st.Pending = st.Embeddable - st.Embedded; st.Pending < 0 {
		st.Pending = 0
	}
	return st
}

func (s *Service) beginIndexing() {
	s.mu.Lock()
	s.idxInProgress = true
	s.idxStartedAt = time.Now()
	s.mu.Unlock()
}

func (s *Service) endIndexing() {
	s.mu.Lock()
	s.idxInProgress = false
	s.mu.Unlock()
}

// vectorIndexWithHash extends VectorIndex with hash and ID tracking methods
// needed for freshness. Implemented by brute.Index.
type vectorIndexWithHash interface {
	VectorIndex

	// UpsertWithHash inserts or updates the vector and its content hash.
	UpsertWithHash(id string, vec []float32, contentHash string)

	// GetHash returns the stored content hash for a node.
	GetHash(id string) string

	// IDs returns all node IDs in the index.
	IDs() []string

	// EmbedderID returns the embedder identifier this index was built with.
	EmbedderID() string

	// Save persists the index to a file.
	Save(path string) error

	// Load restores the index from a file.
	Load(path string) error
}

// VectorIndexWithLookup extends VectorIndex with direct vector lookup by ID.
// Used for semantic clustering where we need to compute pairwise similarity
// across a selected subset of nodes. Implementations should expose this via
// a type assertion (e.g., brute.Index implements this).
type VectorIndexWithLookup interface {
	VectorIndex

	// Vector returns the vector for the given ID, or (nil, false) if not found.
	Vector(id string) ([]float32, bool)
}

// lexicalIndexWithPersist extends LexicalIndex with persistence methods.
type lexicalIndexWithPersist interface {
	LexicalIndex

	// Save persists the index to a file.
	Save(path string) error

	// Load restores the index from a file.
	Load(path string) error
}

// NewService creates a new retrieval service with the given dependencies.
// Pass nil for vindex or lindex to disable that search mode.
func NewService(root string, emb Embedder, vidx vectorIndexWithHash, lidx lexicalIndexWithPersist) *Service {
	cacheDir := filepath.Join(root, ".archai", "cache")
	return &Service{
		root:           root,
		embedder:       emb,
		vindex:         vidx,
		lindex:         lidx,
		denseAvailable: vidx != nil && emb != nil,
		vectorsPath:    filepath.Join(cacheDir, "vectors.json"),
		lexicalPath:    filepath.Join(cacheDir, "bm25.json"),
	}
}

// Load restores both indexes from disk.
func (s *Service) Load() error {
	if s.vindex != nil {
		if err := s.vindex.Load(s.vectorsPath); err != nil {
			return fmt.Errorf("loading vector index: %w", err)
		}
	}
	if lidx, ok := s.lindex.(lexicalIndexWithPersist); ok {
		if err := lidx.Load(s.lexicalPath); err != nil {
			return fmt.Errorf("loading lexical index: %w", err)
		}
	}
	return nil
}

// Save persists both indexes to disk.
func (s *Service) Save() error {
	if s.vindex != nil {
		if err := s.vindex.Save(s.vectorsPath); err != nil {
			return fmt.Errorf("saving vector index: %w", err)
		}
	}
	if lidx, ok := s.lindex.(lexicalIndexWithPersist); ok {
		if err := lidx.Save(s.lexicalPath); err != nil {
			return fmt.Errorf("saving lexical index: %w", err)
		}
	}
	return nil
}

// Index performs a full (re)index of all nodes. Embeddable nodes are
// embedded for dense search (skipping those with unchanged content hash);
// ALL nodes are indexed for BM25.
//
// If embedding fails, dense search is disabled and the error is logged,
// but BM25 indexing continues.
func (s *Service) Index(ctx context.Context, nodes []Node) error {
	s.beginIndexing()
	defer s.endIndexing()

	// Index ALL nodes into BM25 (fast, always works)
	for _, node := range nodes {
		text, _ := EmbedText(node, s.root)
		s.lindex.Upsert(node.ID, text)
	}

	// Embed only embeddable nodes for dense search
	if s.embedder == nil || s.vindex == nil {
		s.setDenseAvailable(false)
		return nil
	}

	// Collect nodes that need embedding (changed content hash)
	toEmbed := make([]Node, 0)
	hashes := make(map[string]string)

	for _, node := range nodes {
		if !node.Embeddable {
			continue
		}

		hash, err := ContentHash(node, s.root)
		if err != nil {
			// Skip nodes we can't hash
			continue
		}
		hashes[node.ID] = hash

		// Check if content changed
		if existingHash := s.vindex.GetHash(node.ID); existingHash != hash {
			toEmbed = append(toEmbed, node)
		}
	}

	// Remove vectors for nodes that no longer exist
	existingIDs := make(map[string]bool)
	for _, node := range nodes {
		if node.Embeddable {
			existingIDs[node.ID] = true
		}
	}
	for _, id := range s.vindex.IDs() {
		if !existingIDs[id] {
			s.vindex.Remove(id)
		}
	}

	// Embed new/changed nodes
	if len(toEmbed) > 0 {
		if err := s.embedNodes(ctx, toEmbed, hashes); err != nil {
			// Log and disable dense, but don't fail the whole index
			fmt.Fprintf(os.Stderr, "retrieval: embedding failed, dense search disabled: %v\n", err)
			s.setDenseAvailable(false)
			return nil
		}
	}

	s.setDenseAvailable(true)
	return nil
}

// Refresh performs incremental index updates. Changed nodes are re-indexed;
// removed node IDs are deleted from both indexes.
func (s *Service) Refresh(ctx context.Context, nodes []Node, removedIDs []string) error {
	s.beginIndexing()
	defer s.endIndexing()

	// Remove deleted nodes from both indexes
	for _, id := range removedIDs {
		s.lindex.Remove(id)
		if s.vindex != nil {
			s.vindex.Remove(id)
		}
	}

	// Update BM25 for all changed nodes
	for _, node := range nodes {
		text, _ := EmbedText(node, s.root)
		s.lindex.Upsert(node.ID, text)
	}

	// Update dense index for embeddable nodes with changed content
	if s.embedder == nil || s.vindex == nil {
		return nil
	}

	toEmbed := make([]Node, 0)
	hashes := make(map[string]string)

	for _, node := range nodes {
		if !node.Embeddable {
			continue
		}

		hash, err := ContentHash(node, s.root)
		if err != nil {
			continue
		}
		hashes[node.ID] = hash

		if existingHash := s.vindex.GetHash(node.ID); existingHash != hash {
			toEmbed = append(toEmbed, node)
		}
	}

	if len(toEmbed) > 0 {
		if err := s.embedNodes(ctx, toEmbed, hashes); err != nil {
			fmt.Fprintf(os.Stderr, "retrieval: embedding failed during refresh: %v\n", err)
			s.setDenseAvailable(false)
			return nil
		}
	}

	return nil
}

// embedNodes embeds the given nodes and updates the vector index.
// For oversized nodes, multiple chunks are embedded and mean-pooled
// into a single vector to preserve the "one node = one vector" invariant.
func (s *Service) embedNodes(ctx context.Context, nodes []Node, hashes map[string]string) error {
	if len(nodes) == 0 {
		return nil
	}

	// Build chunks for all nodes and track which chunks belong to which node
	type chunkInfo struct {
		nodeIdx int
		text    string
	}
	var allChunks []chunkInfo
	chunkCounts := make([]int, len(nodes)) // chunks per node

	for i, node := range nodes {
		chunks, err := BuildChunks(node, s.root)
		if err != nil || len(chunks) == 0 {
			// Fall back to EmbedText for problematic nodes
			text, _ := EmbedText(node, s.root)
			allChunks = append(allChunks, chunkInfo{nodeIdx: i, text: text})
			chunkCounts[i] = 1
			continue
		}
		for _, chunk := range chunks {
			allChunks = append(allChunks, chunkInfo{nodeIdx: i, text: chunk.Text})
		}
		chunkCounts[i] = len(chunks)
	}

	// Extract texts for embedding
	texts := make([]string, len(allChunks))
	for i, c := range allChunks {
		texts[i] = c.text
	}

	// Call embedder once with all chunks
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("calling embedder: %w", err)
	}

	if len(vectors) != len(texts) {
		return fmt.Errorf("embedder returned %d vectors for %d texts", len(vectors), len(texts))
	}

	// Group vectors by node and mean-pool
	vecIdx := 0
	for i, node := range nodes {
		count := chunkCounts[i]
		nodeVecs := vectors[vecIdx : vecIdx+count]
		vecIdx += count

		// Mean-pool chunk vectors into single node vector
		var pooled []float32
		if len(nodeVecs) == 1 {
			pooled = nodeVecs[0]
		} else {
			pooled = MeanPoolVectors(nodeVecs)
		}

		s.vindex.UpsertWithHash(node.ID, pooled, hashes[node.ID])
	}

	return nil
}

// DenseAvailable reports whether dense (vector) search is operational.
func (s *Service) DenseAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.denseAvailable
}

func (s *Service) setDenseAvailable(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.denseAvailable = v
}

// VectorIndex returns the underlying vector index for direct search.
// Returns nil if dense search is not available.
func (s *Service) VectorIndex() VectorIndex {
	if !s.DenseAvailable() {
		return nil
	}
	return s.vindex
}

// VectorIndexWithLookup returns the vector index with lookup capability
// for direct ID-to-vector access. Returns nil if dense search is not
// available or if the index doesn't support lookup.
func (s *Service) VectorIndexWithLookup() VectorIndexWithLookup {
	if !s.DenseAvailable() {
		return nil
	}
	if lookup, ok := s.vindex.(VectorIndexWithLookup); ok {
		return lookup
	}
	return nil
}

// LexicalIndex returns the underlying lexical index for direct search.
func (s *Service) LexicalIndex() LexicalIndex {
	return s.lindex
}

// Root returns the project root directory.
func (s *Service) Root() string {
	return s.root
}

// SetGraph updates the service's graph adjacency map.
// Called when the model is refreshed.
func (s *Service) SetGraph(g *Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}

// Graph returns the current graph (for testing).
func (s *Service) Graph() *Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.graph
}

// EmbedderID returns the identifier of the embedder backing the vector index
// (e.g. an Ollama model name, or "noop" when embeddings are disabled/failed).
// Empty when no vector index is configured.
func (s *Service) EmbedderID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vindex != nil {
		return s.vindex.EmbedderID()
	}
	return ""
}

// IndexFromModels builds nodes and graph from domain models, then indexes.
// This is a convenience method that combines BuildGraph + SetGraph + Index.
func (s *Service) IndexFromModels(ctx context.Context, models []domain.PackageModel) error {
	nodes, graph := BuildGraph(models)
	s.SetGraph(graph)
	return s.Index(ctx, nodes)
}
