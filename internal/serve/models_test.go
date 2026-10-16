package serve

import (
	"os"
	"testing"
)

// The store's directory is named after the reader's version and the model's
// shape, with nothing of the binary in it: a second open lands on the
// directory the first made instead of replacing it.
func TestOpenModelStore_KeyIsStableAcrossOpens(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())
	root := t.TempDir()

	if _, err := OpenModelStore(root); err != nil {
		t.Fatalf("first open: %v", err)
	}
	dir, err := ModelStoreDir(root)
	if err != nil {
		t.Fatalf("store dir: %v", err)
	}
	first := stampDirs(t, dir)
	if len(first) != 1 {
		t.Fatalf("stamp directories after first open = %v, want one", first)
	}

	if _, err := OpenModelStore(root); err != nil {
		t.Fatalf("second open: %v", err)
	}
	if second := stampDirs(t, dir); len(second) != 1 || second[0] != first[0] {
		t.Fatalf("stamp directories after second open = %v, want %v", second, first)
	}
}

func stampDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
