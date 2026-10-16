// Package retrieval provides code search and retrieval capabilities for
// wyrd's domain model. This file defines the ports of the corpus: the dense
// vector store and the lexical index, both keyed by content hash.
package retrieval

import (
	"context"

	"github.com/kgatilin/wyrd/internal/retrieval/types"
)

// Scored is an alias for types.Scored for convenience.
type Scored = types.Scored

// VectorStore is the corpus's dense channel: one vector per content hash (see
// ContentHash), shared by every view of the repository. A hash's text is the
// same wherever it occurs, so a vector stored once serves every worktree that
// has that text.
//
// Implementations must be safe for concurrent use. Vectors handed out may be
// shared and must be treated as immutable.
type VectorStore interface {
	// Has reports whether a vector is stored for the hash. It is answered
	// from memory, so readiness can be polled cheaply.
	Has(hash string) bool

	// Get returns the stored vectors of the given hashes; a hash without one
	// is absent from the map.
	Get(ctx context.Context, hashes []string) (map[string][]float32, error)

	// Put stores vectors by hash. Re-putting a known hash is harmless.
	Put(ctx context.Context, vectors map[string][]float32) error

	// Search returns at most k hashes accepted by keep, by cosine similarity
	// to vec, best first. keep is the asking view's membership test.
	Search(ctx context.Context, vec []float32, k int, keep func(hash string) bool) ([]Scored, error)

	// Save persists whatever was put since the last call. A store that is
	// its own persistence returns nil.
	Save() error
}

// LexicalIndex provides term-based text search using BM25 ranking. In the
// corpus a document id is a content hash.
type LexicalIndex interface {
	// Upsert inserts or updates the document for the given ID.
	Upsert(id, text string)

	// Remove deletes the document for the given ID.
	Remove(id string)

	// Has reports whether a document is indexed under the ID.
	Has(id string) bool

	// Search returns at most k documents accepted by keep, by BM25 score
	// (descending). A nil keep accepts every document. An index that is not
	// in this process returns an error rather than an empty answer when it
	// cannot be asked.
	Search(ctx context.Context, query string, k int, keep func(id string) bool) ([]Scored, error)

	// Len returns the number of documents in the index.
	Len() int
}
