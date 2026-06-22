package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/domain"
)

const (
	modelCacheSchema  = "archai.go-model-cache/v1"
	modelCacheVersion = 1
)

type modelCacheFile struct {
	Schema       string                 `json:"schema"`
	Version      int                    `json:"version"`
	WrittenAt    time.Time              `json:"written_at"`
	Packages     []domain.PackageModel  `json:"packages"`
	PackageFiles map[string][]fileStamp `json:"package_files"`
	ProjectFiles []fileStamp            `json:"project_files"`
}

type fileStamp struct {
	Path    string `json:"path"`
	ModUnix int64  `json:"mod_unix_nano"`
	Size    int64  `json:"size"`
}

func (s *State) readAll(ctx context.Context) ([]domain.PackageModel, error) {
	models, cache, ok := s.readModelCache(ctx)
	if ok {
		return models, nil
	}
	if cache != nil && cache.err != nil && !errors.Is(cache.err, errCacheMiss) {
		fmt.Fprintf(os.Stderr, "serve: ignoring model cache: %v\n", cache.err)
	}
	return s.readAllUncached(ctx)
}

func (s *State) readModelCache(ctx context.Context) ([]domain.PackageModel, *modelCacheLoad, bool) {
	cache, err := loadModelCache(s.root)
	load := &modelCacheLoad{cache: cache, err: err}
	if err != nil {
		return nil, load, false
	}

	current, projectFiles, err := scanModelCacheInputs(s.root)
	if err != nil {
		load.err = err
		return nil, load, false
	}
	if !sameFileStamps(cache.ProjectFiles, projectFiles) {
		load.err = fmt.Errorf("project file changed")
		return nil, load, false
	}

	cachedByPath := make(map[string]domain.PackageModel, len(cache.Packages))
	for _, pkg := range cache.Packages {
		cachedByPath[pkg.Path] = pkg
	}

	changed := make([]string, 0)
	for pkgPath, files := range current {
		cachedFiles, ok := cache.PackageFiles[pkgPath]
		if !ok || !sameFileStamps(cachedFiles, files) {
			changed = append(changed, pkgPath)
		}
	}
	sort.Strings(changed)

	modelsByPath := make(map[string]domain.PackageModel, len(current))
	for pkgPath := range current {
		if containsString(changed, pkgPath) {
			continue
		}
		if pkg, ok := cachedByPath[pkgPath]; ok {
			modelsByPath[pkgPath] = pkg
		}
	}

	if len(changed) > 0 {
		reloaded, err := s.readPackageSet(ctx, changed)
		if err != nil {
			load.err = fmt.Errorf("reload changed packages from cache: %w", err)
			return nil, load, false
		}
		for _, pkg := range reloaded {
			if _, ok := current[pkg.Path]; ok {
				modelsByPath[pkg.Path] = pkg
			}
		}
	}

	models := make([]domain.PackageModel, 0, len(modelsByPath))
	for pkgPath := range current {
		if pkg, ok := modelsByPath[pkgPath]; ok {
			models = append(models, pkg)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Path < models[j].Path })

	needsWrite := len(changed) > 0 ||
		len(cache.PackageFiles) != len(current) ||
		len(cache.Packages) != len(models)
	if needsWrite {
		next := modelCacheFile{
			Schema:       modelCacheSchema,
			Version:      modelCacheVersion,
			WrittenAt:    time.Now().UTC(),
			Packages:     models,
			PackageFiles: current,
			ProjectFiles: projectFiles,
		}
		if err := writeModelCache(s.root, next); err != nil {
			fmt.Fprintf(os.Stderr, "serve: write model cache: %v\n", err)
		}
	}

	return models, load, true
}

type modelCacheLoad struct {
	cache modelCacheFile
	err   error
}

func (s *State) readAllUncached(ctx context.Context) ([]domain.PackageModel, error) {
	models, err := s.readPackages(ctx, []string{"./..."})
	if err != nil {
		return nil, err
	}
	if err := s.writeModelCache(models); err != nil {
		fmt.Fprintf(os.Stderr, "serve: write model cache: %v\n", err)
	}
	return models, nil
}

func (s *State) readPackageSet(ctx context.Context, pkgPaths []string) ([]domain.PackageModel, error) {
	patterns := make([]string, 0, len(pkgPaths))
	for _, pkgPath := range pkgPaths {
		patterns = append(patterns, packageLoadPattern(pkgPath))
	}
	return s.readPackages(ctx, patterns)
}

func (s *State) readPackages(ctx context.Context, patterns []string) ([]domain.PackageModel, error) {
	prevCwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(s.root); err != nil {
		return nil, err
	}
	defer func() { _ = os.Chdir(prevCwd) }()
	return s.reader.Read(ctx, patterns)
}

func packageLoadPattern(pkgPath string) string {
	if pkgPath == "." || pkgPath == "" {
		return "./"
	}
	return "./" + strings.TrimPrefix(pkgPath, "./")
}

func (s *State) writeModelCache(models []domain.PackageModel) error {
	packageFiles, projectFiles, err := scanModelCacheInputs(s.root)
	if err != nil {
		return err
	}
	return writeModelCache(s.root, modelCacheFile{
		Schema:       modelCacheSchema,
		Version:      modelCacheVersion,
		WrittenAt:    time.Now().UTC(),
		Packages:     sortedPackageModels(models),
		PackageFiles: packageFiles,
		ProjectFiles: projectFiles,
	})
}

func loadModelCache(root string) (modelCacheFile, error) {
	data, err := os.ReadFile(modelCachePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return modelCacheFile{}, errCacheMiss
		}
		return modelCacheFile{}, err
	}
	var cache modelCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return modelCacheFile{}, err
	}
	if cache.Schema != modelCacheSchema || cache.Version != modelCacheVersion {
		return modelCacheFile{}, fmt.Errorf("cache schema/version mismatch: got %q v%d, want %q v%d",
			cache.Schema, cache.Version, modelCacheSchema, modelCacheVersion)
	}
	if cache.PackageFiles == nil {
		return modelCacheFile{}, fmt.Errorf("cache missing package manifest")
	}
	return cache, nil
}

var errCacheMiss = errors.New("model cache miss")

func writeModelCache(root string, cache modelCacheFile) error {
	path := modelCachePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".go-model-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func modelCachePath(root string) string {
	return filepath.Join(root, ".archai", "cache", "go-model.json")
}

func scanModelCacheInputs(root string) (map[string][]fileStamp, []fileStamp, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	packages := make(map[string][]fileStamp)
	err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != rootAbs && skipModelCacheDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isGoSourceFile(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		dir := filepath.Dir(path)
		relDir, err := filepath.Rel(rootAbs, dir)
		if err != nil {
			return nil
		}
		pkgPath := filepath.ToSlash(relDir)
		if pkgPath == "." {
			pkgPath = "."
		}
		relFile, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return nil
		}
		packages[pkgPath] = append(packages[pkgPath], stampFor(filepath.ToSlash(relFile), info))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for pkgPath := range packages {
		sortFileStamps(packages[pkgPath])
	}
	projectFiles, err := scanProjectFileStamps(rootAbs)
	if err != nil {
		return nil, nil, err
	}
	return packages, projectFiles, nil
}

func scanProjectFileStamps(root string) ([]fileStamp, error) {
	names := []string{"go.mod", "go.sum"}
	out := make([]fileStamp, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.Mode().IsRegular() {
			out = append(out, stampFor(name, info))
		}
	}
	sortFileStamps(out)
	return out, nil
}

func stampFor(rel string, info os.FileInfo) fileStamp {
	return fileStamp{
		Path:    filepath.ToSlash(rel),
		ModUnix: info.ModTime().UnixNano(),
		Size:    info.Size(),
	}
}

func isGoSourceFile(name string) bool {
	return strings.HasSuffix(name, ".go")
}

func skipModelCacheDir(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "testdata":
		return true
	}
	return false
}

func sameFileStamps(a, b []fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]fileStamp(nil), a...)
	bb := append([]fileStamp(nil), b...)
	sortFileStamps(aa)
	sortFileStamps(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func sortFileStamps(stamps []fileStamp) {
	sort.Slice(stamps, func(i, j int) bool { return stamps[i].Path < stamps[j].Path })
}

func sortedPackageModels(models []domain.PackageModel) []domain.PackageModel {
	out := append([]domain.PackageModel(nil), models...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func containsString(values []string, target string) bool {
	i := sort.SearchStrings(values, target)
	return i < len(values) && values[i] == target
}
