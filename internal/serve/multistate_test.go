package serve

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newGitRepo creates a minimal git repo with a go.mod and one commit,
// so `git worktree list` has a primary worktree to report.
func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.com/multi\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-qm", "init")
	return root
}

// stubLoader returns a StateLoader that increments *count each call
// and yields a bare State rooted at path (no Go extraction). Used to
// assert lazy-load cache behaviour without the reader's overhead.
func stubLoader(count *int64) StateLoader {
	return func(_ context.Context, _, path string) (*State, error) {
		atomic.AddInt64(count, 1)
		return NewState(path), nil
	}
}

func TestMultiState_RefreshAndGet(t *testing.T) {
	root := newGitRepo(t)
	var loadCount int64
	m := NewMultiState(root, stubLoader(&loadCount))

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	names := m.Names()
	if len(names) != 1 {
		t.Fatalf("want 1 worktree, got %d: %v", len(names), names)
	}

	// First Get triggers load.
	if _, err := m.Get(context.Background(), names[0]); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := atomic.LoadInt64(&loadCount); got != 1 {
		t.Errorf("load count after first Get = %d, want 1", got)
	}

	// Second Get hits cache — loader must not re-run.
	if _, err := m.Get(context.Background(), names[0]); err != nil {
		t.Fatalf("Get (second): %v", err)
	}
	if got := atomic.LoadInt64(&loadCount); got != 1 {
		t.Errorf("load count after second Get = %d, want 1 (cache miss)", got)
	}

	// Unknown worktree returns an error.
	if _, err := m.Get(context.Background(), "nope"); err == nil {
		t.Errorf("Get(nope) expected error")
	}
}

// runGit runs a git subcommand against repo and fails the test on error.
func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func samePath(a, b string) bool {
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return a == b
}

// TestMultiState_RefreshDropsRemoved exercises the full Refresh → Get
// → Refresh-drop → Get cycle against a real git repo with an added and
// then removed worktree. It goes through the exported MultiState API
// only; no private fields are touched.
func TestMultiState_RefreshDropsRemoved(t *testing.T) {
	root := newGitRepo(t)

	// Create a sibling worktree 'extra' on a new branch; after Refresh
	// MultiState should see two entries.
	parent := filepath.Dir(root)
	extraPath := filepath.Join(parent, "extra-"+filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "extra-branch", extraPath)
	t.Cleanup(func() {
		// Best-effort: extra may already be removed by the test itself.
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", extraPath).Run()
	})

	var loadCount int64
	m := NewMultiState(root, stubLoader(&loadCount))
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	names := m.Names()
	if len(names) != 2 {
		t.Fatalf("want 2 worktrees after Refresh, got %d: %v", len(names), names)
	}

	// Locate the extra entry by path so we can reference it across the
	// refresh drop. Its Name is set by worktree.Discover to basename.
	var extraName string
	for _, n := range names {
		e, _ := m.Entry(n)
		if samePath(e.Path, extraPath) {
			extraName = n
			break
		}
	}
	if extraName == "" {
		t.Fatalf("could not find extra worktree in Names %v", names)
	}

	// First Get triggers load for the extra worktree.
	if _, err := m.Get(context.Background(), extraName); err != nil {
		t.Fatalf("Get(%q): %v", extraName, err)
	}
	if got := atomic.LoadInt64(&loadCount); got != 1 {
		t.Fatalf("load count after first Get = %d, want 1", got)
	}

	// Second Get hits cache.
	if _, err := m.Get(context.Background(), extraName); err != nil {
		t.Fatalf("Get(%q) second: %v", extraName, err)
	}
	if got := atomic.LoadInt64(&loadCount); got != 1 {
		t.Fatalf("load count after cached Get = %d, want 1", got)
	}

	// Remove the worktree via git, then Refresh. The cached state must
	// be dropped so a subsequent Get (once re-added) triggers a fresh
	// load.
	runGit(t, root, "worktree", "remove", "--force", extraPath)
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh after remove: %v", err)
	}
	if m.Has(extraName) {
		t.Fatalf("extra worktree %q still present after removal", extraName)
	}

	// Querying the removed worktree returns an unknown-worktree error.
	if _, err := m.Get(context.Background(), extraName); err == nil {
		t.Fatalf("Get(%q) on removed worktree expected error", extraName)
	}

	// Re-add the same worktree path + branch, Refresh, Get again —
	// load count should advance to 2, proving the prior cache was
	// dropped and not silently re-used.
	runGit(t, root, "worktree", "add", extraPath, "extra-branch")
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh after re-add: %v", err)
	}
	if !m.Has(extraName) {
		t.Fatalf("extra worktree %q missing after re-add", extraName)
	}
	if _, err := m.Get(context.Background(), extraName); err != nil {
		t.Fatalf("Get(%q) after re-add: %v", extraName, err)
	}
	if got := atomic.LoadInt64(&loadCount); got != 2 {
		t.Errorf("load count after re-add Get = %d, want 2 (cache should have been dropped)", got)
	}
}

// fakeCloser records a single Close call.
type fakeCloser struct {
	closed int64
}

func (f *fakeCloser) Close() error {
	atomic.AddInt64(&f.closed, 1)
	return nil
}

// TestMultiState_WatcherHookLifecycle verifies that a WatcherHook is
// invoked exactly once per loaded worktree (even across concurrent
// Gets), that its closer is released when the worktree is dropped by
// a Refresh, and that MultiState.Close releases any remaining closers.
func TestMultiState_WatcherHookLifecycle(t *testing.T) {
	root := newGitRepo(t)

	// Add a second worktree so we can test per-worktree isolation.
	parent := filepath.Dir(root)
	extraPath := filepath.Join(parent, "hook-"+filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "hook-branch", extraPath)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", extraPath).Run()
	})

	m := NewMultiState(root, stubLoader(new(int64)))

	var (
		hookMu    sync.Mutex
		byName    = map[string]*fakeCloser{}
		hookCalls int
	)
	m.SetWatcherHook(func(_ context.Context, name string, _ *State) (io.Closer, error) {
		hookMu.Lock()
		defer hookMu.Unlock()
		hookCalls++
		c := &fakeCloser{}
		byName[name] = c
		return c, nil
	})

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	names := m.Names()
	if len(names) != 2 {
		t.Fatalf("want 2 worktrees, got %d: %v", len(names), names)
	}

	// Load both states; each should trigger exactly one hook call.
	for _, n := range names {
		if _, err := m.Get(context.Background(), n); err != nil {
			t.Fatalf("Get(%q): %v", n, err)
		}
	}
	// A second Get hits the cache and must not re-run the hook.
	for _, n := range names {
		if _, err := m.Get(context.Background(), n); err != nil {
			t.Fatalf("Get(%q) cached: %v", n, err)
		}
	}
	hookMu.Lock()
	if hookCalls != 2 {
		t.Errorf("hook calls = %d, want 2", hookCalls)
	}
	hookMu.Unlock()

	// Remove the extra worktree; Refresh must close its hook.
	runGit(t, root, "worktree", "remove", "--force", extraPath)
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh after remove: %v", err)
	}

	// Find the extra hook's closer — by process of elimination, it's
	// the one whose name is not the primary.
	primary := filepath.Base(root)
	hookMu.Lock()
	var extraCloser *fakeCloser
	for n, c := range byName {
		if n != primary {
			extraCloser = c
		}
	}
	hookMu.Unlock()
	if extraCloser == nil {
		t.Fatalf("could not locate extra worktree closer")
	}
	if got := atomic.LoadInt64(&extraCloser.closed); got != 1 {
		t.Errorf("extra closer Close count = %d, want 1", got)
	}

	// Close the MultiState — remaining closer(s) must be released.
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	hookMu.Lock()
	primaryCloser := byName[primary]
	hookMu.Unlock()
	if primaryCloser == nil {
		t.Fatalf("primary closer missing")
	}
	if got := atomic.LoadInt64(&primaryCloser.closed); got != 1 {
		t.Errorf("primary closer Close count after MultiState.Close = %d, want 1", got)
	}
}

// TestMultiState_RefreshRejectsDuplicateNames creates two worktrees
// whose basenames collide and verifies Refresh reports an explicit
// error rather than silently dropping one of them.
func TestMultiState_RefreshRejectsDuplicateNames(t *testing.T) {
	root := newGitRepo(t)

	// Build a nested directory whose basename matches root's basename.
	// e.g. if root is /tmp/X/foo, extra sits at /tmp/X/foo/sub/foo.
	parent := filepath.Join(root, "sub")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	dup := filepath.Join(parent, filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "dup-branch", dup)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", dup).Run()
	})

	m := NewMultiState(root, stubLoader(new(int64)))
	err := m.Refresh()
	if err == nil {
		t.Fatalf("Refresh with duplicate basenames: expected error, got nil (names=%v)", m.Names())
	}
	if !strings.Contains(err.Error(), "duplicate worktree name") {
		t.Errorf("error = %q, want contains %q", err.Error(), "duplicate worktree name")
	}
}
