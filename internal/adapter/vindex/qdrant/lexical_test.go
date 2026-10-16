package qdrant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/lindex/bm25"
)

const lexicalRecipe = "lexical@r1"

func lexicalConfig(url string) Config {
	return Config{URL: url, APIKey: testKey, Collection: testCollection, Repo: testRepo, Recipe: lexicalRecipe}
}

// symbols are small documents of the shape the corpus indexes: a package, a
// signature and a doc sentence.
var symbols = map[string]string{
	"verify": "package auth func VerifyToken(raw string) (*Claims, error) VerifyToken checks a JWT signature and expiry",
	"legend": "package render func DrawLegend(w io.Writer, entries []Entry) error DrawLegend writes the diagram legend",
	"parse":  "package auth func parseToken(raw string) token parseToken splits the token header",
	"store":  "package store type TokenStore struct tokens map[string]Token",
	"snake":  "package cfg func load_http_server_config(path string) error",
}

func openLexical(t *testing.T, url string) *Lexical {
	t.Helper()
	l, err := OpenLexical(context.Background(), lexicalConfig(url))
	if err != nil {
		t.Fatalf("OpenLexical: %v", err)
	}
	return l
}

func ids(t *testing.T, l *Lexical, query string, keep func(string) bool) []string {
	t.Helper()
	found, err := l.Search(context.Background(), query, 10, keep)
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	out := make([]string, len(found))
	for i, s := range found {
		out[i] = s.ID
	}
	return out
}

func TestOpenLexicalVerifiesTheSparseVector(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *fakeQdrant)
		wantErr string
	}{
		{name: "both vectors", setup: func(f *fakeQdrant) { f.both = true }},
		{name: "legacy collection", setup: func(*fakeQdrant) {}, wantErr: `no sparse vector named "bm25"`},
		{name: "no idf", setup: func(f *fakeQdrant) { f.both, f.modifier = true, "none" }, wantErr: "lacks the idf modifier"},
		{name: "missing", setup: func(f *fakeQdrant) { f.absent = true }, wantErr: "does not exist"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := newFake(t, 3)
			tc.setup(f)
			_, err := OpenLexical(context.Background(), lexicalConfig(srv.URL))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("OpenLexical: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// The remote index ranks what the local one ranks: the same tokenizer feeds
// both, so an identifier is found by its parts and by its whole name, in
// camelCase and in snake_case.
func TestLexicalSearchFindsIdentifiersLikeTheLocalIndex(t *testing.T) {
	_, srv := newFakeBoth(t, 3)
	remote := openLexical(t, srv.URL)
	local := bm25.New()
	for id, text := range symbols {
		remote.Upsert(id, text)
		local.Upsert(id, text)
	}

	for _, query := range []string{
		"VerifyToken",          // the whole identifier
		"verify",               // one camelCase part
		"token",                // a part shared by several symbols
		"load_http_server",     // snake_case, partial
		"HTTP",                 // an acronym inside snake_case
		"DrawLegend legend",    // a repeated term
		"auth.VerifyToken",     // a qualified name
		"unrelated words only", // no document has any of the terms
	} {
		want, _ := local.Search(context.Background(), query, 10, nil)
		got := ids(t, remote, query, nil)
		if len(got) != len(want) {
			t.Errorf("%q: remote found %v, local found %v", query, got, want)
			continue
		}
		// The leader must agree. Further down, the two differ by design in
		// length normalization (a fixed average against a live one), which
		// can swap near-ties.
		if len(want) > 0 && got[0] != want[0].ID {
			t.Errorf("%q: remote leads with %q, local with %q", query, got[0], want[0].ID)
		}
	}
}

func TestLexicalSearchSeesBufferedWrites(t *testing.T) {
	f, srv := newFakeBoth(t, 3)
	l := openLexical(t, srv.URL)
	l.Upsert("verify", symbols["verify"])
	if len(f.upsert) != 0 {
		t.Fatalf("one document was sent at once: %v", f.upsert)
	}
	if !l.Has("verify") || l.Len() != 1 {
		t.Errorf("buffered document: Has=%v Len=%d", l.Has("verify"), l.Len())
	}
	if got := ids(t, l, "VerifyToken", nil); len(got) != 1 || got[0] != "verify" {
		t.Errorf("search before an explicit flush found %v", got)
	}
	if len(f.upsert) != 1 || f.upsert[0] != 1 {
		t.Errorf("upserts = %v, want the one buffered document", f.upsert)
	}
}

func TestLexicalUpsertSendsFullBatches(t *testing.T) {
	f, srv := newFakeBoth(t, 3)
	l := openLexical(t, srv.URL)
	for i := 0; i < batchSize+10; i++ {
		l.Upsert(fmt.Sprintf("doc-%d", i), "func Handler")
	}
	if len(f.upsert) != 1 || f.upsert[0] != batchSize {
		t.Fatalf("upserts while filling = %v, want one batch of %d", f.upsert, batchSize)
	}
	if err := l.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.upsert) != 2 || f.upsert[1] != 10 {
		t.Errorf("upserts after flush = %v", f.upsert)
	}
}

func TestLexicalFiltersOnRepositoryAndRecipe(t *testing.T) {
	f, srv := newFakeBoth(t, 3)
	l := openLexical(t, srv.URL)
	l.Upsert("verify", symbols["verify"])
	if err := l.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The same text stored by another repository, and by the dense channel
	// under its own recipe: neither is this index's document.
	var mine fakePoint
	for _, p := range f.points {
		mine = p
	}
	f.points["other-repo"] = fakePoint{sparse: mine.sparse, payload: map[string]any{"repo": "repo-b", "recipe": mine.payload["recipe"], "hash": "theirs"}}
	f.points["dense"] = fakePoint{vec: []float32{1, 0, 0}, payload: map[string]any{"repo": testRepo, "recipe": testRecipe, "hash": "verify"}}

	if got := ids(t, l, "VerifyToken", nil); len(got) != 1 || got[0] != "verify" {
		t.Errorf("found %v, want only this repository's document", got)
	}
	reopened := openLexical(t, srv.URL)
	if reopened.Len() != 1 || !reopened.Has("verify") || reopened.Has("theirs") {
		t.Errorf("reopened index holds %d document(s)", reopened.Len())
	}
}

func TestLexicalSearchAppliesKeepToAnOversampledAnswer(t *testing.T) {
	f, srv := newFakeBoth(t, 3)
	l := openLexical(t, srv.URL)
	for id, text := range symbols {
		l.Upsert(id, text)
	}
	found, err := l.Search(context.Background(), "token", 1, func(id string) bool { return id == "store" })
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != "store" {
		t.Errorf("found %v, want the one document keep accepts", found)
	}
	if limit := f.query[len(f.query)-1]["limit"].(float64); int(limit) != minFetch {
		t.Errorf("limit = %v, want the oversampling floor %d", limit, minFetch)
	}
}

func TestLexicalRemoveAndRetainDeleteStoredDocuments(t *testing.T) {
	ctx := context.Background()
	f, srv := newFakeBoth(t, 3)
	l := openLexical(t, srv.URL)
	for id, text := range symbols {
		l.Upsert(id, text)
	}
	if err := l.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	l.Remove("legend")
	if l.Has("legend") {
		t.Error("Has reports a removed document")
	}
	if got := ids(t, l, "DrawLegend", nil); len(got) != 0 {
		t.Errorf("a removed document was found: %v", got)
	}
	// A document removed before it was ever sent costs no request.
	l.Upsert("draft", "func Draft")
	l.Remove("draft")
	deletes := len(f.deleted)
	if err := l.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != deletes {
		t.Errorf("an unsent document was deleted remotely: %v", f.deleted)
	}

	if err := l.Retain(ctx, func(id string) bool { return id == "verify" }); err != nil {
		t.Fatal(err)
	}
	if l.Len() != 1 || !l.Has("verify") {
		t.Errorf("after Retain: Len=%d", l.Len())
	}
	if reopened := openLexical(t, srv.URL); reopened.Len() != 1 {
		t.Errorf("the collection holds %d document(s) after Retain", reopened.Len())
	}
}

// A write that fails stays buffered and is reported by the next call that
// can return an error, instead of the document silently missing.
func TestLexicalReportsAFailedWriteAndRetries(t *testing.T) {
	ctx := context.Background()
	f, srv := newFakeBoth(t, 3)
	l := openLexical(t, srv.URL)
	l.Upsert("verify", symbols["verify"])

	f.mu.Lock()
	f.failWrites = true
	f.mu.Unlock()
	if _, err := l.Search(ctx, "VerifyToken", 5, nil); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Search err = %v, want the write failure", err)
	}
	if err := l.Flush(ctx); err == nil {
		t.Fatal("Flush succeeded while writes fail")
	}

	f.mu.Lock()
	f.failWrites = false
	f.mu.Unlock()
	if got := ids(t, l, "VerifyToken", nil); len(got) != 1 {
		t.Errorf("after recovery found %v", got)
	}
}

func TestBootstrapCreatesTheCollectionOnce(t *testing.T) {
	ctx := context.Background()
	f, srv := newFake(t, 0)
	f.absent = true
	cfg := testConfig(srv.URL, 768)

	created, err := Bootstrap(ctx, cfg)
	if err != nil || !created {
		t.Fatalf("Bootstrap: created=%v err=%v", created, err)
	}
	if f.size != 768 || f.distance != "Cosine" || f.modifier != "idf" {
		t.Errorf("created with size=%d distance=%s modifier=%s", f.size, f.distance, f.modifier)
	}
	if got := strings.Join(f.indexed, ","); got != "repo:keyword,recipe:keyword" {
		t.Errorf("payload indexes = %q", got)
	}
	if err := Check(ctx, cfg); err != nil {
		t.Errorf("Check after Bootstrap: %v", err)
	}

	created, err = Bootstrap(ctx, cfg)
	if err != nil || created {
		t.Errorf("second Bootstrap: created=%v err=%v", created, err)
	}
}

func TestBootstrapRefusesACollectionItCannotUse(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		setup   func(f *fakeQdrant)
		dim     int
		wantErr string
	}{
		{name: "legacy", setup: func(*fakeQdrant) {}, dim: 3, wantErr: "wyrd qdrant migrate"},
		{name: "another dimension", setup: func(f *fakeQdrant) { f.both = true }, dim: 4, wantErr: "vector size 3"},
		{name: "no idf", setup: func(f *fakeQdrant) { f.both, f.modifier = true, "" }, dim: 3, wantErr: "idf modifier"},
		{name: "no dimension", setup: func(f *fakeQdrant) { f.absent = true }, dim: 0, wantErr: "dimension is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := newFake(t, 3)
			tc.setup(f)
			_, err := Bootstrap(ctx, testConfig(srv.URL, tc.dim))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestInspectReportsAMissingCollection(t *testing.T) {
	f, srv := newFake(t, 3)
	f.absent = true
	if _, err := Inspect(context.Background(), testConfig(srv.URL, 3)); !errors.Is(err, ErrNoCollection) {
		t.Fatalf("err = %v, want ErrNoCollection", err)
	}
}

func TestMigrateCopiesALegacyCollectionUnderTheNamedVector(t *testing.T) {
	ctx := context.Background()
	_, legacySrv := newFake(t, 2)
	_, namedSrv := newFakeBoth(t, 2)

	legacy, err := Open(ctx, testConfig(legacySrv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	vectors := map[string][]float32{}
	for i := 0; i < batchSize+5; i++ {
		vectors[fmt.Sprintf("h%d", i)] = []float32{float32(i), 1}
	}
	if err := legacy.Put(ctx, vectors); err != nil {
		t.Fatal(err)
	}

	n, err := Migrate(ctx, testConfig(legacySrv.URL, 0), testConfig(namedSrv.URL, 0))
	if err != nil || n != len(vectors) {
		t.Fatalf("Migrate: n=%d err=%v, want %d", n, err, len(vectors))
	}
	// The copy is found by the same recipe and hash, so nothing is embedded
	// again after the move.
	store, err := Open(ctx, testConfig(namedSrv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	for h := range vectors {
		if !store.Has(h) {
			t.Fatalf("hash %q did not arrive", h)
		}
	}
	got, err := store.Get(ctx, []string{"h7"})
	if err != nil || len(got["h7"]) != 2 {
		t.Errorf("Get after Migrate: %v, %v", got, err)
	}

	if _, err := Migrate(ctx, testConfig(namedSrv.URL, 0), testConfig(namedSrv.URL, 0)); err == nil || !strings.Contains(err.Error(), "not a legacy collection") {
		t.Errorf("migrating from a named collection: %v", err)
	}
}

func TestStoreOnANamedCollection(t *testing.T) {
	ctx := context.Background()
	_, srv := newFakeBoth(t, 2)
	s, err := Open(ctx, testConfig(srv.URL, 2))
	if err != nil {
		t.Fatal(err)
	}
	if s.Schema().Legacy {
		t.Fatal("a named collection was read as legacy")
	}
	if err := s.Put(ctx, map[string][]float32{"a": {1, 0}, "b": {0, 1}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, []string{"a", "b"})
	if err != nil || len(got) != 2 || got["a"][0] != 1 {
		t.Fatalf("Get: %v, %v", got, err)
	}
	found, err := s.Search(ctx, []float32{0.9, 0.1}, 1, nil)
	if err != nil || len(found) != 1 || found[0].ID != "a" {
		t.Fatalf("Search: %v, %v", found, err)
	}
}
