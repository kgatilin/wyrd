package qdrant

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	testKey        = "test-token"
	testCollection = "code"
	testRepo       = "repo-a"
	testRecipe     = "noop@r1"
)

// fakeQdrant speaks the subset of the Qdrant REST API the adapter uses, over
// one in-memory collection, and records the requests it saw. It starts as a
// legacy collection: a single unnamed vector of size dimensions.
type fakeQdrant struct {
	t        *testing.T
	size     int
	distance string
	// absent makes the collection not exist until it is created.
	absent bool
	// foreign reports the vector under a name the adapter does not use.
	foreign bool
	// both makes it a collection with the named dense and sparse vectors;
	// modifier is the sparse vector's.
	both     bool
	modifier string

	mu      sync.Mutex
	points  map[string]fakePoint
	upsert  []int // points per upsert request
	search  []map[string]any
	query   []map[string]any
	deleted []int // points per delete request
	indexed []string
	gets    int
	// failWrites makes every upsert and delete fail.
	failWrites bool
}

type fakePoint struct {
	vec     []float32
	sparse  map[uint32]float32
	payload map[string]any
}

func newFake(t *testing.T, size int) (*fakeQdrant, *httptest.Server) {
	f := &fakeQdrant{t: t, size: size, distance: "Cosine", modifier: "idf", points: map[string]fakePoint{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

// newFakeBoth is a fake whose collection has both named vectors.
func newFakeBoth(t *testing.T, size int) (*fakeQdrant, *httptest.Server) {
	f, srv := newFake(t, size)
	f.both = true
	return f, srv
}

func (f *fakeQdrant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("api-key") != testKey {
		http.Error(w, `{"status":{"error":"forbidden"}}`, http.StatusForbidden)
		return
	}
	prefix := "/collections/" + testCollection
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, `{"status":{"error":"Not found: Collection doesn't exist"}}`, http.StatusNotFound)
		return
	}
	var body map[string]any
	if r.Body != nil && r.Method != http.MethodGet {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, prefix)
	if f.absent && !(r.Method == http.MethodPut && path == "") {
		http.Error(w, `{"status":{"error":"Not found: Collection doesn't exist"}}`, http.StatusNotFound)
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "":
		params := map[string]any{}
		dense := map[string]any{"size": f.size, "distance": f.distance}
		switch {
		case f.both:
			params["vectors"] = map[string]any{DenseVector: dense}
			params["sparse_vectors"] = map[string]any{SparseVector: map[string]any{"modifier": f.modifier}}
		case f.foreign:
			params["vectors"] = map[string]any{"text": dense}
		default:
			params["vectors"] = dense
		}
		reply(w, map[string]any{"config": map[string]any{"params": params}})

	case r.Method == http.MethodPut && path == "":
		if !f.absent {
			http.Error(w, `{"status":{"error":"already exists"}}`, http.StatusConflict)
			return
		}
		dense := body["vectors"].(map[string]any)[DenseVector].(map[string]any)
		f.size, f.distance = int(dense["size"].(float64)), dense["distance"].(string)
		f.modifier, _ = body["sparse_vectors"].(map[string]any)[SparseVector].(map[string]any)["modifier"].(string)
		f.absent, f.both = false, true
		reply(w, true)

	case r.Method == http.MethodPut && path == "/index":
		f.indexed = append(f.indexed, body["field_name"].(string)+":"+body["field_schema"].(string))
		reply(w, map[string]any{"status": "completed"})

	case r.Method == http.MethodPost && path == "/points/scroll":
		ids := f.matching(body["filter"])
		sort.Strings(ids)
		start := 0
		if off, ok := body["offset"].(string); ok {
			start = sort.SearchStrings(ids, off)
		}
		limit := int(body["limit"].(float64))
		end := min(start+limit, len(ids))
		var page []map[string]any
		for _, id := range ids[start:end] {
			pt := map[string]any{"id": id, "payload": map[string]any{"hash": f.points[id].payload["hash"]}}
			if body["with_payload"] == true {
				pt["payload"] = f.points[id].payload
			}
			if body["with_vector"] == true {
				pt["vector"] = f.points[id].vec
			}
			page = append(page, pt)
		}
		var next any
		if end < len(ids) {
			next = ids[end]
		}
		reply(w, map[string]any{"points": page, "next_page_offset": next})

	case r.Method == http.MethodPut && path == "/points":
		if r.URL.Query().Get("wait") != "true" {
			f.t.Errorf("upsert without wait=true")
		}
		if f.failWrites {
			http.Error(w, `{"status":{"error":"disk full"}}`, http.StatusInternalServerError)
			return
		}
		pts := body["points"].([]any)
		f.upsert = append(f.upsert, len(pts))
		for _, raw := range pts {
			p := raw.(map[string]any)
			pt := fakePoint{payload: p["payload"].(map[string]any)}
			switch vec := p["vector"].(type) {
			case []any:
				if f.both {
					f.t.Errorf("bare vector written to a collection with named vectors")
				}
				pt.vec = floats(vec)
			case map[string]any:
				if !f.both {
					f.t.Errorf("named vector written to a legacy collection")
				}
				pt.vec = floats(vec[DenseVector])
				if sp, ok := vec[SparseVector].(map[string]any); ok {
					pt.sparse = map[uint32]float32{}
					values := floats(sp["values"])
					for i, idx := range sp["indices"].([]any) {
						pt.sparse[uint32(idx.(float64))] = values[i]
					}
				}
			}
			f.points[p["id"].(string)] = pt
		}
		reply(w, map[string]any{"status": "completed"})

	case r.Method == http.MethodPost && path == "/points/delete":
		if f.failWrites {
			http.Error(w, `{"status":{"error":"disk full"}}`, http.StatusInternalServerError)
			return
		}
		ids := body["points"].([]any)
		f.deleted = append(f.deleted, len(ids))
		for _, id := range ids {
			delete(f.points, id.(string))
		}
		reply(w, map[string]any{"status": "completed"})

	case r.Method == http.MethodPost && path == "/points":
		f.gets++
		var out []map[string]any
		for _, raw := range body["ids"].([]any) {
			if p, ok := f.points[raw.(string)]; ok {
				var vec any = p.vec
				if f.both {
					vec = map[string]any{DenseVector: p.vec}
				}
				out = append(out, map[string]any{"id": raw, "payload": map[string]any{"hash": p.payload["hash"]}, "vector": vec})
			}
		}
		reply(w, out)

	case r.Method == http.MethodPost && path == "/points/search":
		f.search = append(f.search, body)
		var q []float32
		if named, ok := body["vector"].(map[string]any); ok {
			if !f.both || named["name"] != DenseVector {
				f.t.Errorf("search names vector %v", named["name"])
			}
			q = floats(named["vector"])
		} else {
			if f.both {
				f.t.Errorf("search of a named collection without a vector name")
			}
			q = floats(body["vector"])
		}
		type hit struct {
			id    string
			score float64
		}
		var hits []hit
		for _, id := range f.matching(body["filter"]) {
			if len(f.points[id].vec) > 0 {
				hits = append(hits, hit{id, cos(q, f.points[id].vec)})
			}
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		hits = hits[:min(int(body["limit"].(float64)), len(hits))]
		var out []map[string]any
		for _, h := range hits {
			out = append(out, map[string]any{"id": h.id, "score": h.score, "payload": map[string]any{"hash": f.points[h.id].payload["hash"]}})
		}
		reply(w, out)

	case r.Method == http.MethodPost && path == "/points/query":
		f.query = append(f.query, body)
		if body["using"] != SparseVector {
			f.t.Errorf("query uses vector %v", body["using"])
		}
		q := body["query"].(map[string]any)
		values := floats(q["values"])
		type hit struct {
			id    string
			score float64
		}
		var hits []hit
		// IDF over the documents of the whole collection, as the modifier
		// computes it: the filter narrows what is ranked, not the statistics.
		total := 0
		for _, p := range f.points {
			if p.sparse != nil {
				total++
			}
		}
		for _, id := range f.matching(body["filter"]) {
			score := 0.0
			for i, raw := range q["indices"].([]any) {
				idx := uint32(raw.(float64))
				weight, ok := f.points[id].sparse[idx]
				if !ok {
					continue
				}
				n := 0
				for _, p := range f.points {
					if _, has := p.sparse[idx]; has {
						n++
					}
				}
				idf := math.Log((float64(total)-float64(n)+0.5)/(float64(n)+0.5) + 1)
				score += idf * float64(values[i]) * float64(weight)
			}
			if score > 0 {
				hits = append(hits, hit{id, score})
			}
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].id < hits[j].id
		})
		hits = hits[:min(int(body["limit"].(float64)), len(hits))]
		out := []map[string]any{}
		for _, h := range hits {
			out = append(out, map[string]any{"id": h.id, "score": h.score, "payload": map[string]any{"hash": f.points[h.id].payload["hash"]}})
		}
		reply(w, map[string]any{"points": out})

	default:
		http.Error(w, fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path), http.StatusBadRequest)
	}
}

// matching returns the ids of points whose payload satisfies every must
// clause of filter.
func (f *fakeQdrant) matching(filter any) []string {
	var must []any
	if m, ok := filter.(map[string]any); ok {
		must, _ = m["must"].([]any)
	}
	var ids []string
	for id, p := range f.points {
		ok := true
		for _, raw := range must {
			c := raw.(map[string]any)
			want := c["match"].(map[string]any)["value"]
			if p.payload[c["key"].(string)] != want {
				ok = false
			}
		}
		if ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func reply(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "status": "ok", "time": 0.001})
}

func floats(v any) []float32 {
	raw, _ := v.([]any)
	out := make([]float32, len(raw))
	for i, x := range raw {
		out[i] = float32(x.(float64))
	}
	return out
}

func cos(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func testConfig(url string, dim int) Config {
	return Config{URL: url, APIKey: testKey, Collection: testCollection, Repo: testRepo, Recipe: testRecipe, Dim: dim}
}

func TestOpenVerifiesCollection(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		setup   func(f *fakeQdrant)
		cfg     func(url string) Config
		wantErr string
	}{
		{name: "matching", cfg: func(u string) Config { return testConfig(u, 3) }},
		{name: "size mismatch", cfg: func(u string) Config { return testConfig(u, 4) }, wantErr: "vector size 3"},
		{name: "foreign vector name", setup: func(f *fakeQdrant) { f.foreign = true },
			cfg: func(u string) Config { return testConfig(u, 3) }, wantErr: `nor one named "dense"`},
		{name: "named dense vector", setup: func(f *fakeQdrant) { f.both = true },
			cfg: func(u string) Config { return testConfig(u, 3) }},
		{name: "named size mismatch", setup: func(f *fakeQdrant) { f.both = true },
			cfg: func(u string) Config { return testConfig(u, 4) }, wantErr: "vector size 3"},
		{name: "distance", setup: func(f *fakeQdrant) { f.distance = "Dot" },
			cfg: func(u string) Config { return testConfig(u, 3) }, wantErr: "Dot distance"},
		{name: "missing collection", cfg: func(u string) Config {
			c := testConfig(u, 3)
			c.Collection = "absent"
			return c
		}, wantErr: "does not exist"},
		{name: "bad token", cfg: func(u string) Config {
			c := testConfig(u, 3)
			c.APIKey = "wrong"
			return c
		}, wantErr: "403"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := newFake(t, 3)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, err := Open(ctx, tc.cfg(srv.URL))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Open error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestPutBatchesAndOpenScrollsKnownHashes(t *testing.T) {
	ctx := context.Background()
	f, srv := newFake(t, 2)
	s, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}

	vectors := make(map[string][]float32, batchSize+10)
	for i := range batchSize + 10 {
		vectors[fmt.Sprintf("h%04d", i)] = []float32{1, float32(i)}
	}
	if err := s.Put(ctx, vectors); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(f.upsert) != 2 || f.upsert[0] != batchSize || f.upsert[1] != 10 {
		t.Errorf("upsert batches = %v, want [%d 10]", f.upsert, batchSize)
	}
	if !s.Has("h0000") || s.Has("absent") {
		t.Error("Has does not reflect the put")
	}

	// Another repository's point in the same collection is not ours.
	f.points["foreign"] = fakePoint{vec: []float32{1, 1}, payload: map[string]any{"repo": "repo-b", "recipe": testRecipe, "hash": "theirs"}}

	// A restart learns what the collection holds by scrolling (more than one
	// page), without fetching a vector.
	reopened, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	for h := range vectors {
		if !reopened.Has(h) {
			t.Fatalf("reopened store does not know %s", h)
		}
	}
	if reopened.Has("theirs") {
		t.Error("a point of another repository counted as ours")
	}
	if f.gets != 0 {
		t.Errorf("opening fetched vectors %d time(s)", f.gets)
	}
}

func TestPointIDIsStablePerRecipe(t *testing.T) {
	ctx := context.Background()
	_, srv := newFake(t, 2)
	a, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Open(ctx, testConfig(srv.URL, 2))
	other := testConfig(srv.URL, 2)
	other.Recipe = "noop@r2"
	c, _ := Open(ctx, other)

	if a.pointID("h") != b.pointID("h") {
		t.Error("one hash, one recipe, two point ids")
	}
	if a.pointID("h") == c.pointID("h") || a.pointID("h") == a.pointID("g") {
		t.Error("point ids collide across recipes or hashes")
	}
	if id := a.pointID("h"); len(id) != 36 || id[14] != '5' {
		t.Errorf("point id %q is not a UUID v5", id)
	}
}

func TestSearchFiltersAndOversamples(t *testing.T) {
	ctx := context.Background()
	f, srv := newFake(t, 2)
	s, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, map[string][]float32{
		"exact": {1, 0},
		"close": {0.9, 0.1},
		"far":   {0, 1},
	}); err != nil {
		t.Fatal(err)
	}
	f.points["foreign"] = fakePoint{vec: []float32{1, 0}, payload: map[string]any{"repo": "repo-b", "recipe": testRecipe, "hash": "theirs"}}

	got, err := s.Search(ctx, []float32{1, 0}, 2, func(h string) bool { return h != "exact" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "close" || got[1].ID != "far" {
		t.Errorf("search = %v, want close, far", got)
	}

	req := f.search[len(f.search)-1]
	if limit := int(req["limit"].(float64)); limit < 2*oversample {
		t.Errorf("search limit = %d, want it oversampled", limit)
	}
	must := req["filter"].(map[string]any)["must"].([]any)
	keys := map[string]any{}
	for _, c := range must {
		m := c.(map[string]any)
		keys[m["key"].(string)] = m["match"].(map[string]any)["value"]
	}
	if keys["repo"] != testRepo || keys["recipe"] != testRecipe {
		t.Errorf("search filter = %v, want repo and recipe", keys)
	}
}

func TestGetFetchesVectorsByID(t *testing.T) {
	ctx := context.Background()
	f, srv := newFake(t, 2)
	s, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	vectors := map[string][]float32{}
	var hashes []string
	for i := range batchSize + 1 {
		h := fmt.Sprintf("h%04d", i)
		vectors[h] = []float32{float32(i), 1}
		hashes = append(hashes, h)
	}
	if err := s.Put(ctx, vectors); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, append(hashes, "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != batchSize+1 {
		t.Errorf("fetched %d vectors, want %d", len(got), batchSize+1)
	}
	if v := got["h0007"]; len(v) != 2 || v[0] != 7 {
		t.Errorf("h0007 = %v, want [7 1]", v)
	}
	if f.gets != 2 {
		t.Errorf("fetch requests = %d, want 2 batches", f.gets)
	}
}

func TestSeedUpsertsOnlyMissingHashes(t *testing.T) {
	ctx := context.Background()
	f, srv := newFake(t, 2)
	s, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, map[string][]float32{"present": {1, 1}}); err != nil {
		t.Fatal(err)
	}
	f.upsert = nil

	local := map[string][]float32{"present": {9, 9}, "missing-1": {1, 2}, "missing-2": {3, 4}}
	scan := func(fn func(string, []float32) error) error {
		for h, v := range local {
			if err := fn(h, v); err != nil {
				return err
			}
		}
		return nil
	}

	n, err := s.Seed(ctx, scan)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n != 2 {
		t.Errorf("seeded %d, want the 2 missing", n)
	}
	if len(f.upsert) != 1 || f.upsert[0] != 2 {
		t.Errorf("upserts = %v, want one batch of 2", f.upsert)
	}
	if got, _ := s.Get(ctx, []string{"present"}); got["present"][0] != 1 {
		t.Error("seeding overwrote a vector the collection already had")
	}

	// A second seed finds nothing missing and sends nothing.
	f.upsert = nil
	if n, err := s.Seed(ctx, scan); err != nil || n != 0 || len(f.upsert) != 0 {
		t.Errorf("second seed: n=%d err=%v upserts=%v, want nothing", n, err, f.upsert)
	}
}
