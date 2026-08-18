package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/kgatilin/archai/internal/policy"
)

// PolicyOptions configures the dependency-policy check.
type PolicyOptions struct {
	// OverlayPath is the archai.yaml carrying the `policy:` block. Required.
	OverlayPath string
	// GoModPath is the go.mod validated against the overlay. May be empty.
	GoModPath string
	// Paths are the package patterns to scan (default: ./...).
	Paths []string
	// Format is "text" or "json".
	Format string
	// Warn reports violations but keeps the exit code at zero.
	Warn bool
}

// PolicyResult carries the outcome of a dependency-policy check.
type PolicyResult struct {
	// Defined is false when the overlay declares no `policy:` block; in
	// that case Violations is empty and nothing was evaluated.
	Defined    bool
	Violations []policy.Violation
}

// Policy loads the overlay, parses its dependency policy and reports every
// edge in the current model that the policy forbids.
func (c *Checker) Policy(ctx context.Context, opts PolicyOptions) (PolicyResult, error) {
	cfg, err := c.loadOverlay(opts.OverlayPath, opts.GoModPath)
	if err != nil {
		return PolicyResult{}, err
	}

	spec, err := policy.Parse(cfg.Policy)
	if err != nil {
		return PolicyResult{}, fmt.Errorf("parsing policy: %w", err)
	}
	if !spec.Defined() {
		return PolicyResult{Defined: false}, nil
	}

	models, err := c.source.Read(ensureCtx(ctx), resolvePaths(opts.Paths))
	if err != nil {
		return PolicyResult{}, fmt.Errorf("reading Go packages: %w", err)
	}
	merged, _, err := overlayMerge(models, cfg)
	if err != nil {
		return PolicyResult{}, err
	}

	violations, err := policy.Check(spec, merged, cfg)
	if err != nil {
		return PolicyResult{}, fmt.Errorf("evaluating policy: %w", err)
	}
	return PolicyResult{Defined: true, Violations: violations}, nil
}

// RunPolicy performs the dependency-policy check and writes the report to
// out (overlay-validation detail goes to errOut). The returned error is
// non-nil when the gate fails, unless opts.Warn is set.
func (c *Checker) RunPolicy(ctx context.Context, opts PolicyOptions, out, errOut io.Writer) error {
	if opts.Format != "" && opts.Format != "text" && opts.Format != "json" {
		return fmt.Errorf("--format must be text or json, got %q", opts.Format)
	}

	res, err := c.Policy(ctx, opts)
	if err != nil {
		var invalid *InvalidOverlayError
		if errors.As(err, &invalid) {
			fmt.Fprintf(errOut, "Overlay validation failed:\n%v\n", invalid.Err)
			return fmt.Errorf("overlay validation failed")
		}
		return err
	}
	if !res.Defined {
		fmt.Fprintf(out, "No policy defined in %s (add a 'policy:' block). Nothing to check.\n", opts.OverlayPath)
		return nil
	}

	if opts.Format == "json" {
		if err := WritePolicyJSON(out, res.Violations); err != nil {
			return err
		}
	} else {
		FormatPolicyViolations(out, res.Violations)
	}

	if len(res.Violations) > 0 && !opts.Warn {
		return fmt.Errorf("%d dependency-policy violation(s) found", len(res.Violations))
	}
	return nil
}

// WritePolicyJSON emits the violations as a JSON object {ok, count, violations}.
func WritePolicyJSON(w io.Writer, violations []policy.Violation) error {
	payload := struct {
		OK         bool               `json:"ok"`
		Count      int                `json:"count"`
		Violations []policy.Violation `json:"violations"`
	}{OK: len(violations) == 0, Count: len(violations), Violations: violations}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// FormatPolicyViolations renders a human-readable dependency-policy report.
func FormatPolicyViolations(w io.Writer, violations []policy.Violation) {
	if len(violations) == 0 {
		fmt.Fprintln(w, "OK: no dependency-policy violations.")
		return
	}
	fmt.Fprintf(w, "Found %d dependency-policy violation(s):\n\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(w, "  [%s] %s -> %s\n      %s\n", v.Kind, v.From, v.To, v.Message)
	}
}
