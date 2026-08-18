package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// OverlayOptions configures the overlay layer-rule check.
type OverlayOptions struct {
	// OverlayPath is the archai.yaml to load. Required.
	OverlayPath string
	// GoModPath is the go.mod validated against the overlay. May be empty.
	GoModPath string
	// Paths are the package patterns to scan (default: ./...).
	Paths []string
}

// OverlayResult carries the outcome of an overlay check.
type OverlayResult struct {
	// Violations are the forbidden cross-layer imports found.
	Violations []overlay.Violation
	// PkgLayer maps module-relative package path to assigned layer, so a
	// report can name the layer on both sides of a violating edge.
	PkgLayer map[string]string
}

// InvalidOverlayError reports that the overlay document itself is
// inconsistent — the checks never ran. Err carries the (usually
// multi-line) validation detail.
type InvalidOverlayError struct {
	Path string
	Err  error
}

func (e *InvalidOverlayError) Error() string { return "overlay validation failed" }

func (e *InvalidOverlayError) Unwrap() error { return e.Err }

// Overlay loads and validates the overlay, reads the current model and
// reports every import that breaks a layer rule.
func (c *Checker) Overlay(ctx context.Context, opts OverlayOptions) (OverlayResult, error) {
	cfg, err := c.loadOverlay(opts.OverlayPath, opts.GoModPath)
	if err != nil {
		return OverlayResult{}, err
	}
	model, err := c.readModel(ctx, cfg, opts.Paths)
	if err != nil {
		return OverlayResult{}, err
	}
	return model.overlayResult(), nil
}

// mergedModel is the shared input of every gate: the overlay, the current
// packages with layers assigned, and the layer-rule violations found while
// assigning them. Reading it is the expensive part of a check — a full
// go/packages load — so a run that evaluates several gates reads it once.
type mergedModel struct {
	cfg        *overlay.Config
	packages   []domain.PackageModel
	violations []overlay.Violation
}

// readModel reads the current packages and merges the overlay onto them.
func (c *Checker) readModel(ctx context.Context, cfg *overlay.Config, paths []string) (*mergedModel, error) {
	models, err := c.source.Read(ensureCtx(ctx), resolvePaths(paths))
	if err != nil {
		return nil, fmt.Errorf("reading Go packages: %w", err)
	}
	merged, violations, err := overlayMerge(models, cfg)
	if err != nil {
		return nil, err
	}
	return &mergedModel{cfg: cfg, packages: merged, violations: violations}, nil
}

// overlayResult projects the merged model into the layer-rule report.
func (m *mergedModel) overlayResult() OverlayResult {
	pkgLayer := make(map[string]string)
	for _, p := range m.packages {
		if p.Layer == "" {
			continue
		}
		rel := p.Path
		if m.cfg.Module != "" {
			rel = TrimModulePrefix(m.cfg.Module, p.Path)
		}
		pkgLayer[rel] = p.Layer
	}
	return OverlayResult{Violations: m.violations, PkgLayer: pkgLayer}
}

// overlayMerge merges the overlay onto models, wrapping the error the way
// every gate in this package reports it.
func overlayMerge(models []domain.PackageModel, cfg *overlay.Config) ([]domain.PackageModel, []overlay.Violation, error) {
	merged, violations, err := overlay.Merge(models, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("merging overlay: %w", err)
	}
	return merged, violations, nil
}

// loadOverlay loads the composed overlay and validates it against goModPath.
func (c *Checker) loadOverlay(overlayPath, goModPath string) (*overlay.Config, error) {
	cfg, err := overlay.LoadComposed(overlayPath)
	if err != nil {
		return nil, fmt.Errorf("loading overlay %s: %w", overlayPath, err)
	}
	if err := overlay.Validate(cfg, goModPath); err != nil {
		return nil, &InvalidOverlayError{Path: overlayPath, Err: err}
	}
	return cfg, nil
}

// RunOverlay performs the overlay check and writes the report: violations
// to out, overlay-validation detail to errOut. The returned error is
// non-nil when the gate fails, so a CLI can return it straight from RunE
// and get a non-zero exit code.
func (c *Checker) RunOverlay(ctx context.Context, opts OverlayOptions, out, errOut io.Writer) error {
	res, err := c.Overlay(ctx, opts)
	if err != nil {
		var invalid *InvalidOverlayError
		if errors.As(err, &invalid) {
			fmt.Fprintf(errOut, "Overlay validation failed:\n%v\n", invalid.Err)
			return fmt.Errorf("overlay validation failed")
		}
		return err
	}
	return reportOverlay(out, res)
}

// reportOverlay writes the layer-rule report and returns the gate's verdict.
func reportOverlay(out io.Writer, res OverlayResult) error {
	if len(res.Violations) == 0 {
		fmt.Fprintln(out, "OK: overlay is valid and no layer-rule violations found.")
		return nil
	}
	FormatOverlayViolations(out, res.Violations, res.PkgLayer)
	return fmt.Errorf("%d layer-rule violation(s) found", ViolationCount(res.Violations))
}

// FormatOverlayViolations renders a human-readable report of violations to
// w. pkgLayer maps module-relative package paths to their assigned layer
// so the "layer B" half of each line is accurate.
func FormatOverlayViolations(w io.Writer, violations []overlay.Violation, pkgLayer map[string]string) {
	fmt.Fprintf(w, "Found %d layer-rule violation(s):\n\n", ViolationCount(violations))
	for _, v := range violations {
		for _, imp := range v.Imports {
			targetLayer := pkgLayer[imp]
			if targetLayer == "" {
				targetLayer = "?"
			}
			fmt.Fprintf(w,
				"VIOLATION: package %s (layer %s) imports package %s (layer %s) — not allowed\n",
				v.Package, v.Layer, imp, targetLayer)
		}
	}
}

// ViolationCount totals the individual forbidden imports across violations
// (one Violation groups every bad import of one package).
func ViolationCount(violations []overlay.Violation) int {
	n := 0
	for _, v := range violations {
		n += len(v.Imports)
	}
	return n
}

// TrimModulePrefix returns pkgPath with the module prefix stripped, or
// pkgPath unchanged if the prefix does not apply.
func TrimModulePrefix(module, pkgPath string) string {
	if pkgPath == module {
		return ""
	}
	if len(pkgPath) > len(module) && pkgPath[:len(module)] == module && pkgPath[len(module)] == '/' {
		return pkgPath[len(module)+1:]
	}
	return pkgPath
}

// ResolveOverlay determines the overlay path and accompanying go.mod path.
// When explicitPath is non-empty it is used verbatim (and the adjacent
// go.mod is looked up); when empty we auto-detect ./archai.yaml in the
// working directory. Returns empty strings when no overlay is found.
func ResolveOverlay(explicitPath string) (overlayPath, goModPath string) {
	if explicitPath != "" {
		dir := filepath.Dir(explicitPath)
		gm := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(gm); err != nil {
			gm = ""
		}
		return explicitPath, gm
	}
	candidate := "archai.yaml"
	if _, err := os.Stat(candidate); err != nil {
		return "", ""
	}
	gm := "go.mod"
	if _, err := os.Stat(gm); err != nil {
		gm = ""
	}
	return candidate, gm
}
