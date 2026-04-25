package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/plugin"
	"github.com/kgatilin/archai/internal/target"
)

// Host adapts a *State (plus a logger) into the plugin.Host
// interface. Plugins receive one of these from the daemon's bootstrap;
// it stays valid for the daemon's lifetime.
//
// Design: the adapter is a thin shell over State — almost every
// method delegates straight through. We keep it as its own type
// (rather than implementing plugin.Host on State directly) because
//   - it lets us scope a per-plugin slog.Logger
//   - it lets us add caching/throttling later without touching State
//   - State stays free of any plugin-specific concerns at the public
//     API surface (snap-based reads remain primary).
type Host struct {
	state  *State
	logger *slog.Logger
}

// NewHost returns a Host backed by state. logger is the slog logger
// used for Host.Logger(); pass slog.Default() if no scoped logger is
// available.
func NewHost(state *State, logger *slog.Logger) *Host {
	if logger == nil {
		logger = slog.Default()
	}
	return &Host{state: state, logger: logger}
}

// CurrentModel implements plugin.Host.
func (h *Host) CurrentModel() *plugin.Model {
	if h == nil || h.state == nil {
		return nil
	}
	return h.state.CurrentModel()
}

// Targets implements plugin.Host. It enumerates locked targets under
// .arch/targets/ and returns a sorted slice. The list is rebuilt on
// every call (plugins call this rarely; targets are stable on disk).
func (h *Host) Targets() []plugin.TargetMeta {
	if h == nil || h.state == nil {
		return nil
	}
	metas, err := target.List(h.state.Root())
	if err != nil {
		h.logger.Warn("plugin: list targets", "err", err)
		return nil
	}
	out := make([]plugin.TargetMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, plugin.TargetMeta{
			ID:          m.ID,
			BaseCommit:  m.BaseCommit,
			CreatedAt:   m.CreatedAt,
			Description: m.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Target implements plugin.Host. It loads the target's frozen model
// from .arch/targets/<id>/ and pairs it with the lock metadata.
func (h *Host) Target(id string) (*plugin.TargetSnapshot, error) {
	if h == nil || h.state == nil {
		return nil, errors.New("plugin: host has no state")
	}
	if id == "" {
		return nil, errors.New("plugin: target id is empty")
	}
	root := h.state.Root()

	meta, _, err := target.Show(root, id)
	if err != nil {
		return nil, err
	}

	pkgs, err := loadTargetSnapshotModel(context.Background(), root, id)
	if err != nil {
		return nil, err
	}

	// Targets carry their own overlay; reuse the active overlay for
	// layer/aggregate metadata so plugins see a consistent shape.
	// Future work: load .arch/targets/<id>/overlay.yaml when present
	// and feed it into BuildModel for full provenance.
	cfg := h.snapshotOverlay()
	module := ""
	if cfg != nil {
		module = cfg.Module
	}
	model := plugin.BuildModel(module, pkgs, cfg)

	return &plugin.TargetSnapshot{
		Meta: plugin.TargetMeta{
			ID:          meta.ID,
			BaseCommit:  meta.BaseCommit,
			CreatedAt:   meta.CreatedAt,
			Description: meta.Description,
		},
		Model: model,
	}, nil
}

// ActiveTarget implements plugin.Host. Returns nil when no target is
// active; an error fetching the snapshot is logged (and nil returned)
// since callers can't usefully react to it.
func (h *Host) ActiveTarget() *plugin.TargetSnapshot {
	if h == nil || h.state == nil {
		return nil
	}
	id := h.state.CurrentTarget()
	if id == "" {
		return nil
	}
	snap, err := h.Target(id)
	if err != nil {
		h.logger.Warn("plugin: load active target", "id", id, "err", err)
		return nil
	}
	return snap
}

// Diff implements plugin.Host. fromID/toID may be "" to mean "current
// code model". The implementation matches `archai diff` semantics:
// fromID is the "from" side (defaults to current code), toID is the
// "to" side (defaults to CURRENT target).
func (h *Host) Diff(fromID, toID string) (*plugin.Diff, error) {
	if h == nil || h.state == nil {
		return nil, errors.New("plugin: host has no state")
	}
	root := h.state.Root()
	ctx := context.Background()

	from, err := h.modelByID(ctx, root, fromID)
	if err != nil {
		return nil, fmt.Errorf("plugin: load from %q: %w", fromID, err)
	}
	to, err := h.modelByID(ctx, root, toID)
	if err != nil {
		return nil, fmt.Errorf("plugin: load to %q: %w", toID, err)
	}
	d := diff.Compute(from, to)
	return d, nil
}

// Validate implements plugin.Host. modelID is the target id to
// validate against; "" defaults to CURRENT.
func (h *Host) Validate(modelID string) (*plugin.ValidationReport, error) {
	if h == nil || h.state == nil {
		return nil, errors.New("plugin: host has no state")
	}
	root := h.state.Root()
	id := modelID
	if id == "" {
		cur, err := target.Current(root)
		if err != nil {
			return nil, fmt.Errorf("plugin: read CURRENT: %w", err)
		}
		if cur == "" {
			return nil, errors.New("plugin: no target specified and no CURRENT target")
		}
		id = cur
	}

	d, err := h.Diff("", id)
	if err != nil {
		return nil, err
	}
	report := &plugin.ValidationReport{
		TargetID: id,
		OK:       d.IsEmpty(),
	}
	if !report.OK {
		report.Violations = append(report.Violations, d.Changes...)
	}
	return report, nil
}

// Subscribe implements plugin.Host.
func (h *Host) Subscribe(handler func(plugin.ModelEvent)) plugin.Unsubscribe {
	if h == nil || h.state == nil {
		return func() {}
	}
	return h.state.Bus().Subscribe(handler)
}

// Logger implements plugin.Host.
func (h *Host) Logger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}
	return h.logger
}

// snapshotOverlay returns a copy of the active overlay config.
func (h *Host) snapshotOverlay() *overlay.Config {
	snap := h.state.Snapshot()
	return snap.Overlay
}

// modelByID resolves a Diff side: "" means current code, "<id>" means
// the named target.
func (h *Host) modelByID(ctx context.Context, root, id string) ([]domain.PackageModel, error) {
	if id == "" {
		// Use State's live packages as the "current" view so plugins
		// see exactly what fsnotify-driven extraction last produced.
		snap := h.state.Snapshot()
		return snap.Packages, nil
	}
	return loadTargetSnapshotModel(ctx, root, id)
}

// loadTargetSnapshotModel mirrors cmd/archai's loadTargetModel: it
// reads .arch/targets/<id>/model/**/*.yaml via the YAML adapter.
// Lives in serve so plugins don't have to reimplement target loading.
func loadTargetSnapshotModel(ctx context.Context, root, id string) ([]domain.PackageModel, error) {
	targetDir := filepath.Join(root, ".arch", "targets", id)
	if _, err := os.Stat(targetDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("target %q not found", id)
		}
		return nil, err
	}
	modelDir := filepath.Join(targetDir, "model")
	files, err := collectYAMLFilesUnder(modelDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("target %q has no model files under %s", id, modelDir)
	}
	return yamlAdapter.NewReader().Read(ctx, files)
}

func collectYAMLFilesUnder(root string) ([]string, error) {
	var out []string
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
