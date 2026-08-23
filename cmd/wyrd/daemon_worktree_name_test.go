package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kgatilin/wyrd/internal/serve"
)

// newRepoWithWorktree builds a git repo with one linked worktree and returns
// both roots. The linked worktree stands in for a branch checked out after a
// daemon started, which is the case the record cannot describe.
func newRepoWithWorktree(t *testing.T) (repo, linked string) {
	t.Helper()
	base := t.TempDir()
	repo = filepath.Join(base, "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"),
		[]byte("module example.com/proj\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	run(repo, "init", "-q", "-b", "main")
	run(repo, "add", ".")
	run(repo, "commit", "-qm", "init")

	linked = filepath.Join(base, "feature")
	run(repo, "worktree", "add", "-q", "-b", "feature", linked)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", linked).Run()
	})
	return repo, linked
}

// TestDaemonWorktreeName_UsesCwdWorktreeMissingFromTheRecord is the regression:
// the record lists only the worktrees that existed when the daemon bound its
// listener, and a command run inside a newer one used to be answered from the
// primary checkout without a word about the substitution.
func TestDaemonWorktreeName_UsesCwdWorktreeMissingFromTheRecord(t *testing.T) {
	repo, linked := newRepoWithWorktree(t)
	t.Chdir(linked)

	rec := &serve.DaemonRecord{
		RepoRoot:  repo,
		Caps:      []string{"mcp", "ui", "multi"},
		Worktrees: []string{filepath.Base(repo)}, // startup snapshot
	}
	if got, want := daemonWorktreeName(rec), filepath.Base(linked); got != want {
		t.Fatalf("daemonWorktreeName = %q, want %q (the worktree cwd is in)", got, want)
	}
}

// TestDaemonWorktreeName_PrefersPrimaryFromOutsideTheRepo keeps the other half
// of the rule: addressed from somewhere else entirely, the request goes to the
// warm primary worktree rather than an arbitrary sibling.
func TestDaemonWorktreeName_PrefersPrimaryFromOutsideTheRepo(t *testing.T) {
	repo, _ := newRepoWithWorktree(t)
	t.Chdir(t.TempDir())

	rec := &serve.DaemonRecord{
		RepoRoot:  repo,
		Caps:      []string{"mcp", "ui", "multi"},
		Worktrees: []string{"feature", filepath.Base(repo)},
	}
	if got, want := daemonWorktreeName(rec), filepath.Base(repo); got != want {
		t.Fatalf("daemonWorktreeName = %q, want %q (the primary worktree)", got, want)
	}
}

// TestDaemonWorktreeName_MatchesListedWorktree covers the ordinary case, where
// the record is current and cwd's worktree is in it.
func TestDaemonWorktreeName_MatchesListedWorktree(t *testing.T) {
	repo, linked := newRepoWithWorktree(t)
	t.Chdir(linked)

	rec := &serve.DaemonRecord{
		RepoRoot:  repo,
		Caps:      []string{"mcp", "ui", "multi"},
		Worktrees: []string{filepath.Base(linked), filepath.Base(repo)},
	}
	if got, want := daemonWorktreeName(rec), filepath.Base(linked); got != want {
		t.Fatalf("daemonWorktreeName = %q, want %q", got, want)
	}
}
