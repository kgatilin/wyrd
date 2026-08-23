package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kgatilin/wyrd/internal/check"
	"github.com/kgatilin/wyrd/internal/eventmodel"
	"github.com/kgatilin/wyrd/internal/eventquery"
	"github.com/kgatilin/wyrd/internal/eventsrc"
)

// newEventsCmd groups the questions asked of the event model: is it sound
// (check), where is this thing (search), and how is it wired (wiring).
//
// All three read the declarations off disk. None needs a running daemon,
// because a gate that only answers while a server is up is a gate nobody can
// put in CI, and an agent asking "who folds this" should not have to start one.
func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Inspect and check the event model",
		Long: `Commands for the event model declared in .arch/events.yaml and
imported AsyncAPI documents: check it, search it, and print how it is wired.`,
	}
	cmd.AddCommand(newEventsCheckCmd(), newEventsSearchCmd(), newEventsWiringCmd())
	return cmd
}

// newEventsCheckCmd is the gate.
//
// The plugin contributes its own `wyrd plugin events validate`, which reports
// the same findings in its own wording. This one runs the internal/check gate
// — the same code and the same report `wyrd-check events` runs in CI — so a
// developer reading a build log and a developer reading their terminal are
// reading the same sentences.
func newEventsCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
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
  wyrd events check

  # Just the broken wires
  wyrd events check --only unwired-kind

  # Fail on them
  wyrd events check --only unwired-kind --strict

  # Machine-readable
  wyrd events check --format json`,
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

// loadEventModel reads the project's event declarations off disk and projects
// the graph, which is where per-kind health comes from.
func loadEventModel(cmd *cobra.Command) (*eventmodel.Model, *eventmodel.Graph, error) {
	root, _ := cmd.Flags().GetString("root")
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("resolving cwd: %w", err)
		}
		root = cwd
	}
	locations, err := eventsrc.Locations(root)
	if err != nil {
		return nil, nil, err
	}
	model, err := eventmodel.ReadSources(root, locations)
	if err != nil {
		return nil, nil, err
	}
	if model == nil || len(model.Components) == 0 {
		return nil, nil, fmt.Errorf("no event declarations found under %s", root)
	}
	return model, eventmodel.BuildGraph(model), nil
}

func newEventsSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Find event kinds and components by name, subject, or description",
		Long: `Search the event model and print what each hit is wired to.
For a kind: who appends it, who it triggers, whose state folds it, and the
addresses it travels on. For a component: the kinds it takes, appends and
folds.

Matching is a case-insensitive substring over kind names, component ids, owned
namespaces, subject patterns and descriptions. Payload schemas are left out —
ask for a kind by name with 'wyrd plugin events graph' or the event_kind MCP
tool when you want the shape.

The header states how many matched and how many are printed when --limit cut
the list; --limit 0 prints all of them.

Examples:
  wyrd events search turn.completed
  wyrd events search entrypoint
  wyrd events search 'session.{session}.command'
  wyrd events search cancel --format json`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			model, graph, err := loadEventModel(cmd)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			format, _ := cmd.Flags().GetString("format")

			hits, total := eventquery.Search(model, args[0], limit)
			hits = eventquery.WithHealth(hits, graph)
			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"query": args[0],
					"count": len(hits),
					"total": total,
					"hits":  hits,
				})
			}
			eventquery.FormatHits(cmd.OutOrStdout(), hits, total, args[0])
			return nil
		},
	}
	cmd.Flags().String("root", "", "Root directory to scan (default: current directory)")
	cmd.Flags().Int("limit", 20, "Maximum hits to print (0 for all)")
	cmd.Flags().StringP("format", "f", "text", "Output format: text or json")
	return cmd
}

func newEventsWiringCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiring",
		Short: "Print the whole event model as one flow per line",
		Long: `Print every component and everything it appends, one line per
destination: who receives it, whether it triggers them or is only folded into
their state, and any health finding on the kind.

Every append is resolved against the address it names, not the kind alone. A
component that publishes one kind on several addresses — the port-family
idiom, one address per instance — reaches a different component from each, and
those lines carry an "on <subject>" telling you which address they took.
"(nobody)" marks an append no declared subscriber matches.

This is the event graph in the size an agent can read. For a picture, use
'wyrd plugin events graph' (mermaid or graphml).

Examples:
  wyrd events wiring
  wyrd events wiring --root ./service`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			model, graph, err := loadEventModel(cmd)
			if err != nil {
				return err
			}
			eventquery.Wiring(model, graph, cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().String("root", "", "Root directory to scan (default: current directory)")
	return cmd
}
