package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedder_Embed(t *testing.T) {
	// Create a fake Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Return a fake embedding based on text length
		dim := 8
		embedding := make([]float64, dim)
		for i := range embedding {
			embedding[i] = float64(len(req.Prompt)+i) / 100.0
		}

		resp := embeddingResponse{Embedding: embedding}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := New(WithEndpoint(server.URL), WithModel("test-model"))

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

func TestPromptTemplates(t *testing.T) {
	const q = "find the http handler"
	const d = "func Handler(w, r)"
	cases := []struct {
		model   string
		wantDoc string
		wantQry string
	}{
		{"qwen3-embedding:0.6b", d, "Instruct: " + defaultQueryInstruction + "\nQuery: " + q},
		{"embeddinggemma:300m", "title: none | text: " + d, "task: search result | query: " + q},
		{"nomic-embed-text", "search_document: " + d, "search_query: " + q},
		{"some-unknown-model", d, q},
	}
	for _, tc := range cases {
		e := New(WithModel(tc.model))
		if got := e.docPrompt(d); got != tc.wantDoc {
			t.Errorf("%s docPrompt = %q, want %q", tc.model, got, tc.wantDoc)
		}
		if got := e.queryPrompt(q); got != tc.wantQry {
			t.Errorf("%s queryPrompt = %q, want %q", tc.model, got, tc.wantQry)
		}
	}
}

// TestEmbedQuery_SendsTemplatedPrompt verifies that EmbedQuery sends the
// query-side template (not the raw query) to the server.
func TestEmbedQuery_SendsTemplatedPrompt(t *testing.T) {
	var gotPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotPrompt = req.Prompt
		json.NewEncoder(w).Encode(embeddingResponse{Embedding: []float64{1, 2, 3}})
	}))
	defer server.Close()

	e := New(WithEndpoint(server.URL), WithModel("nomic-embed-text"))
	if _, err := e.EmbedQuery(context.Background(), "hello"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if gotPrompt != "search_query: hello" {
		t.Errorf("server received prompt %q, want %q", gotPrompt, "search_query: hello")
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

func TestEmbedder_ID(t *testing.T) {
	embedder := New(WithModel("test-model"))
	if id := embedder.ID(); id != "ollama:test-model" {
		t.Errorf("expected ID 'ollama:test-model', got %q", id)
	}

	embedder2 := New() // Uses default
	if id := embedder2.ID(); id != "ollama:nomic-embed-text" {
		t.Errorf("expected ID 'ollama:nomic-embed-text', got %q", id)
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
}

func TestEmbedder_Options(t *testing.T) {
	client := &http.Client{}
	embedder := New(
		WithEndpoint("http://custom:1234"),
		WithModel("custom-model"),
		WithHTTPClient(client),
	)

	if embedder.endpoint != "http://custom:1234" {
		t.Errorf("expected custom endpoint, got %q", embedder.endpoint)
	}

	if embedder.model != "custom-model" {
		t.Errorf("expected custom model, got %q", embedder.model)
	}

	if embedder.client != client {
		t.Error("expected custom client")
	}
}
