package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/git"
	"github.com/kgatilin/archai/internal/domain"
)

// TestReviewBase_UsesMergeBaseNotBaseTip is the invariant the whole
// materialized-base machinery exists for: a branch is reviewed against the
// commit it forked from. When the base worktree has moved on, its parsed
// model is not that commit and must not be substituted for it.
func TestReviewBase_UsesMergeBaseNotBaseTip(t *testing.T) {
	t.Setenv("ARCHAI_HOME", t.TempDir())

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
	t.Setenv("ARCHAI_HOME", home)

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
	t.Setenv("ARCHAI_HOME", t.TempDir())

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

func TestBaseTrees_EvictsBeyondLimit(t *testing.T) {
	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype A struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "one")
	first := git.HeadRev(root)
	writeGo(t, root, "api/b.go", "package api\n\ntype B struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "two")
	second := git.HeadRev(root)

	cache := t.TempDir()
	trees := newBaseTrees(cache, 1)
	ctx := context.Background()

	if _, err := trees.Models(ctx, root, first); err != nil {
		t.Fatalf("Models(first): %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, first)); err != nil {
		t.Fatalf("first base tree not materialized: %v", err)
	}
	if _, err := trees.Models(ctx, root, second); err != nil {
		t.Fatalf("Models(second): %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, first)); !os.IsNotExist(err) {
		t.Error("the evicted base tree is still on disk")
	}
	if _, err := os.Stat(filepath.Join(cache, second)); err != nil {
		t.Errorf("the live base tree was removed: %v", err)
	}
}

func TestBaseTrees_UnknownRevisionIsNotCachedAsFailure(t *testing.T) {
	root := newGitRepo(t)
	trees := newBaseTrees(t.TempDir(), 2)
	if _, err := trees.Models(context.Background(), root, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("Models accepted a revision that does not exist")
	}
	trees.mu.Lock()
	cached := len(trees.loads)
	trees.mu.Unlock()
	if cached != 0 {
		t.Errorf("a failed load stayed in the cache (%d entries); the next request would inherit it", cached)
	}
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
