package serve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/domain"
)

func TestMain(m *testing.M) {
	// Disable retrieval in tests to avoid background goroutines
	// that interfere with temp directory cleanup.
	os.Setenv("WYRD_RETRIEVAL_DISABLE", "1")
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
// two packages (internal/foo, internal/bar) plus an wyrd.yaml. The
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

	writeFile(t, filepath.Join(root, "wyrd.yaml"), `module: example.com/fixture
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

	changed, err := st.ReloadPackage(ctx, "internal/foo")
	if err != nil {
		t.Fatalf("ReloadPackage: %v", err)
	}
	if !changed {
		t.Fatal("ReloadPackage reported no change after an edit that adds a struct")
	}
	after := countStructs(t, st, "internal/foo")
	if after <= initial {
		t.Fatalf("expected struct count to grow after reload (initial=%d after=%d)", initial, after)
	}

	// A second reload with nothing edited in between must report no change:
	// this is what keeps a bare file-attribute event from broadcasting a
	// model-changed to every review client.
	changed, err = st.ReloadPackage(ctx, "internal/foo")
	if err != nil {
		t.Fatalf("ReloadPackage (no edit): %v", err)
	}
	if changed {
		t.Fatal("ReloadPackage reported a change for an untouched package")
	}
}

func TestStateReloadPackagePassesTheRestOfTheModelAsKnown(t *testing.T) {
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
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	reload := &recordingReader{
		responses: map[string][]domain.PackageModel{
			"./internal/foo": {{Path: "internal/foo", Name: "foo", Structs: []domain.StructDef{{Name: "Thing"}}}},
		},
	}
	st.reader = reload
	if _, err := st.ReloadPackage(ctx, "internal/foo"); err != nil {
		t.Fatalf("ReloadPackage: %v", err)
	}
	if got, want := reload.knownPaths(0), []string{"internal/bar", "internal/foo"}; !equalStrings(got, want) {
		t.Fatalf("known models handed to the reload = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStateReviewConfigFollowsPrimaryWorktree(t *testing.T) {
	branch := t.TempDir()
	primary := t.TempDir()

	writeFile(t, filepath.Join(branch, "wyrd.yaml"), `module: example.com/fixture
layers:
  domain:
    - "internal/foo/..."
review_views:
  stale:
    title: Stale
    default_expansion: changed
`)
	writeFile(t, filepath.Join(primary, "wyrd.yaml"), `module: example.com/fixture
review_views:
  fresh:
    title: Fresh
    default_expansion: collapsed
review_groups:
  core:
    title: Core
`)

	s := NewState(branch)
	if err := s.ReloadOverlay(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.SetReviewConfigRoot(primary)

	cfg := s.Snapshot().Overlay
	if cfg == nil {
		t.Fatal("overlay config missing")
	}
	if _, ok := cfg.ReviewViews["fresh"]; !ok {
		t.Errorf("ReviewViews = %v, want primary's 'fresh'", cfg.ReviewViews)
	}
	if _, ok := cfg.ReviewViews["stale"]; ok {
		t.Errorf("ReviewViews still contains branch's 'stale': %v", cfg.ReviewViews)
	}
	if _, ok := cfg.ReviewGroups["core"]; !ok {
		t.Errorf("ReviewGroups = %v, want primary's 'core'", cfg.ReviewGroups)
	}
	// Architecture semantics stay per-branch.
	if len(cfg.Layers["domain"]) == 0 {
		t.Errorf("branch layers lost: %v", cfg.Layers)
	}

	// A later overlay reload (watcher path) keeps the override.
	if err := s.ReloadOverlay(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg = s.Snapshot().Overlay
	if _, ok := cfg.ReviewViews["fresh"]; !ok {
		t.Errorf("override lost after reload: %v", cfg.ReviewViews)
	}

	// Same-root (the primary itself) is a no-op.
	p := NewState(primary)
	if err := p.ReloadOverlay(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.SetReviewConfigRoot(primary)
	if _, ok := p.Snapshot().Overlay.ReviewViews["fresh"]; !ok {
		t.Errorf("primary state lost its own config")
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

// hasPackagePath reports whether the state's snapshot holds a package at path.
func hasPackagePath(st *State, path string) bool {
	for _, p := range st.Snapshot().Packages {
		if p.Path == path {
			return true
		}
	}
	return false
}

// TestStateFindOwningPackageResolvesAPackageWhoseSourceIsGone pins the answer
// a deletion needs. The watcher names a file that is no longer there; if the
// lookup consults only the filesystem it climbs past the package that lost the
// file and answers with an ancestor, so that package is never reloaded and
// never removed.
func TestStateFindOwningPackageResolvesAPackageWhoseSourceIsGone(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	gone := filepath.Join(root, "internal", "foo", "foo.go")
	if err := os.RemoveAll(filepath.Join(root, "internal", "foo")); err != nil {
		t.Fatalf("remove package dir: %v", err)
	}

	if got := st.FindOwningPackage(gone); got != "internal/foo" {
		t.Fatalf("FindOwningPackage(%s) = %q, want %q", gone, got, "internal/foo")
	}
}

// TestStateReloadPackageDropsAPackageWhoseDirectoryIsGone checks the other
// half: once the deletion routes to the right package, reloading it removes it.
func TestStateReloadPackageDropsAPackageWhoseDirectoryIsGone(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !hasPackagePath(st, "internal/foo") {
		t.Fatalf("internal/foo missing from the loaded model: %v", packagePaths(st.Snapshot().Packages))
	}

	if err := os.RemoveAll(filepath.Join(root, "internal", "foo")); err != nil {
		t.Fatalf("remove package dir: %v", err)
	}

	changed, err := st.ReloadPackage(ctx, "internal/foo")
	if err != nil {
		t.Fatalf("ReloadPackage: %v", err)
	}
	if !changed {
		t.Fatal("ReloadPackage reported no change after the package was deleted")
	}
	if hasPackagePath(st, "internal/foo") {
		t.Fatalf("internal/foo survived its own source: %v", packagePaths(st.Snapshot().Packages))
	}
}

// TestStateReloadPackageKeepsAPackageItCouldNotRead pins that a failed read
// of a package whose source is still on disk is an error, not a deletion: a
// loader failing under a go.mod mid-edit used to empty the model one event
// at a time, silently.
func TestStateReloadPackageKeepsAPackageItCouldNotRead(t *testing.T) {
	root := newFixture(t)
	reader := &recordingReader{
		responses: map[string][]domain.PackageModel{
			"./...": {
				{Path: "internal/foo", Name: "foo", Structs: []domain.StructDef{{Name: "Thing"}}},
				{Path: "internal/bar", Name: "bar", Structs: []domain.StructDef{{Name: "Bar"}}},
			},
		},
	}
	st := NewState(root, WithReader(reader))
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	reader.fail = true
	changed, err := st.ReloadPackage(ctx, "internal/foo")
	if err == nil {
		t.Fatal("ReloadPackage swallowed a read failure")
	}
	if changed {
		t.Fatal("ReloadPackage reported a change for a package it could not read")
	}
	if got := countStructs(t, st, "internal/foo"); got != 1 {
		t.Fatalf("internal/foo structs = %d, want the model it had (1)", got)
	}
}

// TestStatePruneMissingPackagesDropsWhatTheTreeNoLongerHas covers the case no
// file event can describe: a whole package tree removed by a checkout, where
// nothing is left on disk for the watcher to resolve to a package.
func TestStatePruneMissingPackagesDropsWhatTheTreeNoLongerHas(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(root, "internal", "foo")); err != nil {
		t.Fatalf("remove package dir: %v", err)
	}

	dropped, err := st.PruneMissingPackages(ctx)
	if err != nil {
		t.Fatalf("PruneMissingPackages: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != "internal/foo" {
		t.Fatalf("dropped = %v, want [internal/foo]", dropped)
	}
	if hasPackagePath(st, "internal/foo") {
		t.Fatalf("internal/foo survived the reconcile: %v", packagePaths(st.Snapshot().Packages))
	}
	if !hasPackagePath(st, "internal/bar") {
		t.Fatalf("internal/bar was dropped too: %v", packagePaths(st.Snapshot().Packages))
	}

	// A second pass has nothing left to do.
	dropped, err = st.PruneMissingPackages(ctx)
	if err != nil {
		t.Fatalf("PruneMissingPackages (second pass): %v", err)
	}
	if len(dropped) != 0 {
		t.Fatalf("second pass dropped %v, want nothing", dropped)
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
	// known records the models handed to each partial read, indexed the same
	// way as calls. Nil for a read that arrived through Read.
	known [][]domain.PackageModel
	fail  bool
	// errs answers a read with an error instead of models, keyed like
	// responses.
	errs map[string]error
}

func (r *recordingReader) Read(_ context.Context, paths []string) ([]domain.PackageModel, error) {
	return r.record(paths, nil)
}

func (r *recordingReader) ReadPartial(_ context.Context, paths []string, known []domain.PackageModel) ([]domain.PackageModel, error) {
	return r.record(paths, known)
}

func (r *recordingReader) record(paths []string, known []domain.PackageModel) ([]domain.PackageModel, error) {
	call := append([]string(nil), paths...)
	r.calls = append(r.calls, call)
	r.known = append(r.known, append([]domain.PackageModel(nil), known...))
	if r.fail {
		return nil, fmt.Errorf("reader should not have been called")
	}
	key := strings.Join(paths, ",")
	if err, ok := r.errs[key]; ok {
		return nil, err
	}
	if models, ok := r.responses[key]; ok {
		return append([]domain.PackageModel(nil), models...), nil
	}
	return nil, fmt.Errorf("unexpected read paths %v", paths)
}

// knownPaths returns the package paths handed to the i-th read, sorted.
func (r *recordingReader) knownPaths(i int) []string {
	if i >= len(r.known) {
		return nil
	}
	paths := make([]string, 0, len(r.known[i]))
	for _, m := range r.known[i] {
		paths = append(paths, m.Path)
	}
	sort.Strings(paths)
	return paths
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

func TestBuildEmbedder_ProviderSelection(t *testing.T) {
	// Save and restore environment
	origProvider := os.Getenv("WYRD_EMBED_PROVIDER")
	origDisable := os.Getenv("WYRD_EMBED_DISABLE")
	origAPIKey := os.Getenv("WYRD_EMBED_API_KEY")
	defer func() {
		os.Setenv("WYRD_EMBED_PROVIDER", origProvider)
		os.Setenv("WYRD_EMBED_DISABLE", origDisable)
		os.Setenv("WYRD_EMBED_API_KEY", origAPIKey)
	}()

	tests := []struct {
		name     string
		provider string
		disable  string
		apiKey   string
		wantID   string
		// standIn is set where wantID is not the provider that was asked
		// for, which the caller must be told.
		standIn bool
	}{
		{
			name:   "default_is_ollama",
			wantID: "ollama:qwen3-embedding:0.6b",
		},
		{
			name:     "explicit_ollama",
			provider: "ollama",
			wantID:   "ollama:qwen3-embedding:0.6b",
		},
		{
			name:     "explicit_noop",
			provider: "noop",
			wantID:   "noop",
		},
		{
			name:    "disabled_returns_noop",
			disable: "1",
			wantID:  "noop",
		},
		{
			name:     "openai_without_key_falls_back_to_noop",
			provider: "openai",
			apiKey:   "",
			wantID:   "noop",
			standIn:  true,
		},
		{
			name:     "vertex_without_project_falls_back_to_noop",
			provider: "vertex",
			wantID:   "noop",
			standIn:  true,
		},
		{
			name:     "openai_with_key",
			provider: "openai",
			apiKey:   "test-key",
			wantID:   "openai:text-embedding-3-small",
		},
		{
			name:     "unknown_provider_falls_back_to_ollama",
			provider: "unknown-xyz",
			wantID:   "ollama:qwen3-embedding:0.6b",
			standIn:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("WYRD_EMBED_PROVIDER", tc.provider)
			os.Setenv("WYRD_EMBED_DISABLE", tc.disable)
			os.Setenv("WYRD_EMBED_API_KEY", tc.apiKey)

			t.Setenv("WYRD_VERTEX_PROJECT", "")
			t.Setenv("GOOGLE_CLOUD_PROJECT", "")

			emb, err := buildEmbedder()
			if emb.ID() != tc.wantID {
				t.Errorf("buildEmbedder().ID() = %q, want %q", emb.ID(), tc.wantID)
			}
			if (err != nil) != tc.standIn {
				t.Errorf("buildEmbedder() err = %v, stand-in = %v", err, tc.standIn)
			}
		})
	}
}

func TestStateReloadReadsWhatNoEventReported(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Edits the State was never told about: a new package, a deleted one.
	if err := os.MkdirAll(filepath.Join(root, "internal", "added"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "added", "added.go"),
		[]byte("package added\n\ntype Added struct{}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "internal", "foo")); err != nil {
		t.Fatalf("remove package dir: %v", err)
	}

	changed, err := st.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !changed {
		t.Fatalf("Reload reported no change")
	}
	if !hasPackagePath(st, "internal/added") || hasPackagePath(st, "internal/foo") {
		t.Fatalf("packages after Reload = %v, want internal/added in and internal/foo out",
			packagePaths(st.Snapshot().Packages))
	}

	// Nothing moved since, so a second Reload is not a change.
	changed, err = st.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload (second): %v", err)
	}
	if changed {
		t.Fatalf("second Reload reported a change on an unchanged tree")
	}
}
