package treestamp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, root string) Tree {
	t.Helper()
	tree, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return tree
}

func TestFingerprintFollowsTheModelSourceOnly(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	write(t, filepath.Join(root, "store", "store.go"), "package store\n")
	before := scan(t, root).Fingerprint()

	write(t, filepath.Join(root, "store", "store_test.go"), "package store\n")
	write(t, filepath.Join(root, "testdata", "x.go"), "package x\n")
	write(t, filepath.Join(root, "README.md"), "app\n")
	if got := scan(t, root).Fingerprint(); got != before {
		t.Fatalf("fingerprint moved on files the model is not read from")
	}

	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(root, "store", "store.go"), later, later); err != nil {
		t.Fatal(err)
	}
	if got := scan(t, root).Fingerprint(); got == before {
		t.Fatalf("fingerprint stayed put on an edited source file")
	}
}

// TestFingerprintNamesTheScannedRoot: a subdirectory's scan is a different
// tree, even when it holds every source file.
func TestFingerprintNamesTheScannedRoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "app", "app.go"), "package app\n")
	if scan(t, root).Fingerprint() == scan(t, filepath.Join(root, "app")).Fingerprint() {
		t.Fatalf("a subdirectory has the fingerprint of its parent")
	}
}

func TestChangedNamesDirectoriesAndTheModule(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	write(t, filepath.Join(root, "a", "a.go"), "package a\n")
	write(t, filepath.Join(root, "b", "b.go"), "package b\n")
	prev := scan(t, root)

	write(t, filepath.Join(root, "a", "a.go"), "package a\n\nvar X int\n")
	if err := os.Remove(filepath.Join(root, "b", "b.go")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "c.go"), "package app\n")
	dirs, module := scan(t, root).Changed(prev)
	if len(dirs) != 3 || dirs[0] != "." || dirs[1] != "a" || dirs[2] != "b" || module {
		t.Fatalf("Changed = %v, module %v; want [. a b], false", dirs, module)
	}

	prev = scan(t, root)
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.21\n")
	if _, module := scan(t, root).Changed(prev); !module {
		t.Fatalf("a go.mod edit is not a module change")
	}
}
