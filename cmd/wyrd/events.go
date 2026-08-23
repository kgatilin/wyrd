package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kgatilin/wyrd/internal/check"
)

// newEventsCmd mounts the event-model gate at the top level of the full CLI.
//
// The plugin contributes its own `wyrd plugin events validate`, which reports
// the same findings in its own wording. This one runs the internal/check gate
// — the same code and the same report `wyrd-check events` runs in CI — so a
// developer reading a build log and a developer reading their terminal are
// reading the same sentences.
func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Check the event model declared in .arch/events.yaml and AsyncAPI sources",
		Long: `Validate the project's event declarations and report every finding:
duplicate namespace owners, conflicting subject patterns, unresolved $refs,
incoherent partition keys, orphaned and starved kinds, and unwired kinds —
appended by one component and observed by another on an address that never
matches, so the append reaches nobody.

Exits non-zero when an error-severity finding is reported. Warnings pass
unless --strict; --warn passes regardless. This is the gate wyrd-check runs
in CI, reporting in the same words.

Examples:
  # Everything the validator has to say
  wyrd events

  # Just the broken wires
  wyrd events --only unwired-kind

  # Fail on them
  wyrd events --only unwired-kind --strict

  # Machine-readable
  wyrd events --format json`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _ := cmd.Flags().GetString("root")
			if root == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("resolving cwd: %w", err)
				}
				root = cwd
			}
			only, _ := cmd.Flags().GetStringSlice("only")
			format, _ := cmd.Flags().GetString("format")
			strict, _ := cmd.Flags().GetBool("strict")
			warn, _ := cmd.Flags().GetBool("warn")
			return newChecker().RunEvents(cmd.Context(), check.EventsOptions{
				Root:   root,
				Only:   only,
				Format: format,
				Strict: strict,
				Warn:   warn,
			}, cmd.OutOrStdout(), os.Stderr)
		},
	}
	cmd.Flags().String("root", "", "Root directory to scan (default: current directory)")
	cmd.Flags().StringSlice("only", nil, "Report only these finding kinds (e.g. unwired-kind,orphan-event)")
	cmd.Flags().StringP("format", "f", "text", "Output format: text or json")
	cmd.Flags().Bool("strict", false, "Fail on warnings as well as errors")
	cmd.Flags().Bool("warn", false, "Report findings but exit 0 (do not fail)")
	return cmd
}
