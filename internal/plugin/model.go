package plugin

import (
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// Model is the unified read-only view of the project's architecture
// as seen by plugins. It composes the extracted Go model
// (domain.PackageModel slices) with the overlay-derived layers,
// rules, bounded contexts, aggregates, and configs.
//
// Provenance: every Package carries Layer/Aggregate fields populated
// by overlay.Merge, so callers can tell whether a piece of metadata
// came from code or from archai.yaml without an extra source field.
// A future Source field can be added without changing this struct's
// shape.
type Model struct {
	// Module is the Go module path from go.mod
	// (e.g. "github.com/kgatilin/archai").
	Module string

	// Packages is the merged list of code-extracted packages with
	// overlay-assigned Layer/Aggregate populated where applicable.
	Packages []*domain.PackageModel

	// Layers lists the architectural layers declared in archai.yaml.
	// Empty when no overlay is loaded.
	Layers []*Layer

	// LayerRules lists the per-layer outbound allow-lists. A layer
	// missing from this list is treated as "no outbound dependencies
	// allowed" (matching overlay.Merge's semantics).
	LayerRules []*LayerRule

	// BCs is the list of bounded contexts. Reserved for future
	// overlay extensions; populated empty in M12 since the current
	// overlay schema has no BC concept yet.
	BCs []*BoundedContext

	// Aggregates lists overlay-declared domain aggregates by name.
	Aggregates []*Aggregate

	// Configs lists fully-qualified type names tagged as
	// configuration entry points by archai.yaml.
	Configs []*ConfigType
}

// Layer is one entry from archai.yaml's layers map.
type Layer struct {
	// Name is the layer identifier (e.g. "domain", "application").
	Name string

	// PackageGlobs is the list of package globs that match into this
	// layer. Module-relative paths, identical to the on-disk YAML.
	PackageGlobs []string
}

// LayerRule encodes the outbound dependency allow-list for one layer.
type LayerRule struct {
	// Layer is the source layer.
	Layer string

	// AllowedLayers lists the layers Layer may import. Same-layer
	// imports are always implicitly allowed (mirrors overlay.Merge).
	AllowedLayers []string
}

// BoundedContext is reserved for a future overlay extension that
// groups aggregates into DDD-style contexts. Empty in M12.
type BoundedContext struct {
	Name        string
	Aggregates  []string
	Description string
}

// Aggregate is one entry from archai.yaml's aggregates map.
type Aggregate struct {
	// Name is the aggregate identifier.
	Name string

	// Root is the fully-qualified type name of the aggregate root,
	// e.g. "github.com/kgatilin/archai/internal/domain.PackageModel".
	Root string
}

// ConfigType is one entry from archai.yaml's configs list.
type ConfigType struct {
	// FQTypeName is the fully-qualified type name flagged as a
	// configuration entry point.
	FQTypeName string
}

// BuildModel composes packages + overlay config into a unified Model.
// pkgs may already have been overlay-merged (Layer/Aggregate populated)
// or not — BuildModel does not re-run the merge; it just plumbs the
// values through. cfg may be nil when no overlay is loaded.
//
// The returned Model holds pointers into pkgs so callers should not
// mutate the slice afterwards.
func BuildModel(module string, pkgs []domain.PackageModel, cfg *overlay.Config) *Model {
	m := &Model{Module: module}

	m.Packages = make([]*domain.PackageModel, len(pkgs))
	for i := range pkgs {
		m.Packages[i] = &pkgs[i]
	}

	if cfg == nil {
		return m
	}

	if module == "" && cfg.Module != "" {
		m.Module = cfg.Module
	}

	for name, globs := range cfg.Layers {
		globsCopy := make([]string, len(globs))
		copy(globsCopy, globs)
		m.Layers = append(m.Layers, &Layer{Name: name, PackageGlobs: globsCopy})
	}
	sortLayers(m.Layers)

	for name, allowed := range cfg.LayerRules {
		allowedCopy := make([]string, len(allowed))
		copy(allowedCopy, allowed)
		m.LayerRules = append(m.LayerRules, &LayerRule{Layer: name, AllowedLayers: allowedCopy})
	}
	sortLayerRules(m.LayerRules)

	for name, agg := range cfg.Aggregates {
		m.Aggregates = append(m.Aggregates, &Aggregate{Name: name, Root: agg.Root})
	}
	sortAggregates(m.Aggregates)

	for _, fq := range cfg.Configs {
		m.Configs = append(m.Configs, &ConfigType{FQTypeName: fq})
	}

	return m
}

// FindPackage returns the PackageModel with the given module-relative
// path, or nil when no such package is loaded.
func (m *Model) FindPackage(path string) *domain.PackageModel {
	if m == nil {
		return nil
	}
	for _, p := range m.Packages {
		if p != nil && p.Path == path {
			return p
		}
	}
	return nil
}

// PackagesInLayer returns the packages assigned to the named layer.
func (m *Model) PackagesInLayer(layer string) []*domain.PackageModel {
	if m == nil {
		return nil
	}
	var out []*domain.PackageModel
	for _, p := range m.Packages {
		if p != nil && p.Layer == layer {
			out = append(out, p)
		}
	}
	return out
}
