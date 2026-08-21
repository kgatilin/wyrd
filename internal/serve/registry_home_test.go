package serve

import "testing"

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
