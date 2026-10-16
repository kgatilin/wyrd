package serve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
)

// TestReviewBase_UsesMergeBaseNotBaseTip is the invariant the whole
// materialized-base machinery exists for: a branch is reviewed against the
// commit it forked from. When the base worktree has moved on, its parsed
// model is not that commit and must not be substituted for it.
func TestReviewBase_UsesMergeBaseNotBaseTip(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())

	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype Existing struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "api")
	branchPoint := git.HeadRev(root)

	feature := filepath.Join(t.TempDir(), "feature")
	runGit(t, root, "worktree", "add", "-q", "-b", "feature", feature)
	writeGo(t, feature, "api/feature.go", "package api\n\ntype Feature struct{}\n")
	runGit(t, feature, "add", ".")
	runGit(t, feature, "commit", "-qm", "feature")

	writeGo(t, root, "api/later.go", "package api\n\ntype Later struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "main moves on")

	multi := NewMultiState(root, DefaultStateLoader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	multi.SetReviewBaseRef("main")
	name := worktreeNameFor(t, multi, feature)

	base, err := multi.ReviewBase(context.Background(), name, "")
	if err != nil {
		t.Fatalf("ReviewBase: %v", err)
	}
	if base.Rev != branchPoint {
		t.Errorf("Rev = %s, want the branch point %s", base.Rev, branchPoint)
	}
	if base.Ref != "main" {
		t.Errorf("Ref = %q, want main", base.Ref)
	}
	if len(base.Models) == 0 {
		t.Fatal("no base models parsed")
	}
	names := declaredTypeNames(base.Models)
	if !names["Existing"] {
		t.Errorf("base model missing the type present at the branch point: %v", names)
	}
	if names["Later"] {
		t.Errorf("base model contains a type added on main after the branch point: %v", names)
	}
	if names["Feature"] {
		t.Errorf("base model contains the branch's own new type: %v", names)
	}
}

// When the base worktree is clean and sits exactly on the merge base — the
// state right after a rebase — its already-parsed model is the commit's
// model, and nothing needs materializing.
func TestReviewBase_ReusesBaseWorktreeWhenItIsTheMergeBase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WYRD_HOME", home)

	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype Existing struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "api")

	feature := filepath.Join(t.TempDir(), "feature")
	runGit(t, root, "worktree", "add", "-q", "-b", "feature", feature)
	writeGo(t, feature, "api/feature.go", "package api\n\ntype Feature struct{}\n")

	multi := NewMultiState(root, DefaultStateLoader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	multi.SetReviewBaseRef("main")

	base, err := multi.ReviewBase(context.Background(), worktreeNameFor(t, multi, feature), "")
	if err != nil {
		t.Fatalf("ReviewBase: %v", err)
	}
	if len(base.Models) == 0 {
		t.Fatal("no base models resolved")
	}
	if _, err := os.Stat(filepath.Join(home, baseTreeDirName)); !os.IsNotExist(err) {
		t.Error("a base tree was materialized even though the base worktree already is the merge base")
	}
}

// A worktree with no configured base ref, or one whose base ref does not
// exist, reports no models rather than falling back to some other commit.
func TestReviewBase_UnresolvableRefYieldsNoModels(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())

	root := newGitRepo(t)
	multi := NewMultiState(root, DefaultStateLoader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	base, err := multi.ReviewBase(context.Background(), multi.Default(), "")
	if err != nil || base.Models != nil || base.Rev != "" {
		t.Errorf("no base ref configured: got %+v, err %v", base, err)
	}

	multi.SetReviewBaseRef("does-not-exist")
	base, err = multi.ReviewBase(context.Background(), multi.Default(), "")
	if err != nil {
		t.Fatalf("ReviewBase: %v", err)
	}
	if base.Models != nil || base.Rev != "" {
		t.Errorf("unknown base ref resolved anyway: %+v", base)
	}
}

func testStore(t *testing.T) *modelstore.Store {
	t.Helper()
	store, err := modelstore.Open(t.TempDir(), "test-binary")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

// A materialized tree exists to produce a snapshot. Once the snapshot is
// recorded the tree is gone, and the next request reads the store.
func TestBaseTrees_RecordsTheSnapshotAndDropsTheTree(t *testing.T) {
	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype A struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "one")
	rev := git.HeadRev(root)

	cache := t.TempDir()
	store := testStore(t)
	trees := newBaseTrees(cache)
	versions, err := trees.Versions(context.Background(), store, root, rev)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if !declaredTypeNames(modelsOfVersions(versions))["A"] {
		t.Fatalf("base commit parsed without its type: %+v", versions)
	}
	if _, err := os.Stat(filepath.Join(cache, rev)); !os.IsNotExist(err) {
		t.Errorf("the materialized tree outlived its snapshot: %v", err)
	}
	stored, ok, err := store.Snapshot(rev)
	if err != nil || !ok {
		t.Fatalf("no snapshot recorded for %s: ok %v err %v", rev, ok, err)
	}
	if len(stored) != len(versions) || &stored[0].Model.Structs[0] != &versions[0].Model.Structs[0] {
		t.Error("the store does not hold the instances the producer returned")
	}
}

// The sweep is what keeps the directory bounded across restarts: it removes
// the trees earlier daemons left behind, never a load in flight, and nothing
// recent enough to be another process's load.
func TestBaseTrees_SweepRemovesLeftoversButNotALoadInFlight(t *testing.T) {
	cache := t.TempDir()
	old := time.Now().Add(-2 * baseTreeStaleAfter)
	mk := func(name string, mod time.Time) {
		dir := filepath.Join(cache, name)
		if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	mk("leftover1", old)
	mk("leftover2", old)
	mk("inflight", old)
	mk("inflight.tmp123", old)
	mk("recent", time.Now())

	trees := newBaseTrees(cache)
	trees.loads["inflight"] = &baseTreeLoad{done: make(chan struct{})}
	trees.sweep()

	for name, want := range map[string]bool{
		"leftover1": false, "leftover2": false,
		"inflight": true, "inflight.tmp123": true, "recent": true,
	} {
		_, err := os.Stat(filepath.Join(cache, name))
		if got := err == nil; got != want {
			t.Errorf("%s present = %v, want %v", name, got, want)
		}
	}
}

func TestBaseTrees_UnknownRevisionIsNotCachedAsFailure(t *testing.T) {
	root := newGitRepo(t)
	trees := newBaseTrees(t.TempDir())
	if _, err := trees.Versions(context.Background(), testStore(t), root, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("Versions accepted a revision that does not exist")
	}
	trees.mu.Lock()
	cached := len(trees.loads)
	trees.mu.Unlock()
	if cached != 0 {
		t.Errorf("a failed load stayed in flight (%d entries); the next request would inherit it", cached)
	}
}

// TestBaseTrees_HuskIsReExported pins the difference between a directory that
// exists and a tree that was published. A directory evicted while it was being
// parsed used to come back — the parser recreated it to write its model cache —
// holding no source. Accepting it would poison the revision: every review
// against that commit would fail to parse and nothing would re-export.
func TestBaseTrees_HuskIsReExported(t *testing.T) {
	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype A struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "one")
	rev := git.HeadRev(root)

	cache := t.TempDir()
	dir := filepath.Join(cache, rev)
	if err := os.MkdirAll(filepath.Join(dir, ".archai", "cache"), 0o755); err != nil {
		t.Fatalf("create husk: %v", err)
	}

	versions, err := newBaseTrees(cache).Versions(context.Background(), testStore(t), root, rev)
	if err != nil {
		t.Fatalf("Versions over a husk: %v", err)
	}
	if !declaredTypeNames(modelsOfVersions(versions))["A"] {
		t.Fatalf("the husk was parsed instead of a re-exported tree: %+v", versions)
	}
}

// A merge base some State or earlier request already recorded is answered
// from the store: nothing is materialized and nothing is parsed.
func TestReviewBase_ServedFromTheStoreWithoutMaterializing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WYRD_HOME", home)

	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype Existing struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "api")
	branchPoint := git.HeadRev(root)
	feature := filepath.Join(t.TempDir(), "feature")
	runGit(t, root, "worktree", "add", "-q", "-b", "feature", feature)
	writeGo(t, feature, "api/feature.go", "package api\n\ntype Feature struct{}\n")
	writeGo(t, root, "api/later.go", "package api\n\ntype Later struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "main moves on")

	store := testStore(t)
	recorded := []domain.PackageModel{{Path: "api", Name: "api", Structs: []domain.StructDef{{Name: "Recorded"}}}}
	if _, err := store.Record(branchPoint, recorded); err != nil {
		t.Fatalf("Record: %v", err)
	}
	multi := NewMultiState(root, DefaultStateLoader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	multi.SetReviewBaseRef("main")
	multi.SetModelStore(store)

	base, err := multi.ReviewBase(context.Background(), worktreeNameFor(t, multi, feature), "")
	if err != nil {
		t.Fatalf("ReviewBase: %v", err)
	}
	if names := declaredTypeNames(base.Models); !names["Recorded"] || len(names) != 1 {
		t.Fatalf("base models = %v, want the recorded snapshot", names)
	}
	if _, err := os.Stat(filepath.Join(home, baseTreeDirName)); !os.IsNotExist(err) {
		t.Error("a base tree was materialized although the store had the snapshot")
	}
}

func modelsOfVersions(versions []modelstore.Version) []domain.PackageModel {
	out := make([]domain.PackageModel, len(versions))
	for i, v := range versions {
		out[i] = v.Model
	}
	return out
}

func writeGo(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func worktreeNameFor(t *testing.T, multi *MultiState, path string) string {
	t.Helper()
	for _, entry := range multi.Worktrees() {
		if samePath(entry.Path, path) {
			return entry.Name
		}
	}
	t.Fatalf("worktree %s not discovered: %+v", path, multi.Worktrees())
	return ""
}

func declaredTypeNames(models []domain.PackageModel) map[string]bool {
	names := map[string]bool{}
	for _, pkg := range models {
		for _, s := range pkg.Structs {
			names[s.Name] = true
		}
	}
	return names
}

// TestReloadWorktree_BranchReadsOnlyItsDiff pins what Refresh costs: on a
// branch, the packages its diff touches (committed, uncommitted, untracked)
// are read over the base's model and nothing else is; a package the branch
// model had lost comes back from the base. On the base worktree Refresh is a
// full read.
func TestReloadWorktree_BranchReadsOnlyItsDiff(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())

	root := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte(".archai/\n"), 0o644); err != nil {
		t.Fatalf("exclude: %v", err)
	}
	writeGo(t, root, "api/api.go", "package api\n\ntype Existing struct{}\n")
	writeGo(t, root, "other/other.go", "package other\n\ntype Other struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "api and other")

	feature := filepath.Join(t.TempDir(), "feature")
	runGit(t, root, "worktree", "add", "-q", "-b", "feature", feature)
	writeGo(t, feature, "api/api.go", "package api\n\ntype Existing struct{}\ntype Edited struct{}\n")
	writeGo(t, feature, "client/client.go", "package client\n\ntype Client struct{}\n")

	api := domain.PackageModel{Path: "api", Name: "api", Structs: []domain.StructDef{{Name: "Existing"}}}
	other := domain.PackageModel{Path: "other", Name: "other", Structs: []domain.StructDef{{Name: "Other"}}}
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	readers := map[string]*recordingReader{
		real(root): {responses: map[string][]domain.PackageModel{
			"./...": {api, other},
		}},
		// The branch's own model has lost "other", the way a dropped
		// package used to stay lost.
		real(feature): {responses: map[string][]domain.PackageModel{
			"./...": {api},
			"./api,./client": {
				{Path: "api", Name: "api", Structs: []domain.StructDef{{Name: "Existing"}, {Name: "Edited"}}},
				{Path: "client", Name: "client", Structs: []domain.StructDef{{Name: "Client"}}},
			},
		}},
	}
	loader := func(ctx context.Context, _, path string) (*State, error) {
		st := NewState(path, WithReader(readers[real(path)]))
		return st, st.Load(ctx)
	}

	multi := NewMultiState(root, loader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	multi.SetReviewBaseRef("main")
	ctx := context.Background()
	branch := worktreeNameFor(t, multi, feature)

	changed, err := multi.ReloadWorktree(ctx, branch, "main")
	if err != nil {
		t.Fatalf("ReloadWorktree(branch): %v", err)
	}
	if !changed {
		t.Fatal("ReloadWorktree(branch) reported no change")
	}
	if got, want := flattenCalls(readers[real(feature)].calls), "./...;./api,./client"; got != want {
		t.Fatalf("branch reads = %q, want %q", got, want)
	}
	st, err := multi.Get(ctx, branch)
	if err != nil {
		t.Fatalf("Get(branch): %v", err)
	}
	names := declaredTypeNames(st.Snapshot().Packages)
	for _, want := range []string{"Existing", "Edited", "Client", "Other"} {
		if !names[want] {
			t.Errorf("branch model missing %s after Refresh: %v", want, names)
		}
	}

	baseName := worktreeNameFor(t, multi, root)
	if _, err := multi.ReloadWorktree(ctx, baseName, "main"); err != nil {
		t.Fatalf("ReloadWorktree(base): %v", err)
	}
	if got, want := flattenCalls(readers[real(root)].calls), "./...;./..."; got != want {
		t.Fatalf("base reads = %q, want a full read on Refresh (%q)", got, want)
	}
}

func TestChangedPackages(t *testing.T) {
	pkgs, full := changedPackages([]git.FileStat{
		{Path: "api/api.go"},
		{Path: "api/api_test.go"},
		{Path: "main.go"},
		{Path: "docs/readme.md"},
		{Path: "next/moved.go", OldPath: "prev/moved.go"},
	})
	if full {
		t.Fatal("a diff without go.mod/go.sum asked for a full read")
	}
	if got, want := strings.Join(pkgs, ","), ".,api,next,prev"; got != want {
		t.Fatalf("packages = %q, want %q", got, want)
	}
	if _, full := changedPackages([]git.FileStat{{Path: "api/api.go"}, {Path: "go.sum"}}); !full {
		t.Fatal("a diff touching go.sum did not ask for a full read")
	}
}
