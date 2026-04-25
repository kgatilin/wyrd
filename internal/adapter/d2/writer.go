package d2

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
)

// writer writes domain.PackageModel structures as D2 diagram files.
type writer struct{}

// NewWriter creates a new D2 diagram writer that implements service.ModelWriter.
func NewWriter() service.ModelWriter {
	return &writer{}
}

// Write generates a D2 diagram from a package model and writes it to the output.
func (w *writer) Write(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error {
	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Build D2 content
	builder := newD2TextBuilder()
	content := builder.Build(model, opts.PublicOnly)

	// Write to stdout if requested
	if opts.ToStdout {
		fmt.Print(content)
		return nil
	}

	// Determine output path
	outputPath := opts.OutputPath
	if outputPath == "" {
		return fmt.Errorf("output path is required when not writing to stdout")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", dir, err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing D2 file %s: %w", outputPath, err)
	}

	return nil
}

// WriteCombined generates a single D2 diagram from multiple packages.
// The default render is the public API surface (OverviewModePublic).
// Callers that need internal detail should use WriteCombinedWithMode.
func (w *writer) WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error {
	return w.WriteCombinedWithMode(ctx, models, outputPath, OverviewModePublic)
}

// WriteCombinedWithMode is the mode-aware variant of WriteCombined.
// Empty / unknown modes fall back to OverviewModePublic.
func (w *writer) WriteCombinedWithMode(ctx context.Context, models []domain.PackageModel, outputPath string, mode OverviewMode) error {
	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Build combined D2 content using the requested mode.
	builder := newCombinedBuilderWithMode(mode)
	content := builder.Build(models)

	// Ensure parent directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", dir, err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing combined D2 file %s: %w", outputPath, err)
	}

	return nil
}

// BuildOverviewSource is the in-memory entry point used by HTTP/CLI
// callers when they need the D2 source string (rather than a file). It
// intentionally lives next to the writer so callers don't have to depend
// on the unexported builder.
func BuildOverviewSource(models []domain.PackageModel, mode OverviewMode) string {
	builder := newCombinedBuilderWithMode(mode)
	return builder.Build(models)
}
