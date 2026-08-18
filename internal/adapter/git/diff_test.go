package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNumstatZ_RenameUsesNewPath(t *testing.T) {
	// A rename record leaves the path field empty and emits old/new as
	// two following NUL fields.
	out := "3\t1\tinternal/a.go\x00" + "0\t0\t\x00old/b.go\x00new/b.go\x00" + "-\t-\tassets/logo.png\x00"
	entries := parseNumstatZ(out)
	if len(entries) != 3 {
		t.Fatalf("parseNumstatZ returned %d entries, want 3: %+v", len(entries), entries)
	}
	if entries[0].path != "internal/a.go" || entries[0].insertions != 3 || entries[0].deletions != 1 {
		t.Errorf("regular entry = %+v", entries[0])
	}
	if entries[1].path != "new/b.go" {
		t.Errorf("rename entry path = %q, want new/b.go", entries[1].path)
	}
	if !entries[2].binary {
		t.Errorf("binary entry not flagged: %+v", entries[2])
	}
}

func TestParseNameStatusZ_RenameCarriesBothPaths(t *testing.T) {
	out := "M\x00internal/a.go\x00R100\x00old/b.go\x00new/b.go\x00A\x00internal/c.go\x00"
	entries := parseNameStatusZ(out)
	if len(entries) != 3 {
		t.Fatalf("parseNameStatusZ returned %d entries, want 3: %+v", len(entries), entries)
	}
	if entries[1].status != "R100" || entries[1].oldPath != "old/b.go" || entries[1].path != "new/b.go" {
		t.Errorf("rename entry = %+v", entries[1])
	}
	if entries[2].path != "internal/c.go" {
		t.Errorf("entry after a rename = %+v (parser lost sync)", entries[2])
	}
}

func TestSplitPatchByFile(t *testing.T) {
	full := strings.Join([]string{
		"diff --git a/a.go b/a.go",
		"index 111..222 100644",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		`diff --git "a/has space.go" "b/has space.go"`,
		"@@ -0,0 +1 @@",
		"+hi",
		"",
	}, "\n")

	got := splitPatchByFile(full)
	if len(got) != 2 {
		t.Fatalf("splitPatchByFile returned %d files, want 2: %v", len(got), got)
	}
	if !strings.HasPrefix(got["a.go"], "diff --git a/a.go b/a.go") || !strings.Contains(got["a.go"], "+new") {
		t.Errorf("a.go patch = %q", got["a.go"])
	}
	if _, ok := got["has space.go"]; !ok {
		t.Errorf("quoted path not unquoted: %v", keys(got))
	}
}

func TestCapPatchTrimsOnLineBoundary(t *testing.T) {
	patch, truncated := capPatch(strings.Repeat("+line\n", maxPatchBytes))
	if !truncated {
		t.Fatal("oversized patch not marked truncated")
	}
	if len(patch) > maxPatchBytes {
		t.Errorf("capped patch is %d bytes, want <= %d", len(patch), maxPatchBytes)
	}
	if strings.HasSuffix(patch, "+lin") {
		t.Error("patch was cut mid-line")
	}
}

// TestDiff covers the whole adapter against a real repository: the diff
// must be three-dot (only this branch's commits, not the base's), and it
// must include both uncommitted edits and untracked files.
func TestDiff(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--initial-branch=main")
	write("base.go", "package base\n")
	git("add", ".")
	git("commit", "-m", "base")

	git("checkout", "-b", "feature")
	write("internal/added.go", "package internal\n\nfunc Added() {}\n")
	write("base.go", "package base\n\nvar Changed = 1\n")
	git("add", ".")
	git("commit", "-m", "feature work")

	// The base moves ahead after the branch point. A two-dot diff would
	// report this file as *removed* by the branch; a three-dot one ignores it.
	git("checkout", "main")
	write("moved-on.go", "package base\n")
	git("add", ".")
	git("commit", "-m", "base moves on")
	git("checkout", "feature")

	write("uncommitted.go", "package base\n") // untracked
	write("base.go", "package base\n\nvar Changed = 2\n")

	res, err := Diff(repo, "main")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if res.Branch != "feature" {
		t.Errorf("Branch = %q, want feature", res.Branch)
	}
	if res.BaseRef != "main" || res.BaseRev == "HEAD" {
		t.Errorf("base not resolved to a merge base: ref=%q rev=%q", res.BaseRef, res.BaseRev)
	}

	byPath := map[string]FileStat{}
	for _, f := range res.Files {
		byPath[f.Path] = f
	}
	if _, ok := byPath["moved-on.go"]; ok {
		t.Error("diff is two-dot: it reports a file the base added after the branch point")
	}
	added, ok := byPath["internal/added.go"]
	if !ok {
		t.Fatalf("committed addition missing from diff: %v", keys(byPath))
	}
	if added.Status != "A" || added.Insertions == 0 || !strings.Contains(added.Patch, "func Added()") {
		t.Errorf("committed addition = %+v", added)
	}
	changed, ok := byPath["base.go"]
	if !ok {
		t.Fatal("modified file missing from diff")
	}
	if !strings.Contains(changed.Patch, "var Changed = 2") {
		t.Error("diff stops at HEAD: the uncommitted edit is missing")
	}
	untracked, ok := byPath["uncommitted.go"]
	if !ok {
		t.Fatalf("untracked file missing from diff: %v", keys(byPath))
	}
	if !untracked.Untracked || untracked.Status != "A" || untracked.Insertions != 1 {
		t.Errorf("untracked entry = %+v", untracked)
	}
}

// TestDiffUnknownBaseFallsBackToEmptyTree: a repo without the review base
// ref — and without any commit — still yields the working tree rather than
// an error.
func TestDiffUnknownBaseFallsBackToEmptyTree(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "-C", repo, "init", "--initial-branch=trunk")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Diff(repo, "main")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if res.BaseRev != emptyTreeRev {
		t.Errorf("BaseRev = %q, want the empty-tree fallback for an unborn HEAD", res.BaseRev)
	}
	if res.Branch != "trunk" {
		t.Errorf("Branch = %q, want trunk", res.Branch)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "new.go" {
		t.Errorf("Files = %+v, want the untracked file", res.Files)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
