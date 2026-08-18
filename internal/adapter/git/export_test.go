package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExportTreeWritesTheCommitNotTheWorkingTree(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
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

	run("init", "--initial-branch=main")
	write("go.mod", "module example.com/m\n\ngo 1.22\n")
	write("internal/pkg/a.go", "package pkg\n\nconst At = \"first\"\n")
	run("add", ".")
	run("commit", "-m", "first")
	first := HeadRev(repo)

	write("internal/pkg/a.go", "package pkg\n\nconst At = \"second\"\n")
	write("internal/pkg/b.go", "package pkg\n")
	run("add", ".")
	run("commit", "-m", "second")

	// Uncommitted and untracked noise must not leak into the export.
	write("internal/pkg/a.go", "package pkg\n\nconst At = \"dirty\"\n")
	write("internal/pkg/untracked.go", "package pkg\n")

	dest := filepath.Join(t.TempDir(), "tree")
	if err := ExportTree(repo, first, dest); err != nil {
		t.Fatalf("ExportTree: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "internal", "pkg", "a.go"))
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if string(got) != "package pkg\n\nconst At = \"first\"\n" {
		t.Errorf("exported a.go = %q, want the first commit's content", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "internal", "pkg", "b.go")); !os.IsNotExist(err) {
		t.Error("a file added after the exported commit is present in the export")
	}
	if _, err := os.Stat(filepath.Join(dest, "internal", "pkg", "untracked.go")); !os.IsNotExist(err) {
		t.Error("an untracked working-tree file leaked into the export")
	}
	if _, err := os.Stat(filepath.Join(dest, "go.mod")); err != nil {
		t.Errorf("go.mod missing from the export: %v", err)
	}
}

func TestExportTreeRejectsEscapingEntries(t *testing.T) {
	if _, err := safeJoin("/tmp/dest", "../../etc/passwd"); err == nil {
		t.Error("safeJoin accepted a path escaping the destination")
	}
	if _, err := safeJoin("/tmp/dest", "/etc/passwd"); err == nil {
		t.Error("safeJoin accepted an absolute path")
	}
	got, err := safeJoin("/tmp/dest", "internal/pkg/a.go")
	if err != nil || got != filepath.Join("/tmp/dest", "internal", "pkg", "a.go") {
		t.Errorf("safeJoin(ordinary path) = %q, %v", got, err)
	}
}

func TestMergeBaseAndCleanliness(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
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
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--initial-branch=main")
	write("a.txt", "one\n")
	run("add", ".")
	run("commit", "-m", "base")
	branchPoint := HeadRev(repo)

	run("checkout", "-b", "feature")
	write("b.txt", "two\n")
	run("add", ".")
	run("commit", "-m", "feature")

	run("checkout", "main")
	write("c.txt", "three\n")
	run("add", ".")
	run("commit", "-m", "main moves on")
	run("checkout", "feature")

	rev, ok := MergeBase(repo, "main")
	if !ok {
		t.Fatal("MergeBase(main) not resolved")
	}
	if rev != branchPoint {
		t.Errorf("MergeBase = %s, want the branch point %s (not the base tip)", rev, branchPoint)
	}
	if _, ok := MergeBase(repo, "nope"); ok {
		t.Error("MergeBase resolved a ref that does not exist")
	}

	if !IsClean(repo) {
		t.Error("IsClean = false on a freshly checked-out tree")
	}
	write("a.txt", "dirty\n")
	if IsClean(repo) {
		t.Error("IsClean = true with a modified file")
	}
	run("checkout", "--", "a.txt")
	write("untracked.txt", "x\n")
	if IsClean(repo) {
		t.Error("IsClean = true with an untracked file (it would be parsed into the model)")
	}
}
