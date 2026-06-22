// Package ollama provides an Embedder adapter for local Ollama server.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

// DefaultEndpoint is the default Ollama API endpoint.
const DefaultEndpoint = "http://localhost:11434"

// DefaultModel is the default embedding model. Qwen3-Embedding-0.6B is the
// strongest lightweight code-retrieval model available locally via Ollama.
const DefaultModel = "qwen3-embedding:0.6b"

// DefaultBatchSize is how many texts are sent to Ollama's /api/embed in one
// request. Batching dramatically speeds up bulk indexing vs one call per node.
const DefaultBatchSize = 64

// DefaultConcurrency is how many /api/embed batches are sent to Ollama at once.
// The real bottleneck on bulk indexing is model inference, which Ollama
// serializes per request; firing several batches concurrently lets Ollama keep
// multiple workers busy (raise OLLAMA_NUM_PARALLEL to match).
const DefaultConcurrency = 4

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
	endpoint    string
	model       string
	client      *http.Client
	style       promptStyle
	queryInstr  string
	batchSize   int
	concurrency int

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

// WithBatchSize sets how many texts are sent per /api/embed request.
func WithBatchSize(n int) Option {
	return func(e *Embedder) {
		if n > 0 {
			e.batchSize = n
		}
	}
}

// WithConcurrency sets how many /api/embed batches are sent concurrently.
func WithConcurrency(n int) Option {
	return func(e *Embedder) {
		if n > 0 {
			e.concurrency = n
		}
	}
}

// New creates a new Ollama embedder. Configuration is read from:
// 1. Explicit options (highest priority)
// 2. Environment variables: ARCHAI_EMBED_ENDPOINT, ARCHAI_EMBED_MODEL
// 3. Defaults: localhost:11434, qwen3-embedding:0.6b
func New(opts ...Option) *Embedder {
	e := &Embedder{
		endpoint:    DefaultEndpoint,
		model:       DefaultModel,
		client:      http.DefaultClient,
		queryInstr:  defaultQueryInstruction,
		batchSize:   DefaultBatchSize,
		concurrency: DefaultConcurrency,
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
	if env := os.Getenv("ARCHAI_EMBED_BATCH"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n > 0 {
			e.batchSize = n
		}
	}
	if env := os.Getenv("ARCHAI_EMBED_CONCURRENCY"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n > 0 {
			e.concurrency = n
		}
	}

	// Apply explicit options
	for _, opt := range opts {
		opt(e)
	}

	// Prompt style is derived from the final model name.
	e.style = detectStyle(e.model)

	return e
}

// embedRequest is the JSON body for Ollama's /api/embed endpoint, which
// accepts a batch of inputs in a single call.
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse is the JSON response from Ollama's /api/embed endpoint.
type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed generates embeddings for the given documents, batching the requests to
// Ollama's /api/embed endpoint.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	prompts := make([]string, len(texts))
	for i, t := range texts {
		prompts[i] = e.docPrompt(t)
	}
	return e.embedPrompts(ctx, prompts)
}

// EmbedQuery embeds a single search query, applying the model-specific query
// instruction/prefix so the query lands in the same space as documents.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := e.embedPrompts(ctx, []string{e.queryPrompt(query)})
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("ollama: empty embedding for query")
	}
	return vecs[0], nil
}

// embedPrompts embeds already-templated prompts, splitting them into batches
// that are dispatched concurrently (bounded by the configured concurrency).
// Results are written back in input order; the first error cancels the rest.
func (e *Embedder) embedPrompts(ctx context.Context, prompts []string) ([][]float32, error) {
	results := make([][]float32, len(prompts))
	batch := e.batchSize
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	conc := e.concurrency
	if conc <= 0 {
		conc = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	fail := func(err error) {
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	for start := 0; start < len(prompts); start += batch {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}

		start := start
		end := min(start+batch, len(prompts))
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			vecs, err := e.embedBatch(ctx, prompts[start:end])
			if err != nil {
				fail(fmt.Errorf("embedding batch [%d:%d]: %w", start, end, err))
				return
			}
			if len(vecs) != end-start {
				fail(fmt.Errorf("ollama returned %d embeddings for %d inputs", len(vecs), end-start))
				return
			}
			for i, vec := range vecs {
				results[start+i] = vec
				e.dimOnce.Do(func() { e.dim = len(vec) })
			}
		}()
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// embedBatch sends a single /api/embed request for the given prompts.
func (e *Embedder) embedBatch(ctx context.Context, prompts []string) ([][]float32, error) {
	bodyBytes, err := json.Marshal(embedRequest{Model: e.model, Input: prompts})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := e.endpoint + "/api/embed"
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

	var respBody embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	out := make([][]float32, len(respBody.Embeddings))
	for i, emb := range respBody.Embeddings {
		vec := make([]float32, len(emb))
		for j, v := range emb {
			vec[j] = float32(v)
		}
		out[i] = vec
	}
	return out, nil
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
