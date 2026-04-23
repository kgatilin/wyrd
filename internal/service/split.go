package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// SplitOptions configures the split operation.
type SplitOptions struct {
	// DiagramPath is the path to the combined D2 diagram file to split.
	DiagramPath string

	// DryRun, when true, reports what would be created without writing files.
	DryRun bool
}

// SplitResult contains the result of splitting a combined diagram.
type SplitResult struct {
	// Files contains the result for each package that would be/was written.
	Files []SplitFileResult

	// DryRun indicates whether this was a dry-run (no files created).
	DryRun bool
}

// SplitFileResult contains the result for a single package file.
type SplitFileResult struct {
	// PackagePath is the package directory path (e.g., "pkg/newfeature").
	PackagePath string

	// FilePath is the path to the spec file (e.g., "pkg/newfeature/.arch/pub-spec.d2").
	FilePath string

	// Created is true if the file was created (false for dry-run or error).
	Created bool

	// Error is the per-file error, if any occurred.
	Error error
}

// Split reads a combined D2 diagram and splits it into per-package spec files.
// Each package gets a pub-spec.d2 file in its .arch directory.
func (s *Service) Split(ctx context.Context, opts SplitOptions) (*SplitResult, error) {
	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Select reader based on input file extension
	reader := s.d2Reader
	if (strings.HasSuffix(opts.DiagramPath, ".yaml") || strings.HasSuffix(opts.DiagramPath, ".yml")) && s.yamlReader != nil {
		reader = s.yamlReader
	}

	// Read the combined diagram file
	models, err := reader.Read(ctx, []string{opts.DiagramPath})
	if err != nil {
		return nil, fmt.Errorf("reading diagram: %w", err)
	}

	result := &SplitResult{
		Files:  make([]SplitFileResult, 0, len(models)),
		DryRun: opts.DryRun,
	}

	// Process each package
	for _, pkg := range models {
		// Check context cancellation between packages
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		fileResult := s.splitPackage(ctx, pkg, opts.DryRun)
		result.Files = append(result.Files, fileResult)
	}

	return result, nil
}

// splitPackage creates the spec file for a single package.
func (s *Service) splitPackage(ctx context.Context, pkg domain.PackageModel, dryRun bool) SplitFileResult {
	pkgDir := pkg.Path
	archDir := filepath.Join(pkgDir, ".arch")
	outputPath := filepath.Join(archDir, "pub-spec.d2")

	result := SplitFileResult{
		PackagePath: pkgDir,
		FilePath:    outputPath,
		Created:     false,
	}

	if dryRun {
		return result
	}

	// Create package and .arch directories if they don't exist
	// This supports "plan first" workflow where packages don't exist yet
	if err := os.MkdirAll(archDir, 0755); err != nil {
		result.Error = fmt.Errorf("creating directory %s: %w", archDir, err)
		return result
	}

	// Write the spec file with public symbols only
	writeOpts := domain.WriteOptions{
		OutputPath: outputPath,
		PublicOnly: true,
	}

	if err := s.d2Writer.Write(ctx, pkg, writeOpts); err != nil {
		result.Error = fmt.Errorf("writing spec file: %w", err)
		return result
	}

	result.Created = true
	return result
}
