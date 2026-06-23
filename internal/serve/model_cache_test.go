package serve

import (
	"reflect"
	"testing"
)

func TestComputeSchemaFingerprint_StableAndShapeSensitive(t *testing.T) {
	type inner struct{ A string }
	type v1 struct {
		X int
		Y inner
	}
	type v2 struct {
		X int
		Y inner
		Z string // added field — must change the fingerprint
	}

	f1 := computeSchemaFingerprint(reflect.TypeOf(v1{}))
	f1again := computeSchemaFingerprint(reflect.TypeOf(v1{}))
	f2 := computeSchemaFingerprint(reflect.TypeOf(v2{}))

	if f1 == "" {
		t.Fatal("fingerprint is empty")
	}
	if f1 != f1again {
		t.Errorf("fingerprint not deterministic: %q vs %q", f1, f1again)
	}
	if f1 == f2 {
		t.Errorf("fingerprint unchanged after adding a field (%q) — stale caches would not be rejected", f1)
	}
}

func TestComputeSchemaFingerprint_RecursiveTypeTerminates(t *testing.T) {
	// A self-referential type must not send the walker into infinite
	// recursion (domain.TypeRef and friends are effectively recursive).
	type node struct {
		Name     string
		Children []node
	}
	if got := computeSchemaFingerprint(reflect.TypeOf(node{})); got == "" {
		t.Fatal("recursive type produced empty fingerprint")
	}
}

func TestLoadModelCache_RejectsStaleFingerprint(t *testing.T) {
	root := t.TempDir()

	valid := modelCacheFile{
		Schema:            modelCacheSchema,
		Version:           modelCacheVersion,
		SchemaFingerprint: modelCacheSchemaFingerprint,
		PackageFiles:      map[string][]fileStamp{},
	}
	if err := writeModelCache(root, valid); err != nil {
		t.Fatalf("writeModelCache(valid): %v", err)
	}
	if _, err := loadModelCache(root); err != nil {
		t.Fatalf("valid cache rejected: %v", err)
	}

	// A cache written by a binary with a different model shape carries a
	// different (or empty) fingerprint and must be rejected so the daemon
	// re-parses instead of loading models with zero-valued new fields.
	stale := valid
	stale.SchemaFingerprint = "0000000000000000"
	if err := writeModelCache(root, stale); err != nil {
		t.Fatalf("writeModelCache(stale): %v", err)
	}
	if _, err := loadModelCache(root); err == nil {
		t.Fatal("stale-fingerprint cache was accepted; want rejection")
	}
}
