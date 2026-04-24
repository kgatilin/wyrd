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

	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
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
}

// NewState returns an empty State rooted at root. Callers must invoke
// Load before querying the state.
func NewState(root string) *State {
	return &State{
		root:     root,
		reader:   golang.NewReader(),
		packages: make(map[string]domain.PackageModel),
	}
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

	return nil
}

// ReloadPackage re-extracts the single package identified by its
// module-relative path (e.g. "internal/serve") and splices the result
// into the in-memory model. A package that no longer exists is removed.
func (s *State) ReloadPackage(ctx context.Context, pkgPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pkgPath == "" {
		return fmt.Errorf("serve: empty package path")
	}

	pattern := "./" + strings.TrimPrefix(pkgPath, "./")
	if pkgPath == "." {
		pattern = "./"
	}

	// The Go reader resolves relative patterns against cwd; scope the
	// change to this call so caller-visible cwd is untouched.
	prevCwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("serve: resolving cwd: %w", err)
	}
	if err := os.Chdir(s.root); err != nil {
		return fmt.Errorf("serve: chdir %s: %w", s.root, err)
	}
	defer func() { _ = os.Chdir(prevCwd) }()

	models, err := s.reader.Read(ctx, []string{pattern})
	if err != nil {
		// Package may have been removed; drop it and return no error so
		// the watcher loop stays alive.
		delete(s.packages, pkgPath)
		return nil
	}

	// Remove the old entry so stale data cannot leak if the reader
	// returned zero packages (deleted/empty dir).
	delete(s.packages, pkgPath)
	for _, m := range models {
		s.packages[m.Path] = m
	}
	return nil
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

// readAll runs the Go reader over the whole project. Extracted into a
// helper so Load can be kept readable.
func (s *State) readAll(ctx context.Context) ([]domain.PackageModel, error) {
	// We resolve paths relative to the configured root by temporarily
	// changing working directory. go/packages loads relative patterns
	// based on cwd; the project may be anywhere on disk.
	prevCwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(s.root); err != nil {
		return nil, err
	}
	defer func() { _ = os.Chdir(prevCwd) }()

	return s.reader.Read(ctx, []string{"./..."})
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
