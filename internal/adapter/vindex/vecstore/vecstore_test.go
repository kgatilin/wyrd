package vecstore

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
)

const testRecipe = "ns@r1"

func put(t *testing.T, s *Store, vectors map[string][]float32) {
	t.Helper()
	if err := s.Put(context.Background(), vectors); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func get(t *testing.T, s *Store, hash string) ([]float32, bool) {
	t.Helper()
	got, err := s.Get(context.Background(), []string{hash})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	vec, ok := got[hash]
	return vec, ok
}

func TestStoreRoundTrip(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "missing", "dir"), testRecipe)
	s := New(path, testRecipe)
	put(t, s, map[string][]float32{"h1": {1, 2, 3}, "h2": {4, 5, 6}})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reopened := New(path, testRecipe)
	if !reopened.Has("h1") || !reopened.Has("h2") || reopened.Len() != 2 {
		t.Fatalf("reopened store: h1=%v h2=%v len=%d", reopened.Has("h1"), reopened.Has("h2"), reopened.Len())
	}
	if vec, _ := get(t, reopened, "h2"); len(vec) != 3 || vec[2] != 6 {
		t.Errorf("h2 = %v, want [4 5 6]", vec)
	}
}

func TestStoreSaveAppendsOnlyNewVectors(t *testing.T) {
	path := Path(t.TempDir(), testRecipe)
	s := New(path, testRecipe)
	put(t, s, map[string][]float32{"h1": {1, 2}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// A save with nothing new writes nothing.
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); after.Size() != before.Size() {
		t.Fatalf("an idle save changed the file: %d → %d bytes", before.Size(), after.Size())
	}

	// A known hash is not re-recorded; a new one is appended.
	put(t, s, map[string][]float32{"h1": {9, 9}, "h2": {3, 4}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	record := int64(2 + len("h2") + 4 + 2*4)
	if got := after.Size() - before.Size(); got != record {
		t.Errorf("second save wrote %d bytes, want one appended record of %d", got, record)
	}
	if vec, _ := get(t, New(path, testRecipe), "h1"); vec[0] != 1 {
		t.Errorf("h1 = %v, want the first vector: entries are immutable", vec)
	}
}

func TestStoreSearchScoresOnlyKeptHashes(t *testing.T) {
	s := New("", testRecipe)
	put(t, s, map[string][]float32{
		"exact": {1, 0},
		"close": {0.9, 0.1},
		"far":   {0, 1},
	})
	ctx := context.Background()

	got, err := s.Search(ctx, []float32{1, 0}, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "exact" || got[1].ID != "close" {
		t.Errorf("unfiltered = %v, want exact, close", got)
	}

	got, _ = s.Search(ctx, []float32{1, 0}, 2, func(h string) bool { return h != "exact" })
	if len(got) != 2 || got[0].ID != "close" || got[1].ID != "far" {
		t.Errorf("filtered = %v, want close, far: keep applies before the cut", got)
	}
}

func TestStoreInMemoryWritesNothing(t *testing.T) {
	s := New("", testRecipe)
	put(t, s, map[string][]float32{"h": {1}})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !s.Has("h") {
		t.Fatal("in-memory store lost a vector")
	}
}

func TestStoreReadsLegacyFormatAndRewritesIt(t *testing.T) {
	path := Path(t.TempDir(), testRecipe)
	writeLegacy(t, path, legacyFile{
		Magic: legacyMagic, Version: legacyVersion, Namespace: testRecipe,
		Vectors: map[string][]float32{"old": {1, 2}},
	})

	s := New(path, testRecipe)
	if !s.Has("old") {
		t.Fatal("v1 vector not read")
	}
	put(t, s, map[string][]float32{"new": {3, 4}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	appendable, err := Scan(path, testRecipe, func(string, []float32) error { return nil })
	if err != nil || !appendable {
		t.Fatalf("after save: appendable=%v err=%v, want the v2 format", appendable, err)
	}
	reopened := New(path, testRecipe)
	if !reopened.Has("old") || !reopened.Has("new") {
		t.Error("rewrite lost a vector")
	}
}

func TestStoreDropsTruncatedTailAndRewrites(t *testing.T) {
	path := Path(t.TempDir(), testRecipe)
	s := New(path, testRecipe)
	put(t, s, map[string][]float32{"a": {1, 2}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	put(t, s, map[string][]float32{"b": {3, 4}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data[:len(data)-2], 0o644); err != nil {
		t.Fatal(err)
	}

	cut := New(path, testRecipe)
	if !cut.Has("a") || cut.Has("b") {
		t.Fatalf("truncated file: a=%v b=%v, want only the whole record", cut.Has("a"), cut.Has("b"))
	}
	put(t, cut, map[string][]float32{"c": {5, 6}})
	if err := cut.Save(); err != nil {
		t.Fatal(err)
	}
	if appendable, err := Scan(path, testRecipe, func(string, []float32) error { return nil }); !appendable || err != nil {
		t.Fatalf("after save: appendable=%v err=%v, want a clean file", appendable, err)
	}
	if re := New(path, testRecipe); !re.Has("a") || !re.Has("c") {
		t.Error("rewrite after a truncated tail lost a vector")
	}
}

func TestStoreIgnoresUnusableFiles(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, path string)
	}{
		{
			name: "corrupt",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("not a vector file at all"), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
		},
		{
			name: "wrong legacy header",
			write: func(t *testing.T, path string) {
				writeLegacy(t, path, legacyFile{Magic: "something-else", Version: 99, Namespace: testRecipe,
					Vectors: map[string][]float32{"hash": {1}}})
			},
		},
		{
			name: "other recipe",
			write: func(t *testing.T, path string) {
				other := New(path, "other@r1")
				put(t, other, map[string][]float32{"hash": {1}})
				if err := other.Save(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := Path(t.TempDir(), testRecipe)
			tc.write(t, path)

			if New(path, testRecipe).Has("hash") {
				t.Fatal("unusable file served a vector")
			}
			s := New(path, testRecipe)
			put(t, s, map[string][]float32{"fresh": {7}})
			if err := s.Save(); err != nil {
				t.Fatalf("Save over unusable file: %v", err)
			}
			if !New(path, testRecipe).Has("fresh") {
				t.Fatal("rewrite over an unusable file did not persist")
			}
		})
	}
}

func TestFileNameSanitizesRecipe(t *testing.T) {
	tests := []struct {
		recipe string
		want   string
	}{
		{"ollama:qwen3-embedding:0.6b@r1", "ollama-qwen3-embedding-0.6b@r1"},
		{"openai/text-embed@r2", "openai-text-embed@r2"},
		{"", "unnamed"},
		{"///", "unnamed"},
	}
	for _, tc := range tests {
		if got := fileName(tc.recipe); got != tc.want {
			t.Errorf("fileName(%q) = %q, want %q", tc.recipe, got, tc.want)
		}
	}
}

func writeLegacy(t *testing.T, path string, rec legacyFile) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := gob.NewEncoder(f).Encode(rec); err != nil {
		t.Fatalf("encode: %v", err)
	}
}
