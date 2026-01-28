// Package service provides business operations for diagram generation.
package service

import (
	"context"

	"github.com/kgatilin/archai/internal/domain"
)

// ModelReader reads package models from a source (code or diagrams).
// This interface is implemented by:
// - adapter/golang.Reader (reads Go source code)
// - adapter/d2.Reader (reads D2 files, planned for US-3)
type ModelReader interface {
	Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
}

// ModelWriter writes package models to a destination.
// This interface is implemented by:
// - adapter/d2.Writer (writes D2 diagram files)
type ModelWriter interface {
	// Write generates a diagram for a single package.
	Write(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error

	// WriteCombined generates a single diagram from multiple packages.
	// Combined mode always renders public API only with package-level containers.
	WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error
}

// ComposeMode specifies which diagram files to compose from.
type ComposeMode int

const (
	ComposeModeAuto ComposeMode = iota // Default: use pub.d2 files (code-generated)
	ComposeModeSpec                    // Only use *-spec.d2 files (target specs)
)

// ComposeOptions configures the compose operation.
type ComposeOptions struct {
	Paths      []string    // Package paths to search for .arch/ folders
	OutputPath string      // Required: path to output combined diagram
	Mode       ComposeMode // Which files to compose from
}

// ComposeResult contains the result of a compose operation.
type ComposeResult struct {
	OutputPath   string   // Path to generated combined diagram
	PackageCount int      // Number of packages included
	SkippedPaths []string // Packages skipped due to missing .arch/
}
