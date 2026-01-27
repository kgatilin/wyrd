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

	// Debug enables verbose output for troubleshooting dependency detection.
	Debug bool

	// DebugPrintf is the function to use for debug output. If nil, fmt.Printf is used.
	DebugPrintf func(format string, args ...any)
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
		debugf("\n=== DEBUG: Package Analysis ===\n")
		debugf("Paths requested: %v\n", opts.Paths)
		debugf("Packages found: %d\n\n", len(packages))

		for _, pkg := range packages {
			debugf("--- Package: %s (name: %s) ---\n", pkg.Path, pkg.Name)
			debugf("  Interfaces: %d\n", len(pkg.Interfaces))
			for _, iface := range pkg.Interfaces {
				debugf("    - %s (exported: %v, file: %s, methods: %d)\n",
					iface.Name, iface.IsExported, iface.SourceFile, len(iface.Methods))
			}
			debugf("  Structs: %d\n", len(pkg.Structs))
			for _, s := range pkg.Structs {
				debugf("    - %s (exported: %v, file: %s, fields: %d, methods: %d)\n",
					s.Name, s.IsExported, s.SourceFile, len(s.Fields), len(s.Methods))
			}
			debugf("  Functions: %d\n", len(pkg.Functions))
			for _, fn := range pkg.Functions {
				debugf("    - %s (exported: %v, file: %s, stereotype: %v)\n",
					fn.Name, fn.IsExported, fn.SourceFile, fn.Stereotype)
			}
			debugf("  TypeDefs: %d\n", len(pkg.TypeDefs))
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
