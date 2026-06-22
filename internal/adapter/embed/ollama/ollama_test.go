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
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Return one fake embedding per input, based on text length.
		dim := 8
		resp := embedResponse{Embeddings: make([][]float64, len(req.Input))}
		for k, in := range req.Input {
			emb := make([]float64, dim)
			for i := range emb {
				emb[i] = float64(len(in)+i) / 100.0
			}
			resp.Embeddings[k] = emb
		}
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
	var gotInput []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotInput = req.Input
		json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float64{{1, 2, 3}}})
	}))
	defer server.Close()

	e := New(WithEndpoint(server.URL), WithModel("nomic-embed-text"))
	if _, err := e.EmbedQuery(context.Background(), "hello"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if len(gotInput) != 1 || gotInput[0] != "search_query: hello" {
		t.Errorf("server received input %q, want [%q]", gotInput, "search_query: hello")
	}
}

// TestEmbed_BatchesRequests verifies that more inputs than the batch size are
// split across multiple /api/embed calls and reassembled in order.
func TestEmbed_BatchesRequests(t *testing.T) {
	var calls int
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		calls++
		batchSizes = append(batchSizes, len(req.Input))
		resp := embedResponse{Embeddings: make([][]float64, len(req.Input))}
		for i, in := range req.Input {
			resp.Embeddings[i] = []float64{float64(len(in))} // 1-dim, encodes input length
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := New(WithEndpoint(server.URL), WithModel("test-model"), WithBatchSize(2))
	texts := []string{"a", "bb", "ccc", "dddd", "eeeee"} // 5 inputs, batch 2 → 3 calls
	vecs, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 batched calls, got %d (sizes %v)", calls, batchSizes)
	}
	if len(vecs) != 5 {
		t.Fatalf("expected 5 vectors, got %d", len(vecs))
	}
	// Order preserved: vec value encodes input length (docPrompt is raw for test-model).
	want := []float32{1, 2, 3, 4, 5}
	for i, v := range vecs {
		if len(v) != 1 || v[0] != want[i] {
			t.Errorf("vec[%d] = %v, want [%v]", i, v, want[i])
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

func TestEmbedder_ID(t *testing.T) {
	embedder := New(WithModel("test-model"))
	if id := embedder.ID(); id != "ollama:test-model" {
		t.Errorf("expected ID 'ollama:test-model', got %q", id)
	}

	embedder2 := New() // Uses default
	if id := embedder2.ID(); id != "ollama:"+DefaultModel {
		t.Errorf("expected ID 'ollama:%s', got %q", DefaultModel, id)
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
