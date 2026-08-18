package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/target"
)

// DriftOptions configures the target-drift check.
type DriftOptions struct {
	// ProjectRoot is the directory holding .arch/. Required.
	ProjectRoot string
	// TargetID names the locked target; empty means the CURRENT target.
	TargetID string
	// Format is "text", "yaml" or "json".
	Format string
}

// DriftResult carries the outcome of a target-drift check.
type DriftResult struct {
	// TargetID is the target actually compared against (CURRENT resolved).
	TargetID string
	// Diff is how the current code differs from that target.
	Diff *diff.Diff
}

// Drift compares the project's current model against a locked target.
func (c *Checker) Drift(ctx context.Context, opts DriftOptions) (DriftResult, error) {
	targetID := opts.TargetID
	if targetID == "" {
		cur, err := target.Current(opts.ProjectRoot)
		if err != nil {
			return DriftResult{}, fmt.Errorf("reading CURRENT: %w", err)
		}
		if cur == "" {
			return DriftResult{}, errors.New("no target specified and no CURRENT target set; use --target <id> or `archai target use <id>`")
		}
		targetID = cur
	}

	ctx = ensureCtx(ctx)
	current, err := c.LoadCurrentModel(ctx, opts.ProjectRoot)
	if err != nil {
		return DriftResult{}, fmt.Errorf("loading current model: %w", err)
	}
	targetModel, err := c.LoadTargetModel(ctx, opts.ProjectRoot, targetID)
	if err != nil {
		return DriftResult{}, fmt.Errorf("loading target %q: %w", targetID, err)
	}

	return DriftResult{TargetID: targetID, Diff: diff.Compute(current, targetModel)}, nil
}

// RunDrift performs the target-drift check and writes the report to out.
// The returned error is non-nil when the code has drifted from the target.
func (c *Checker) RunDrift(ctx context.Context, opts DriftOptions, out io.Writer) error {
	res, err := c.Drift(ctx, opts)
	if err != nil {
		return err
	}
	if res.Diff.IsEmpty() {
		fmt.Fprintf(out, "code matches target %q\n", res.TargetID)
		return nil
	}

	switch opts.Format {
	case "", "text":
		// CI-friendly: one violation per line, "<op> <kind> <path>".
		for _, c := range res.Diff.Changes {
			fmt.Fprintf(out, "%s %s %s\n", c.Op, c.Kind, c.Path)
		}
	case "yaml":
		s, err := diff.FormatYAML(res.Diff)
		if err != nil {
			return err
		}
		fmt.Fprint(out, s)
	case "json":
		s, err := diff.FormatJSON(res.Diff)
		if err != nil {
			return err
		}
		fmt.Fprint(out, s)
	default:
		return fmt.Errorf("unsupported format %q (use text, yaml, or json)", opts.Format)
	}
	return fmt.Errorf("drift detected: %d change(s) against target %q", len(res.Diff.Changes), res.TargetID)
}

// LoadCurrentModel loads the project's current architecture model: the
// package-level .arch/*.yaml specs when present, else the Go source.
func (c *Checker) LoadCurrentModel(ctx context.Context, projectRoot string) ([]domain.PackageModel, error) {
	files, err := findCurrentYAMLSpecs(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		return c.specs.Read(ensureCtx(ctx), files)
	}
	return c.source.Read(ensureCtx(ctx), defaultPaths)
}

// LoadTargetModel loads the frozen model from .arch/targets/<id>/model/.
func (c *Checker) LoadTargetModel(ctx context.Context, projectRoot, id string) ([]domain.PackageModel, error) {
	targetDir := filepath.Join(projectRoot, ".arch", "targets", id)
	if _, err := os.Stat(targetDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("target %q not found", id)
		}
		return nil, err
	}
	modelDir := filepath.Join(targetDir, "model")
	files, err := collectYAMLFiles(modelDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("target %q has no model files under %s", id, modelDir)
	}
	return c.specs.Read(ensureCtx(ctx), files)
}

// findCurrentYAMLSpecs walks projectRoot for package-level .arch/*.yaml
// files. The .arch/targets tree is skipped so locked targets don't leak
// into the "current" model.
func findCurrentYAMLSpecs(projectRoot string) ([]string, error) {
	var out []string
	targetsTree := filepath.Join(projectRoot, ".arch", "targets")

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip the targets tree entirely.
			if path == targetsTree || strings.HasPrefix(path, targetsTree+string(os.PathSeparator)) {
				return filepath.SkipDir
			}
			// Skip hidden directories except `.arch` itself.
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != ".arch" && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}
		// Only include files located directly inside a .arch directory.
		if filepath.Base(filepath.Dir(path)) != ".arch" {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// collectYAMLFiles returns every *.yaml / *.yml file under root.
func collectYAMLFiles(root string) ([]string, error) {
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
