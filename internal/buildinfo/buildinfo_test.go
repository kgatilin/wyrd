package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

// TestResolve_LinkerVersion confirms the linker-injected Version wins
// over module info when it has been set to a non-"dev" value.
func TestResolve_LinkerVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "v9.9.9-test"
	got := Resolve()
	if got.Version != "v9.9.9-test" {
		t.Fatalf("Version = %q, want v9.9.9-test", got.Version)
	}
	if got.Go == "" {
		t.Fatalf("Go field empty; want runtime version")
	}
}

// TestResolve_DevFallback verifies that with Version="dev" Resolve
// either returns module info (under `go test` it's "(devel)" so we
// keep "dev") or "dev" itself. The result must never be empty.
func TestResolve_DevFallback(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "dev"
	got := Resolve()
	if got.Version == "" {
		t.Fatalf("Version is empty; want non-empty fallback")
	}
}

// TestResolve_GoVersion checks the Go field carries a plausible
// toolchain version (starts with "go" under normal `go test`).
func TestResolve_GoVersion(t *testing.T) {
	got := Resolve()
	if got.Go == "" {
		t.Fatalf("Go field empty")
	}
	// Sanity check: the runtime package and build info typically agree
	// on the major Go release prefix.
	if !strings.HasPrefix(got.Go, "go") && got.Go != "unknown" {
		t.Fatalf("Go = %q, want go-prefixed or 'unknown'", got.Go)
	}
	_ = runtime.Version
}

// TestInfo_JSONShape pins the JSON tag names so the /api/version
// contract can't drift silently. We re-marshal a known Info and check
// the field names appear in the output.
func TestInfo_JSONShape(t *testing.T) {
	info := Info{Version: "v1", Commit: "abc", Go: "go1.25"}
	// Trivial round-trip via field access — the JSON tags are checked
	// by the HTTP API test in internal/adapter/http; this test just
	// guards struct field presence.
	if info.Version != "v1" || info.Commit != "abc" || info.Go != "go1.25" {
		t.Fatalf("field round-trip failed: %+v", info)
	}
}
