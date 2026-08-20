package serve

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// lockedBuffer is a concurrency-safe log sink: MultiState logs failures from
// the background load goroutine while the test reads them.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestMultiState_FailedLoadIsRecordedThrottledAndRetried covers the failure
// path a broken worktree takes: a load that fails caches no State, so without
// a recorded failure it is invisible (transports keep answering "parsing")
// and every poll restarts the doomed load. The failure must therefore be
// remembered, logged, and throttled — and forgotten once a load succeeds.
func TestMultiState_FailedLoadIsRecordedThrottledAndRetried(t *testing.T) {
	root := newGitRepo(t)

	loadErr := errors.New("package errors: pattern dist/bundle.json: no matching files found")
	var calls int64
	loader := func(_ context.Context, _, path string) (*State, error) {
		if atomic.AddInt64(&calls, 1) == 1 {
			return nil, loadErr
		}
		return NewState(path), nil
	}

	m := NewMultiState(root, loader)
	logs := &lockedBuffer{}
	m.SetLogOut(logs)
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	name := m.Names()[0]

	// A failing load reports the error and caches nothing.
	st, err := m.Get(context.Background(), name)
	if st != nil || !errors.Is(err, loadErr) {
		t.Fatalf("Get on failing loader = (%v, %v), want (nil, %v)", st, err, loadErr)
	}
	if st, ok := m.Loaded(name); ok || st != nil {
		t.Fatalf("Loaded after failure = (%v, %v), want (nil, false)", st, ok)
	}

	// The failure is recorded, with a retry deadline in the future.
	retryAt, gotErr := m.LoadError(name)
	if !errors.Is(gotErr, loadErr) {
		t.Fatalf("LoadError = %v, want %v", gotErr, loadErr)
	}
	if !retryAt.After(time.Now()) {
		t.Errorf("LoadError retryAt = %v, want a deadline in the future", retryAt)
	}

	// Polling within the retry window must not re-run the loader: the HTTP
	// layer asks every couple of seconds and the load is expensive.
	for i := 0; i < 5; i++ {
		if _, ok := m.Loaded(name); ok {
			t.Fatalf("Loaded reported success on attempt %d", i)
		}
	}
	if _, err := m.Get(context.Background(), name); !errors.Is(err, loadErr) {
		t.Fatalf("Get within retry window = %v, want the recorded %v", err, loadErr)
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("loader calls within retry window = %d, want 1 (throttled)", got)
	}

	// The failure is logged exactly once — the load handle is discarded, so
	// nothing else in the daemon would ever mention it.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "loading worktree") {
		if time.Now().After(deadline) {
			t.Fatalf("failure was never logged; log = %q", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	logged := logs.String()
	if n := strings.Count(logged, "loading worktree"); n != 1 {
		t.Errorf("failure logged %d times, want 1: %q", n, logged)
	}
	if !strings.Contains(logged, name) || !strings.Contains(logged, "no matching files found") {
		t.Errorf("log = %q, want it to name the worktree and the loader error", logged)
	}

	// Once the retry window elapses the loader runs again, and a load that
	// succeeds clears the recorded failure.
	prev := loadRetryInterval
	loadRetryInterval = time.Nanosecond
	t.Cleanup(func() { loadRetryInterval = prev })

	st, err = m.Get(context.Background(), name)
	if err != nil || st == nil {
		t.Fatalf("Get after retry window = (%v, %v), want a loaded state", st, err)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Errorf("loader calls after retry window = %d, want 2", got)
	}
	if _, gotErr := m.LoadError(name); gotErr != nil {
		t.Errorf("LoadError after successful load = %v, want nil", gotErr)
	}
}

func TestMultiState_DefaultPrefersRootWorktree(t *testing.T) {
	root := newGitRepo(t)
	parent := filepath.Dir(root)
	extraPath := filepath.Join(parent, "feature-"+filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "feature-branch", extraPath)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", extraPath).Run()
	})

	var loadCount int64
	m := NewMultiState(extraPath, stubLoader(&loadCount))
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if got := m.Default(); got != filepath.Base(extraPath) {
		t.Fatalf("Default() = %q, want root worktree %q", got, filepath.Base(extraPath))
	}
}

func TestMultiState_GetByRefCachesBaseWorktree(t *testing.T) {
	root := newGitRepo(t)
	parent := filepath.Dir(root)
	extraPath := filepath.Join(parent, "feature-"+filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "feature-branch", extraPath)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", extraPath).Run()
	})

	var (
		mu     sync.Mutex
		counts = map[string]int{}
	)
	loader := func(_ context.Context, name, path string) (*State, error) {
		mu.Lock()
		counts[name]++
		mu.Unlock()
		return NewState(path), nil
	}
	m := NewMultiState(root, loader)
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	baseState, baseName, err := m.GetByRef(context.Background(), "main")
	if err != nil {
		t.Fatalf("GetByRef(main): %v", err)
	}
	if baseState == nil || baseName == "" {
		t.Fatalf("GetByRef(main) = (%v, %q), want state and name", baseState, baseName)
	}
	mu.Lock()
	if counts[baseName] != 1 {
		t.Fatalf("load count for base %q = %d, want 1", baseName, counts[baseName])
	}
	mu.Unlock()

	again, againName, err := m.GetByRef(context.Background(), "main")
	if err != nil {
		t.Fatalf("GetByRef(main) cached: %v", err)
	}
	if againName != baseName || again != baseState {
		t.Fatalf("cached base = (%p, %q), want (%p, %q)", again, againName, baseState, baseName)
	}
	byName, byNameRef, err := m.GetByRef(context.Background(), baseName)
	if err != nil {
		t.Fatalf("GetByRef(%q): %v", baseName, err)
	}
	if byNameRef != baseName || byName != baseState {
		t.Fatalf("base by name = (%p, %q), want (%p, %q)", byName, byNameRef, baseState, baseName)
	}
	missing, missingName, err := m.GetByRef(context.Background(), "missing")
	if err != nil {
		t.Fatalf("GetByRef(missing): %v", err)
	}
	if missing != nil || missingName != "" {
		t.Fatalf("missing ref = (%v, %q), want nil empty", missing, missingName)
	}

	mu.Lock()
	defer mu.Unlock()
	if counts[baseName] != 1 {
		t.Fatalf("load count for base %q after cached lookups = %d, want 1", baseName, counts[baseName])
	}
}

func TestMultiState_GetCoalescesConcurrentLoads(t *testing.T) {
	root := newGitRepo(t)

	var (
		loadCount    int64
		started      = make(chan struct{})
		release      = make(chan struct{})
		closeStarted sync.Once
	)
	loader := func(_ context.Context, _, path string) (*State, error) {
		atomic.AddInt64(&loadCount, 1)
		closeStarted.Do(func() { close(started) })
		<-release
		return NewState(path), nil
	}

	m := NewMultiState(root, loader)
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	names := m.Names()
	if len(names) != 1 {
		t.Fatalf("want 1 worktree, got %d: %v", len(names), names)
	}

	const callers = 8
	start := make(chan struct{})
	errs := make(chan error, callers)
	states := make(chan *State, callers)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			st, err := m.Get(context.Background(), names[0])
			if err != nil {
				errs <- err
				return
			}
			states <- st
		}()
	}

	close(start)
	<-started
	if got := atomic.LoadInt64(&loadCount); got != 1 {
		t.Fatalf("load count while first load is blocked = %d, want 1", got)
	}
	close(release)
	wg.Wait()
	close(errs)
	close(states)

	for err := range errs {
		t.Errorf("Get returned error: %v", err)
	}

	var first *State
	var gotStates int
	for st := range states {
		gotStates++
		if first == nil {
			first = st
			continue
		}
		if st != first {
			t.Fatalf("concurrent Get returned different states: %p and %p", first, st)
		}
	}
	if gotStates != callers {
		t.Fatalf("successful Get count = %d, want %d", gotStates, callers)
	}
	if got := atomic.LoadInt64(&loadCount); got != 1 {
		t.Fatalf("load count after concurrent Gets = %d, want 1", got)
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

// TestMultiState_EnsureKnownDiscoversPostStartupWorktree proves the
// refresh-on-miss path: a worktree created after the initial Refresh is
// invisible to Has but is picked up by EnsureKnown (which re-scans on a
// miss), while a name that never existed still resolves to false.
func TestMultiState_EnsureKnownDiscoversPostStartupWorktree(t *testing.T) {
	root := newGitRepo(t)

	m := NewMultiState(root, stubLoader(new(int64)))
	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(m.Names()) != 1 {
		t.Fatalf("want 1 worktree after initial Refresh, got %v", m.Names())
	}

	// Create a sibling worktree AFTER the initial Refresh — the daemon's
	// entry set does not know about it yet.
	parent := filepath.Dir(root)
	extraPath := filepath.Join(parent, "late-"+filepath.Base(root))
	runGit(t, root, "worktree", "add", "-b", "late-branch", extraPath)
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", extraPath).Run()
	})
	extraName := filepath.Base(extraPath)

	// Has does not see it (no re-discovery), reproducing the "unknown
	// worktree" symptom.
	if m.Has(extraName) {
		t.Fatalf("Has(%q) = true before re-discovery, want false", extraName)
	}

	// EnsureKnown re-scans on the miss and now resolves the new worktree.
	if !m.EnsureKnown(extraName) {
		t.Fatalf("EnsureKnown(%q) = false, want true after refresh-on-miss", extraName)
	}
	if !m.Has(extraName) {
		t.Fatalf("Has(%q) = false after EnsureKnown, want true", extraName)
	}

	// A name that never existed still returns false (and does not panic).
	if m.EnsureKnown("does-not-exist") {
		t.Fatalf("EnsureKnown(does-not-exist) = true, want false")
	}
	if m.EnsureKnown("") {
		t.Fatalf("EnsureKnown(\"\") = true, want false")
	}
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

// The loaded hook is how a transport learns that a worktree's model is ready,
// which is what lets it precompute an answer at daemon start rather than on the
// first request. It must fire once per successful load, carry the State that
// was loaded, and — because the work it exists for is slow by definition —
// never hold up the load or the callers waiting on it.
func TestMultiState_LoadedHookFiresOffTheLoadPath(t *testing.T) {
	root := newGitRepo(t)
	m := NewMultiState(root, stubLoader(new(int64)))

	var (
		hookMu sync.Mutex
		seen   []string
		states []*State
	)
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	m.SetLoadedHook(func(name string, state *State) {
		entered <- struct{}{}
		<-release // a hook is free to take its time
		hookMu.Lock()
		seen = append(seen, name)
		states = append(states, state)
		hookMu.Unlock()
	})

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	name := m.Default()

	loaded := make(chan *State, 1)
	go func() {
		state, err := m.Get(context.Background(), name)
		if err != nil {
			t.Errorf("Get: %v", err)
		}
		loaded <- state
	}()

	// The load completes while the hook is still running.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the loaded hook never ran")
	}
	var state *State
	select {
	case state = <-loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked hook held up the load")
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		hookMu.Lock()
		done := len(seen) == 1
		hookMu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the loaded hook never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}

	hookMu.Lock()
	defer hookMu.Unlock()
	if seen[0] != name {
		t.Errorf("hook name = %q, want %q", seen[0], name)
	}
	if states[0] != state {
		t.Error("hook got a different State than the one the load cached")
	}

	// A second Get is served from the cache, so the hook does not fire again.
	if _, err := m.Get(context.Background(), name); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(seen) != 1 {
		t.Errorf("hook calls = %d, want 1 — a cached Get is not a load", len(seen))
	}
}

// A load that fails caches nothing, so there is no ready model to act on.
func TestMultiState_LoadedHookSkipsAFailedLoad(t *testing.T) {
	root := newGitRepo(t)
	m := NewMultiState(root, func(context.Context, string, string) (*State, error) {
		return nil, errors.New("pattern ./...: no matching files found")
	})
	m.SetLogOut(io.Discard)

	var calls int64
	m.SetLoadedHook(func(string, *State) { atomic.AddInt64(&calls, 1) })

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, err := m.Get(context.Background(), m.Default()); err == nil {
		t.Fatal("Get = nil error, want the loader's failure")
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("hook calls = %d, want 0 — a failed load has no model to act on", got)
	}
}
