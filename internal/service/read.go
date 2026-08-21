package service

import (
	"context"
	"fmt"

	"github.com/kgatilin/wyrd/internal/domain"
)

// ReadModels is the exported counterpart of readPackages. CLI callsites
// that want to bypass the full Generate pipeline (e.g. combined-full
// mode in cmd/wyrd) use it to read the model through the same entry
// point the pipeline uses.
func (s *Service) ReadModels(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	return s.readPackages(ctx, paths)
}

// readPackages reads the model for the given input paths. It is the
// single read entry point shared by generateInternal and
// generateCombined.
func (s *Service) readPackages(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	if s.goReader == nil {
		return nil, fmt.Errorf("service has no reader configured")
	}
	return s.goReader.Read(ctx, paths)
}
