package vertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeVertex answers the predict endpoint with a vector of dim elements whose
// first one is the length of the text, so a test can tell results apart.
// before runs first and may answer the request itself.
func fakeVertex(t *testing.T, dim int, before func(w http.ResponseWriter, r *http.Request, req predictRequest) bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req predictRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if before != nil && before(w, r, req) {
			return
		}
		vec := make([]float32, dim)
		vec[0] = float32(len(req.Instances[0].Content))
		vec[1] = 1
		var resp predictResponse
		resp.Predictions = make([]struct {
			Embeddings struct {
				Values []float32 `json:"values"`
			} `json:"embeddings"`
		}, 1)
		resp.Predictions[0].Embeddings.Values = vec
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTest(srv *httptest.Server, opts ...Option) *Embedder {
	base := []Option{
		WithEndpoint(srv.URL),
		WithProject("demo-project"),
		WithLocation("us-central1"),
		WithDim(4),
		WithRetry(3, time.Millisecond),
		WithTokenSource(func(context.Context) (string, error) { return "test-token", nil }),
	}
	return New(append(base, opts...)...)
}

func TestEmbed_SendsOneDocumentPerRequest(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
		reqs  []predictRequest
		auth  string
	)
	srv := fakeVertex(t, 4, func(_ http.ResponseWriter, r *http.Request, req predictRequest) bool {
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.URL.Path)
		reqs = append(reqs, req)
		auth = r.Header.Get("Authorization")
		return false
	})

	vecs, err := newTest(srv).Embed(context.Background(), []string{"a", "bbb", "cc"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vecs))
	}
	// Results keep the order of the texts whatever order the requests ran in:
	// the first element grows with the text's length.
	if !(vecs[0][0] < vecs[2][0] && vecs[2][0] < vecs[1][0]) {
		t.Errorf("vectors out of order: %v", vecs)
	}

	want := "/v1/projects/demo-project/locations/us-central1/publishers/google/models/gemini-embedding-001:predict"
	if len(reqs) != 3 {
		t.Fatalf("got %d requests, want 3", len(reqs))
	}
	for i, req := range reqs {
		if paths[i] != want {
			t.Errorf("path = %q, want %q", paths[i], want)
		}
		if len(req.Instances) != 1 {
			t.Errorf("request carries %d instances, want 1", len(req.Instances))
		}
		if got := req.Instances[0].TaskType; got != DefaultDocumentTask {
			t.Errorf("task type = %q, want %q", got, DefaultDocumentTask)
		}
		if req.Parameters.OutputDimensionality != 4 {
			t.Errorf("outputDimensionality = %d, want 4", req.Parameters.OutputDimensionality)
		}
	}
	if auth != "Bearer test-token" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestEmbedQuery_UsesTheQueryTask(t *testing.T) {
	var task string
	srv := fakeVertex(t, 4, func(_ http.ResponseWriter, _ *http.Request, req predictRequest) bool {
		task = req.Instances[0].TaskType
		return false
	})
	if _, err := newTest(srv).EmbedQuery(context.Background(), "where is auth handled"); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if task != DefaultQueryTask {
		t.Errorf("task type = %q, want %q", task, DefaultQueryTask)
	}
}

func TestEmbed_ReturnsUnitVectors(t *testing.T) {
	srv := fakeVertex(t, 4, nil)
	vec, err := newTest(srv).EmbedQuery(context.Background(), "abc")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("squared norm = %v, want 1", sum)
	}
}

func TestEmbed_RetriesThrottlingAndServerErrors(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		var calls atomic.Int32
		srv := fakeVertex(t, 4, func(w http.ResponseWriter, _ *http.Request, _ predictRequest) bool {
			if calls.Add(1) < 3 {
				http.Error(w, "try later", code)
				return true
			}
			return false
		})
		if _, err := newTest(srv).Embed(context.Background(), []string{"doc"}); err != nil {
			t.Errorf("status %d: Embed: %v", code, err)
		}
		if got := calls.Load(); got != 3 {
			t.Errorf("status %d: %d requests, want 3", code, got)
		}
	}
}

func TestEmbed_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := fakeVertex(t, 4, func(w http.ResponseWriter, _ *http.Request, _ predictRequest) bool {
		calls.Add(1)
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
		return true
	})
	_, err := newTest(srv).Embed(context.Background(), []string{"doc"})
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v, want the 429 and its body", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("%d requests, want 3", got)
	}
}

// A query is retried once: its caller is waiting and has the lexical channel
// to answer from.
func TestEmbedQuery_RetriesOnce(t *testing.T) {
	var calls atomic.Int32
	srv := fakeVertex(t, 4, func(w http.ResponseWriter, _ *http.Request, _ predictRequest) bool {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return true
	})
	if _, err := newTest(srv, WithRetry(6, time.Millisecond)).EmbedQuery(context.Background(), "q"); err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("%d requests, want 2", got)
	}
}

func TestEmbed_DoesNotRetryARejectedRequest(t *testing.T) {
	var calls atomic.Int32
	srv := fakeVertex(t, 4, func(w http.ResponseWriter, _ *http.Request, _ predictRequest) bool {
		calls.Add(1)
		http.Error(w, "permission denied", http.StatusForbidden)
		return true
	})
	if _, err := newTest(srv).Embed(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("%d requests, want 1", got)
	}
}

func TestEmbed_RejectsAnotherDimensionality(t *testing.T) {
	srv := fakeVertex(t, 8, nil)
	_, err := newTest(srv).EmbedQuery(context.Background(), "q")
	if err == nil || !strings.Contains(err.Error(), "8 dimensions, 4 requested") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmbed_BoundsRequestsInFlight(t *testing.T) {
	var inFlight, peak atomic.Int32
	srv := fakeVertex(t, 4, func(http.ResponseWriter, *http.Request, predictRequest) bool {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return false
	})
	texts := make([]string, 20)
	for i := range texts {
		texts[i] = "t"
	}
	if _, err := newTest(srv, WithConcurrency(3)).Embed(context.Background(), texts); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got := peak.Load(); got > 3 {
		t.Errorf("%d requests in flight, limit 3", got)
	}
}

func TestEmbed_WithoutProject(t *testing.T) {
	t.Setenv("WYRD_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	e := New(WithTokenSource(func(context.Context) (string, error) { return "t", nil }))
	if e.HasProject() {
		t.Fatal("HasProject without a project")
	}
	if _, err := e.EmbedQuery(context.Background(), "q"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestID_CarriesEverythingThatChangesAVector(t *testing.T) {
	base := New(WithProject("p"))
	if got, want := base.ID(), "vertex:gemini-embedding-001/768/RETRIEVAL_DOCUMENT+CODE_RETRIEVAL_QUERY"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	if base.Dim() != 768 {
		t.Errorf("Dim = %d, want 768 before any embed", base.Dim())
	}
	for name, other := range map[string]*Embedder{
		"dim":   New(WithProject("p"), WithDim(1536)),
		"model": New(WithProject("p"), WithModel("another-model")),
		"task":  New(WithProject("p"), WithTasks("", "RETRIEVAL_QUERY")),
	} {
		if other.ID() == base.ID() {
			t.Errorf("changing the %s left the ID at %q", name, base.ID())
		}
	}
	// The project and location say who is billed and where, not what a
	// vector means: moving between them keeps the stored vectors.
	if moved := New(WithProject("q"), WithLocation("europe-west4")); moved.ID() != base.ID() {
		t.Errorf("changing project and location changed the ID to %q", moved.ID())
	}
}

func TestURL_GlobalLocationHasNoRegionalHost(t *testing.T) {
	global := New(WithProject("p"), WithLocation("global")).url()
	if !strings.HasPrefix(global, "https://aiplatform.googleapis.com/v1/projects/p/locations/global/") {
		t.Errorf("global url = %q", global)
	}
	regional := New(WithProject("p"), WithLocation("europe-west4")).url()
	if !strings.HasPrefix(regional, "https://europe-west4-aiplatform.googleapis.com/v1/projects/p/locations/europe-west4/") {
		t.Errorf("regional url = %q", regional)
	}
}
