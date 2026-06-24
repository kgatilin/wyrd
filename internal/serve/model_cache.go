package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/buildinfo"
	"github.com/kgatilin/archai/internal/domain"
)

const (
	modelCacheSchema  = "archai.go-model-cache/v1"
	modelCacheVersion = 1
)

// modelCacheSchemaFingerprint is a structural hash of domain.PackageModel.
// It is recomputed from the live type at startup, so adding/removing/renaming
// any field anywhere in the model tree changes the fingerprint and forces
// stale caches (written by a binary with a different model shape) to be
// rejected and re-parsed. Without this, a cache written before a field was
// added (e.g. Span on the symbol types) is silently reused and JSON unmarshal
// leaves the new field zero-valued — which previously dropped source spans and
// broke search `file`/`body` and body-aware embeddings.
var modelCacheSchemaFingerprint = computeSchemaFingerprint(reflect.TypeOf(domain.PackageModel{}))

type modelCacheFile struct {
	Schema            string                 `json:"schema"`
	Version           int                    `json:"version"`
	SchemaFingerprint string                 `json:"schema_fingerprint"`
	BuildVersion      string                 `json:"build_version"`
	BinaryStamp       string                 `json:"binary_stamp"`
	WrittenAt         time.Time              `json:"written_at"`
	Packages          []domain.PackageModel  `json:"packages"`
	PackageFiles      map[string][]fileStamp `json:"package_files"`
	ProjectFiles      []fileStamp            `json:"project_files"`
}

// binaryIdentity reports the running binary's version and an on-disk stamp of
// its executable. It exists because the schema fingerprint only captures the
// SHAPE of domain.PackageModel — not the parser LOGIC that fills it. A parser
// improvement that emits different edges from identical source with an
// unchanged struct shape (e.g. new generic-type-argument edges) would
// otherwise be masked by a cache written by the previous binary. Comparing the
// build version plus the executable's mtime/size busts the cache on any
// rebuild-and-restart. Empty values are treated as "unknown" and force a
// re-parse, which is the safe direction (never serve stale).
func binaryIdentity() (version, stamp string) {
	version = buildinfo.Resolve().Version
	exe, err := os.Executable()
	if err != nil {
		return version, ""
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return version, ""
	}
	return version, fmt.Sprintf("%d:%d", fi.ModTime().UnixNano(), fi.Size())
}

// computeSchemaFingerprint walks the structural shape of t (field names and
// types, recursing through structs/slices/maps/pointers) and returns a stable
// short hash. Recursive types are broken with a type-name marker.
func computeSchemaFingerprint(t reflect.Type) string {
	var sb strings.Builder
	writeTypeShape(&sb, t, map[reflect.Type]bool{})
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:8])
}

func writeTypeShape(sb *strings.Builder, t reflect.Type, seen map[reflect.Type]bool) {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array:
		sb.WriteByte('[')
		writeTypeShape(sb, t.Elem(), seen)
		sb.WriteByte(']')
	case reflect.Map:
		sb.WriteString("map[")
		writeTypeShape(sb, t.Key(), seen)
		sb.WriteByte(']')
		writeTypeShape(sb, t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			sb.WriteString("@" + t.String())
			return
		}
		seen[t] = true
		sb.WriteByte('{')
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // unexported: not serialized by encoding/json
			}
			sb.WriteString(f.Name)
			sb.WriteByte(':')
			writeTypeShape(sb, f.Type, seen)
			sb.WriteByte(';')
		}
		sb.WriteByte('}')
	default:
		sb.WriteString(t.Kind().String())
	}
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
		buildVersion, binaryStamp := binaryIdentity()
		next := modelCacheFile{
			Schema:            modelCacheSchema,
			Version:           modelCacheVersion,
			SchemaFingerprint: modelCacheSchemaFingerprint,
			BuildVersion:      buildVersion,
			BinaryStamp:       binaryStamp,
			WrittenAt:         time.Now().UTC(),
			Packages:          models,
			PackageFiles:      current,
			ProjectFiles:      projectFiles,
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
	buildVersion, binaryStamp := binaryIdentity()
	return writeModelCache(s.root, modelCacheFile{
		Schema:            modelCacheSchema,
		Version:           modelCacheVersion,
		SchemaFingerprint: modelCacheSchemaFingerprint,
		BuildVersion:      buildVersion,
		BinaryStamp:       binaryStamp,
		WrittenAt:         time.Now().UTC(),
		Packages:          sortedPackageModels(models),
		PackageFiles:      packageFiles,
		ProjectFiles:      projectFiles,
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
	if cache.SchemaFingerprint != modelCacheSchemaFingerprint {
		return modelCacheFile{}, fmt.Errorf("cache model-shape fingerprint mismatch: got %q, want %q (model struct changed; re-parsing)",
			cache.SchemaFingerprint, modelCacheSchemaFingerprint)
	}
	if wantVersion, wantStamp := binaryIdentity(); cache.BuildVersion != wantVersion || cache.BinaryStamp != wantStamp {
		return modelCacheFile{}, fmt.Errorf("cache built by a different binary: got version %q stamp %q, want version %q stamp %q (parser logic may differ; re-parsing)",
			cache.BuildVersion, cache.BinaryStamp, wantVersion, wantStamp)
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
