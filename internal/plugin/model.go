package plugin

import (
	"sort"

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

// BoundedContext groups aggregates into a DDD-style context and
// (optionally) records its context-map relationships with other
// contexts. Populated from archai.yaml's bounded_contexts: map
// (M14, issue #72).
type BoundedContext struct {
	// Name is the context identifier (map key in archai.yaml).
	Name string

	// Description is the optional human-readable summary.
	Description string

	// Aggregates lists the aggregate names that belong to this
	// context. Each name resolves to an entry in Model.Aggregates.
	Aggregates []string

	// Upstream lists the bounded contexts this one consumes from
	// (i.e. dependencies). Names refer to other entries in
	// Model.BCs.
	Upstream []string

	// Downstream lists the bounded contexts that consume from this
	// one. The loader normalises Upstream/Downstream so the graph
	// is bidirectional and de-duplicated.
	Downstream []string

	// Relationship is an optional context-map relationship
	// qualifier ("shared-kernel", "customer-supplier", "conformist",
	// "acl", "open-host"). Empty when unspecified.
	Relationship string
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

	if len(cfg.BoundedContexts) > 0 {
		m.BCs = buildBoundedContexts(cfg.BoundedContexts)
	}

	return m
}

// buildBoundedContexts converts the overlay map into the plugin.Model
// view, normalising upstream/downstream into a bidirectional, de-duplicated
// graph. For every "A upstream: [B]" the loader inserts a corresponding
// "B downstream: [A]" so widgets can render the graph without re-walking
// the inverse direction. Self-references and unknown contexts are dropped
// here (Validate has already flagged them as errors); we still skip them
// defensively so a partially valid model does not panic the UI.
func buildBoundedContexts(src map[string]overlay.BoundedContext) []*BoundedContext {
	// dedupe sets keyed by context name
	upstream := make(map[string]map[string]struct{}, len(src))
	downstream := make(map[string]map[string]struct{}, len(src))

	for name, bc := range src {
		if upstream[name] == nil {
			upstream[name] = make(map[string]struct{})
		}
		if downstream[name] == nil {
			downstream[name] = make(map[string]struct{})
		}
		for _, ref := range bc.Upstream {
			if ref == "" || ref == name {
				continue
			}
			if _, ok := src[ref]; !ok {
				continue
			}
			upstream[name][ref] = struct{}{}
			if downstream[ref] == nil {
				downstream[ref] = make(map[string]struct{})
			}
			downstream[ref][name] = struct{}{}
		}
		for _, ref := range bc.Downstream {
			if ref == "" || ref == name {
				continue
			}
			if _, ok := src[ref]; !ok {
				continue
			}
			downstream[name][ref] = struct{}{}
			if upstream[ref] == nil {
				upstream[ref] = make(map[string]struct{})
			}
			upstream[ref][name] = struct{}{}
		}
	}

	out := make([]*BoundedContext, 0, len(src))
	for name, bc := range src {
		aggsCopy := make([]string, len(bc.Aggregates))
		copy(aggsCopy, bc.Aggregates)

		out = append(out, &BoundedContext{
			Name:         name,
			Description:  bc.Description,
			Aggregates:   aggsCopy,
			Upstream:     sortedSetKeys(upstream[name]),
			Downstream:   sortedSetKeys(downstream[name]),
			Relationship: bc.Relationship,
		})
	}
	sortBoundedContexts(out)
	return out
}

func sortedSetKeys(s map[string]struct{}) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// FindBoundedContext returns the named bounded context, or nil when
// no context with that name is declared.
func (m *Model) FindBoundedContext(name string) *BoundedContext {
	if m == nil || name == "" {
		return nil
	}
	for _, bc := range m.BCs {
		if bc != nil && bc.Name == name {
			return bc
		}
	}
	return nil
}

// BoundedContextForAggregate returns the bounded context that contains
// the named aggregate, or nil when none does.
func (m *Model) BoundedContextForAggregate(aggregate string) *BoundedContext {
	if m == nil || aggregate == "" {
		return nil
	}
	for _, bc := range m.BCs {
		if bc == nil {
			continue
		}
		for _, a := range bc.Aggregates {
			if a == aggregate {
				return bc
			}
		}
	}
	return nil
}

// BoundedContextForPackage returns the bounded context whose
// aggregates include any aggregate assigned to the given package's
// declared Aggregate name. Returns nil when the package has no
// aggregate assigned or the aggregate is not part of any context.
func (m *Model) BoundedContextForPackage(pkg *domain.PackageModel) *BoundedContext {
	if m == nil || pkg == nil || pkg.Aggregate == "" {
		return nil
	}
	return m.BoundedContextForAggregate(pkg.Aggregate)
}

// PackagesInBoundedContext returns the packages whose Aggregate is in
// the named bounded context. Returns nil for unknown contexts.
func (m *Model) PackagesInBoundedContext(bcName string) []*domain.PackageModel {
	if m == nil {
		return nil
	}
	bc := m.FindBoundedContext(bcName)
	if bc == nil {
		return nil
	}
	in := make(map[string]struct{}, len(bc.Aggregates))
	for _, a := range bc.Aggregates {
		in[a] = struct{}{}
	}
	var out []*domain.PackageModel
	for _, p := range m.Packages {
		if p == nil {
			continue
		}
		if _, ok := in[p.Aggregate]; ok {
			out = append(out, p)
		}
	}
	return out
}
