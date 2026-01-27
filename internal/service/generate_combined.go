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

	// Read all packages from Go source code
	packages, err := s.goReader.Read(ctx, opts.Paths)
	if err != nil {
		return nil, fmt.Errorf("reading packages: %w", err)
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
