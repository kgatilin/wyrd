// Package ollama provides an Embedder adapter for local Ollama server.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
)

// DefaultEndpoint is the default Ollama API endpoint.
const DefaultEndpoint = "http://localhost:11434"

// DefaultModel is the default embedding model.
const DefaultModel = "nomic-embed-text"

// Embedder implements retrieval.Embedder using a local Ollama server.
type Embedder struct {
	endpoint string
	model    string
	client   *http.Client

	// dim is cached after first successful embed
	dimOnce sync.Once
	dim     int
}

// Option configures the Embedder.
type Option func(*Embedder)

// WithEndpoint sets the Ollama API endpoint.
func WithEndpoint(endpoint string) Option {
	return func(e *Embedder) {
		if endpoint != "" {
			e.endpoint = endpoint
		}
	}
}

// WithModel sets the embedding model name.
func WithModel(model string) Option {
	return func(e *Embedder) {
		if model != "" {
			e.model = model
		}
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(e *Embedder) {
		if client != nil {
			e.client = client
		}
	}
}

// New creates a new Ollama embedder. Configuration is read from:
// 1. Explicit options (highest priority)
// 2. Environment variables: ARCHAI_EMBED_ENDPOINT, ARCHAI_EMBED_MODEL
// 3. Defaults: localhost:11434, nomic-embed-text
func New(opts ...Option) *Embedder {
	e := &Embedder{
		endpoint: DefaultEndpoint,
		model:    DefaultModel,
		client:   http.DefaultClient,
	}

	// Read from environment
	if env := os.Getenv("ARCHAI_EMBED_ENDPOINT"); env != "" {
		e.endpoint = env
	}
	if env := os.Getenv("ARCHAI_EMBED_MODEL"); env != "" {
		e.model = env
	}

	// Apply explicit options
	for _, opt := range opts {
		opt(e)
	}

	return e
}

// embeddingRequest is the JSON body for Ollama's /api/embeddings endpoint.
type embeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// embeddingResponse is the JSON response from Ollama's /api/embeddings endpoint.
type embeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Embed generates embeddings for the given texts.
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

		vec, err := e.embedOne(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding text %d: %w", i, err)
		}
		results[i] = vec

		// Cache dimension from first result
		e.dimOnce.Do(func() {
			e.dim = len(vec)
		})
	}

	return results, nil
}

func (e *Embedder) embedOne(ctx context.Context, text string) ([]float32, error) {
	reqBody := embeddingRequest{
		Model:  e.model,
		Prompt: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := e.endpoint + "/api/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var respBody embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	// Convert float64 to float32
	vec := make([]float32, len(respBody.Embedding))
	for i, v := range respBody.Embedding {
		vec[i] = float32(v)
	}

	return vec, nil
}

// Dim returns the embedding dimension. Returns 0 if no embedding has been
// generated yet. The dimension is cached after the first successful embed.
func (e *Embedder) Dim() int {
	return e.dim
}

// ID returns the embedder identifier in "provider:model" format.
func (e *Embedder) ID() string {
	return "ollama:" + e.model
}
