// Package overlay provides the schema and loader for archai.yaml,
// the user-authored overlay file that describes the target architecture
// (layers, layer rules, aggregates, and configs) for a Go module.
//
// The overlay is intentionally declarative: it names architectural
// concepts (layers, aggregates) and maps them to concrete Go packages
// and types. Later milestones (M3b, M3c) consume this Config to drive
// analysis, diffing, and CLI behavior; this package only defines the
// schema, loads it from disk, and validates it in isolation.
package overlay

// Config is the top-level archai.yaml document.
//
// Field semantics:
//   - Module: the Go module path (e.g. "github.com/kgatilin/archai").
//     Must match the module directive in go.mod.
//   - Layers: named architectural layers, each mapped to one or more
//     package globs (e.g. "internal/domain/...").
//   - LayerRules: per-layer allow-list of other layers it may depend on.
//     A layer absent from LayerRules is treated as "no outbound
//     dependencies allowed" by downstream consumers.
//   - Aggregates: named domain aggregates rooted at a single type.
//   - Configs: fully-qualified type names to surface as configuration
//     entry points.
type Config struct {
	Module     string               `yaml:"module"`
	Layers     map[string][]string  `yaml:"layers"`
	LayerRules map[string][]string  `yaml:"layer_rules"`
	Aggregates map[string]Aggregate `yaml:"aggregates"`
	Configs    []string             `yaml:"configs"`
}

// Aggregate describes a domain aggregate by its root type.
// Root is a fully-qualified type name, e.g.
// "github.com/kgatilin/archai/internal/domain.PackageModel".
type Aggregate struct {
	Root string `yaml:"root"`
}
