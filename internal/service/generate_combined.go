package service

import (
	"context"
	"fmt"

	"github.com/kgatilin/archai/internal/domain"
)

// GenerateCombinedOptions configures combined diagram generation.
// Combined mode always generates public API only into a single file.
type GenerateCombinedOptions struct {
	// Paths are package patterns to include (e.g., "./internal/...", "./cmd/...").
	Paths []string

	// OutputPath is the path to the output file (required).
	OutputPath string

	// Debug enables verbose output for troubleshooting dependency detection.
	Debug bool

	// DebugPrintf is the function to use for debug output. If nil, fmt.Printf is used.
	DebugPrintf func(format string, args ...any)
}

// GenerateCombinedResult contains the result of combined diagram generation.
type GenerateCombinedResult struct {
	// OutputPath is the path to the generated diagram file.
	OutputPath string

	// PackageCount is the number of packages included in the diagram.
	PackageCount int
}

// GenerateCombined creates a single diagram from multiple packages.
// Unlike Generate (split mode), this always produces public API only
// and writes to a single output file.
func (s *Service) GenerateCombined(ctx context.Context, opts GenerateCombinedOptions) (*GenerateCombinedResult, error) {
	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Setup debug printf
	debugf := opts.DebugPrintf
	if debugf == nil {
		debugf = func(format string, args ...any) {
			fmt.Printf(format, args...)
		}
	}

	// Read all packages from Go source code
	packages, err := s.goReader.Read(ctx, opts.Paths)
	if err != nil {
		return nil, fmt.Errorf("reading packages: %w", err)
	}

	// Debug: output package and dependency information
	if opts.Debug {
		debugf("\n=== DEBUG: Package Analysis (Combined Mode) ===\n")
		debugf("Paths requested: %v\n", opts.Paths)
		debugf("Packages found: %d\n\n", len(packages))

		for _, pkg := range packages {
			debugf("--- Package: %s (name: %s) ---\n", pkg.Path, pkg.Name)
			debugf("  Interfaces: %d (exported: %d)\n", len(pkg.Interfaces), len(pkg.ExportedInterfaces()))
			for _, iface := range pkg.Interfaces {
				debugf("    - %s (exported: %v, file: %s, methods: %d)\n",
					iface.Name, iface.IsExported, iface.SourceFile, len(iface.Methods))
			}
			debugf("  Structs: %d (exported: %d)\n", len(pkg.Structs), len(pkg.ExportedStructs()))
			for _, s := range pkg.Structs {
				debugf("    - %s (exported: %v, file: %s, fields: %d, methods: %d)\n",
					s.Name, s.IsExported, s.SourceFile, len(s.Fields), len(s.Methods))
			}
			debugf("  Functions: %d (exported: %d)\n", len(pkg.Functions), len(pkg.ExportedFunctions()))
			for _, fn := range pkg.Functions {
				debugf("    - %s (exported: %v, file: %s, stereotype: %v)\n",
					fn.Name, fn.IsExported, fn.SourceFile, fn.Stereotype)
			}
			debugf("  TypeDefs: %d (exported: %d)\n", len(pkg.TypeDefs), len(pkg.ExportedTypeDefs()))
			for _, td := range pkg.TypeDefs {
				debugf("    - %s (exported: %v, file: %s, constants: %d)\n",
					td.Name, td.IsExported, td.SourceFile, len(td.Constants))
			}
			debugf("  Dependencies (raw): %d\n", len(pkg.Dependencies))
			for _, dep := range pkg.Dependencies {
				debugf("    - %s.%s [%s] -> %s.%s [%s] (kind: %s, throughExported: %v, external: %v)\n",
					dep.From.Package, dep.From.Symbol, dep.From.File,
					dep.To.Package, dep.To.Symbol, dep.To.File,
					dep.Kind, dep.ThroughExported, dep.To.External)
			}
			debugf("\n")
		}
		debugf("=== END DEBUG ===\n\n")
	}

	// Filter to packages with exported symbols
	var filtered []domain.PackageModel
	for _, pkg := range packages {
		if pkg.HasExportedSymbols() {
			filtered = append(filtered, pkg)
		}
	}

	// Handle case where no packages have exported symbols
	if len(filtered) == 0 {
		return &GenerateCombinedResult{
			OutputPath:   opts.OutputPath,
			PackageCount: 0,
		}, nil
	}

	// Write combined diagram
	if err := s.d2Writer.WriteCombined(ctx, filtered, opts.OutputPath); err != nil {
		return nil, fmt.Errorf("writing combined diagram: %w", err)
	}

	return &GenerateCombinedResult{
		OutputPath:   opts.OutputPath,
		PackageCount: len(filtered),
	}, nil
}
