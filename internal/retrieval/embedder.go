// Package retrieval provides code search and retrieval capabilities for
// archai's domain model. It projects PackageModel into embeddable Node
// chunks and supports hybrid search (dense + BM25) with graph-based expansion.
package retrieval

import "context"

// Embedder generates vector embeddings for text chunks.
type Embedder interface {
	// Embed produces embeddings for the given texts. Returns a slice of
	// vectors where each vector corresponds to the input text at the same
	// index. The vector dimension is Dim().
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dim returns the dimensionality of the embedding vectors.
	Dim() int

	// ID returns a stable identifier for this embedder in the format
	// "provider:model" (e.g., "ollama:nomic-embed-text"). Used to
	// invalidate cached vectors when the embedder model changes.
	ID() string
}
