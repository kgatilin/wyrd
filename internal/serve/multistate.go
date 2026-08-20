package serve

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/kgatilin/archai/internal/adapter/git"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/worktree"
)

// StateLoader builds a fresh State for a newly-discovered worktree.
// It is invoked the first time a worktree's State is requested from a
// MultiState; subsequent requests hit the per-worktree cache. The
// default production loader is DefaultStateLoader, which calls
// NewState(path).Load(ctx).
type StateLoader func(ctx context.Context, name, path string) (*State, error)

// WatcherHook is invoked by MultiState the first time a worktree's
// State is loaded. Implementations typically spin up a per-worktree
// fsnotify watcher whose handler re-extracts the loaded State on file
// changes. The returned io.Closer is tracked by MultiState and closed
// when the worktree is dropped from a Refresh or when Close is called.
// A nil hook disables per-worktree watching (used by lightweight tests
// and by callers that prefer manual refreshes).
type WatcherHook func(ctx context.Context, name string, state *State) (io.Closer, error)

// LoadedHook is invoked once a worktree's State has been loaded and wired.
// It exists so a transport can start work that needs the parsed model — a
// subscription to the State's event bus, an answer precomputed before anyone
// asks for it — at the moment the model becomes available, without MultiState
// learning what that work is. Each call runs on its own goroutine, so a hook
// that does real work cannot hold up the load or the callers waiting on it.
type LoadedHook func(name string, state *State)

// DefaultStateLoader is the production StateLoader used when
// NewMultiState is called without one. It builds a fresh State
// anchored at path and loads the full Go + overlay + target model.
func DefaultStateLoader(ctx context.Context, _, path string) (*State, error) {
	state := NewState(path)
	if err := state.Load(ctx); err != nil {
		return nil, err
	}
	return state, nil
}

// MultiState holds one State per discovered worktree and lazy-loads
// each State on first access. The set of worktrees is fixed at
// construction but can be refreshed by calling Refresh() again — this
// is intended for SIGHUP or a periodic poller.
//
// MultiState is safe for concurrent use.
type MultiState struct {
	mu sync.Mutex

	// root is the project root (one of the discovered worktrees, used
	// as the anchor for git worktree list).
	root string

	// entries is the current list of known worktrees, keyed by Name.
	entries map[string]worktree.Entry

	// order is the lexical list of names (matches entries).
	order []string

	// defaultName is the worktree matching root. It is preferred over
	// first-alphabetical so `archai serve --repo .` opens the worktree
	// the user actually started from.
	defaultName string

	// states is the lazy-loaded State for each worktree name.
	states map[string]*State

	// loading tracks in-flight State loads by worktree name. Without
	// this, concurrent first requests for the same worktree can run the
	// expensive go/packages load more than once and discard the loser.
	loading map[string]*stateLoad

	// failures records the most recent failed load per worktree. A failed
	// load caches nothing, so without this the failure would be invisible
	// (transports keep reporting "parsing") and every poll would restart
	// the doomed load. Cleared when a load for that name succeeds or when
	// the retry window expires.
	failures map[string]loadFailure

	// logOut receives load diagnostics. nil means os.Stderr; the daemon
	// wires its own log sink via SetLogOut.
	logOut io.Writer

	// loader is the factory that builds a fresh State for a worktree.
	loader StateLoader

	// watcherHook, when non-nil, is invoked the first time a State is
	// loaded for a worktree. The returned closer is tracked in
	// watchers and released on Refresh-drop / Close.
	watcherHook WatcherHook

	// loadedHook, when non-nil, is invoked once per successful load so a
	// transport can act on a freshly available model.
	loadedHook LoadedHook

	// watchers tracks per-worktree closers registered by watcherHook.
	watchers map[string]io.Closer

	// baseTrees caches the materialized+parsed base commits (see
	// ReviewBase). Created on first use so a daemon that never diffs
	// never touches the cache directory.
	baseTrees *baseTrees

	// baseRef is the review-base git ref (e.g. "main") used for
	// diff-scoped analysis. When non-empty, every loaded worktree State is
	// wired with a resolver that loads this ref's models on demand. Empty
	// disables base resolution (the diff tool reports "no base configured").
	baseRef string

	// refreshMu serializes miss-driven re-discovery (see EnsureKnown) so
	// concurrent requests for an as-yet-unknown worktree collapse into a
	// single `git worktree list` scan. It is distinct from mu, which guards
	// the entry/state maps; Refresh takes mu internally.
	refreshMu sync.Mutex

	// lastRefresh is the wall-clock time of the most recent miss-driven
	// Refresh, used to debounce git invocations when an unknown name is
	// requested repeatedly. Guarded by refreshMu.
	lastRefresh time.Time
}

// refreshDebounce bounds how often a miss-driven re-discovery shells out to
// git. Within this window a second miss reuses the last scan instead of
// running `git worktree list` again, so repeatedly requesting a name that
// truly does not exist cannot stampede git.
const refreshDebounce = 2 * time.Second

// loadRetryInterval bounds how often a worktree whose load failed is retried.
// A failed load installs no fsnotify watcher, so nothing in the worktree can
// trigger a reload — this window is the only recovery path short of a daemon
// restart, which is why it is short. It is also what stops a transport that
// polls every couple of seconds from re-running the (slow, doomed) load on
// every request. A var, not a const, so package-internal tests can shrink it.
var loadRetryInterval = 30 * time.Second

// loadFailure is the recorded outcome of a background load that failed.
type loadFailure struct {
	err error
	at  time.Time
}

// SetReviewBaseRef configures the review-base ref injected into every
// worktree State as its base-model resolver. Safe to call once at daemon
// startup before States are materialized; States loaded afterwards pick it
// up, and already-loaded States are re-wired.
func (m *MultiState) SetReviewBaseRef(ref string) {
	m.mu.Lock()
	m.baseRef = ref
	states := make([]struct {
		name  string
		state *State
	}, 0, len(m.states))
	for name, st := range m.states {
		states = append(states, struct {
			name  string
			state *State
		}{name, st})
	}
	m.mu.Unlock()
	for _, s := range states {
		m.wireBaseResolver(s.name, s.state)
	}
}

// wireBaseResolver injects (or clears) the base-model resolver on a loaded
// State. The resolver returns the models of the *merge base* (see
// ReviewBase), so every consumer of a review diff — the canvas, the public
// surface, the MCP diff tool — answers "what this branch changed" rather
// than "how this branch differs from wherever the base has got to".
func (m *MultiState) wireBaseResolver(name string, state *State) {
	m.mu.Lock()
	baseRef := m.baseRef
	m.mu.Unlock()
	if baseRef == "" {
		state.setBaseResolver(nil)
		return
	}
	thisName := name
	state.setBaseResolver(func(ctx context.Context) ([]domain.PackageModel, error) {
		base, err := m.ReviewBase(ctx, thisName, "")
		if err != nil {
			return nil, err
		}
		return base.Models, nil
	})
}

// ReviewBase is the resolved answer to "what is this worktree reviewed
// against". Models is nil when there is nothing to diff — no base ref
// configured, the ref does not exist, or the worktree already *is* the base
// commit with nothing uncommitted on top.
type ReviewBase struct {
	// Models is the package model of the base commit's tree.
	Models []domain.PackageModel
	// Ref is the configured review base ref (e.g. "main").
	Ref string
	// Rev is the merge base of Ref and the worktree's HEAD — the commit the
	// models above were parsed from. Empty when it could not be resolved.
	Rev string
	// Worktree names the checkout sitting on Ref, when there is one. It is
	// used for labels and to mark the base in the branch picker; the models
	// come from Rev, which is often an older commit than that checkout.
	Worktree string
}

// ReviewBase resolves the review base for the named worktree: the tree at
// merge-base(ref, worktree HEAD). An empty ref means the daemon's configured
// review base; callers that carry their own (the HTTP surfaces accept
// ?base=) pass it explicitly.
//
// Two fast paths avoid parsing a second tree, because parsing is the whole
// cost here:
//
//   - the base worktree is clean and sits exactly on the merge base (the
//     usual state right after a rebase) — its already-loaded model is the
//     commit's model, so reuse it;
//   - this worktree is clean and sits on the merge base itself — there is
//     nothing to diff.
//
// Otherwise the base commit is materialized and parsed once, and cached.
func (m *MultiState) ReviewBase(ctx context.Context, name, ref string) (ReviewBase, error) {
	baseRef := ref
	if baseRef == "" {
		m.mu.Lock()
		baseRef = m.baseRef
		m.mu.Unlock()
	}
	if baseRef == "" {
		return ReviewBase{}, nil
	}
	entry, ok := m.Entry(name)
	if !ok || entry.Path == "" {
		return ReviewBase{Ref: baseRef}, nil
	}

	out := ReviewBase{Ref: baseRef}
	if baseName, ok := m.FindRef(baseRef); ok {
		out.Worktree = baseName
	}

	rev, ok := git.MergeBase(entry.Path, baseRef)
	if !ok {
		// No such ref (or unrelated histories): report the base identity we
		// have, but never fall back to the base worktree's tip — a wrong
		// base reads as a large, confident, backwards diff.
		return out, nil
	}
	out.Rev = rev

	if out.Worktree != "" && out.Worktree != name {
		if baseEntry, ok := m.Entry(out.Worktree); ok && baseEntry.Path != "" {
			if git.HeadRev(baseEntry.Path) == rev && git.IsClean(baseEntry.Path) {
				state, err := m.Get(ctx, out.Worktree)
				if err != nil {
					return out, err
				}
				out.Models = state.Snapshot().Packages
				return out, nil
			}
		}
	}

	if git.HeadRev(entry.Path) == rev && git.IsClean(entry.Path) {
		return out, nil // this worktree is the base commit; nothing to diff
	}

	trees, err := m.ensureBaseTrees()
	if err != nil {
		return out, err
	}
	models, err := trees.Models(ctx, entry.Path, rev)
	if err != nil {
		return out, err
	}
	out.Models = models
	return out, nil
}

// ensureBaseTrees lazily creates the materialized-base cache. It is keyed by
// the repository's main worktree root so every worktree of a repo shares one
// cache — branches off the same base commit then pay for it once.
func (m *MultiState) ensureBaseTrees() (*baseTrees, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.baseTrees != nil {
		return m.baseTrees, nil
	}
	repoRoot := m.root
	if main, ok := worktree.RepoRoot(m.root); ok {
		repoRoot = main
	}
	dir, err := BaseTreeDir(repoRoot)
	if err != nil {
		return nil, err
	}
	m.baseTrees = newBaseTrees(dir, defaultBaseTreeLimit)
	return m.baseTrees, nil
}

// NewMultiState constructs a MultiState rooted at projectRoot, using
// loader to materialize per-worktree States on first access. Pass
// DefaultStateLoader for normal daemon use; lightweight alternatives
// are useful for transport-level tests that want to assert lazy
// behaviour without invoking the Go reader.
//
// The initial worktree list is populated by Refresh(); callers are
// expected to invoke Refresh() once before serving requests.
func NewMultiState(projectRoot string, loader StateLoader) *MultiState {
	if loader == nil {
		loader = DefaultStateLoader
	}
	return &MultiState{
		root:     projectRoot,
		entries:  make(map[string]worktree.Entry),
		states:   make(map[string]*State),
		loading:  make(map[string]*stateLoad),
		failures: make(map[string]loadFailure),
		loader:   loader,
		watchers: make(map[string]io.Closer),
	}
}

// SetLogOut directs load diagnostics (failed worktree loads) at w. The daemon
// passes its own log sink so a broken worktree shows up in the same stream as
// the rest of the daemon's output instead of on a bare os.Stderr.
func (m *MultiState) SetLogOut(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logOut = w
}

func (m *MultiState) logf(format string, args ...any) {
	m.mu.Lock()
	out := m.logOut
	m.mu.Unlock()
	if out == nil {
		out = os.Stderr
	}
	fmt.Fprintf(out, format, args...)
}

// SetWatcherHook installs a WatcherHook that will be invoked the next
// time a worktree's State is loaded. It is safe to call before Refresh;
// already-loaded states are not retroactively watched.
func (m *MultiState) SetWatcherHook(hook WatcherHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watcherHook = hook
}

// SetLoadedHook installs a LoadedHook invoked after each successful worktree
// load. Safe to call before Refresh; already-loaded states are not replayed,
// so install it before anything can trigger a load.
func (m *MultiState) SetLoadedHook(hook LoadedHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadedHook = hook
}

// Refresh re-discovers worktrees via `git worktree list --porcelain`
// and replaces the internal entry set. Previously-loaded States for
// worktrees that still exist are retained (so lazy caches survive a
// refresh); States for removed worktrees are dropped, and any per-
// worktree watchers registered via WatcherHook are closed.
//
// Refresh returns an error when two discovered worktrees share the
// same basename (e.g. /a/proj and /b/proj). The operator is expected
// to rename or relocate one of them; silent last-write-wins would
// hide one worktree from all transports.
func (m *MultiState) Refresh() error {
	entries, err := worktree.Discover(m.root)
	if err != nil {
		return fmt.Errorf("serve: discover worktrees: %w", err)
	}
	m.mu.Lock()

	next := make(map[string]worktree.Entry, len(entries))
	order := make([]string, 0, len(entries))
	defaultName := ""
	for _, e := range entries {
		if e.Name == "" {
			continue
		}
		if prev, dup := next[e.Name]; dup {
			m.mu.Unlock()
			return fmt.Errorf(
				"serve: duplicate worktree name %q (paths %q and %q) — rename one worktree directory to disambiguate",
				e.Name, prev.Path, e.Path,
			)
		}
		next[e.Name] = e
		order = append(order, e.Name)
		if defaultName == "" && sameDiscoveredPath(e.Path, m.root) {
			defaultName = e.Name
		}
	}
	sort.Strings(order)
	if defaultName == "" && len(order) > 0 {
		defaultName = order[0]
	}
	m.entries = next
	m.order = order
	m.defaultName = defaultName

	// Drop cached states whose worktrees have disappeared, and close
	// any watchers they held. We collect closers under the lock and
	// close them after releasing it so a slow Close cannot deadlock
	// callers of MultiState.
	var toClose []io.Closer
	for name := range m.states {
		if _, ok := next[name]; !ok {
			delete(m.states, name)
			if c, ok := m.watchers[name]; ok && c != nil {
				toClose = append(toClose, c)
			}
			delete(m.watchers, name)
		}
	}
	// Forget failures for worktrees that no longer exist, so a worktree
	// removed and re-created is retried immediately rather than inheriting
	// the old checkout's throttled failure.
	for name := range m.failures {
		if _, ok := next[name]; !ok {
			delete(m.failures, name)
		}
	}
	m.mu.Unlock()

	for _, c := range toClose {
		_ = c.Close()
	}
	return nil
}

// Close releases every per-worktree watcher tracked by the MultiState.
// Safe to call multiple times; states themselves are not mutated.
func (m *MultiState) Close() error {
	m.mu.Lock()
	closers := make([]io.Closer, 0, len(m.watchers))
	for _, c := range m.watchers {
		if c != nil {
			closers = append(closers, c)
		}
	}
	m.watchers = make(map[string]io.Closer)
	m.mu.Unlock()

	var firstErr error
	for _, c := range closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Worktrees returns the discovered entries in lexical order.
func (m *MultiState) Worktrees() []worktree.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]worktree.Entry, 0, len(m.order))
	for _, n := range m.order {
		out = append(out, m.entries[n])
	}
	return out
}

// Names returns the discovered worktree names in lexical order.
func (m *MultiState) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

// Has reports whether name is a known worktree.
func (m *MultiState) Has(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.entries[name]
	return ok
}

// EnsureKnown reports whether name is a discovered worktree, re-scanning
// worktrees once (debounced) when it is not yet known. This lets the daemon
// pick up worktrees created after startup without a restart: the entry set is
// treated as a cache of the git worktree list, and the first request naming a
// new worktree invalidates it via a re-discovery, after which the name
// resolves normally. A genuinely-nonexistent name still returns false, but at
// most one git scan runs per refreshDebounce window regardless of how often it
// is requested.
func (m *MultiState) EnsureKnown(name string) bool {
	if name == "" {
		return false
	}
	if m.Has(name) {
		return true
	}
	m.refreshOnMiss()
	return m.Has(name)
}

// refreshOnMiss runs a debounced, serialized Refresh. refreshMu serializes
// miss-driven refreshes so concurrent misses collapse into a single git scan
// (the second caller sees the just-updated lastRefresh and skips). Refresh
// errors are swallowed: a failed re-scan leaves the entry set unchanged and
// the caller falls through to "unknown worktree" as before.
func (m *MultiState) refreshOnMiss() {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if !m.lastRefresh.IsZero() && time.Since(m.lastRefresh) < refreshDebounce {
		return
	}
	_ = m.Refresh()
	m.lastRefresh = time.Now()
}

// FindRef resolves a worktree by its UI/server ref. The ref may be a
// discovered worktree name or a git branch name such as "main".
func (m *MultiState) FindRef(ref string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ref == "" {
		return "", false
	}
	if _, ok := m.entries[ref]; ok {
		return ref, true
	}
	for _, name := range m.order {
		if m.entries[name].Branch == ref {
			return name, true
		}
	}
	return "", false
}

// Default returns the first worktree name in lexical order, or ""
// when no worktrees have been discovered.
func (m *MultiState) Default() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.defaultName
}

// Get returns (and lazy-loads) the State for the given worktree name,
// blocking until the load completes. Returns an error when name is
// unknown or when the underlying load fails. Subsequent calls return
// the cached State. Callers that must not block on the cold parse
// (e.g. the MCP tools/call transport) should use Loaded instead.
func (m *MultiState) Get(ctx context.Context, name string) (*State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	state, load, err := m.ensureLoad(name)
	if err != nil {
		return nil, err
	}
	if state != nil {
		return state, nil
	}
	return load.wait(ctx)
}

// Loaded returns the State for name if it is already loaded, otherwise it
// kicks off the (idempotent) background load and returns (nil, false)
// immediately — the caller should report "loading" rather than block. This
// keeps the cold go/packages parse off the request goroutine so a transport
// like MCP tools/call can answer "still loading" instead of timing out.
// Unknown names also return (nil, false); callers gate on Has() first.
func (m *MultiState) Loaded(name string) (*State, bool) {
	state, _, err := m.ensureLoad(name)
	if err != nil || state == nil {
		return nil, false
	}
	return state, true
}

// LoadError reports the most recent failed load for name: the time at which
// the load will next be retried and the error. A zero-value return (zero
// time, nil) means the worktree is loaded, loading, or has never been asked
// for. Transports use it to answer "failed" instead of an eternal "parsing".
func (m *MultiState) LoadError(name string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.failures[name]
	if !ok {
		return time.Time{}, nil
	}
	return f.at.Add(loadRetryInterval), f.err
}

// ensureLoad guarantees a load for name is cached, in flight, or freshly
// started. It returns either the cached State (load complete) or the
// in-flight stateLoad handle to wait on — never both. The expensive load
// runs in a background goroutine under a process-lifetime context, decoupled
// from any request context so indexing is not cancelled when the triggering
// request returns.
//
// A worktree whose last load failed inside loadRetryInterval short-circuits
// with the recorded error rather than starting another load: nothing about
// the worktree has changed, and the transports poll far faster than the load
// takes.
func (m *MultiState) ensureLoad(name string) (*State, *stateLoad, error) {
	m.mu.Lock()
	entry, ok := m.entries[name]
	if !ok {
		m.mu.Unlock()
		return nil, nil, fmt.Errorf("serve: unknown worktree %q", name)
	}
	if s, ok := m.states[name]; ok {
		m.mu.Unlock()
		return s, nil, nil
	}
	if load, ok := m.loading[name]; ok {
		m.mu.Unlock()
		return nil, load, nil
	}
	if f, ok := m.failures[name]; ok {
		if time.Since(f.at) < loadRetryInterval {
			m.mu.Unlock()
			return nil, nil, f.err
		}
		// Retry window elapsed: drop the record before starting the new
		// load so callers see "loading" again rather than a stale failure.
		delete(m.failures, name)
	}
	load := &stateLoad{done: make(chan struct{})}
	m.loading[name] = load
	m.mu.Unlock()

	go m.runLoad(name, entry, load)
	return nil, load, nil
}

// runLoad performs the expensive worktree load (parse + index kickoff + base
// resolver + watcher) and resolves load. It runs in its own goroutine, so
// loads for different worktrees never serialize and same-name concurrent
// requests dedup via m.loading.
func (m *MultiState) runLoad(name string, entry worktree.Entry, load *stateLoad) {
	ctx := context.Background()
	loaded, err := m.loader(ctx, name, entry.Path)
	if err != nil {
		m.finishLoad(name, load, nil, err)
		return
	}

	m.mu.Lock()
	current, stillKnown := m.entries[name]
	if !stillKnown || current.Path != entry.Path {
		m.mu.Unlock()
		m.finishLoad(name, load, nil, fmt.Errorf("serve: worktree %q changed while loading", name))
		return
	}
	m.states[name] = loaded
	hook := m.watcherHook
	loadedHook := m.loadedHook
	m.mu.Unlock()

	// Wire the base-model resolver so diff-scoped tools on this worktree can
	// reach the review base. No-op when no base ref is configured.
	m.wireBaseResolver(name, loaded)

	// Review UI config (review_views / review_groups) follows the primary
	// checkout, not the branch's possibly-stale archai.yaml copy.
	loaded.SetReviewConfigRoot(m.root)

	// The State is committed and fully wired, so anything keyed off "this
	// worktree's model is ready" can start. Its own goroutine: the hook is
	// free to do the real work that makes it worth having.
	if loadedHook != nil {
		go loadedHook(name, loaded)
	}

	// Spin up the per-worktree watcher. If the hook fails we keep the loaded
	// state (the transport is still usable — just without auto-reload) and
	// record the error on the load; the daemon logs it and carries on.
	if hook != nil {
		closer, herr := hook(ctx, name, loaded)
		if herr != nil {
			m.finishLoad(name, load, loaded, fmt.Errorf("serve: watcher hook for %q: %w", name, herr))
			return
		}
		if closer != nil {
			m.mu.Lock()
			m.watchers[name] = closer
			m.mu.Unlock()
		}
	}
	m.finishLoad(name, load, loaded, nil)
}

// finishLoad resolves load and records the outcome. A load that produced no
// State is a failure: it is remembered (so LoadError can report it and
// ensureLoad can throttle the retry) and logged once, because nothing else in
// the daemon would ever mention it — the handle is discarded here.
func (m *MultiState) finishLoad(name string, load *stateLoad, state *State, err error) {
	m.mu.Lock()
	if m.loading[name] == load {
		delete(m.loading, name)
	}
	failed := state == nil && err != nil
	if failed {
		m.failures[name] = loadFailure{err: err, at: time.Now()}
	} else {
		// A State that loaded but whose watcher hook failed still carries a
		// non-nil err; it is usable, so it must not be recorded as a failure.
		delete(m.failures, name)
	}
	load.state = state
	load.err = err
	close(load.done)
	m.mu.Unlock()

	if failed {
		m.logf("serve: loading worktree %q failed: %v (retrying in at most %s)\n", name, err, loadRetryInterval)
	}
}

type stateLoad struct {
	done  chan struct{}
	state *State
	err   error
}

func (l *stateLoad) wait(ctx context.Context) (*State, error) {
	select {
	case <-l.done:
		return l.state, l.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetByRef resolves ref with FindRef and returns the cached State for
// that worktree, loading it once if necessary. A missing ref is not an
// error; callers can use this for optional review bases such as "main".
func (m *MultiState) GetByRef(ctx context.Context, ref string) (*State, string, error) {
	name, ok := m.FindRef(ref)
	if !ok {
		return nil, "", nil
	}
	state, err := m.Get(ctx, name)
	if err != nil {
		return nil, name, err
	}
	return state, name, nil
}

// Entry returns the Entry for the named worktree (path, branch, …).
// Returns false when name is unknown.
func (m *MultiState) Entry(name string) (worktree.Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	return e, ok
}

func sameDiscoveredPath(a, b string) bool {
	a = normalizeDiscoveredPath(a)
	b = normalizeDiscoveredPath(b)
	return a != "" && a == b
}

func normalizeDiscoveredPath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}
