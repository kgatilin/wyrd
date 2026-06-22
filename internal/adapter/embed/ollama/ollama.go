// Package ollama provides an Embedder adapter for local Ollama server.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// DefaultEndpoint is the default Ollama API endpoint.
const DefaultEndpoint = "http://localhost:11434"

// DefaultModel is the default embedding model.
const DefaultModel = "nomic-embed-text"

// defaultQueryInstruction is the task description fed to instruction-style
// retrieval models (Qwen3-Embedding) when embedding a search query. It can be
// overridden via the ARCHAI_EMBED_QUERY_INSTRUCTION environment variable.
const defaultQueryInstruction = "Given a code search query, retrieve relevant Go code symbols (functions, methods, types, interfaces) that satisfy it."

// promptStyle selects the model-specific document/query prompt templates.
// Retrieval embedding models are trained with asymmetric prompts: a document
// side and a query side. Sending raw text degrades retrieval quality.
type promptStyle int

const (
	styleRaw   promptStyle = iota // unknown model: no templating
	styleQwen3                    // Qwen3-Embedding: instruction-prefixed query, raw document
	styleGemma                    // EmbeddingGemma: "task:.. | query:.." / "title:.. | text:.."
	styleNomic                    // nomic-embed-text: "search_query:" / "search_document:"
)

func detectStyle(model string) promptStyle {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "qwen3-embedding"):
		return styleQwen3
	case strings.Contains(m, "embeddinggemma"):
		return styleGemma
	case strings.Contains(m, "nomic-embed"):
		return styleNomic
	default:
		return styleRaw
	}
}

// Embedder implements retrieval.Embedder using a local Ollama server.
type Embedder struct {
	endpoint   string
	model      string
	client     *http.Client
	style      promptStyle
	queryInstr string

	// dim is cached after first successful embed
	dimOnce sync.Once
	dim     int
}

// docPrompt formats an indexable document for the configured model.
func (e *Embedder) docPrompt(text string) string {
	switch e.style {
	case styleGemma:
		return "title: none | text: " + text
	case styleNomic:
		return "search_document: " + text
	default: // styleQwen3 documents are raw; styleRaw is raw
		return text
	}
}

// queryPrompt formats a search query for the configured model.
func (e *Embedder) queryPrompt(text string) string {
	switch e.style {
	case styleQwen3:
		return "Instruct: " + e.queryInstr + "\nQuery: " + text
	case styleGemma:
		return "task: search result | query: " + text
	case styleNomic:
		return "search_query: " + text
	default:
		return text
	}
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
		endpoint:   DefaultEndpoint,
		model:      DefaultModel,
		client:     http.DefaultClient,
		queryInstr: defaultQueryInstruction,
	}

	// Read from environment
	if env := os.Getenv("ARCHAI_EMBED_ENDPOINT"); env != "" {
		e.endpoint = env
	}
	if env := os.Getenv("ARCHAI_EMBED_MODEL"); env != "" {
		e.model = env
	}
	if env := os.Getenv("ARCHAI_EMBED_QUERY_INSTRUCTION"); env != "" {
		e.queryInstr = env
	}

	// Apply explicit options
	for _, opt := range opts {
		opt(e)
	}

	// Prompt style is derived from the final model name.
	e.style = detectStyle(e.model)

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

		vec, err := e.embedOne(ctx, e.docPrompt(text))
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

// EmbedQuery embeds a single search query, applying the model-specific query
// instruction/prefix so the query lands in the same space as documents.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vec, err := e.embedOne(ctx, e.queryPrompt(query))
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	e.dimOnce.Do(func() {
		e.dim = len(vec)
	})
	return vec, nil
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
