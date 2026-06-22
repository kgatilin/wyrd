package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedder_Embed(t *testing.T) {
	// Create a fake OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check authorization header
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Return fake embeddings based on text length
		dim := 8
		data := make([]embeddingData, len(req.Input))
		for i, text := range req.Input {
			embedding := make([]float32, dim)
			for j := range embedding {
				embedding[j] = float32(len(text)+i+j) / 100.0
			}
			data[i] = embeddingData{
				Index:     i,
				Embedding: embedding,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := New(
		WithEndpoint(server.URL),
		WithModel("test-model"),
		WithAPIKey("test-key"),
	)

	texts := []string{"hello", "world", "test"}
	vecs, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vecs) != len(texts) {
		t.Errorf("expected %d vectors, got %d", len(texts), len(vecs))
	}

	for i, vec := range vecs {
		if len(vec) != 8 {
			t.Errorf("vector %d: expected dim 8, got %d", i, len(vec))
		}
	}

	// Dim should be cached
	if embedder.Dim() != 8 {
		t.Errorf("expected Dim() = 8, got %d", embedder.Dim())
	}
}

func TestEmbedder_BatchRequest(t *testing.T) {
	requestCount := 0
	var receivedInputs [][]string

	// Create a fake server that tracks requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		receivedInputs = append(receivedInputs, req.Input)

		// Return embeddings
		data := make([]embeddingData, len(req.Input))
		for i := range req.Input {
			data[i] = embeddingData{
				Index:     i,
				Embedding: make([]float32, 4),
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Use small batch size to force multiple requests
	embedder := New(
		WithEndpoint(server.URL),
		WithMaxBatch(2),
	)

	texts := []string{"a", "b", "c", "d", "e"}
	vecs, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	// Should have made 3 requests: [a,b], [c,d], [e]
	if requestCount != 3 {
		t.Errorf("expected 3 requests, got %d", requestCount)
	}

	if len(vecs) != 5 {
		t.Errorf("expected 5 vectors, got %d", len(vecs))
	}

	// Verify batch sizes
	if len(receivedInputs) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(receivedInputs))
	}
	if len(receivedInputs[0]) != 2 || len(receivedInputs[1]) != 2 || len(receivedInputs[2]) != 1 {
		t.Errorf("unexpected batch sizes: %v", []int{len(receivedInputs[0]), len(receivedInputs[1]), len(receivedInputs[2])})
	}
}

func TestEmbedder_OutOfOrderResponse(t *testing.T) {
	// Server returns embeddings in reverse order
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Return embeddings in reverse order
		data := make([]embeddingData, len(req.Input))
		for i := range req.Input {
			reverseIdx := len(req.Input) - 1 - i
			embedding := make([]float32, 4)
			// Make each embedding distinguishable by its original index
			embedding[0] = float32(reverseIdx)
			data[i] = embeddingData{
				Index:     reverseIdx,
				Embedding: embedding,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL))

	texts := []string{"text0", "text1", "text2"}
	vecs, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	// Verify vectors are in correct order despite out-of-order response
	for i, vec := range vecs {
		if vec[0] != float32(i) {
			t.Errorf("vector %d: expected first element %d, got %f", i, i, vec[0])
		}
	}
}

func TestEmbedder_Empty(t *testing.T) {
	embedder := New()

	vecs, err := embedder.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestEmbedder_ContextCancellation(t *testing.T) {
	// Create a server that blocks
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := embedder.Embed(ctx, []string{"test"})
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestEmbedder_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL))

	_, err := embedder.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Error("expected error on server error")
	}
}

func TestEmbedder_InvalidIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return an invalid index
		resp := embeddingResponse{
			Data: []embeddingData{
				{Index: 99, Embedding: []float32{1, 2, 3}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL))

	_, err := embedder.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Error("expected error on invalid index")
	}
}

func TestEmbedder_MismatchedCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return fewer embeddings than requested
		resp := embeddingResponse{
			Data: []embeddingData{
				{Index: 0, Embedding: []float32{1, 2, 3}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL))

	_, err := embedder.Embed(context.Background(), []string{"test1", "test2"})
	if err == nil {
		t.Error("expected error on mismatched count")
	}
}

func TestEmbedder_ID(t *testing.T) {
	embedder := New(WithModel("test-model"))
	if id := embedder.ID(); id != "openai:test-model" {
		t.Errorf("expected ID 'openai:test-model', got %q", id)
	}

	embedder2 := New() // Uses default
	if id := embedder2.ID(); id != "openai:text-embedding-3-small" {
		t.Errorf("expected ID 'openai:text-embedding-3-small', got %q", id)
	}
}

func TestEmbedder_Defaults(t *testing.T) {
	embedder := New()

	if embedder.endpoint != DefaultEndpoint {
		t.Errorf("expected default endpoint %q, got %q", DefaultEndpoint, embedder.endpoint)
	}

	if embedder.model != DefaultModel {
		t.Errorf("expected default model %q, got %q", DefaultModel, embedder.model)
	}

	if embedder.maxBatch != DefaultMaxBatch {
		t.Errorf("expected default maxBatch %d, got %d", DefaultMaxBatch, embedder.maxBatch)
	}
}

func TestEmbedder_Options(t *testing.T) {
	client := &http.Client{}
	embedder := New(
		WithEndpoint("http://custom:1234"),
		WithModel("custom-model"),
		WithAPIKey("secret-key"),
		WithMaxBatch(50),
		WithHTTPClient(client),
	)

	if embedder.endpoint != "http://custom:1234" {
		t.Errorf("expected custom endpoint, got %q", embedder.endpoint)
	}

	if embedder.model != "custom-model" {
		t.Errorf("expected custom model, got %q", embedder.model)
	}

	if embedder.apiKey != "secret-key" {
		t.Errorf("expected custom API key")
	}

	if embedder.maxBatch != 50 {
		t.Errorf("expected maxBatch 50, got %d", embedder.maxBatch)
	}

	if embedder.client != client {
		t.Error("expected custom client")
	}
}

func TestEmbedder_HasAPIKey(t *testing.T) {
	e1 := New()
	if e1.HasAPIKey() {
		t.Error("expected no API key without explicit config")
	}

	e2 := New(WithAPIKey("secret"))
	if !e2.HasAPIKey() {
		t.Error("expected API key to be set")
	}
}

func TestEmbedder_DimDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResponse{
			Data: []embeddingData{
				{Index: 0, Embedding: make([]float32, 1536)}, // OpenAI ada-002 dimension
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL))

	// Dim should be 0 before first embed
	if embedder.Dim() != 0 {
		t.Errorf("expected Dim() = 0 before embed, got %d", embedder.Dim())
	}

	_, err := embedder.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	// Dim should be discovered
	if embedder.Dim() != 1536 {
		t.Errorf("expected Dim() = 1536 after embed, got %d", embedder.Dim())
	}
}
