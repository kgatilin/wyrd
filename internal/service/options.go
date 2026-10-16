// Package service provides business operations for diagram generation.
package service

import (
	"context"
	"errors"

	"github.com/kgatilin/wyrd/internal/domain"
)

// ErrNothingToModel is wrapped by a ModelReader when every package the paths
// named failed to load: a directory of Go files outside the main module, or
// whose files the build constraints all exclude. A whole-project read skips
// such a package with a warning; a read scoped to just those packages has
// nothing left to return, and a caller that asked about them alone needs to
// tell "these have no model" from a read that broke.
var ErrNothingToModel = errors.New("nothing to model")

// ModelReader reads package models from a source (code or diagrams).
// This interface is implemented by:
// - adapter/golang.Reader (reads Go source code)
// - adapter/d2.Reader (reads D2 files, planned for US-3)
type ModelReader interface {
	Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
}

// PartialModelReader is a ModelReader that can read part of a project without
// losing the facts that only the whole project carries. Which concrete types
// implement an interface is one of those: a read scoped to a few packages
// cannot see the implementations living in the rest, so an interface-dispatched
// call has nothing to fan out to and its edges are dropped. Callers that
// already hold models for the packages they are not re-reading pass them as
// `known`, and get edges as complete as a full read's.
//
// Implemented by adapter/golang.Reader. Callers type-assert and fall back to
// Read when the reader does not support it.
type PartialModelReader interface {
	ModelReader
	ReadPartial(ctx context.Context, paths []string, known []domain.PackageModel) ([]domain.PackageModel, error)
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
