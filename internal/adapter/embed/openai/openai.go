// Package openai provides an Embedder adapter for OpenAI-compatible embedding APIs.
// It supports any service implementing the /v1/embeddings endpoint (OpenAI, Azure,
// local proxies, etc.).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
)

// DefaultEndpoint is the default OpenAI API endpoint.
const DefaultEndpoint = "https://api.openai.com"

// DefaultModel is the default embedding model.
const DefaultModel = "text-embedding-3-small"

// DefaultMaxBatch is the maximum number of texts per request.
// OpenAI allows up to 2048, but we use a conservative default.
const DefaultMaxBatch = 100

// Embedder implements retrieval.Embedder using an OpenAI-compatible API.
type Embedder struct {
	endpoint string
	model    string
	apiKey   string
	maxBatch int
	client   *http.Client

	// dim is cached after first successful embed
	dimOnce sync.Once
	dim     int
}

// Option configures the Embedder.
type Option func(*Embedder)

// WithEndpoint sets the API endpoint (base URL without /v1/embeddings).
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

// WithAPIKey sets the API key for authorization.
func WithAPIKey(key string) Option {
	return func(e *Embedder) {
		if key != "" {
			e.apiKey = key
		}
	}
}

// WithMaxBatch sets the maximum number of texts per request.
func WithMaxBatch(n int) Option {
	return func(e *Embedder) {
		if n > 0 {
			e.maxBatch = n
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

// New creates a new OpenAI-compatible embedder. Configuration is read from:
// 1. Explicit options (highest priority)
// 2. Environment variables:
//   - ARCHAI_EMBED_ENDPOINT (default: https://api.openai.com)
//   - ARCHAI_EMBED_MODEL (default: text-embedding-3-small)
//   - ARCHAI_EMBED_API_KEY (required for OpenAI, read from env only)
//
// 3. Defaults
func New(opts ...Option) *Embedder {
	e := &Embedder{
		endpoint: DefaultEndpoint,
		model:    DefaultModel,
		maxBatch: DefaultMaxBatch,
		client:   http.DefaultClient,
	}

	// Read from environment
	if env := os.Getenv("ARCHAI_EMBED_ENDPOINT"); env != "" {
		e.endpoint = env
	}
	if env := os.Getenv("ARCHAI_EMBED_MODEL"); env != "" {
		e.model = env
	}
	// API key ONLY from environment (never stored on disk)
	e.apiKey = os.Getenv("ARCHAI_EMBED_API_KEY")

	// Apply explicit options
	for _, opt := range opts {
		opt(e)
	}

	return e
}

// HasAPIKey reports whether an API key is configured.
func (e *Embedder) HasAPIKey() bool {
	return e.apiKey != ""
}

// embeddingRequest is the JSON body for the /v1/embeddings endpoint.
type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embeddingResponse is the JSON response from the /v1/embeddings endpoint.
type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// Embed generates embeddings for the given texts. Texts are batched according
// to maxBatch to handle large inputs efficiently.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))

	// Process in batches
	for start := 0; start < len(texts); start += e.maxBatch {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := start + e.maxBatch
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		vecs, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embedding batch [%d:%d]: %w", start, end, err)
		}

		// Copy results to correct positions
		for i, vec := range vecs {
			results[start+i] = vec
		}

		// Cache dimension from first successful result
		if len(vecs) > 0 && len(vecs[0]) > 0 {
			e.dimOnce.Do(func() {
				e.dim = len(vecs[0])
			})
		}
	}

	return results, nil
}

// EmbedQuery embeds a single search query. OpenAI-compatible embedding models
// (text-embedding-3 family) are symmetric and need no query instruction, so
// the query is embedded as-is in the same space as documents.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("openai: empty embedding for query")
	}
	return vecs[0], nil
}

// embedBatch sends a single batch request to the API.
func (e *Embedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Model: e.model,
		Input: texts,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := e.endpoint + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var respBody embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(respBody.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(respBody.Data))
	}

	// Results may come back out of order; sort by index
	results := make([][]float32, len(texts))
	for _, d := range respBody.Data {
		if d.Index < 0 || d.Index >= len(results) {
			return nil, fmt.Errorf("invalid embedding index %d", d.Index)
		}
		results[d.Index] = d.Embedding
	}

	return results, nil
}

// Dim returns the embedding dimension. Returns 0 if no embedding has been
// generated yet. The dimension is cached after the first successful embed.
func (e *Embedder) Dim() int {
	return e.dim
}

// ID returns the embedder identifier in "provider:model" format.
func (e *Embedder) ID() string {
	return "openai:" + e.model
}
