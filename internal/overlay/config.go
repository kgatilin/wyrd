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
	Module          string                     `yaml:"module"`
	Layers          map[string][]string        `yaml:"layers"`
	LayerRules      map[string][]string        `yaml:"layer_rules"`
	Aggregates      map[string]Aggregate       `yaml:"aggregates"`
	Configs         []string                   `yaml:"configs"`
	BoundedContexts map[string]BoundedContext  `yaml:"bounded_contexts,omitempty"`
	Adapters        map[string]Adapter         `yaml:"adapters,omitempty"`
	Serve           ServeConfig                `yaml:"serve,omitempty"`
}

// ServeConfig captures persistent settings for `archai serve`. Each
// field has a CLI flag counterpart that takes precedence when set;
// values here act as the project-level default so a workstation does
// not have to repeat them on every invocation.
//
// Field semantics:
//   - HTTPAddr: TCP listen address ("host:port") for the HTTP transport.
//     Empty falls through to the daemon's flag default.
type ServeConfig struct {
	HTTPAddr string `yaml:"http_addr,omitempty"`
}

// Aggregate describes a domain aggregate by its root type.
// Root is a fully-qualified type name, e.g.
// "github.com/kgatilin/archai/internal/domain.PackageModel".
type Aggregate struct {
	Root string `yaml:"root"`
}

// BoundedContext groups one or more aggregates into a DDD-style
// bounded context and (optionally) records its relationships with
// other contexts.
//
// Field semantics:
//   - Name: optional human-readable display name (e.g. "Model").
//     When empty, UIs should fall back to the map key.
//   - Description: optional human-readable summary.
//   - Aggregates: names of aggregates that belong to this context.
//     Each must be defined in Config.Aggregates. An aggregate may
//     belong to at most one bounded context.
//   - Upstream: names of bounded contexts this one depends on.
//   - Downstream: names of bounded contexts that depend on this one.
//   - Relationship: optional context-map relationship qualifier.
//     Allowed values: "shared-kernel", "customer-supplier",
//     "conformist", "acl", "open-host". Empty means unspecified.
type BoundedContext struct {
	Name         string   `yaml:"name,omitempty"`
	Description  string   `yaml:"description,omitempty"`
	Aggregates   []string `yaml:"aggregates,omitempty"`
	Upstream     []string `yaml:"upstream,omitempty"`
	Downstream   []string `yaml:"downstream,omitempty"`
	Relationship string   `yaml:"relationship,omitempty"`
}

// Adapter describes a hexagonal-architecture adapter: a concrete
// integration point between the domain model and the outside world.
// Adapters are classified by Direction so consumers (diagrams, the
// dashboard, lint rules) can render or reason about them as inbound
// (driving), outbound (driven), or bidirectional ports.
//
// Field semantics:
//   - Name: optional human-readable display name. When empty, UIs
//     should fall back to the map key.
//   - Direction: one of "inbound", "outbound", "bidirectional".
//     Required.
//   - Description: optional human-readable summary.
//   - Packages: package globs (relative to the module root) whose
//     contents implement the adapter. Same syntax as layer globs.
type Adapter struct {
	Name        string   `yaml:"name,omitempty"`
	Direction   string   `yaml:"direction"`
	Description string   `yaml:"description,omitempty"`
	Packages    []string `yaml:"packages,omitempty"`
}

// AdapterDirections is the closed set of allowed values for
// Adapter.Direction.
var AdapterDirections = []string{
	"inbound",
	"outbound",
	"bidirectional",
}

// BoundedContextRelationships is the closed set of allowed
// relationship qualifiers for BoundedContext.Relationship.
var BoundedContextRelationships = []string{
	"shared-kernel",
	"customer-supplier",
	"conformist",
	"acl",
	"open-host",
}
