package overlay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPathPrefersCanonicalName(t *testing.T) {
	root := t.TempDir()
	if got := DiscoverPath(root); got != "" {
		t.Fatalf("empty dir: want no overlay, got %q", got)
	}

	legacy := filepath.Join(root, LegacyFileName)
	if err := os.WriteFile(legacy, []byte("module: example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DiscoverPath(root); got != legacy {
		t.Fatalf("legacy only: want %q, got %q", legacy, got)
	}

	canonical := filepath.Join(root, FileName)
	if err := os.WriteFile(canonical, []byte("module: example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DiscoverPath(root); got != canonical {
		t.Fatalf("both present: want %q, got %q", canonical, got)
	}
}
