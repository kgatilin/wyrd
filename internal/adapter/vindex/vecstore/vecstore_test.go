package vecstore

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")

	store := New(dir)
	c := store.Namespace("ollama:model@r1")
	c.Put("hash-a", []float32{1, 2, 3})
	c.Put("hash-b", []float32{4, 5, 6})

	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A second store over the same directory sees the persisted vectors.
	reloaded := New(dir).Namespace("ollama:model@r1")
	vec, ok := reloaded.Get("hash-a")
	if !ok {
		t.Fatal("hash-a missing after reload")
	}
	if len(vec) != 3 || vec[0] != 1 || vec[2] != 3 {
		t.Fatalf("hash-a = %v, want [1 2 3]", vec)
	}
	if _, ok := reloaded.Get("hash-b"); !ok {
		t.Fatal("hash-b missing after reload")
	}
	if _, ok := reloaded.Get("hash-missing"); ok {
		t.Fatal("unknown hash reported as cached")
	}
	if _, ok := reloaded.Get(""); ok {
		t.Fatal("empty hash reported as cached")
	}
}

func TestStoreSaveCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "nested")

	store := New(dir)
	store.Namespace("ns@r1").Put("hash", []float32{1})
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
}

func TestStoreNamespacesAreIsolated(t *testing.T) {
	dir := t.TempDir()

	store := New(dir)
	store.Namespace("model-a@r1").Put("hash", []float32{1, 1})
	store.Namespace("model-b@r1").Put("hash", []float32{2, 2})
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := New(dir)
	a, ok := reloaded.Namespace("model-a@r1").Get("hash")
	if !ok || a[0] != 1 {
		t.Fatalf("model-a hash = %v (ok=%v), want [1 1]", a, ok)
	}
	b, ok := reloaded.Namespace("model-b@r1").Get("hash")
	if !ok || b[0] != 2 {
		t.Fatalf("model-b hash = %v (ok=%v), want [2 2]", b, ok)
	}
	if _, ok := reloaded.Namespace("model-c@r1").Get("hash"); ok {
		t.Fatal("unused namespace served a vector from a sibling namespace")
	}
}

func TestStoreNamespaceReturnsSameCache(t *testing.T) {
	store := New(t.TempDir())
	store.Namespace("ns@r1").Put("hash", []float32{1})
	if _, ok := store.Namespace("ns@r1").Get("hash"); !ok {
		t.Fatal("second Namespace call returned a different cache")
	}
}

func TestStorePutIsIdempotentAndTracksDirt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ns@r1"+fileExt)

	store := New(dir)
	c := store.Namespace("ns@r1")
	c.Put("hash", []float32{1, 2})
	if err := store.Save(); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first Save: %v", err)
	}

	// Re-putting known hashes (what seeding a loaded index does) must not
	// rewrite the file.
	c.Put("hash", []float32{9, 9})
	c.Put("", []float32{1})
	c.Put("empty-vec", nil)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clean namespace rewrote its file (err=%v)", err)
	}

	// The original vector is untouched by the duplicate Put.
	vec, ok := c.Get("hash")
	if !ok || vec[0] != 1 || vec[1] != 2 {
		t.Fatalf("hash = %v (ok=%v), want [1 2]", vec, ok)
	}

	// A genuinely new hash marks the namespace dirty again.
	c.Put("hash-2", []float32{3, 4})
	if err := store.Save(); err != nil {
		t.Fatalf("third Save: %v", err)
	}
	again, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after third Save: %v", err)
	}
	if again.Size() <= first.Size() {
		t.Fatalf("file did not grow after a new hash: %d then %d", first.Size(), again.Size())
	}
}

func TestStoreSaveMergesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()

	// Two stores over the same directory, each loading before the other
	// writes — the situation two daemons over one repo produce.
	first := New(dir)
	second := New(dir)
	first.Namespace("ns@r1").Put("hash-first", []float32{1})
	second.Namespace("ns@r1").Put("hash-second", []float32{2})

	if err := first.Save(); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := second.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	merged := New(dir).Namespace("ns@r1")
	if _, ok := merged.Get("hash-first"); !ok {
		t.Error("first writer's vector was clobbered")
	}
	if _, ok := merged.Get("hash-second"); !ok {
		t.Error("second writer's vector was clobbered")
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
				if err := os.WriteFile(path, []byte("not a gob stream at all"), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
		},
		{
			name: "wrong header",
			write: func(t *testing.T, path string) {
				writeRecord(t, path, fileFormat{
					Magic:     "something-else",
					Version:   99,
					Namespace: "ns@r1",
					Vectors:   map[string][]float32{"hash": {1}},
				})
			},
		},
		{
			name: "wrong namespace",
			write: func(t *testing.T, path string) {
				writeRecord(t, path, fileFormat{
					Magic:     fileMagic,
					Version:   fileVersion,
					Namespace: "other@r1",
					Vectors:   map[string][]float32{"hash": {1}},
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "ns@r1"+fileExt)
			tc.write(t, path)

			// Construction must not fail, and the cache must read as empty.
			c := New(dir).Namespace("ns@r1")
			if _, ok := c.Get("hash"); ok {
				t.Fatal("unusable file served a vector")
			}

			// It must still be writable afterwards.
			store := New(dir)
			store.Namespace("ns@r1").Put("fresh", []float32{7})
			if err := store.Save(); err != nil {
				t.Fatalf("Save over unusable file: %v", err)
			}
			if _, ok := New(dir).Namespace("ns@r1").Get("fresh"); !ok {
				t.Fatal("rewrite over an unusable file did not persist")
			}
		})
	}
}

func TestFileNameSanitizesNamespace(t *testing.T) {
	tests := []struct {
		ns   string
		want string
	}{
		{"ollama:qwen3-embedding:0.6b@r1", "ollama-qwen3-embedding-0.6b@r1"},
		{"openai/text-embed@r2", "openai-text-embed@r2"},
		{"", "unnamed"},
		{"///", "unnamed"},
	}
	for _, tc := range tests {
		if got := fileName(tc.ns); got != tc.want {
			t.Errorf("fileName(%q) = %q, want %q", tc.ns, got, tc.want)
		}
	}
}

func writeRecord(t *testing.T, path string, rec fileFormat) {
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
