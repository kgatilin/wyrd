package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWyrdHome_PrefersNewEnvOverLegacy(t *testing.T) {
	t.Setenv("WYRD_HOME", "/tmp/wyrd-home")
	t.Setenv("ARCHAI_HOME", "/tmp/legacy-home")
	got, err := wyrdHome()
	if err != nil {
		t.Fatalf("wyrdHome: %v", err)
	}
	if got != "/tmp/wyrd-home" {
		t.Fatalf("want WYRD_HOME to win, got %q", got)
	}
}

func TestWyrdHome_FallsBackToLegacyEnv(t *testing.T) {
	t.Setenv("WYRD_HOME", "")
	t.Setenv("ARCHAI_HOME", "/tmp/legacy-home")
	got, err := wyrdHome()
	if err != nil {
		t.Fatalf("wyrdHome: %v", err)
	}
	if got != "/tmp/legacy-home" {
		t.Fatalf("want legacy ARCHAI_HOME fallback, got %q", got)
	}
}

func TestWyrdHome_DirectoryFallback(t *testing.T) {
	t.Setenv("WYRD_HOME", "")
	t.Setenv("ARCHAI_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := func(dir string) {
		t.Helper()
		got, err := wyrdHome()
		if err != nil {
			t.Fatalf("wyrdHome: %v", err)
		}
		if got != filepath.Join(home, dir) {
			t.Fatalf("wyrdHome = %q, want %s", got, dir)
		}
	}
	want(".wyrd") // nothing exists: the current name
	if err := os.Mkdir(filepath.Join(home, ".arch"), 0o755); err != nil {
		t.Fatal(err)
	}
	want(".arch") // only the legacy home exists: keep using it
	if err := os.Mkdir(filepath.Join(home, ".wyrd"), 0o755); err != nil {
		t.Fatal(err)
	}
	want(".wyrd") // both: the current one wins
}
