package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kgatilin/archai/internal/domain"
)

// GenerateOptions configures the generate operation (split mode).
// For combined mode, use GenerateCombinedOptions with GenerateCombined method.
type GenerateOptions struct {
	// Paths are package paths to analyze (e.g., "./internal/...", "./cmd/...").
	Paths []string

	// PublicOnly generates only pub.d2 (public API diagram).
	// If both PublicOnly and InternalOnly are false, both diagrams are generated.
	PublicOnly bool

	// InternalOnly generates only internal.d2 (full implementation diagram).
	// If both PublicOnly and InternalOnly are false, both diagrams are generated.
	InternalOnly bool
}

// GenerateResult contains the result of generating diagrams for a package.
type GenerateResult struct {
	// PackagePath is the relative path of the package that was processed.
	PackagePath string

	// PubFile is the path to the generated pub.d2 file, if created.
	PubFile string

	// InternalFile is the path to the generated internal.d2 file, if created.
	InternalFile string

	// Error is the per-package error, if any occurred during generation.
	Error error
}

// Generate creates D2 diagrams from Go source code.
// In split mode (default), it creates .arch/ folders in each package directory
// with pub.d2 and/or internal.d2 files.
func (s *Service) Generate(ctx context.Context, opts GenerateOptions) ([]GenerateResult, error) {
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

	var results []GenerateResult
	for _, pkg := range packages {
		// Check context cancellation between packages
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result := s.generatePackageDiagrams(ctx, pkg, opts)
		results = append(results, result)
	}

	return results, nil
}

// generatePackageDiagrams generates diagrams for a single package.
func (s *Service) generatePackageDiagrams(ctx context.Context, pkg domain.PackageModel, opts GenerateOptions) GenerateResult {
	result := GenerateResult{PackagePath: pkg.Path}

	// Determine output directory
	archDir := s.resolveArchDir(pkg.Path)

	// Generate pub.d2 unless InternalOnly is set
	if !opts.InternalOnly {
		pubPath := filepath.Join(archDir, "pub.d2")
		writeOpts := domain.WriteOptions{
			OutputPath: pubPath,
			PublicOnly: true,
		}

		if err := s.d2Writer.Write(ctx, pkg, writeOpts); err != nil {
			result.Error = fmt.Errorf("writing pub.d2: %w", err)
			return result
		}
		result.PubFile = pubPath
	}

	// Generate internal.d2 unless PublicOnly is set
	if !opts.PublicOnly {
		internalPath := filepath.Join(archDir, "internal.d2")
		writeOpts := domain.WriteOptions{
			OutputPath: internalPath,
			PublicOnly: false,
		}

		if err := s.d2Writer.Write(ctx, pkg, writeOpts); err != nil {
			result.Error = fmt.Errorf("writing internal.d2: %w", err)
			return result
		}
		result.InternalFile = internalPath
	}

	return result
}

// resolveArchDir returns the .arch directory path for a package.
// The package path is relative to the module root (e.g., "internal/service").
// This assumes the command is run from the module root directory.
func (s *Service) resolveArchDir(pkgPath string) string {
	// Handle root package
	if pkgPath == "" || pkgPath == "." {
		return ".arch"
	}
	return filepath.Join(pkgPath, ".arch")
}
