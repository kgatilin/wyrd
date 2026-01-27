// Package main provides the CLI entry point for archai.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/golang"
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

	diagramCmd.AddCommand(generateCmd)

	// Execute root command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runGenerate executes the diagram generation command.
func runGenerate(cmd *cobra.Command, args []string) error {
	// Wire up dependencies
	goReader := golang.NewReader()
	d2Writer := d2.NewWriter()
	svc := service.NewService(goReader, d2Writer)

	// Build options from flags
	pubOnly, _ := cmd.Flags().GetBool("pub")
	internalOnly, _ := cmd.Flags().GetBool("internal")
	output, _ := cmd.Flags().GetString("output")

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
		Paths:        args,
		PublicOnly:   pubOnly,
		InternalOnly: internalOnly,
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
