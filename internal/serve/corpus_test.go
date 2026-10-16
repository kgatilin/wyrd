package serve

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/embed/noop"
	"github.com/kgatilin/wyrd/internal/adapter/lindex/bm25"
	"github.com/kgatilin/wyrd/internal/adapter/vindex/vecstore"
	"github.com/kgatilin/wyrd/internal/retrieval"
)

// TestStatesShareOneCorpus walks the point of the corpus: a sibling worktree
// with byte-identical sources indexes without embedding anything and is ready
// at once, and the repository's retrieval state is persisted once — under the
// corpus directory, not under any worktree.
func TestStatesShareOneCorpus(t *testing.T) {
	// The package TestMain disables retrieval; these tests are about the
	// retrieval wiring, so turn it back on with a deterministic embedder.
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")
	ctx := context.Background()

	dir := t.TempDir()
	emb := &countingEmbedder{Embedder: noop.New()}
	recipe := retrieval.Recipe(emb.ID())
	lexPath := filepath.Join(dir, "lexical@r1.bm25")
	corpus := retrieval.NewCorpus(emb, vecstore.New(vecstore.Path(dir, recipe), recipe), bm25.New(), lexPath)

	first := newFixture(t)
	st1 := loadAndIndex(t, ctx, first, corpus)
	embedded := emb.count()
	if embedded == 0 {
		t.Fatal("first worktree embedded nothing")
	}
	if s := st1.Retrieval().IndexStatus(); s.Pending != 0 || s.Embedded == 0 {
		t.Fatalf("first worktree status = %+v", s)
	}

	second := newFixture(t)
	st2 := loadAndIndex(t, ctx, second, corpus)
	if more := emb.count() - embedded; more != 0 {
		t.Errorf("sibling worktree embedded %d text(s); want 0, all already in the corpus", more)
	}
	if s := st2.Retrieval().IndexStatus(); s.Pending != 0 || s.Embedded != st1.Retrieval().IndexStatus().Embedded {
		t.Errorf("sibling worktree status = %+v, want the same coverage as the first", s)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("corpus directory = %v, want one lexical and one vector file", names)
	}
	for _, root := range []string{first, second} {
		for _, f := range []string{"vectors.json", "bm25.json"} {
			if _, err := os.Stat(filepath.Join(root, ".archai", "cache", f)); !os.IsNotExist(err) {
				t.Errorf("%s: per-worktree %s written", root, f)
			}
		}
	}
}

func TestOpenCorpusPersistsUnderWyrdHome(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")
	t.Setenv("WYRD_HOME", t.TempDir())
	ctx := context.Background()

	root := newFixture(t)
	corpus, err := OpenCorpus(root)
	if err != nil {
		t.Fatalf("OpenCorpus: %v", err)
	}
	loadAndIndex(t, ctx, root, corpus)

	dir, err := CorpusDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		filepath.Base(vecstore.Path(dir, retrieval.Recipe("noop"))),
		"lexical@r1.bm25",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not persisted: %v", name, err)
		}
	}

	// A reopened corpus serves a fresh State without embedding.
	reopened, err := OpenCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	st := NewState(root, WithCorpus(reopened))
	if err := st.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if s := st.Retrieval().IndexStatus(); s.Embeddable == 0 || s.Pending != 0 {
		t.Errorf("state over a reopened corpus = %+v, want ready before any embedding", s)
	}
	<-st.RetrievalReady()
}

func TestStateWithoutCorpusIndexes(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")

	root := newFixture(t)
	st := loadAndIndex(t, context.Background(), root, nil)
	if status := st.Retrieval().IndexStatus(); status.Embedded == 0 {
		t.Errorf("index is empty without a shared corpus: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(root, ".archai", "cache")); !os.IsNotExist(err) {
		t.Error("a private corpus wrote to the worktree")
	}
}

// loadAndIndex loads a State over root and blocks until its embedding pass
// (and the save after it) has finished.
func loadAndIndex(t *testing.T, ctx context.Context, root string, corpus *retrieval.Corpus) *State {
	t.Helper()

	var opts []StateOption
	if corpus != nil {
		opts = append(opts, WithCorpus(corpus))
	}
	st := NewState(root, opts...)
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load(%s): %v", root, err)
	}
	ready := st.RetrievalReady()
	if ready == nil {
		t.Fatalf("retrieval disabled for %s", root)
	}
	<-ready
	return st
}

// countingEmbedder counts the texts it embeds.
type countingEmbedder struct {
	retrieval.Embedder
	mu    sync.Mutex
	texts int
}

func (e *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.texts += len(texts)
	e.mu.Unlock()
	return e.Embedder.Embed(ctx, texts)
}

func (e *countingEmbedder) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.texts
}

func TestQdrantFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    bool
		wantURL string
		wantKey string
	}{
		{name: "nothing set"},
		{name: "tenant connection only", env: map[string]string{"QDRANT_URL": "http://q", "QDRANT_API_KEY": "k"}},
		{name: "collection without url", env: map[string]string{"WYRD_QDRANT_COLLECTION": "code"}},
		{name: "generic connection + collection",
			env:  map[string]string{"QDRANT_URL": "http://q", "QDRANT_API_KEY": "k", "WYRD_QDRANT_COLLECTION": "code"},
			want: true, wantURL: "http://q", wantKey: "k"},
		{name: "wyrd names win",
			env: map[string]string{"QDRANT_URL": "http://q", "QDRANT_API_KEY": "k",
				"WYRD_QDRANT_URL": "http://w", "WYRD_QDRANT_API_KEY": "wk", "WYRD_QDRANT_COLLECTION": "code"},
			want: true, wantURL: "http://w", wantKey: "wk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []string{"WYRD_QDRANT_URL", "WYRD_QDRANT_API_KEY", "WYRD_QDRANT_COLLECTION", "QDRANT_URL", "QDRANT_API_KEY"} {
				t.Setenv(n, tc.env[n])
			}
			cfg, ok := qdrantFromEnv()
			if ok != tc.want {
				t.Fatalf("selected = %v, want %v", ok, tc.want)
			}
			if ok && (cfg.URL != tc.wantURL || cfg.APIKey != tc.wantKey || cfg.Collection != "code") {
				t.Errorf("config = %+v", cfg)
			}
		})
	}
}

// fakeCollection is the least of a Qdrant collection OpenCorpus touches: the
// collection info, an empty scroll, upserts (counted, by channel) and an
// empty search. It is a legacy collection unless both is set.
type fakeCollection struct {
	size int
	// both gives it the named dense and sparse vectors.
	both bool

	mu       sync.Mutex
	upserted int
	// lexical counts the upserted points that carry the sparse vector.
	lexical int
}

func (f *fakeCollection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := any(nil)
	switch {
	case r.Method == http.MethodGet:
		dense := map[string]any{"size": f.size, "distance": "Cosine"}
		params := map[string]any{"vectors": dense}
		if f.both {
			params = map[string]any{
				"vectors":        map[string]any{"dense": dense},
				"sparse_vectors": map[string]any{"bm25": map[string]any{"modifier": "idf"}},
			}
		}
		result = map[string]any{"config": map[string]any{"params": params}}
	case strings.HasSuffix(r.URL.Path, "/points/scroll"):
		result = map[string]any{"points": []any{}, "next_page_offset": nil}
	case strings.HasSuffix(r.URL.Path, "/points/query"):
		result = map[string]any{"points": []any{}}
	case r.Method == http.MethodPut:
		var body struct {
			Points []struct {
				Vector map[string]json.RawMessage `json:"vector"`
			} `json:"points"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.upserted += len(body.Points)
		for _, p := range body.Points {
			if _, ok := p.Vector["bm25"]; ok {
				f.lexical++
			}
		}
		result = map[string]any{"status": "completed"}
	default:
		result = []any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "status": "ok"})
}

func (f *fakeCollection) lexicalCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lexical
}

func (f *fakeCollection) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.upserted
}

func TestOpenCorpusQdrantSeedsFromLocalFileAndWritesNothingLocally(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")
	t.Setenv("WYRD_HOME", t.TempDir())
	root := newFixture(t)
	dir, err := CorpusDir(root)
	if err != nil {
		t.Fatal(err)
	}
	dim := noop.New().Dim()

	// The repository already has vectors in its local file.
	recipe := retrieval.Recipe("noop")
	localPath := vecstore.Path(dir, recipe)
	local := vecstore.New(localPath, recipe)
	vec := make([]float32, dim)
	vec[0] = 1
	if err := local.Put(context.Background(), map[string][]float32{"h1": vec, "h2": vec}); err != nil {
		t.Fatal(err)
	}
	if err := local.Save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(localPath)

	fake := &fakeCollection{size: dim}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("WYRD_QDRANT_URL", srv.URL)
	t.Setenv("WYRD_QDRANT_COLLECTION", "code")

	corpus, err := OpenCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := fake.count(); n != 2 {
		t.Fatalf("seeded %d point(s), want the 2 in the local file", n)
	}

	loadAndIndex(t, context.Background(), root, corpus)
	if fake.count() <= 2 {
		t.Error("indexing stored no vectors in the collection")
	}
	after, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("seeding removed the local file: %v", err)
	}
	if after.Size() != before.Size() {
		t.Errorf("the local vector file changed (%d → %d bytes) with qdrant selected", before.Size(), after.Size())
	}
}

func TestOpenCorpusFallsBackWhenCollectionMismatches(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")
	t.Setenv("WYRD_HOME", t.TempDir())
	root := newFixture(t)

	srv := httptest.NewServer(&fakeCollection{size: noop.New().Dim() + 1})
	defer srv.Close()
	t.Setenv("WYRD_QDRANT_URL", srv.URL)
	t.Setenv("WYRD_QDRANT_COLLECTION", "code")

	corpus, err := OpenCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	loadAndIndex(t, context.Background(), root, corpus)

	dir, _ := CorpusDir(root)
	if _, err := os.Stat(vecstore.Path(dir, retrieval.Recipe("noop"))); err != nil {
		t.Errorf("fallback did not use the local vector file: %v", err)
	}
}

func TestOpenCorpusQdrantTakesTheDimensionFromTheLocalFileWhenTheEmbedderIsDown(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "ollama")
	t.Setenv("WYRD_EMBED_MODEL", "probe-model")
	t.Setenv("WYRD_HOME", t.TempDir())
	root := newFixture(t)
	dir, err := CorpusDir(root)
	if err != nil {
		t.Fatal(err)
	}

	// Ollama reports its dimension only after a first call, and here it is
	// down: nothing listens on the endpoint.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + dead.Addr().String()
	_ = dead.Close()
	t.Setenv("WYRD_EMBED_ENDPOINT", endpoint)

	const dim = 7
	recipe := retrieval.Recipe("ollama:probe-model")
	localPath := vecstore.Path(dir, recipe)
	local := vecstore.New(localPath, recipe)
	vec := make([]float32, dim)
	vec[0] = 1
	if err := local.Put(context.Background(), map[string][]float32{"h1": vec}); err != nil {
		t.Fatal(err)
	}
	if err := local.Save(); err != nil {
		t.Fatal(err)
	}

	fake := &fakeCollection{size: dim}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("WYRD_QDRANT_URL", srv.URL)
	t.Setenv("WYRD_QDRANT_COLLECTION", "code")

	if _, err := OpenCorpus(root); err != nil {
		t.Fatal(err)
	}
	if n := fake.count(); n != 1 {
		t.Fatalf("seeded %d point(s): qdrant was not selected with the embedder down", n)
	}
}

// A collection with both vectors takes the lexical documents too, and the
// lexical file is not written: the collection is the index.
func TestOpenCorpusQdrantHoldsTheLexicalIndexToo(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")
	t.Setenv("WYRD_HOME", t.TempDir())
	root := newFixture(t)

	fake := &fakeCollection{size: noop.New().Dim(), both: true}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("WYRD_QDRANT_URL", srv.URL)
	t.Setenv("WYRD_QDRANT_COLLECTION", "code")

	corpus, err := OpenCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	st := loadAndIndex(t, context.Background(), root, corpus)
	if err := st.Retrieval().Save(); err != nil {
		t.Fatal(err)
	}
	if fake.lexicalCount() == 0 {
		t.Error("indexing stored no lexical documents in the collection")
	}
	dir, _ := CorpusDir(root)
	if _, err := os.Stat(filepath.Join(dir, retrieval.LexicalRecipe()+".bm25")); err == nil {
		t.Error("the lexical file was written with the index in qdrant")
	}
	if faults := corpus.Faults(); len(faults) != 0 {
		t.Errorf("faults after a clean index: %v", faults)
	}
}

// A legacy collection keeps working as the dense channel, with the lexical
// index in its file as before.
func TestOpenCorpusLegacyCollectionKeepsTheLexicalIndexLocal(t *testing.T) {
	t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
	t.Setenv("WYRD_EMBED_PROVIDER", "noop")
	t.Setenv("WYRD_HOME", t.TempDir())
	root := newFixture(t)

	fake := &fakeCollection{size: noop.New().Dim()}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("WYRD_QDRANT_URL", srv.URL)
	t.Setenv("WYRD_QDRANT_COLLECTION", "code")

	corpus, err := OpenCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	st := loadAndIndex(t, context.Background(), root, corpus)
	if err := st.Retrieval().Save(); err != nil {
		t.Fatal(err)
	}
	if fake.count() == 0 || fake.lexicalCount() != 0 {
		t.Errorf("collection got %d point(s), %d lexical; want vectors only", fake.count(), fake.lexicalCount())
	}
	dir, _ := CorpusDir(root)
	if _, err := os.Stat(filepath.Join(dir, retrieval.LexicalRecipe()+".bm25")); err != nil {
		t.Errorf("the lexical file was not written: %v", err)
	}
}

// fakeEmbeddings is an OpenAI-compatible /v1/embeddings of dim dimensions.
func fakeEmbeddings(t *testing.T, dim int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dim)
			vec[i%dim] = 1
			data[i] = map[string]any{"index": i, "embedding": vec}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// With WYRD_REQUIRE_REMOTE every condition that otherwise degrades a channel
// to something local fails the start instead.
func TestOpenCorpusRequireRemoteFailsInsteadOfFallingBack(t *testing.T) {
	const dim = 4
	tests := []struct {
		name    string
		env     map[string]string
		fake    *fakeCollection // nil: Qdrant not configured
		wantErr string
	}{
		{name: "both channels remote", fake: &fakeCollection{size: dim, both: true}},
		{name: "noop embedder", env: map[string]string{"WYRD_EMBED_PROVIDER": "noop"},
			fake: &fakeCollection{size: dim, both: true}, wantErr: "noop stand-in"},
		{name: "embedding disabled", env: map[string]string{"WYRD_EMBED_DISABLE": "1"},
			fake: &fakeCollection{size: dim, both: true}, wantErr: "noop stand-in"},
		{name: "provider without credentials", env: map[string]string{"WYRD_EMBED_API_KEY": ""},
			fake: &fakeCollection{size: dim, both: true}, wantErr: "WYRD_EMBED_API_KEY is not set"},
		{name: "unknown provider", env: map[string]string{"WYRD_EMBED_PROVIDER": "nobody"},
			fake: &fakeCollection{size: dim, both: true}, wantErr: "unknown WYRD_EMBED_PROVIDER"},
		{name: "no qdrant", wantErr: "Qdrant is not configured"},
		{name: "another dimension", fake: &fakeCollection{size: dim + 1, both: true}, wantErr: "vector size 5"},
		{name: "legacy collection", fake: &fakeCollection{size: dim}, wantErr: "wyrd qdrant migrate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WYRD_REQUIRE_REMOTE", "1")
			t.Setenv("WYRD_RETRIEVAL_DISABLE", "0")
			t.Setenv("WYRD_EMBED_DISABLE", "")
			t.Setenv("WYRD_EMBED_PROVIDER", "openai")
			t.Setenv("WYRD_EMBED_API_KEY", "test-key")
			t.Setenv("WYRD_EMBED_MODEL", "test-model")
			t.Setenv("WYRD_EMBED_ENDPOINT", fakeEmbeddings(t, dim))
			t.Setenv("WYRD_HOME", t.TempDir())
			t.Setenv("WYRD_QDRANT_URL", "")
			t.Setenv("QDRANT_URL", "")
			t.Setenv("WYRD_QDRANT_COLLECTION", "")
			if tc.fake != nil {
				srv := httptest.NewServer(tc.fake)
				defer srv.Close()
				t.Setenv("WYRD_QDRANT_URL", srv.URL)
				t.Setenv("WYRD_QDRANT_COLLECTION", "code")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := OpenCorpus(newFixture(t))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("OpenCorpus: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}
