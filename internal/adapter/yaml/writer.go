package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
	yamlv3 "gopkg.in/yaml.v3"
)

type writer struct{}

// NewWriter creates a new YAML ModelWriter.
func NewWriter() service.ModelWriter {
	return &writer{}
}

// Write generates a YAML file from a single package model.
func (w *writer) Write(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	spec := toSpec(model, opts.PublicOnly)
	data, err := yamlv3.Marshal(&spec)
	if err != nil {
		return fmt.Errorf("marshaling YAML for package %s: %w", model.Path, err)
	}

	if opts.ToStdout {
		fmt.Print(string(data))
		return nil
	}

	outputPath := opts.OutputPath
	if outputPath == "" {
		return fmt.Errorf("output path is required when not writing to stdout")
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", dir, err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing YAML file %s: %w", outputPath, err)
	}

	return nil
}

// WriteCombined generates a single YAML file from multiple packages.
// Combined mode always renders public API only.
func (w *writer) WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Sort packages for deterministic output.
	sorted := make([]domain.PackageModel, len(models))
	copy(sorted, models)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	var specs []PackageSpec
	for _, model := range sorted {
		if !model.HasExportedSymbols() {
			continue
		}
		specs = append(specs, toSpec(model, true))
	}

	// Wrap in a top-level document with schema version.
	doc := struct {
		Schema   string        `yaml:"schema"`
		Packages []PackageSpec `yaml:"packages"`
	}{
		Schema:   "archai/v1",
		Packages: specs,
	}

	data, err := yamlv3.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshaling combined YAML: %w", err)
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", dir, err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing combined YAML file %s: %w", outputPath, err)
	}

	return nil
}
