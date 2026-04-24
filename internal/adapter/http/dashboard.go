package http

import (
	"context"
	"fmt"
	nethttp "net/http"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/mod/modfile"

	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
)

// dashboardData is the full data model for the dashboard page. Each
// section is computed best-effort — a missing overlay or absent target
// is not an error, it just leaves the corresponding section empty.
type dashboardData struct {
	pageData

	Module    string // Module path, e.g. "github.com/kgatilin/archai"
	GoVersion string // Go toolchain directive from go.mod, e.g. "1.25.1"

	CurrentTarget string // Active target id (empty if none locked)
	HasTarget     bool   // True when CurrentTarget is non-empty
	DriftStatus   string // "matches", "drifted", "unknown" (no target), or "error"
	DriftCount    int    // Number of changes when drifted
	DriftMessage  string // Free-form explanation (errors, "no target set", etc.)

	PackageCount   int
	TypeCount      int // structs + interfaces + typedefs
	FunctionCount  int
	InterfaceCount int

	// HasLayerMap is true when the overlay declares at least one layer;
	// the dashboard then renders a small client-side Cytoscape preview
	// that fetches /api/layers/mini. Previously this was a server-side
	// D2→SVG render; M8 (#46) moved it to the browser.
	HasLayerMap bool
}

// handleDashboard renders the dashboard at "/". It composes a
// dashboardData from a fresh state Snapshot plus the on-disk go.mod
// and (optionally) the active target snapshot.
func (s *Server) handleDashboard(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.URL.Path != "/" {
		nethttp.NotFound(w, r)
		return
	}

	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	data := dashboardData{
		pageData: s.basePageData(r, "Dashboard", "/"),
	}

	// Module + Go version from go.mod (best-effort — empty on failure).
	if snap.Overlay != nil && snap.Overlay.Module != "" {
		data.Module = snap.Overlay.Module
	}
	if module, goVer, ok := readGoModInfo(filepath.Join(snap.Root, "go.mod")); ok {
		if data.Module == "" {
			data.Module = module
		}
		data.GoVersion = goVer
	}

	// Counts across packages.
	for _, p := range snap.Packages {
		data.PackageCount++
		data.InterfaceCount += len(p.Interfaces)
		data.FunctionCount += len(p.Functions)
		data.TypeCount += len(p.Structs) + len(p.Interfaces) + len(p.TypeDefs)
	}

	// Target + drift status.
	data.CurrentTarget = snap.CurrentTarget
	data.HasTarget = snap.CurrentTarget != ""
	switch {
	case !data.HasTarget:
		data.DriftStatus = "unknown"
		data.DriftMessage = "no target selected (lock one with `archai target lock`)"
	default:
		status, count, msg := computeDrift(r.Context(), snap.Root, snap.CurrentTarget, snap.Packages)
		data.DriftStatus = status
		data.DriftCount = count
		data.DriftMessage = msg
	}

	// Layer map preview — only render the <div> when the overlay
	// defines layers; the browser fetches /api/layers/mini to hydrate it.
	if snap.Overlay != nil && len(snap.Overlay.Layers) > 0 {
		data.HasLayerMap = true
	}

	s.renderPage(w, "index.html", data)
}

// readGoModInfo returns (module, goVersion, ok) for the go.mod at
// path. ok=false when the file is missing or unparseable; callers
// should treat that as "no data" rather than a fatal error.
func readGoModInfo(path string) (string, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	f, err := modfile.Parse(filepath.Base(path), data, nil)
	if err != nil {
		return "", "", false
	}
	var module, goVer string
	if f.Module != nil {
		module = f.Module.Mod.Path
	}
	if f.Go != nil {
		goVer = f.Go.Version
	}
	return module, goVer, true
}

// computeDrift compares the in-memory current model against the locked
// target snapshot on disk. Returns a status ("matches" / "drifted" /
// "error"), a change count, and a human-readable message.
func computeDrift(ctx context.Context, root, targetID string, current []domain.PackageModel) (string, int, string) {
	if targetID == "" {
		return "unknown", 0, "no target selected"
	}
	targetDir := filepath.Join(root, ".arch", "targets", targetID)
	modelDir := filepath.Join(targetDir, "model")
	if _, err := os.Stat(modelDir); err != nil {
		return "error", 0, fmt.Sprintf("target %q has no model on disk", targetID)
	}
	files, err := collectTargetYAMLFiles(modelDir)
	if err != nil {
		return "error", 0, fmt.Sprintf("reading target: %v", err)
	}
	if len(files) == 0 {
		return "error", 0, fmt.Sprintf("target %q has no model files", targetID)
	}
	targetModel, err := yamlAdapter.NewReader().Read(ctx, files)
	if err != nil {
		return "error", 0, fmt.Sprintf("parsing target: %v", err)
	}
	d := diff.Compute(current, targetModel)
	if d.IsEmpty() {
		return "matches", 0, "current code matches target"
	}
	return "drifted", len(d.Changes), fmt.Sprintf("%d change(s) between current code and target", len(d.Changes))
}

// collectTargetYAMLFiles walks root and returns every *.yaml / *.yml
// file. Duplicated from cmd/archai/main.go intentionally — pulling the
// CLI helper into a shared package would widen the dependency surface
// of the http adapter for no real gain.
func collectTargetYAMLFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".yaml" || ext == ".yml" {
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
