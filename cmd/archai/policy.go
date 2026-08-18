package main

import (
	"fmt"
	"os"

	"github.com/kgatilin/archai/internal/check"
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
	if chdir != "" {
		if err := os.Chdir(chdir); err != nil {
			return fmt.Errorf("chdir %s: %w", chdir, err)
		}
	}

	overlayPath, goModPath := check.ResolveOverlay(overlayFlag)
	if overlayPath == "" {
		return fmt.Errorf("no overlay found: pass --overlay or create archai.yaml in the current directory")
	}

	return newChecker().RunPolicy(cmd.Context(), check.PolicyOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
		Paths:       args,
		Format:      format,
		Warn:        warn,
	}, cmd.OutOrStdout(), os.Stderr)
}
