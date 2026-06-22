package serve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/domain"
)

func TestMain(m *testing.M) {
	// Disable retrieval in tests to avoid background goroutines
	// that interfere with temp directory cleanup.
	os.Setenv("ARCHAI_RETRIEVAL_DISABLE", "1")
	os.Exit(m.Run())
}

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

func TestStateLoadUsesModelCacheWhenSourcesUnchanged(t *testing.T) {
	root := newFixture(t)
	initial := &recordingReader{
		responses: map[string][]domain.PackageModel{
			"./...": {
				{Path: "internal/foo", Name: "foo"},
				{Path: "internal/bar", Name: "bar"},
			},
		},
	}
	st := NewState(root, WithReader(initial))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("initial Load: %v", err)
	}
	if got := len(initial.calls); got != 1 {
		t.Fatalf("initial reader calls = %d, want 1", got)
	}
	if _, err := os.Stat(modelCachePath(root)); err != nil {
		t.Fatalf("model cache not written: %v", err)
	}
	before, err := os.Stat(modelCachePath(root))
	if err != nil {
		t.Fatalf("stat cache before cached load: %v", err)
	}

	cached := &recordingReader{fail: true}
	st = NewState(root, WithReader(cached))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("cached Load: %v", err)
	}
	if got := len(cached.calls); got != 0 {
		t.Fatalf("cached reader calls = %d, want 0: %v", got, cached.calls)
	}
	if got := len(st.Snapshot().Packages); got != 2 {
		t.Fatalf("cached packages = %d, want 2", got)
	}
	after, err := os.Stat(modelCachePath(root))
	if err != nil {
		t.Fatalf("stat cache after cached load: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("clean cache hit rewrote cache: before=%s after=%s", before.ModTime(), after.ModTime())
	}
}

func TestStateLoadModelCacheReloadsOnlyChangedPackages(t *testing.T) {
	root := newFixture(t)
	initial := &recordingReader{
		responses: map[string][]domain.PackageModel{
			"./...": {
				{Path: "internal/foo", Name: "foo", Structs: []domain.StructDef{{Name: "Thing"}}},
				{Path: "internal/bar", Name: "bar", Structs: []domain.StructDef{{Name: "Bar"}}},
			},
		},
	}
	st := NewState(root, WithReader(initial))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	fooFile := filepath.Join(root, "internal", "foo", "foo.go")
	writeFile(t, fooFile, `package foo

type Thing struct{ Name string }
type Second struct{ X int }
`)
	touchFile(t, fooFile)

	incremental := &recordingReader{
		responses: map[string][]domain.PackageModel{
			"./internal/foo": {
				{
					Path:    "internal/foo",
					Name:    "foo",
					Structs: []domain.StructDef{{Name: "Thing"}, {Name: "Second"}},
				},
			},
		},
	}
	st = NewState(root, WithReader(incremental))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("incremental Load: %v", err)
	}
	if got, want := flattenCalls(incremental.calls), "./internal/foo"; got != want {
		t.Fatalf("incremental reader calls = %q, want %q", got, want)
	}
	if got := countStructs(t, st, "internal/foo"); got != 2 {
		t.Fatalf("internal/foo structs = %d, want 2", got)
	}
	if got := countStructs(t, st, "internal/bar"); got != 1 {
		t.Fatalf("internal/bar structs = %d, want 1 from cache", got)
	}
}

func TestStateLoadIgnoresModelCacheVersionMismatch(t *testing.T) {
	root := newFixture(t)
	if err := os.MkdirAll(filepath.Dir(modelCachePath(root)), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(modelCachePath(root), []byte(`{"schema":"old","version":0}`), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	reader := &recordingReader{
		responses: map[string][]domain.PackageModel{
			"./...": {{Path: "internal/foo", Name: "foo"}},
		},
	}
	st := NewState(root, WithReader(reader))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := flattenCalls(reader.calls), "./..."; got != want {
		t.Fatalf("reader calls = %q, want %q", got, want)
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

type recordingReader struct {
	responses map[string][]domain.PackageModel
	calls     [][]string
	fail      bool
}

func (r *recordingReader) Read(_ context.Context, paths []string) ([]domain.PackageModel, error) {
	call := append([]string(nil), paths...)
	r.calls = append(r.calls, call)
	if r.fail {
		return nil, fmt.Errorf("reader should not have been called")
	}
	key := strings.Join(paths, ",")
	if models, ok := r.responses[key]; ok {
		return append([]domain.PackageModel(nil), models...), nil
	}
	return nil, fmt.Errorf("unexpected read paths %v", paths)
}

func flattenCalls(calls [][]string) string {
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		parts = append(parts, strings.Join(call, ","))
	}
	return strings.Join(parts, ";")
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	ts := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
