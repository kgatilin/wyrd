package serve

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"sync"

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

	// loader is the factory that builds a fresh State for a worktree.
	loader StateLoader

	// watcherHook, when non-nil, is invoked the first time a State is
	// loaded for a worktree. The returned closer is tracked in
	// watchers and released on Refresh-drop / Close.
	watcherHook WatcherHook

	// watchers tracks per-worktree closers registered by watcherHook.
	watchers map[string]io.Closer

	// baseRef is the review-base git ref (e.g. "main") used for
	// diff-scoped analysis. When non-empty, every loaded worktree State is
	// wired with a resolver that loads this ref's models on demand. Empty
	// disables base resolution (the diff tool reports "no base configured").
	baseRef string
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
// State. The resolver loads the base ref's worktree State lazily and returns
// its package snapshot; when the State being wired *is* the base worktree, it
// resolves to nil so a diff against self is empty rather than recursive.
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
		bs, baseName, err := m.GetByRef(ctx, baseRef)
		if err != nil || bs == nil {
			return nil, err
		}
		if baseName == thisName {
			return nil, nil // this State is the base; no self-diff
		}
		return bs.Snapshot().Packages, nil
	})
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
		loader:   loader,
		watchers: make(map[string]io.Closer),
	}
}

// SetWatcherHook installs a WatcherHook that will be invoked the next
// time a worktree's State is loaded. It is safe to call before Refresh;
// already-loaded states are not retroactively watched.
func (m *MultiState) SetWatcherHook(hook WatcherHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watcherHook = hook
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

// ensureLoad guarantees a load for name is cached, in flight, or freshly
// started. It returns either the cached State (load complete) or the
// in-flight stateLoad handle to wait on — never both. The expensive load
// runs in a background goroutine under a process-lifetime context, decoupled
// from any request context so indexing is not cancelled when the triggering
// request returns.
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
	m.mu.Unlock()

	// Wire the base-model resolver so diff-scoped tools on this worktree can
	// reach the review base. No-op when no base ref is configured.
	m.wireBaseResolver(name, loaded)

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

func (m *MultiState) finishLoad(name string, load *stateLoad, state *State, err error) {
	m.mu.Lock()
	if m.loading[name] == load {
		delete(m.loading, name)
	}
	load.state = state
	load.err = err
	close(load.done)
	m.mu.Unlock()
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
