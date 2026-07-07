package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/policy"
	"github.com/spf13/cobra"
)

// newPolicyCmd builds the `archai policy` command group. The dependency
// policy is authored under a `policy:` block in archai.yaml (see
// internal/policy and docs/features/dependency-policy/design.md) and checked
// against the current Go model.
func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Check dependency policy (allowed/forbidden imports)",
		Long: `Evaluate the dependency policy declared in archai.yaml against the
current Go source. The policy is a concise, path-based description of which
packages may depend on which — allow/forbid edges plus reachability rules —
authored under a 'policy:' block. See docs/features/dependency-policy.`,
	}

	checkCmd := &cobra.Command{
		Use:   "check [packages...]",
		Short: "Report dependency-policy violations in the current code",
		Long: `Load the archai.yaml overlay, extract the current Go model, and report
every edge that violates the policy. Exits non-zero when violations exist
(unless --warn), so it can gate CI.

Packages default to ./... ; pass explicit patterns to scope the scan.`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE:         runPolicyCheck,
	}
	checkCmd.Flags().StringP("chdir", "C", "", "Change to this directory before reading (like 'go -C'); scans that module")
	checkCmd.Flags().String("overlay", "", "Path to archai.yaml overlay (default: auto-detect archai.yaml)")
	checkCmd.Flags().String("format", "text", "Output format: text or json")
	checkCmd.Flags().Bool("warn", false, "Report violations but exit 0 (do not fail)")
	cmd.AddCommand(checkCmd)

	return cmd
}

func runPolicyCheck(cmd *cobra.Command, args []string) error {
	chdir, _ := cmd.Flags().GetString("chdir")
	overlayFlag, _ := cmd.Flags().GetString("overlay")
	format, _ := cmd.Flags().GetString("format")
	warn, _ := cmd.Flags().GetBool("warn")
	if format != "text" && format != "json" {
		return fmt.Errorf("--format must be text or json, got %q", format)
	}
	if chdir != "" {
		if err := os.Chdir(chdir); err != nil {
			return fmt.Errorf("chdir %s: %w", chdir, err)
		}
	}

	overlayPath, goModPath := resolveOverlay(overlayFlag)
	if overlayPath == "" {
		return fmt.Errorf("no overlay found: pass --overlay or create archai.yaml in the current directory")
	}

	cfg, err := overlay.LoadComposed(overlayPath)
	if err != nil {
		return fmt.Errorf("loading overlay %s: %w", overlayPath, err)
	}
	if err := overlay.Validate(cfg, goModPath); err != nil {
		fmt.Fprintf(os.Stderr, "Overlay validation failed:\n%v\n", err)
		return fmt.Errorf("overlay validation failed")
	}

	spec, err := policy.Parse(cfg.Policy)
	if err != nil {
		return fmt.Errorf("parsing policy: %w", err)
	}
	if !spec.Defined() {
		fmt.Fprintf(cmd.OutOrStdout(), "No policy defined in %s (add a 'policy:' block). Nothing to check.\n", overlayPath)
		return nil
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	paths := args
	if len(paths) == 0 {
		paths = []string{"./..."}
	}
	models, err := golang.NewReader().Read(ctx, paths)
	if err != nil {
		return fmt.Errorf("reading Go packages: %w", err)
	}
	mergedModels, _, err := overlay.Merge(models, cfg)
	if err != nil {
		return fmt.Errorf("merging overlay: %w", err)
	}

	violations, err := policy.Check(spec, mergedModels, cfg)
	if err != nil {
		return fmt.Errorf("evaluating policy: %w", err)
	}

	if format == "json" {
		if err := writePolicyJSON(cmd, violations); err != nil {
			return err
		}
	} else {
		printPolicyViolations(cmd, violations)
	}

	if len(violations) > 0 && !warn {
		return fmt.Errorf("%d dependency-policy violation(s) found", len(violations))
	}
	return nil
}

// writePolicyJSON emits the violations as a JSON object {ok, violations}.
func writePolicyJSON(cmd *cobra.Command, violations []policy.Violation) error {
	payload := struct {
		OK         bool               `json:"ok"`
		Count      int                `json:"count"`
		Violations []policy.Violation `json:"violations"`
	}{OK: len(violations) == 0, Count: len(violations), Violations: violations}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// printPolicyViolations renders a human-readable report.
func printPolicyViolations(cmd *cobra.Command, violations []policy.Violation) {
	w := cmd.OutOrStdout()
	if len(violations) == 0 {
		fmt.Fprintln(w, "OK: no dependency-policy violations.")
		return
	}
	fmt.Fprintf(w, "Found %d dependency-policy violation(s):\n\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(w, "  [%s] %s -> %s\n      %s\n", v.Kind, v.From, v.To, v.Message)
	}
}
