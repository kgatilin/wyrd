// Package serve implements the long-running `archai serve` daemon: an
// in-memory model of the project (extracted Go packages + overlay config +
// active target id) kept current via an fsnotify watcher.
//
// The daemon is the backbone for future MCP stdio (M5b) and HTTP (M7a)
// transports; those transports read from the shared State via Snapshot()
// and are wired through Serve() in daemon.go.
package serve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"time"

	"github.com/kgatilin/archai/internal/adapter/embed/noop"
	"github.com/kgatilin/archai/internal/adapter/embed/ollama"
	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/adapter/lindex/bm25"
	"github.com/kgatilin/archai/internal/adapter/vindex/brute"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/plugin"
	"github.com/kgatilin/archai/internal/retrieval"
	"github.com/kgatilin/archai/internal/service"
	"github.com/kgatilin/archai/internal/target"
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
	packages map[string]domain.PackageModel

	// overlayCfg is the parsed archai.yaml (may be nil if absent).
	overlayCfg *overlay.Config

	// overlayPath is the on-disk archai.yaml location used for reloads.
	// Empty when no overlay was found on Load.
	overlayPath string

	// goModPath is the adjacent go.mod used for overlay validation.
	goModPath string

	// currentTarget is the active target id (may be empty).
	currentTarget string

	// bus broadcasts ModelEvents to plugin subscribers. Lazily
	// constructed on first access via Bus(); shared by every Host
	// adapter built from this State.
	bus *plugin.EventBus

	// retrieval is the code search service holding dense+BM25 indexes.
	// May be nil if retrieval is disabled.
	retrieval *retrieval.Service

	// retrievalReady is closed when the initial retrieval indexing completes.
	// Nil if retrieval is disabled.
	retrievalReady chan struct{}
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

	pkgs := make([]domain.PackageModel, 0, len(s.packages))
	for _, p := range s.packages {
		pkgs = append(pkgs, p)
	}

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

// WithReader replaces the default Go-only reader with a multi-language
// reader. Used by the CLI to wire `archai serve` through the same
// service.Service-based dispatch as `archai diagram generate`, so Java
// (and any other future language) projects load correctly.
func WithReader(r service.ModelReader) StateOption {
	return func(s *State) { s.reader = r }
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
		packages: make(map[string]domain.PackageModel),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Snapshot is a read-only view of the State. All slices/maps are
// independent of the live State and safe to retain.
type Snapshot struct {
	Root          string
	Packages      []domain.PackageModel
	Overlay       *overlay.Config
	CurrentTarget string
}

// Snapshot returns a consistent read-only copy of the state.
func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pkgs := make([]domain.PackageModel, 0, len(s.packages))
	for _, p := range s.packages {
		pkgs = append(pkgs, p)
	}

	var cfgCopy *overlay.Config
	if s.overlayCfg != nil {
		cp := *s.overlayCfg
		cfgCopy = &cp
	}

	return Snapshot{
		Root:          s.root,
		Packages:      pkgs,
		Overlay:       cfgCopy,
		CurrentTarget: s.currentTarget,
	}
}

// Root returns the project root directory.
func (s *State) Root() string {
	return s.root
}

// Load performs the initial full extraction: Go packages under ./...,
// archai.yaml overlay (if present), and the active target id
// (.arch/targets/CURRENT). Errors extracting packages are returned; a
// missing overlay or CURRENT file is not an error.
//
// Load also initializes the retrieval service and starts background
// indexing. Dense embedding (Ollama) runs in a background goroutine
// to avoid blocking serve startup. BM25 indexing is synchronous (fast).
func (s *State) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	models, err := s.readAll(ctx)
	if err != nil {
		return fmt.Errorf("serve: loading packages: %w", err)
	}

	s.packages = make(map[string]domain.PackageModel, len(models))
	for _, m := range models {
		s.packages[m.Path] = m
	}

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

// initRetrievalLocked sets up the retrieval service and starts background indexing.
// Must be called with mu held.
func (s *State) initRetrievalLocked(ctx context.Context, models []domain.PackageModel) {
	// Skip retrieval initialization if disabled
	if os.Getenv("ARCHAI_RETRIEVAL_DISABLE") == "1" {
		return
	}

	// Build embedder - try Ollama, fall back to noop
	emb := buildEmbedder()

	// Create indexes
	vidx := brute.New(emb.ID(), emb.Dim())
	lidx := bm25.New()

	// Create service
	svc := retrieval.NewService(s.root, emb, vidx, lidx)

	// Try to load persisted indexes
	if err := svc.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "serve: loading retrieval indexes: %v\n", err)
	}

	s.retrieval = svc
	s.retrievalReady = make(chan struct{})

	// BM25 indexing is synchronous (fast)
	// Dense indexing happens in background (may be slow with Ollama)
	// IndexFromModels builds both nodes and graph adjacency
	go func() {
		defer close(s.retrievalReady)

		if err := svc.IndexFromModels(ctx, models); err != nil {
			fmt.Fprintf(os.Stderr, "serve: retrieval indexing: %v\n", err)
			return
		}

		if err := svc.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "serve: saving retrieval indexes: %v\n", err)
		}
	}()
}

// buildEmbedder creates an embedder, trying Ollama first then falling back to noop.
func buildEmbedder() retrieval.Embedder {
	// Check if Ollama should be disabled
	if os.Getenv("ARCHAI_EMBED_DISABLE") == "1" {
		return noop.New()
	}
	// Default to Ollama (it will fail gracefully if not running)
	return ollama.New()
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

// ReloadPackage re-extracts the single package identified by its
// module-relative path (e.g. "internal/serve") and splices the result
// into the in-memory model. A package that no longer exists is removed.
// Also refreshes the retrieval indexes for changed/removed nodes.
func (s *State) ReloadPackage(ctx context.Context, pkgPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pkgPath == "" {
		return fmt.Errorf("serve: empty package path")
	}

	// Collect old node IDs for this package (to detect removals)
	var oldNodeIDs []string
	if oldPkg, ok := s.packages[pkgPath]; ok {
		oldNodes := retrieval.BuildNodes([]domain.PackageModel{oldPkg})
		for _, n := range oldNodes {
			oldNodeIDs = append(oldNodeIDs, n.ID)
		}
	}

	models, err := s.readPackageSet(ctx, []string{pkgPath})
	if err != nil {
		// Package may have been removed; drop it and return no error so
		// the watcher loop stays alive.
		delete(s.packages, pkgPath)
		if err := s.writeModelCache(mapValues(s.packages)); err != nil {
			fmt.Fprintf(os.Stderr, "serve: write model cache: %v\n", err)
		}
		// Refresh retrieval: all old nodes are now removed
		s.refreshRetrievalLocked(ctx, nil, oldNodeIDs)
		return nil
	}

	// Remove the old entry so stale data cannot leak if the reader
	// returned zero packages (deleted/empty dir).
	delete(s.packages, pkgPath)
	for _, m := range models {
		s.packages[m.Path] = m
	}
	if err := s.writeModelCache(mapValues(s.packages)); err != nil {
		fmt.Fprintf(os.Stderr, "serve: write model cache: %v\n", err)
	}

	// Refresh retrieval indexes
	newNodes := retrieval.BuildNodes(models)

	// Compute removed IDs: old IDs that are not in new nodes
	newNodeSet := make(map[string]bool)
	for _, n := range newNodes {
		newNodeSet[n.ID] = true
	}
	var removedIDs []string
	for _, id := range oldNodeIDs {
		if !newNodeSet[id] {
			removedIDs = append(removedIDs, id)
		}
	}

	s.refreshRetrievalLocked(ctx, newNodes, removedIDs)
	return nil
}

// refreshRetrievalLocked updates the retrieval indexes for changed nodes.
// Must be called with mu held.
func (s *State) refreshRetrievalLocked(ctx context.Context, changedNodes []retrieval.Node, removedIDs []string) {
	if s.retrieval == nil {
		return
	}
	if err := s.retrieval.Refresh(ctx, changedNodes, removedIDs); err != nil {
		fmt.Fprintf(os.Stderr, "serve: retrieval refresh: %v\n", err)
		return
	}
	if err := s.retrieval.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "serve: saving retrieval indexes: %v\n", err)
	}
	// Rebuild the graph from the full model for expand/search_graph operations
	allModels := mapValues(s.packages)
	_, graph := retrieval.BuildGraph(allModels)
	s.retrieval.SetGraph(graph)
}

// ReloadOverlay re-reads archai.yaml from disk and updates the cached
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
// write lock. Missing archai.yaml clears the cached config.
func (s *State) reloadOverlayLocked() error {
	overlayPath := filepath.Join(s.root, "archai.yaml")
	goModPath := filepath.Join(s.root, "go.mod")
	if _, err := os.Stat(overlayPath); err != nil {
		// No overlay on disk: clear cache, not an error.
		s.overlayCfg = nil
		s.overlayPath = ""
		s.goModPath = ""
		return nil
	}
	cfg, err := overlay.LoadComposed(overlayPath)
	if err != nil {
		return err
	}
	s.overlayCfg = cfg
	s.overlayPath = overlayPath
	if _, err := os.Stat(goModPath); err == nil {
		s.goModPath = goModPath
	} else {
		s.goModPath = ""
	}
	return nil
}

func mapValues(in map[string]domain.PackageModel) []domain.PackageModel {
	out := make([]domain.PackageModel, 0, len(in))
	for _, value := range in {
		out = append(out, value)
	}
	return out
}

// FindOwningPackage walks up from the directory of path until it finds
// a directory containing .go files, then converts that absolute
// directory into a module-relative package path by stripping the root
// prefix. Returns "" when no owning package is found (e.g. the change
// was outside the project root or in a non-Go directory tree).
func (s *State) FindOwningPackage(path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.root, path)
	}
	dir := filepath.Dir(abs)
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		rootAbs = s.root
	}

	for {
		if !strings.HasPrefix(dir, rootAbs) {
			return ""
		}
		if hasGoFiles(dir) {
			rel, err := filepath.Rel(rootAbs, dir)
			if err != nil {
				return ""
			}
			if rel == "." {
				return "."
			}
			return filepath.ToSlash(rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// hasGoFiles reports whether dir contains at least one .go file
// (excluding _test.go files is unnecessary here — we only need to know
// the directory is a Go package for reload purposes).
func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".go" {
			return true
		}
	}
	return false
}
