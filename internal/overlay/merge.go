package overlay

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// Violation describes a forbidden cross-layer import discovered during
// Merge. It lists the offending source package, the layer it was
// assigned to, and the imported package paths (module-relative) whose
// layers are not allowed by LayerRules for the source layer.
type Violation struct {
	// Package is the module-relative path of the importing package
	// (e.g. "internal/service").
	Package string

	// Layer is the layer assigned to Package.
	Layer string

	// Imports lists the module-relative paths of imported packages
	// whose layers are not allowed by the source layer's rules. The
	// entries are sorted lexically for deterministic output.
	Imports []string
}

// Merge applies cfg to models: it annotates each PackageModel with its
// Layer and Aggregate (when applicable) and returns the list of
// layer-rule violations discovered among the packages' dependencies.
//
// Rules:
//   - For each package, Merge assigns the first layer whose globs match
//     pkg.Path (iteration order of layer names is lexical, so results
//     are deterministic).
//   - For each aggregate, Merge assigns pkg.Aggregate to the package
//     that contains the aggregate root type. The root is encoded as
//     "<module>/<pkgPath>.<TypeName>"; Merge strips cfg.Module to match
//     pkg.Path.
//   - For each Dependency in a PackageModel, if the target is internal
//     (External == false) and resolves to a known layer, and that
//     layer is not in the source layer's LayerRules allow-list, a
//     Violation is recorded.
//
// Merge returns the updated slice (same backing array, fields mutated
// in place) and an aggregated []Violation (one entry per source
// package with at least one forbidden import).
//
// TODO(M3c): Configs tagging is not implemented here. Configs apply
// to specific Go types rather than whole packages, so it requires
// per-type annotation support in the domain model (e.g. on StructDef
// or TypeDef). Once that exists, Merge should also tag the matching
// type definitions.
func Merge(models []domain.PackageModel, cfg *Config) ([]domain.PackageModel, []Violation, error) {
	if cfg == nil {
		return models, nil, fmt.Errorf("overlay: nil config")
	}

	layerNames := sortedKeys(cfg.Layers)

	// Assign layer to each package.
	for i := range models {
		pkgPath := modulePath(cfg.Module, models[i].Path)
		for _, layer := range layerNames {
			if matchAnyGlob(cfg.Layers[layer], pkgPath) {
				models[i].Layer = layer
				break
			}
		}
	}

	// Assign aggregates. Aggregates are keyed by name; iterate in
	// lexical order for deterministic assignment when multiple
	// aggregates somehow resolve to the same package.
	aggNames := sortedKeys(cfg.Aggregates)
	for _, name := range aggNames {
		agg := cfg.Aggregates[name]
		pkgPath, typeName, ok := splitAggregateRoot(cfg.Module, agg.Root)
		if !ok {
			// Validation catches malformed refs; at this point just skip.
			continue
		}
		_ = typeName // reserved for future per-type tagging (Configs TODO)
		for i := range models {
			if models[i].Path == pkgPath {
				models[i].Aggregate = name
				break
			}
		}
	}

	// Build a quick lookup from pkg path -> layer for violation detection.
	pkgLayer := make(map[string]string, len(models))
	for _, m := range models {
		if m.Layer != "" {
			pkgLayer[m.Path] = m.Layer
		}
	}

	// Detect violations.
	var violations []Violation
	for _, m := range models {
		if m.Layer == "" {
			continue
		}
		allowed, hasRule := cfg.LayerRules[m.Layer]
		allowSet := make(map[string]struct{}, len(allowed))
		for _, a := range allowed {
			allowSet[a] = struct{}{}
		}
		// Same-layer imports are always allowed.
		allowSet[m.Layer] = struct{}{}

		badImports := make(map[string]struct{})
		for _, dep := range m.Dependencies {
			if dep.To.External {
				continue
			}
			if !strings.HasPrefix(dep.To.Package, cfg.Module) {
				// Not a package from this module — skip.
				continue
			}
			relPath := modulePath(cfg.Module, dep.To.Package)
			if relPath == "" || relPath == m.Path {
				continue
			}
			targetLayer, ok := pkgLayer[relPath]
			if !ok {
				// Target has no layer — nothing to check.
				continue
			}
			if !hasRule {
				// No outbound rules: any cross-layer import is a violation.
				if targetLayer != m.Layer {
					badImports[relPath] = struct{}{}
				}
				continue
			}
			if _, ok := allowSet[targetLayer]; !ok {
				badImports[relPath] = struct{}{}
			}
		}
		if len(badImports) > 0 {
			imps := make([]string, 0, len(badImports))
			for k := range badImports {
				imps = append(imps, k)
			}
			sort.Strings(imps)
			violations = append(violations, Violation{
				Package: m.Path,
				Layer:   m.Layer,
				Imports: imps,
			})
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		return violations[i].Package < violations[j].Package
	})

	return models, violations, nil
}

// modulePath strips the module prefix from a fully-qualified package
// path, returning the module-relative path used by overlay globs.
// If pkgPath is already relative (no module prefix) it is returned as-is.
func modulePath(module, pkgPath string) string {
	if module == "" {
		return pkgPath
	}
	if pkgPath == module {
		return ""
	}
	if strings.HasPrefix(pkgPath, module+"/") {
		return strings.TrimPrefix(pkgPath, module+"/")
	}
	return pkgPath
}

// splitAggregateRoot splits a fully-qualified aggregate root reference
// ("<module>/<pkg>.<Type>") into its module-relative package path and
// type name. Returns ok=false if the reference is malformed or does
// not belong to cfg.Module.
func splitAggregateRoot(module, root string) (pkgPath, typeName string, ok bool) {
	dot := strings.LastIndex(root, ".")
	if dot <= 0 || dot == len(root)-1 {
		return "", "", false
	}
	fqPkg := root[:dot]
	typeName = root[dot+1:]
	pkgPath = modulePath(module, fqPkg)
	// If modulePath returned the input unchanged and it still starts
	// with a domain-looking prefix, treat it as not belonging to the
	// module. Merge only tags types inside the current module.
	if module != "" && fqPkg != module && !strings.HasPrefix(fqPkg, module+"/") {
		return "", "", false
	}
	return pkgPath, typeName, true
}

// matchAnyGlob reports whether any glob in globs matches pkgPath.
func matchAnyGlob(globs []string, pkgPath string) bool {
	for _, g := range globs {
		if matchGlob(g, pkgPath) {
			return true
		}
	}
	return false
}

// matchGlob implements overlay glob matching. Supported forms:
//   - "pkg/..."  matches pkg and any sub-package (recursive prefix).
//   - "pkg/*"    matches exactly one additional path segment.
//   - "pkg/foo"  exact match.
//
// The leading segments before "*" or "..." must match literally.
// Matching is done on module-relative package paths (forward slashes).
func matchGlob(glob, pkgPath string) bool {
	// Recursive "..." suffix.
	if strings.HasSuffix(glob, "/...") {
		prefix := strings.TrimSuffix(glob, "/...")
		return pkgPath == prefix || strings.HasPrefix(pkgPath, prefix+"/")
	}
	if glob == "..." {
		return true
	}
	// Single-segment "*" wildcard. Only support trailing-segment
	// wildcards plus any pure filepath.Match pattern. We normalize
	// using path.Match (forward slashes) for POSIX semantics.
	if strings.Contains(glob, "*") {
		ok, _ := path.Match(glob, pkgPath)
		if ok {
			return true
		}
		// Fall back to filepath.Match for platforms where path.Match
		// rejects patterns with embedded slashes in segments.
		ok2, _ := filepath.Match(glob, pkgPath)
		return ok2
	}
	// Exact match.
	return glob == pkgPath
}
