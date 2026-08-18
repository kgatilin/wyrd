// Package main provides archai-check: the validation-only archai CLI.
//
// It runs exactly the architecture gates — overlay layer rules, the
// dependency policy, and drift against a locked target — and nothing else.
// No diagram rendering, no HTTP server, no MCP surface, no embedded review
// UI, which is why it links to a fraction of the full binary's size and
// builds without Node.js. Use it in CI; use `archai` on a workstation.
//
// The gates themselves live in internal/check and are shared with
// `archai overlay check` / `archai policy check` / `archai validate`, so
// the two binaries can never disagree about what passes.
package main

import (
	"fmt"
	"os"

	"github.com/kgatilin/archai/internal/adapter/golang"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/buildinfo"
	"github.com/kgatilin/archai/internal/check"
	"github.com/spf13/cobra"
)

// Version is overridden at build time via
// `-ldflags "-X main.Version=v1.2.3"`, matching the archai binary.
var Version = "dev"

func init() {
	if Version != "" && Version != "dev" {
		buildinfo.Version = Version
	}
}

func newChecker() *check.Checker {
	return check.New(golang.NewReader(), yamlAdapter.NewReader())
}

func main() {
	root := &cobra.Command{
		Use:   "archai-check",
		Short: "Validation-only archai: architecture gates for CI",
		Long: `Run archai's architecture gates and nothing else.

archai-check is the CI build of archai: same overlay, same rules, same
report wording as ` + "`archai overlay check`" + `, ` + "`archai policy check`" + ` and
` + "`archai validate`" + `, without the diagram renderer, HTTP server, MCP
server or review UI. Every command exits non-zero when its gate fails.`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringP("chdir", "C", "", "Change to this directory before checking (like 'go -C')")
	root.PersistentFlags().String("overlay", "", "Path to archai.yaml overlay (default: auto-detect archai.yaml)")

	root.AddCommand(newOverlayCmd(), newPolicyCmd(), newTargetCmd(), newAllCmd(), newVersionCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// chdir honours the persistent -C flag before any path is resolved.
func chdir(cmd *cobra.Command) error {
	dir, _ := cmd.Flags().GetString("chdir")
	if dir == "" {
		return nil
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("chdir %s: %w", dir, err)
	}
	return nil
}

// resolveOverlay applies -C and then locates the overlay, failing with the
// same message the full CLI uses when there is none.
func resolveOverlay(cmd *cobra.Command) (overlayPath, goModPath string, err error) {
	if err := chdir(cmd); err != nil {
		return "", "", err
	}
	flag, _ := cmd.Flags().GetString("overlay")
	overlayPath, goModPath = check.ResolveOverlay(flag)
	if overlayPath == "" {
		return "", "", fmt.Errorf("no overlay found: pass --overlay or create archai.yaml in the current directory")
	}
	return overlayPath, goModPath, nil
}

func newOverlayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "overlay [packages...]",
		Short: "Check layer rules declared in archai.yaml",
		Long: `Validate the composed overlay (root archai.yaml plus package-local
.arch/overlay.yaml fragments) and report every import that breaks a layer
rule. Packages default to ./... .`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			overlayPath, goModPath, err := resolveOverlay(cmd)
			if err != nil {
				return err
			}
			return newChecker().RunOverlay(cmd.Context(), check.OverlayOptions{
				OverlayPath: overlayPath,
				GoModPath:   goModPath,
				Paths:       args,
			}, cmd.OutOrStdout(), os.Stderr)
		},
	}
}

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy [packages...]",
		Short: "Check the dependency policy declared in archai.yaml",
		Long: `Evaluate the 'policy:' block in archai.yaml against the current Go
source and report every edge it forbids. Packages default to ./... .`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			overlayPath, goModPath, err := resolveOverlay(cmd)
			if err != nil {
				return err
			}
			format, _ := cmd.Flags().GetString("format")
			warn, _ := cmd.Flags().GetBool("warn")
			return newChecker().RunPolicy(cmd.Context(), check.PolicyOptions{
				OverlayPath: overlayPath,
				GoModPath:   goModPath,
				Paths:       args,
				Format:      format,
				Warn:        warn,
			}, cmd.OutOrStdout(), os.Stderr)
		},
	}
	cmd.Flags().String("format", "text", "Output format: text or json")
	cmd.Flags().Bool("warn", false, "Report violations but exit 0 (do not fail)")
	return cmd
}

func newTargetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Check the current code against a locked target (drift)",
		Long: `Compare the project's current architecture model against a locked
target and exit non-zero when they have drifted apart. Violations print one
per line as "<op> <kind> <path>"; --target overrides the CURRENT target.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := chdir(cmd); err != nil {
				return err
			}
			projectRoot, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving cwd: %w", err)
			}
			targetID, _ := cmd.Flags().GetString("target")
			format, _ := cmd.Flags().GetString("format")
			return newChecker().RunDrift(cmd.Context(), check.DriftOptions{
				ProjectRoot: projectRoot,
				TargetID:    targetID,
				Format:      format,
			}, cmd.OutOrStdout())
		},
	}
	cmd.Flags().String("target", "", "Target id to validate against (defaults to CURRENT)")
	cmd.Flags().StringP("format", "f", "text", "Output format: text, yaml, or json")
	return cmd
}

// newAllCmd runs every gate that the overlay actually declares, so a CI job
// is one command. Both gates run even when the first fails — a build log
// that stops at the first violation hides the rest.
func newAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "all [packages...]",
		Short: "Run the overlay and policy gates",
		Long: `Run the overlay layer-rule gate and the dependency-policy gate over
the same model. Both run even if the first reports violations; the exit code
is non-zero when either failed.`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			overlayPath, goModPath, err := resolveOverlay(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			c := newChecker()

			overlayErr := c.RunOverlay(cmd.Context(), check.OverlayOptions{
				OverlayPath: overlayPath,
				GoModPath:   goModPath,
				Paths:       args,
			}, out, os.Stderr)
			if overlayErr != nil {
				fmt.Fprintf(os.Stderr, "overlay: %v\n", overlayErr)
			}

			fmt.Fprintln(out)

			policyErr := c.RunPolicy(cmd.Context(), check.PolicyOptions{
				OverlayPath: overlayPath,
				GoModPath:   goModPath,
				Paths:       args,
			}, out, os.Stderr)
			if policyErr != nil {
				fmt.Fprintf(os.Stderr, "policy: %v\n", policyErr)
			}

			switch {
			case overlayErr != nil && policyErr != nil:
				return fmt.Errorf("overlay and dependency-policy gates failed")
			case overlayErr != nil:
				return fmt.Errorf("overlay gate failed")
			case policyErr != nil:
				return fmt.Errorf("dependency-policy gate failed")
			}
			return nil
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print archai-check version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "archai-check %s\n", buildinfo.Resolve().Version)
		},
	}
}
