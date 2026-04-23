package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
	yamlv3 "gopkg.in/yaml.v3"
)

type reader struct{}

// NewReader creates a new YAML ModelReader.
func NewReader() service.ModelReader {
	return &reader{}
}

// Read parses YAML files at the given paths and returns package models.
// Supports both single-package (.arch/pub.yaml) and combined multi-package files.
func (r *reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	var result []domain.PackageModel

	for _, path := range paths {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		models, err := r.readPath(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		result = append(result, models...)
	}

	return result, nil
}

func (r *reader) readPath(path string) ([]domain.PackageModel, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	if info.IsDir() {
		return r.readDirectory(path)
	}

	return r.readFile(path)
}

// readDirectory scans for .yaml files in .arch/ subdirectories.
func (r *reader) readDirectory(root string) ([]domain.PackageModel, error) {
	var result []domain.PackageModel

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		// Only read files inside .arch directories.
		if !strings.Contains(path, ".arch"+string(filepath.Separator)) &&
			!strings.Contains(path, ".arch/") {
			return nil
		}

		models, err := r.readFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		result = append(result, models...)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// readFile parses a single YAML file. Detects whether it's a single-package
// or combined multi-package document.
func (r *reader) readFile(path string) ([]domain.PackageModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	// Try combined format first (has "packages" key).
	var combined struct {
		Schema   string        `yaml:"schema"`
		Packages []PackageSpec `yaml:"packages"`
	}
	if err := yamlv3.Unmarshal(data, &combined); err == nil && len(combined.Packages) > 0 {
		var models []domain.PackageModel
		for _, spec := range combined.Packages {
			models = append(models, fromSpec(spec))
		}
		return models, nil
	}

	// Try single-package format.
	var spec PackageSpec
	if err := yamlv3.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing YAML in %s: %w", path, err)
	}

	if spec.Package == "" && spec.Name == "" {
		return nil, nil // Empty or unrecognized file.
	}

	return []domain.PackageModel{fromSpec(spec)}, nil
}
