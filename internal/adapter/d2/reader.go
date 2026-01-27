package d2

import (
	"context"
	"errors"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
)

// reader reads D2 diagram files and converts them to domain.PackageModel structures.
// This is a placeholder for US-3+ functionality.
type reader struct{}

// NewReader creates a new D2 diagram reader that implements service.ModelReader.
func NewReader() service.ModelReader {
	return &reader{}
}

// Read parses D2 diagram files and returns package models.
// This functionality is not yet implemented (planned for US-3).
func (r *reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	return nil, errors.New("d2.Reader: not implemented (planned for US-3)")
}
