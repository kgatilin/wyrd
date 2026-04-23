package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// writeFile is a tiny helper for building fixture trees inside tests.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// newFixture creates a minimal Go module under t.TempDir() containing
// two packages (internal/foo, internal/bar) plus an archai.yaml. The
// returned path is the module root.
func newFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.21\n")

	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), `package foo

// Thing is a trivial exported struct so the extractor has something to find.
type Thing struct {
	Name string
}

// New returns a Thing.
func New() *Thing { return &Thing{} }
`)

	writeFile(t, filepath.Join(root, "internal", "bar", "bar.go"), `package bar

// Bar is a trivial exported struct.
type Bar struct{}
`)

	writeFile(t, filepath.Join(root, "archai.yaml"), `module: example.com/fixture
layers:
  domain:
    - "internal/foo/..."
    - "internal/bar/..."
layer_rules:
  domain: []
`)

	return root
}

func TestStateLoadExtractsPackages(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)

	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	snap := st.Snapshot()
	if len(snap.Packages) == 0 {
		t.Fatalf("expected at least one package, got 0")
	}
	// Overlay should have loaded.
	if snap.Overlay == nil {
		t.Fatalf("expected overlay to be loaded")
	}
	if snap.Overlay.Module != "example.com/fixture" {
		t.Fatalf("overlay module = %q, want %q", snap.Overlay.Module, "example.com/fixture")
	}

	// Spot-check that at least one of our fixture packages is present.
	found := false
	for _, p := range snap.Packages {
		if p.Path == "internal/foo" || p.Path == "internal/bar" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected internal/foo or internal/bar in model, got: %v", packagePaths(snap.Packages))
	}
}

func TestStateReloadPackageUpdatesModel(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)

	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Capture the initial struct count for internal/foo.
	initial := countStructs(t, st, "internal/foo")

	// Edit foo.go to add a second struct and reload just that package.
	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), `package foo

type Thing struct{ Name string }
type Second struct{ X int }

func New() *Thing { return &Thing{} }
`)

	if err := st.ReloadPackage(ctx, "internal/foo"); err != nil {
		t.Fatalf("ReloadPackage: %v", err)
	}
	after := countStructs(t, st, "internal/foo")
	if after <= initial {
		t.Fatalf("expected struct count to grow after reload (initial=%d after=%d)", initial, after)
	}
}

func TestStateSwitchTargetUpdatesID(t *testing.T) {
	st := NewState(t.TempDir())
	if got := st.Snapshot().CurrentTarget; got != "" {
		t.Fatalf("initial CurrentTarget = %q, want empty", got)
	}
	if err := st.SwitchTarget("v1"); err != nil {
		t.Fatalf("SwitchTarget: %v", err)
	}
	if got := st.Snapshot().CurrentTarget; got != "v1" {
		t.Fatalf("after switch, CurrentTarget = %q, want %q", got, "v1")
	}
	if err := st.SwitchTarget(""); err != nil {
		t.Fatalf("SwitchTarget clear: %v", err)
	}
	if got := st.Snapshot().CurrentTarget; got != "" {
		t.Fatalf("after clear, CurrentTarget = %q, want empty", got)
	}
}

func TestStateFindOwningPackage(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		path string
		want string
	}{
		{filepath.Join(root, "internal", "foo", "foo.go"), "internal/foo"},
		{filepath.Join(root, "internal", "foo", "nonexistent.go"), "internal/foo"},
		{filepath.Join(root, "internal", "bar", "bar.go"), "internal/bar"},
	}
	for _, tc := range cases {
		got := st.FindOwningPackage(tc.path)
		if got != tc.want {
			t.Errorf("FindOwningPackage(%s) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// countStructs returns the number of structs extracted for the given
// module-relative package path in the state.
func countStructs(t *testing.T, st *State, pkgPath string) int {
	t.Helper()
	for _, p := range st.Snapshot().Packages {
		if p.Path == pkgPath {
			return len(p.Structs)
		}
	}
	t.Fatalf("package %q not in snapshot", pkgPath)
	return 0
}

// packagePaths flattens snapshot.Packages to its Path slice (for error output).
func packagePaths(ps []domain.PackageModel) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Path)
	}
	return out
}
