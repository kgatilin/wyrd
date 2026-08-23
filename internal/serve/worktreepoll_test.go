package serve

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/worktree"
)

// TestServe_PollTracksWorktreeCreatedAfterStartup drives the whole tracking
// path: a worktree that appears while the daemon is running must reach the
// registry record — the list every CLI client picks its target worktree from —
// and must be parsed without waiting for someone to ask for it by name.
func TestServe_PollTracksWorktreeCreatedAfterStartup(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())
	root := newGitRepo(t)

	var loaded sync.Map
	loader := func(_ context.Context, name, path string) (*State, error) {
		loaded.Store(name, true)
		return NewState(path), nil
	}

	multi := NewMultiState(root, loader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	fake := &fakeHTTPTransport{readyCh: make(chan string, 1)}
	opts := Options{
		Root:       root,
		MultiState: multi,
		HTTPAddr:   "127.0.0.1:0",
		HTTPServerFactory: func(*State) (HTTPTransport, error) {
			return fake, nil
		},
		LogOut:               io.Discard,
		WorktreePollInterval: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, opts) }()

	select {
	case <-fake.readyCh:
	case err := <-done:
		t.Fatalf("Serve returned before HTTP startup: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP did not start within 2s")
	}

	repoRoot, ok := worktree.RepoRoot(root)
	if !ok {
		repoRoot = root
	}
	rec, err := ReadGlobalRecord(repoRoot)
	if err != nil {
		t.Fatalf("ReadGlobalRecord: %v", err)
	}
	if rec == nil {
		t.Fatal("no registry record after HTTP startup")
	}
	if len(rec.Worktrees) != 1 {
		t.Fatalf("worktrees at startup = %v, want exactly the primary one", rec.Worktrees)
	}

	extraPath := filepath.Join(filepath.Dir(root), "polled-"+filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "polled-branch", extraPath)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", extraPath).Run()
	})
	extraName := filepath.Base(extraPath)

	// The poll is the only thing that can notice: nothing has requested the
	// new worktree, so the miss-driven re-discovery never runs.
	awaitCondition(t, 5*time.Second, "registry record to list "+extraName, func() bool {
		rec, err := ReadGlobalRecord(repoRoot)
		return err == nil && rec != nil && slices.Contains(rec.Worktrees, extraName)
	})
	awaitCondition(t, 5*time.Second, "the new worktree to be parsed", func() bool {
		_, ok := loaded.Load(extraName)
		return ok
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return within 3s of cancel")
	}

	// Shutdown still clears the record; republishing must not have left a
	// second copy under a different key.
	rec, err = ReadGlobalRecord(repoRoot)
	if err != nil {
		t.Fatalf("ReadGlobalRecord after shutdown: %v", err)
	}
	if rec != nil {
		t.Fatalf("registry record survived shutdown: %+v", rec)
	}
}

// TestDaemonRecordPublisher_RepublishBeforePublish covers the window before
// the listener binds: there is no record on disk yet, so a worktree change
// arriving early must be a no-op rather than write a half-built record.
func TestDaemonRecordPublisher_RepublishBeforePublish(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())
	var p daemonRecordPublisher

	updated, err := p.republishWorktrees([]string{"main"})
	if err != nil {
		t.Fatalf("republishWorktrees: %v", err)
	}
	if updated {
		t.Fatal("republishWorktrees reported an update before anything was published")
	}
}

// awaitCondition polls until cond holds, failing the test with what it was
// waiting for when it never does.
func awaitCondition(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
