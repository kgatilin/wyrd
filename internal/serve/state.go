// Package serve implements the long-running `wyrd serve` daemon: an
// in-memory model of the project (extracted Go packages + overlay config +
// active target id) kept current via an fsnotify watcher.
//
// The daemon is the backbone for future MCP stdio (M5b) and HTTP (M7a)
// transports; those transports read from the shared State via Snapshot()
// and are wired through Serve() in daemon.go.
package serve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	"time"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/overlay"
	"github.com/kgatilin/wyrd/internal/plugin"
	"github.com/kgatilin/wyrd/internal/retrieval"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
	"github.com/kgatilin/wyrd/internal/service"
	"github.com/kgatilin/wyrd/internal/target"
)

// State holds the in-memory model consumed by the daemon's transports.
// All reads go through Snapshot(); mutations go through the Reload*/Switch
// methods which take an exclusive lock. State is safe for concurrent use.
type State struct {
	mu sync.RWMutex

	// root is the project root directory (absolute path).
	root string

	// reader is the Go model reader. Stored so incremental reloads can
	// re-use the same concrete reader (and its cached module path).
	reader service.ModelReader

	// packages is the current extracted model, keyed by PackageModel.Path.
	// Each entry is a Version: a model the store holds is the store's
	// canonical instance, shared with every other State, so it is read-only.
	packages map[string]modelstore.Version

	// stamps are the content stamps of the tree, taken before the read that
	// produced packages. Sync compares them with the disk.
	stamps treeStamps

	// readMu serializes the operations that read the tree and replace the
	// model (Sync, Reload, ReloadOver), so a read over the current model is
	// never installed over a model replaced while it ran.
	readMu sync.Mutex

	// store is the repository's shared model store. Nil means every cold
	// start is a full parse and nothing is shared or persisted.
	store *modelstore.Store

	// baseRef is the ref whose merge base a cold start loads over when the
	// store has no snapshot of HEAD. Empty: only HEAD's snapshot is tried.
	baseRef string

	// overlayCfg is the parsed wyrd.yaml (may be nil if absent).
	overlayCfg *overlay.Config

	// overlayPath is the on-disk wyrd.yaml location used for reloads.
	// Empty when no overlay was found on Load.
	overlayPath string

	// goModPath is the adjacent go.mod used for overlay validation.
	goModPath string

	// reviewConfigRoot, when non-empty and different from root, points at the
	// primary worktree checkout whose wyrd.yaml supplies the review UI
	// config (review_views + review_groups) for this State. Architecture
	// semantics (layers, policy, contexts, …) stay per-worktree; presentation
	// config follows the primary checkout so stale branches don't pin an old
	// review UI. Set by MultiState after load.
	reviewConfigRoot string

	// currentTarget is the active target id (may be empty).
	currentTarget string

	// bus broadcasts ModelEvents to plugin subscribers. Lazily
	// constructed on first access via Bus(); shared by every Host
	// adapter built from this State.
	bus *plugin.EventBus

	// retrieval is this worktree's view of the corpus. May be nil if
	// retrieval is disabled.
	retrieval *retrieval.Service

	// corpus is the repository's retrieval corpus, shared with the daemon's
	// other worktree States. Nil: the State builds a private in-memory one.
	corpus *retrieval.Corpus

	// retrievalReady is closed when the initial retrieval indexing completes.
	// Nil if retrieval is disabled.
	retrievalReady chan struct{}

	// baseResolver, when non-nil, returns the base-branch (review base)
	// package models for diff-scoped analysis. Injected by MultiState, which
	// holds the configured base ref and can load the base worktree on
	// demand. Nil for standalone States (no multi-worktree context) and for
	// the base State itself, where it resolves to nil (no self-diff).
	baseResolver func(ctx context.Context) ([]domain.PackageModel, error)
}

// setBaseResolver wires the base-branch model resolver. Called by MultiState
// after a worktree State is loaded.
func (s *State) setBaseResolver(f func(ctx context.Context) ([]domain.PackageModel, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseResolver = f
}

// BaseModels returns the review-base (e.g. "main") package models for diff
// against this State's current snapshot. Returns (nil, nil) when no base is
// configured (standalone daemon, or this State *is* the base) — callers treat
// that as "no base available", not an error.
func (s *State) BaseModels(ctx context.Context) ([]domain.PackageModel, error) {
	s.mu.RLock()
	f := s.baseResolver
	s.mu.RUnlock()
	if f == nil {
		return nil, nil
	}
	return f(ctx)
}

// Bus returns the State's plugin event bus, creating it on first
// access. Callers (the daemon's reload handler) publish ModelEvents
// here after each successful state mutation; plugins subscribe via
// Host.Subscribe.
func (s *State) Bus() *plugin.EventBus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bus == nil {
		s.bus = plugin.NewEventBus()
	}
	return s.bus
}

// PublishPackageReload broadcasts a ModelEventKindPackageReload event.
// Called by the watcher's batch handler after a successful package
// reload. paths are module-relative package paths.
func (s *State) PublishPackageReload(paths []string) {
	s.Bus().Publish(plugin.ModelEvent{
		Kind:  plugin.ModelEventKindPackageReload,
		Paths: append([]string(nil), paths...),
		At:    time.Now(),
	})
}

// PublishOverlayReload broadcasts a ModelEventKindOverlayReload event.
func (s *State) PublishOverlayReload() {
	s.Bus().Publish(plugin.ModelEvent{
		Kind: plugin.ModelEventKindOverlayReload,
		At:   time.Now(),
	})
}

// PublishTargetSwitch broadcasts a ModelEventKindTargetSwitch event
// carrying the new active target id.
func (s *State) PublishTargetSwitch(id string) {
	s.Bus().Publish(plugin.ModelEvent{
		Kind:   plugin.ModelEventKindTargetSwitch,
		Target: id,
		At:     time.Now(),
	})
}

// CurrentModel returns the unified Model assembled from the State's
// current packages + overlay. Safe for concurrent use; the returned
// Model is a snapshot detached from State (callers may retain it but
// should re-call CurrentModel after a ModelEvent).
func (s *State) CurrentModel() *plugin.Model {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pkgs := modelsOf(s.packages)

	var cfgCopy *overlay.Config
	module := ""
	if s.overlayCfg != nil {
		cp := *s.overlayCfg
		cfgCopy = &cp
		module = cp.Module
	}
	return plugin.BuildModel(module, pkgs, cfgCopy)
}

// CurrentTarget returns the active target id (may be empty).
func (s *State) CurrentTarget() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentTarget
}

// StateOption configures optional fields on a freshly-constructed State.
type StateOption func(*State)

// WithReader replaces the reader the State would construct for
// itself. Used by the CLI so every State the daemon builds shares one
// reader instance.
func WithReader(r service.ModelReader) StateOption {
	return func(s *State) { s.reader = r }
}

// WithModelStore shares the repository's model store with this State. A cold
// start then loads the snapshot of HEAD, or else of merge-base(baseRef, HEAD),
// with the packages the worktree's diff against it touches read on top, and a
// full parse of a clean worktree is recorded as the snapshot of its HEAD.
// Every State the daemon builds for one repository must receive the same
// store: sharing is what it is for.
func WithModelStore(store *modelstore.Store, baseRef string) StateOption {
	return func(s *State) {
		s.store = store
		s.baseRef = baseRef
	}
}

// WithCorpus shares the repository's retrieval corpus with this State, whose
// retrieval service becomes one view over it. Every State the daemon builds
// for the same repository must receive the same corpus, so a text is indexed
// and embedded once for all of them.
func WithCorpus(c *retrieval.Corpus) StateOption {
	return func(s *State) { s.corpus = c }
}

// WithRetrieval sets a pre-configured retrieval service. Used in tests
// to inject a retrieval service without triggering full Load.
func WithRetrieval(svc *retrieval.Service) StateOption {
	return func(s *State) { s.retrieval = svc }
}

// NewStateWithRetrieval is a convenience constructor for tests that
// creates a State with a pre-configured retrieval service.
func NewStateWithRetrieval(root string, svc *retrieval.Service) *State {
	return NewState(root, WithRetrieval(svc))
}

// NewState returns an empty State rooted at root. Callers must invoke
// Load before querying the state.
//
// The default reader is the Go-only reader from adapter/golang, which
// preserves existing behaviour for tests and Go projects. Pass
// WithReader to swap in a multi-language pipeline.
func NewState(root string, opts ...StateOption) *State {
	s := &State{
		root:     root,
		reader:   golang.NewReader(),
		packages: make(map[string]modelstore.Version),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Snapshot is a read-only view of the State. All slices/maps are
// independent of the live State and safe to retain.
//
// Packages are shared with the State and with every other holder of the same
// package Version: the slice is the caller's, the models in it are read-only.
type Snapshot struct {
	Root     string
	Packages []domain.PackageModel
	// Versions maps each package path to the hash of its Version, the key a
	// content-addressed index can share across worktrees.
	Versions      map[string]string
	Overlay       *overlay.Config
	CurrentTarget string
}

// Snapshot returns a consistent read-only copy of the state.
func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pkgs := make([]domain.PackageModel, 0, len(s.packages))
	versions := make(map[string]string, len(s.packages))
	for path, v := range s.packages {
		pkgs = append(pkgs, v.Model)
		versions[path] = v.Hash
	}

	var cfgCopy *overlay.Config
	if s.overlayCfg != nil {
		cp := *s.overlayCfg
		cfgCopy = &cp
	}

	return Snapshot{
		Root:          s.root,
		Packages:      pkgs,
		Versions:      versions,
		Overlay:       cfgCopy,
		CurrentTarget: s.currentTarget,
	}
}

// Root returns the project root directory.
func (s *State) Root() string {
	return s.root
}

// Load performs the initial extraction: Go packages (see coldStart),
// wyrd.yaml overlay (if present), and the active target id
// (.arch/targets/CURRENT). Errors extracting packages are returned; a
// missing overlay or CURRENT file is not an error.
//
// Load also initializes the retrieval service and starts background
// indexing. Dense embedding (Ollama) runs in a background goroutine
// to avoid blocking serve startup. BM25 indexing is synchronous (fast).
func (s *State) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stamps, err := scanStamps(s.root)
	if err != nil {
		return fmt.Errorf("serve: scanning sources: %w", err)
	}
	versions, err := s.coldStart(ctx)
	if err != nil {
		return fmt.Errorf("serve: loading packages: %w", err)
	}
	s.packages = byPath(versions)
	s.stamps = stamps
	models := modelsOf(s.packages)

	// Overlay: best-effort. Missing file → leave nil.
	if err := s.reloadOverlayLocked(); err != nil {
		return fmt.Errorf("serve: loading overlay: %w", err)
	}

	// CURRENT target: best-effort.
	cur, err := target.Current(s.root)
	if err != nil {
		return fmt.Errorf("serve: reading CURRENT: %w", err)
	}
	s.currentTarget = cur

	// Initialize retrieval service with indexes
	s.initRetrievalLocked(ctx, models)

	return nil
}

// initRetrievalLocked builds this worktree's view of the corpus and starts
// the background embedding pass. The view itself — hashes, lexical documents
// — is built here, under the lock, so no later update can be overtaken by it;
// only the embedder's work runs in the background.
// Must be called with mu held.
func (s *State) initRetrievalLocked(ctx context.Context, models []domain.PackageModel) {
	if os.Getenv("WYRD_RETRIEVAL_DISABLE") == "1" {
		return
	}

	corpus := s.corpus
	if corpus == nil {
		corpus = privateCorpus()
	}
	svc := retrieval.NewService(s.root, corpus)
	nodes, graph := retrieval.BuildGraph(models)
	svc.SetGraph(graph)
	if err := svc.Update(ctx, nodes, nil); err != nil {
		fmt.Fprintf(os.Stderr, "serve: retrieval indexing: %v\n", err)
	}

	s.retrieval = svc
	s.retrievalReady = make(chan struct{})
	ready := s.retrievalReady
	go func() {
		defer close(ready)
		s.embedAndSave(context.WithoutCancel(ctx), svc)
	}()
}

// embedAndSave runs the view's embedding pass and persists the corpus.
func (s *State) embedAndSave(ctx context.Context, svc *retrieval.Service) {
	if err := svc.EmbedPending(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "serve: retrieval embedding: %v\n", err)
	}
	if err := svc.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "serve: saving retrieval corpus: %v\n", err)
	}
}

// Retrieval returns the retrieval service, or nil if not initialized.
func (s *State) Retrieval() *retrieval.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retrieval
}

// RetrievalReady returns a channel that is closed when initial retrieval indexing completes.
// Returns nil if retrieval is disabled.
func (s *State) RetrievalReady() <-chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retrievalReady
}

// Reload re-reads the whole worktree from disk — every package, the overlay
// and the active target — and reports whether anything differs from what the
// State held. It is the answer to a missed file event: the watcher keeps the
// model current one package at a time, and when an event never arrives
// (a share that does not forward it, a watch the kernel refused) nothing
// else ever looks at the disk again. The read is a full parse, never a
// stored snapshot, and like every full parse of a clean worktree it is
// recorded as the snapshot of HEAD. Retrieval is refreshed for the packages
// that changed only.
func (s *State) Reload(ctx context.Context) (bool, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	stamps, err := scanStamps(s.root)
	if err != nil {
		return false, fmt.Errorf("serve: scanning sources: %w", err)
	}
	// The parse is the slow part and needs no lock; holding it would stall
	// every reader of the model for the length of a full read.
	versions, err := s.readAll(ctx)
	if err != nil {
		return false, fmt.Errorf("serve: reloading packages: %w", err)
	}
	_, changed, err := s.replaceModel(ctx, versions, stamps)
	return changed, err
}

// ReloadOver rebuilds the model as base with the packages in changed read
// from disk. It is Refresh on a branch: base is the model of the commit the
// branch is reviewed against, changed is every package the branch's diff
// touches, and a package the diff does not touch is by definition the
// base's. So a Refresh costs the branch's packages rather than a parse of
// the repository. A changed package with no Go source left on disk is
// dropped; one the reader fails on fails the whole call and leaves the model
// as it was.
func (s *State) ReloadOver(ctx context.Context, base []modelstore.Version, changed []string) (bool, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	stamps, err := scanStamps(s.root)
	if err != nil {
		return false, fmt.Errorf("serve: scanning sources: %w", err)
	}
	versions, err := s.readOver(ctx, base, changed)
	if err != nil {
		return false, err
	}
	_, moved, err := s.replaceModel(ctx, versions, stamps)
	return moved, err
}

// coldStart reads the model a State starts with. With a store, a snapshot of
// HEAD — or, failing that, of the merge base with baseRef — is the base, and
// only the packages the worktree's diff against it touches are read from
// disk. Everything else is a full parse: no store, no snapshot, a diff that
// touches go.mod or go.sum, or a diff git cannot produce.
func (s *State) coldStart(ctx context.Context) ([]modelstore.Version, error) {
	base, rev := s.storedBase()
	if base == nil {
		return s.readAll(ctx)
	}
	d, err := git.Diff(s.root, rev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: diff of %s against %s, parsing in full: %v\n", s.root, short(rev), err)
		return s.readAll(ctx)
	}
	changed, full := changedPackages(d.Files)
	if full {
		return s.readAll(ctx)
	}
	return s.readOver(ctx, base, changed)
}

// storedBase returns the stored snapshot a cold start can build on and its
// rev: HEAD's when the store has one, else the merge base's with baseRef.
func (s *State) storedBase() ([]modelstore.Version, string) {
	if s.store == nil {
		return nil, ""
	}
	head := git.HeadRev(s.root)
	if head == "" {
		return nil, ""
	}
	revs := []string{head}
	if s.baseRef != "" {
		if mb, ok := git.MergeBase(s.root, s.baseRef); ok && mb != head {
			revs = append(revs, mb)
		}
	}
	for _, rev := range revs {
		versions, ok, err := s.store.Snapshot(rev)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			continue
		}
		if ok {
			return versions, rev
		}
	}
	return nil, ""
}

// readAll parses the whole worktree. When the worktree is clean, and still
// clean on the same HEAD once the parse is done, the result is recorded as
// the snapshot of HEAD, which is how a branch gets its base without a parse.
func (s *State) readAll(ctx context.Context) ([]modelstore.Version, error) {
	head := s.cleanHead()
	models, err := s.readPackages(ctx, []string{"./..."})
	if err != nil {
		return nil, err
	}
	if head != "" && s.cleanHead() == head {
		versions, err := s.store.Record(head, models)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
		return versions, nil
	}
	return s.store.Versions(models), nil
}

// cleanHead returns HEAD when the worktree matches it exactly and a store is
// there to record a snapshot of it, and "" otherwise.
func (s *State) cleanHead() string {
	if s.store == nil {
		return ""
	}
	head := git.HeadRev(s.root)
	if head == "" || !git.IsClean(s.root) {
		return ""
	}
	return head
}

// readOver returns base with the packages in changed read from disk over it.
// A changed package with no Go source left on disk is dropped; the read gets
// the rest of the model as known, so interface dispatch into base packages
// keeps its edges.
func (s *State) readOver(ctx context.Context, base []modelstore.Version, changed []string) ([]modelstore.Version, error) {
	onDisk, err := goPackageDirs(s.root)
	if err != nil {
		return nil, fmt.Errorf("serve: scanning packages: %w", err)
	}
	drop := make(map[string]bool, len(changed))
	var read []string
	for _, pkgPath := range changed {
		drop[pkgPath] = true
		if onDisk[pkgPath] {
			read = append(read, pkgPath)
		}
	}
	versions := make([]modelstore.Version, 0, len(base)+len(read))
	known := make([]domain.PackageModel, 0, len(base))
	for _, v := range base {
		if !drop[v.Model.Path] {
			versions = append(versions, v)
			known = append(known, v.Model)
		}
	}
	if len(read) > 0 {
		sort.Strings(read)
		fresh, err := s.readPackageSet(ctx, read, known)
		// A changed directory the reader cannot model (another module, every
		// file excluded by build tags) is what a full read skips too.
		if errors.Is(err, service.ErrNothingToModel) {
			fresh, err = nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("serve: reloading changed packages: %w", err)
		}
		versions = append(versions, s.store.Versions(fresh)...)
	}
	return versions, nil
}

// replaceModel installs versions as the whole model and stamps as the tree
// they were read from, re-reads the overlay and the active target, and
// refreshes retrieval for the packages that differ. It returns those packages
// and whether anything moved, the overlay and the target included.
func (s *State) replaceModel(ctx context.Context, versions []modelstore.Version, stamps treeStamps) ([]string, bool, error) {
	cur, err := target.Current(s.root)
	if err != nil {
		return nil, false, fmt.Errorf("serve: reading CURRENT: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := byPath(versions)
	var moved []string
	var changedNodes []retrieval.Node
	var removedIDs []string
	for path, old := range s.packages {
		m, ok := next[path]
		if ok && old.Hash == m.Hash {
			continue
		}
		moved = append(moved, path)
		var newIDs map[string]bool
		if ok {
			nodes := retrieval.BuildNodes([]domain.PackageModel{m.Model})
			changedNodes = append(changedNodes, nodes...)
			newIDs = make(map[string]bool, len(nodes))
			for _, n := range nodes {
				newIDs[n.ID] = true
			}
		}
		for _, n := range retrieval.BuildNodes([]domain.PackageModel{old.Model}) {
			if !newIDs[n.ID] {
				removedIDs = append(removedIDs, n.ID)
			}
		}
	}
	for path, m := range next {
		if _, ok := s.packages[path]; !ok {
			moved = append(moved, path)
			changedNodes = append(changedNodes, retrieval.BuildNodes([]domain.PackageModel{m.Model})...)
		}
	}
	sort.Strings(moved)

	oldOverlay, oldTarget := s.overlayCfg, s.currentTarget
	if err := s.reloadOverlayLocked(); err != nil {
		return nil, false, fmt.Errorf("serve: reloading overlay: %w", err)
	}
	s.currentTarget = cur
	s.packages = next
	s.stamps = stamps

	if len(moved) > 0 {
		s.refreshRetrievalLocked(ctx, changedNodes, removedIDs)
	}
	return moved, len(moved) > 0 || cur != oldTarget || !reflect.DeepEqual(oldOverlay, s.overlayCfg), nil
}

// refreshRetrievalLocked updates the view for changed nodes and rebuilds its
// graph, then embeds in the background. Embedding waits for the corpus's one
// pass at a time, possibly behind another worktree's cold pass, so it must not
// run under this State's lock.
// Must be called with mu held.
func (s *State) refreshRetrievalLocked(ctx context.Context, changedNodes []retrieval.Node, removedIDs []string) {
	if s.retrieval == nil {
		return
	}
	if err := s.retrieval.Update(ctx, changedNodes, removedIDs); err != nil {
		fmt.Fprintf(os.Stderr, "serve: retrieval refresh: %v\n", err)
		return
	}
	_, graph := retrieval.BuildGraph(modelsOf(s.packages))
	s.retrieval.SetGraph(graph)
	go s.embedAndSave(context.WithoutCancel(ctx), s.retrieval)
}

// ReloadOverlay re-reads wyrd.yaml from disk and updates the cached
// config. Missing files clear the cached config without returning an
// error (the daemon can run without an overlay).
func (s *State) ReloadOverlay(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadOverlayLocked()
}

// SwitchTarget updates the active target id. An empty id clears it
// (equivalent to removing .arch/targets/CURRENT).
func (s *State) SwitchTarget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentTarget = id
	return nil
}

// reloadOverlayLocked reloads the overlay assuming the caller holds the
// write lock. A missing overlay document clears the cached config.
func (s *State) reloadOverlayLocked() error {
	overlayPath := overlay.DiscoverPath(s.root)
	goModPath := filepath.Join(s.root, "go.mod")
	if overlayPath == "" {
		// No overlay on disk: clear cache, not an error. Review UI config can
		// still come from the primary checkout.
		s.overlayCfg = s.withPrimaryReviewConfig(nil)
		s.overlayPath = ""
		s.goModPath = ""
		return nil
	}
	cfg, err := overlay.LoadComposed(overlayPath)
	if err != nil {
		return err
	}
	s.overlayCfg = s.withPrimaryReviewConfig(cfg)
	s.overlayPath = overlayPath
	if _, err := os.Stat(goModPath); err == nil {
		s.goModPath = goModPath
	} else {
		s.goModPath = ""
	}
	return nil
}

// SetReviewConfigRoot points this State at the primary worktree whose
// wyrd.yaml owns the review UI config, then re-applies the overlay so the
// override takes effect immediately. A root equal to this State's own root
// (or empty) is a no-op.
func (s *State) SetReviewConfigRoot(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if root == "" || root == s.root {
		s.reviewConfigRoot = ""
		return
	}
	s.reviewConfigRoot = root
	_ = s.reloadOverlayLocked() // keep prior overlay error semantics: best-effort
}

// withPrimaryReviewConfig overlays the review UI fields (review_views,
// review_groups) from the primary checkout's wyrd.yaml onto cfg. When the
// primary overlay is missing or unreadable, cfg is returned unchanged — a
// stale-but-working branch config beats none.
func (s *State) withPrimaryReviewConfig(cfg *overlay.Config) *overlay.Config {
	if s.reviewConfigRoot == "" {
		return cfg
	}
	primaryPath := overlay.DiscoverPath(s.reviewConfigRoot)
	if primaryPath == "" {
		return cfg
	}
	primary, err := overlay.LoadComposed(primaryPath)
	if err != nil || primary == nil {
		return cfg
	}
	if len(primary.ReviewViews) == 0 && len(primary.ReviewGroups) == 0 {
		return cfg
	}
	if cfg == nil {
		return &overlay.Config{
			ReviewViews:  primary.ReviewViews,
			ReviewGroups: primary.ReviewGroups,
		}
	}
	merged := *cfg
	merged.ReviewViews = primary.ReviewViews
	merged.ReviewGroups = primary.ReviewGroups
	return &merged
}

// modelsOf returns the models of a State's package map, in a slice the caller
// owns (the models in it are shared).
func modelsOf(in map[string]modelstore.Version) []domain.PackageModel {
	out := make([]domain.PackageModel, 0, len(in))
	for _, v := range in {
		out = append(out, v.Model)
	}
	return out
}

// versionList returns the State's Versions sorted by package path.
func (s *State) versionList() []modelstore.Version {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]modelstore.Version, 0, len(s.packages))
	for _, v := range s.packages {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model.Path < out[j].Model.Path })
	return out
}

func byPath(versions []modelstore.Version) map[string]modelstore.Version {
	out := make(map[string]modelstore.Version, len(versions))
	for _, v := range versions {
		out[v.Model.Path] = v
	}
	return out
}
