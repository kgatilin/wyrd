// Package noop provides a deterministic no-op Embedder for testing and
// graceful degradation when no real embedder is available.
package noop

import (
	"context"
	"hash/fnv"
)

// DefaultDim is the default vector dimension for the noop embedder.
const DefaultDim = 64

// Embedder generates deterministic pseudo-embeddings based on text hashes.
// Useful for testing and as a fallback when Ollama is unavailable.
type Embedder struct {
	dim int
}

// Option configures the Embedder.
type Option func(*Embedder)

// WithDim sets the embedding dimension.
func WithDim(dim int) Option {
	return func(e *Embedder) {
		if dim > 0 {
			e.dim = dim
		}
	}
}

// New creates a noop embedder that produces deterministic vectors.
func New(opts ...Option) *Embedder {
	e := &Embedder{dim: DefaultDim}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Embed generates deterministic embeddings by hashing each text.
// The same text always produces the same vector.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		results[i] = e.hashToVector(text)
	}
	return results, nil
}

// hashToVector converts text to a deterministic vector using FNV-1a hash.
// The vector is normalized to unit length.
func (e *Embedder) hashToVector(text string) []float32 {
	h := fnv.New64a()
	h.Write([]byte(text))
	seed := h.Sum64()

	vec := make([]float32, e.dim)
	var sumSq float32

	// Generate pseudo-random values using a simple LCG seeded by the hash
	state := seed
	for i := range vec {
		// Linear congruential generator
		state = state*6364136223846793005 + 1442695040888963407
		// Convert to float in [-1, 1]
		vec[i] = float32(int64(state>>33)-int64(1<<30)) / float32(1<<30)
		sumSq += vec[i] * vec[i]
	}

	// Normalize to unit vector
	if sumSq > 0 {
		norm := sqrt32(sumSq)
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// sqrt32 computes the square root of a float32.
func sqrt32(x float32) float32 {
	// Newton-Raphson iteration
	if x <= 0 {
		return 0
	}
	guess := x / 2
	for i := 0; i < 10; i++ {
		guess = (guess + x/guess) / 2
	}
	return guess
}

// Dim returns the embedding dimension.
func (e *Embedder) Dim() int {
	return e.dim
}

// ID returns the embedder identifier.
func (e *Embedder) ID() string {
	return "noop"
}
