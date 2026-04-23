// Package main provides the CLI entry point for archai.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/golang"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/service"
	"github.com/spf13/cobra"
)

func main() {
	// Root command
	rootCmd := &cobra.Command{
		Use:   "archai",
		Short: "Architecture diagram generator for Go projects",
		Long: `archai generates D2 architecture diagrams from Go source code.

It analyzes Go packages and creates visual representations of the code structure,
including interfaces, structs, functions, and their relationships.`,
	}

	// Diagram command group
	diagramCmd := &cobra.Command{
		Use:   "diagram",
		Short: "Commands for working with architecture diagrams",
		Long:  "Commands for generating, splitting, and composing architecture diagrams.",
	}
	rootCmd.AddCommand(diagramCmd)

	// Generate subcommand
	generateCmd := &cobra.Command{
		Use:   "generate [packages...]",
		Short: "Generate D2 diagrams from Go packages",
		Long: `Generate D2 architecture diagrams from Go source code.

Analyzes the specified Go packages and creates D2 diagram files in .arch/ folders
within each package directory.

By default, generates both:
  - pub.d2: Public API (exported symbols only)
  - internal.d2: Full implementation (all symbols)

Examples:
  # Generate diagrams for all packages in internal/
  archai diagram generate ./internal/...

  # Generate only public API diagrams
  archai diagram generate ./internal/... --pub

  # Generate only internal diagrams
  archai diagram generate ./internal/... --internal

  # Generate combined diagram to single file
  archai diagram generate ./internal/... -o architecture.d2`,
		Args: cobra.MinimumNArgs(1),
		RunE: runGenerate,
	}

	// Add flags
	generateCmd.Flags().Bool("pub", false, "Generate only pub.d2 (public API)")
	generateCmd.Flags().Bool("internal", false, "Generate only internal.d2 (full implementation)")
	generateCmd.Flags().StringP("output", "o", "", "Output to single file (combined mode)")
	generateCmd.Flags().StringP("format", "f", "d2", "Output format: d2 or yaml")
	generateCmd.Flags().Bool("debug", false, "Print debug information about packages and dependencies")

	diagramCmd.AddCommand(generateCmd)

	// Split subcommand
	splitCmd := &cobra.Command{
		Use:   "split <diagram-file>",
		Short: "Split combined diagram into per-package specs",
		Long: `Split a combined D2 diagram into per-package specification files.

Takes a combined diagram (e.g., created with 'diagram generate -o') and creates
individual pub-spec.d2 files in each package's .arch/ directory.

The -spec.d2 suffix indicates these are target specifications (what the architecture
should look like) as opposed to pub.d2 files generated from actual code.

Examples:
  # Split a combined diagram into per-package specs
  archai diagram split docs/architecture.d2

  # Preview what would be created without writing files
  archai diagram split docs/architecture.d2 --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runSplit,
	}
	splitCmd.Flags().Bool("dry-run", false, "Show what would be created without writing files")
	diagramCmd.AddCommand(splitCmd)

	// Compose subcommand
	composeCmd := &cobra.Command{
		Use:   "compose [packages...]",
		Short: "Compose a single diagram from saved .arch/ spec files",
		Long: `Compose reads saved .arch/ specification files from multiple packages
and combines them into a single diagram file.

By default, uses pub.d2 files (code-generated diagrams).
Use --spec to compose from pub-spec.d2 files (target specifications).

Examples:
  # Compose from code-generated diagrams (default)
  archai diagram compose ./internal/... --output=docs/current-architecture.d2

  # Compose from saved specs (target state)
  archai diagram compose ./internal/... --spec --output=docs/target-architecture.d2`,
		Args: cobra.MinimumNArgs(1),
		RunE: runCompose,
	}
	composeCmd.Flags().StringP("output", "o", "", "Output file path (required)")
	composeCmd.Flags().Bool("spec", false, "Use *-spec.d2 files instead of pub.d2")
	_ = composeCmd.MarkFlagRequired("output")
	diagramCmd.AddCommand(composeCmd)

	// Execute root command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runGenerate executes the diagram generation command.
func runGenerate(cmd *cobra.Command, args []string) error {
	// Build options from flags
	pubOnly, _ := cmd.Flags().GetBool("pub")
	internalOnly, _ := cmd.Flags().GetBool("internal")
	output, _ := cmd.Flags().GetString("output")
	format, _ := cmd.Flags().GetString("format")
	debug, _ := cmd.Flags().GetBool("debug")

	// Wire up dependencies based on format
	goReader := golang.NewReader()
	d2Reader := d2.NewReader()

	var writer service.ModelWriter
	var fileExt string
	switch format {
	case "yaml":
		writer = yamlAdapter.NewWriter()
		fileExt = ".yaml"
	case "d2":
		writer = d2.NewWriter()
		fileExt = ".d2"
	default:
		return fmt.Errorf("unsupported format %q (use d2 or yaml)", format)
	}

	yamlReader := yamlAdapter.NewReader()
	yamlWriter := yamlAdapter.NewWriter()
	svc := service.NewService(goReader, d2Reader, writer, service.WithYAML(yamlReader, yamlWriter))

	// Validate flags
	if pubOnly && internalOnly {
		return fmt.Errorf("cannot specify both --pub and --internal flags")
	}

	// Get context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Combined mode: --output flag present
	if output != "" {
		// --pub and --internal don't apply to combined mode
		if pubOnly || internalOnly {
			fmt.Fprintln(os.Stderr, "Note: --pub and --internal flags are ignored in combined mode (always public)")
		}

		opts := service.GenerateCombinedOptions{
			Paths:      args,
			OutputPath: output,
			Debug:      debug,
		}

		result, err := svc.GenerateCombined(ctx, opts)
		if err != nil {
			return fmt.Errorf("generation failed: %w", err)
		}

		if result.PackageCount == 0 {
			fmt.Println("No packages with exported symbols found")
			return nil
		}

		fmt.Printf("Combined diagram generated: %s\n", result.OutputPath)
		fmt.Printf("Packages included: %d\n", result.PackageCount)
		return nil
	}

	// Split mode: existing logic
	opts := service.GenerateOptions{
		Paths:         args,
		PublicOnly:    pubOnly,
		InternalOnly:  internalOnly,
		FileExtension: fileExt,
		Debug:         debug,
	}

	results, err := svc.Generate(ctx, opts)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Display results
	successCount := 0
	errorCount := 0

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", r.PackagePath, r.Error)
			errorCount++
		} else {
			if r.PubFile != "" {
				fmt.Printf("Created %s\n", r.PubFile)
			}
			if r.InternalFile != "" {
				fmt.Printf("Created %s\n", r.InternalFile)
			}
			successCount++
		}
	}

	// Summary
	fmt.Printf("\nProcessed %d package(s): %d succeeded, %d failed\n", len(results), successCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("generation completed with %d error(s)", errorCount)
	}

	return nil
}

// runSplit executes the diagram split command.
func runSplit(cmd *cobra.Command, args []string) error {
	// Wire up dependencies
	goReader := golang.NewReader()
	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	svc := service.NewService(goReader, d2Reader, d2Writer, service.WithYAML(yamlAdapter.NewReader(), yamlAdapter.NewWriter()))

	// Get flags
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Get context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	opts := service.SplitOptions{
		DiagramPath: args[0],
		DryRun:      dryRun,
	}

	result, err := svc.Split(ctx, opts)
	if err != nil {
		return fmt.Errorf("split failed: %w", err)
	}

	// Display results
	successCount := 0
	errorCount := 0

	for _, r := range result.Files {
		if r.Error != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", r.PackagePath, r.Error)
			errorCount++
		} else {
			if result.DryRun {
				fmt.Printf("Would create: %s\n", r.FilePath)
			} else {
				fmt.Printf("Created: %s\n", r.FilePath)
			}
			successCount++
		}
	}

	// Summary
	if result.DryRun {
		fmt.Printf("\n%d spec file(s) would be created\n", successCount)
	} else {
		fmt.Printf("\nSplit complete: %d spec file(s) created\n", successCount)
	}

	if errorCount > 0 {
		return fmt.Errorf("split completed with %d error(s)", errorCount)
	}

	return nil
}

// runCompose executes the diagram compose command.
func runCompose(cmd *cobra.Command, args []string) error {
	// Wire up dependencies
	goReader := golang.NewReader()
	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	svc := service.NewService(goReader, d2Reader, d2Writer, service.WithYAML(yamlAdapter.NewReader(), yamlAdapter.NewWriter()))

	// Get flags
	outputPath, _ := cmd.Flags().GetString("output")
	specOnly, _ := cmd.Flags().GetBool("spec")

	// Determine mode
	mode := service.ComposeModeAuto
	if specOnly {
		mode = service.ComposeModeSpec
	}

	// Get context
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Execute compose
	result, err := svc.Compose(ctx, service.ComposeOptions{
		Paths:      args,
		OutputPath: outputPath,
		Mode:       mode,
	})
	if err != nil {
		return fmt.Errorf("compose failed: %w", err)
	}

	// Display result
	fmt.Printf("Composed %d packages into %s\n", result.PackageCount, result.OutputPath)
	if len(result.SkippedPaths) > 0 {
		fmt.Printf("Skipped %d packages (no .arch/ folder or missing files):\n", len(result.SkippedPaths))
		for _, path := range result.SkippedPaths {
			fmt.Printf("  - %s\n", path)
		}
	}

	return nil
}
