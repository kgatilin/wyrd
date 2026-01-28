package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	// Find spec files
	files, skipped, err := findSpecFiles(opts.Paths, opts.Mode)
	if err != nil {
		return nil, fmt.Errorf("finding spec files: %w", err)
	}

	if len(files) == 0 {
		return nil, errors.New("no spec files found in specified packages")
	}

	// Read spec files into models
	models, err := s.d2Reader.Read(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("reading spec files: %w", err)
	}

	// Write combined diagram
	if err := s.d2Writer.WriteCombined(ctx, models, opts.OutputPath); err != nil {
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

		// Select file based on mode
		var targetFile string
		switch mode {
		case ComposeModeSpec:
			targetFile = filepath.Join(archDir, "pub-spec.d2")
		default: // ComposeModeAuto
			targetFile = filepath.Join(archDir, "pub.d2")
		}

		// Check if target file exists
		if _, err := os.Stat(targetFile); os.IsNotExist(err) {
			skipped = append(skipped, pkgPath)
			continue
		}

		files = append(files, targetFile)
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
