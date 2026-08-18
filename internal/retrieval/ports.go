// Package retrieval provides code search and retrieval capabilities for
// archai's domain model. This file defines the port interfaces for vector
// and lexical indexes.
package retrieval

import "github.com/kgatilin/archai/internal/retrieval/types"

// Scored is an alias for types.Scored for convenience.
type Scored = types.Scored

// VectorIndex stores and searches dense vector embeddings.
type VectorIndex interface {
	// Upsert inserts or updates the vector for the given ID.
	Upsert(id string, vec []float32)

	// Remove deletes the vector for the given ID.
	Remove(id string)

	// Search returns the top-k results by cosine similarity (descending).
	Search(vec []float32, k int) []Scored

	// Len returns the number of vectors in the index.
	Len() int
}

// VectorCache is a repo-level, content-addressed cache of node vectors,
// shared across the worktrees of one repository. Keys are content hashes
// (see ContentHash), which makes an entry valid anywhere the same text is
// embedded with the same model and recipe — nothing worktree-specific is in
// the key. The Service consults it before calling the Embedder, so a fresh
// worktree inherits the embedding pass of the ones indexed before it.
//
// Implementations must be safe for concurrent use. Vectors handed out by Get
// may be shared with other callers and must be treated as immutable.
type VectorCache interface {
	// Get returns the cached vector for a content hash, if any.
	Get(contentHash string) ([]float32, bool)

	// Put records a vector for a content hash. Entries are immutable, so
	// putting a known hash is a no-op.
	Put(contentHash string, vec []float32)
}

// LexicalIndex provides term-based text search using BM25 ranking.
type LexicalIndex interface {
	// Upsert inserts or updates the document for the given ID.
	Upsert(id, text string)

	// Remove deletes the document for the given ID.
	Remove(id string)

	// Search returns the top-k results by BM25 score (descending).
	Search(query string, k int) []Scored

	// Len returns the number of documents in the index.
	Len() int
}
