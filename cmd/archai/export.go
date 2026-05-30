package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/adapter/uigraph"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/target"
	"github.com/spf13/cobra"
)

// newExportCmd returns the `archai export` command group.
func newExportCmd() *cobra.Command {
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export architecture data in various formats",
		Long:  "Export the project's architecture model in formats suitable for external tools.",
	}

	exportCmd.AddCommand(newExportUICmd())
	return exportCmd
}

// newExportUICmd returns the `archai export ui` subcommand.
func newExportUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui [paths...]",
		Short: "Export UIGraph JSON for the POC review UI",
		Long: `Project the current architecture model into a UIGraph JSON file
suitable for the POC review UI.

The output includes bounded contexts, components (packages), internals
(interfaces/structs), members (methods/fields), ports, and edges.
When a target is specified, diff flags are computed and a PR summary
is included.

Examples:
  # Export all internal packages to stdout
  archai export ui ./internal/...

  # Export to a file
  archai export ui ./internal/... -o web/public/archgraph.json

  # Export with diff against a locked target
  archai export ui ./internal/... --target baseline -o archgraph.json`,
		Args: cobra.MinimumNArgs(0),
		RunE: runExportUI,
	}

	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.Flags().String("target", "", "Target id to diff against (defaults to CURRENT if set)")
	cmd.Flags().String("overlay", "", "Path to archai.yaml overlay (default: auto-detect)")

	return cmd
}

func runExportUI(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	targetID, _ := cmd.Flags().GetString("target")
	overlayFlag, _ := cmd.Flags().GetString("overlay")

	// Default paths
	paths := args
	if len(paths) == 0 {
		paths = []string{"./..."}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}

	// Load current model
	current, err := loadUICurrentModel(ctx, paths)
	if err != nil {
		return fmt.Errorf("loading current model: %w", err)
	}

	// Load overlay if available
	var cfg *overlay.Config
	overlayPath, _ := resolveOverlay(overlayFlag)
	if overlayPath != "" {
		cfg, err = overlay.LoadComposed(overlayPath)
		if err != nil {
			// Non-fatal: proceed without overlay
			fmt.Fprintf(os.Stderr, "Warning: loading overlay %s: %v\n", overlayPath, err)
			cfg = nil
		}
	}

	// Optionally compute diff
	var d *diff.Diff
	if targetID == "" {
		// Try CURRENT target
		cur, _ := target.Current(projectRoot)
		if cur != "" {
			targetID = cur
		}
	}
	if targetID != "" {
		targetModel, err := loadTargetModel(ctx, projectRoot, targetID)
		if err != nil {
			// Non-fatal: proceed without diff
			fmt.Fprintf(os.Stderr, "Warning: loading target %q: %v\n", targetID, err)
		} else {
			computed := diff.Compute(current, targetModel)
			d = computed
		}
	}

	// Project to UIGraph
	g, err := uigraph.Project(current, cfg, d)
	if err != nil {
		return fmt.Errorf("projecting UIGraph: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	data = append(data, '\n')

	// Write output
	var w io.Writer = cmd.OutOrStdout()
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

// loadUICurrentModel loads packages from the given paths using the Go reader.
func loadUICurrentModel(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	reader := golang.NewReader()
	return reader.Read(ctx, paths)
}
