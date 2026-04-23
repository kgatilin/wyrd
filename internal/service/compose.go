package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// Compose combines saved .arch/ spec files into a single diagram.
func (s *Service) Compose(ctx context.Context, opts ComposeOptions) (*ComposeResult, error) {
	// Validate options
	if opts.OutputPath == "" {
		return nil, errors.New("output path is required")
	}
	if len(opts.Paths) == 0 {
		return nil, errors.New("at least one package path is required")
	}

	// Find spec files (both D2 and YAML)
	files, skipped, err := findSpecFiles(opts.Paths, opts.Mode)
	if err != nil {
		return nil, fmt.Errorf("finding spec files: %w", err)
	}

	if len(files) == 0 {
		return nil, errors.New("no spec files found in specified packages")
	}

	// Partition files by format and read with the appropriate reader
	var d2Files, yamlFiles []string
	for _, f := range files {
		if strings.HasSuffix(f, ".yaml") || strings.HasSuffix(f, ".yml") {
			yamlFiles = append(yamlFiles, f)
		} else {
			d2Files = append(d2Files, f)
		}
	}

	var models []domain.PackageModel
	if len(d2Files) > 0 {
		m, err := s.d2Reader.Read(ctx, d2Files)
		if err != nil {
			return nil, fmt.Errorf("reading D2 spec files: %w", err)
		}
		models = append(models, m...)
	}
	if len(yamlFiles) > 0 && s.yamlReader != nil {
		m, err := s.yamlReader.Read(ctx, yamlFiles)
		if err != nil {
			return nil, fmt.Errorf("reading YAML spec files: %w", err)
		}
		models = append(models, m...)
	}

	// Determine output writer based on output file extension
	writer := s.d2Writer
	if (strings.HasSuffix(opts.OutputPath, ".yaml") || strings.HasSuffix(opts.OutputPath, ".yml")) && s.yamlWriter != nil {
		writer = s.yamlWriter
	}

	// Write combined diagram
	if err := writer.WriteCombined(ctx, models, opts.OutputPath); err != nil {
		return nil, fmt.Errorf("writing combined diagram: %w", err)
	}

	return &ComposeResult{
		OutputPath:   opts.OutputPath,
		PackageCount: len(models),
		SkippedPaths: skipped,
	}, nil
}

// findSpecFiles locates .arch/ spec files in package directories.
func findSpecFiles(paths []string, mode ComposeMode) (files []string, skipped []string, err error) {
	// Expand glob patterns first
	expandedPaths, err := expandGlobPatterns(paths)
	if err != nil {
		return nil, nil, err
	}

	for _, pkgPath := range expandedPaths {
		archDir := filepath.Join(pkgPath, ".arch")

		// Check if .arch directory exists
		info, err := os.Stat(archDir)
		if os.IsNotExist(err) || !info.IsDir() {
			skipped = append(skipped, pkgPath)
			continue
		}

		// Select file based on mode, trying D2 first then YAML
		var candidates []string
		switch mode {
		case ComposeModeSpec:
			candidates = []string{
				filepath.Join(archDir, "pub-spec.d2"),
				filepath.Join(archDir, "pub-spec.yaml"),
			}
		default: // ComposeModeAuto
			candidates = []string{
				filepath.Join(archDir, "pub.d2"),
				filepath.Join(archDir, "pub.yaml"),
			}
		}

		found := false
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				files = append(files, candidate)
				found = true
				break
			}
		}
		if !found {
			skipped = append(skipped, pkgPath)
			continue
		}
	}

	return files, skipped, nil
}

// expandGlobPatterns converts Go-style patterns to concrete paths.
func expandGlobPatterns(patterns []string) ([]string, error) {
	var paths []string
	for _, pattern := range patterns {
		if base, found := strings.CutSuffix(pattern, "/..."); found {
			// Recursive pattern - find all subdirectories

			// Check if base exists
			if _, err := os.Stat(base); os.IsNotExist(err) {
				return nil, fmt.Errorf("path does not exist: %s", base)
			}

			err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
					// Check if this looks like a Go package (has .go files or .arch folder)
					if hasGoFiles(path) || hasArchFolder(path) {
						paths = append(paths, path)
					}
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("expanding pattern %s: %w", pattern, err)
			}
		} else {
			// Concrete path - pass through unchanged
			paths = append(paths, pattern)
		}
	}
	return paths, nil
}

func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func hasArchFolder(dir string) bool {
	archPath := filepath.Join(dir, ".arch")
	info, err := os.Stat(archPath)
	return err == nil && info.IsDir()
}
