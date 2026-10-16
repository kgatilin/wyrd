package serve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/service"
)

// readPackageSet re-reads part of the project. known carries the models the
// caller holds for everything it is not re-reading: without them the reader
// cannot see which types implement an interface outside this set, and every
// interface-dispatched call out of the re-read packages loses its edges.
func (s *State) readPackageSet(ctx context.Context, pkgPaths []string, known []domain.PackageModel) ([]domain.PackageModel, error) {
	patterns := make([]string, 0, len(pkgPaths))
	for _, pkgPath := range pkgPaths {
		patterns = append(patterns, packageLoadPattern(pkgPath))
	}
	return s.readPackagesWithKnown(ctx, patterns, known)
}

// readPackagesMu serializes the chdir the Go reader needs. The working
// directory is process-global, so two loads running at once — two worktrees
// warming concurrently, or a worktree and a materialized base commit — could
// otherwise parse each other's tree.
var readPackagesMu sync.Mutex

func (s *State) readPackages(ctx context.Context, patterns []string) ([]domain.PackageModel, error) {
	return s.readPackagesWithKnown(ctx, patterns, nil)
}

func (s *State) readPackagesWithKnown(ctx context.Context, patterns []string, known []domain.PackageModel) ([]domain.PackageModel, error) {
	readPackagesMu.Lock()
	defer readPackagesMu.Unlock()

	prevCwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(s.root); err != nil {
		return nil, err
	}
	defer func() { _ = os.Chdir(prevCwd) }()
	if partial, ok := s.reader.(service.PartialModelReader); ok && len(known) > 0 {
		return partial.ReadPartial(ctx, patterns, known)
	}
	return s.reader.Read(ctx, patterns)
}

func packageLoadPattern(pkgPath string) string {
	if pkgPath == "." || pkgPath == "" {
		return "./"
	}
	return "./" + strings.TrimPrefix(pkgPath, "./")
}

// goPackageDirs returns the module-relative directories under root that hold
// Go source, skipping the trees the Go tool skips (dot and underscore
// directories, testdata, vendor, node_modules).
func goPackageDirs(root string) (map[string]bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dirs := make(map[string]bool)
	err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootAbs && skipSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, filepath.Dir(path))
		if err != nil {
			return nil
		}
		dirs[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dirs, nil
}

func skipSourceDir(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "testdata":
		return true
	}
	return false
}
