package check

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/kgatilin/wyrd/internal/policy"
)

// AllOptions configures a combined run of the overlay and dependency-policy
// gates.
type AllOptions struct {
	// OverlayPath is the wyrd.yaml to load. Required.
	OverlayPath string
	// GoModPath is the go.mod validated against the overlay. May be empty.
	GoModPath string
	// Paths are the package patterns to scan (default: ./...).
	Paths []string
}

// RunAll evaluates the overlay layer rules and the dependency policy over a
// single read of the model — the go/packages load dominates a check's
// runtime, and running the gates separately would pay for it twice.
//
// Both gates always report: a build log that stops at the first violation
// hides the rest. The returned error names whichever gates failed.
func (c *Checker) RunAll(ctx context.Context, opts AllOptions, out, errOut io.Writer) error {
	cfg, err := c.loadOverlay(opts.OverlayPath, opts.GoModPath)
	if err != nil {
		var invalid *InvalidOverlayError
		if errors.As(err, &invalid) {
			fmt.Fprintf(errOut, "Overlay validation failed:\n%v\n", invalid.Err)
			return fmt.Errorf("overlay validation failed")
		}
		return err
	}

	spec, err := policy.Parse(cfg.Policy)
	if err != nil {
		return fmt.Errorf("parsing policy: %w", err)
	}

	model, err := c.readModel(ctx, cfg, opts.Paths)
	if err != nil {
		return err
	}

	overlayErr := reportOverlay(out, model.overlayResult())

	policyRes := PolicyResult{}
	if spec.Defined() {
		policyRes, err = model.policyResult(spec)
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(out)
	policyErr := reportPolicy(out, policyRes, PolicyOptions{OverlayPath: opts.OverlayPath})

	switch {
	case overlayErr != nil && policyErr != nil:
		fmt.Fprintf(errOut, "overlay: %v\npolicy: %v\n", overlayErr, policyErr)
		return fmt.Errorf("overlay and dependency-policy gates failed")
	case overlayErr != nil:
		return fmt.Errorf("overlay gate failed: %w", overlayErr)
	case policyErr != nil:
		return fmt.Errorf("dependency-policy gate failed: %w", policyErr)
	}
	return nil
}
