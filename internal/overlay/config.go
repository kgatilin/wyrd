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
	Module          string                    `yaml:"module"`
	Layers          map[string][]string       `yaml:"layers"`
	LayerRules      map[string][]string       `yaml:"layer_rules"`
	Aggregates      map[string]Aggregate      `yaml:"aggregates"`
	Configs         []string                  `yaml:"configs"`
	BoundedContexts map[string]BoundedContext `yaml:"bounded_contexts,omitempty"`
	Adapters        map[string]Adapter        `yaml:"adapters,omitempty"`
	ReviewViews     map[string]ReviewView     `yaml:"review_views,omitempty"`
	ReviewGroups    map[string]ReviewGroup    `yaml:"review_groups,omitempty"`
	PackageOwners   map[string]PackageOwner   `yaml:"package_owners,omitempty"`
	Serve           ServeConfig               `yaml:"serve,omitempty"`
	Diagrams        DiagramConfig             `yaml:"diagrams,omitempty"`
}

// DiagramConfig captures project-level presentation settings for generated
// diagrams. Empty fields fall through to adapter defaults.
type DiagramConfig struct {
	D2 D2DiagramConfig `yaml:"d2,omitempty"`
}

// D2DiagramConfig contains settings for generated D2 source.
type D2DiagramConfig struct {
	Styles D2StylesConfig `yaml:"styles,omitempty"`
}

// D2StylesConfig overrides the semantic palette used by the D2 adapter.
type D2StylesConfig struct {
	Domain  D2SemanticStyle `yaml:"domain,omitempty"`
	Service D2SemanticStyle `yaml:"service,omitempty"`
	Factory D2SemanticStyle `yaml:"factory,omitempty"`
	Value   D2SemanticStyle `yaml:"value,omitempty"`
	Legend  D2LegendStyle   `yaml:"legend,omitempty"`
}

// D2SemanticStyle overrides one semantic style category. Container fields
// apply to package/file containers and legend samples. Class fields apply to
// D2 class-shaped symbols, where style.fill is also used for member-name text.
type D2SemanticStyle struct {
	ContainerFill      string `yaml:"container_fill,omitempty"`
	ContainerFontColor string `yaml:"container_font_color,omitempty"`
	ClassFill          string `yaml:"class_fill,omitempty"`
	ClassFontColor     string `yaml:"class_font_color,omitempty"`
}

// D2LegendStyle overrides the generated D2 legend container.
type D2LegendStyle struct {
	Fill   string `yaml:"fill,omitempty"`
	Stroke string `yaml:"stroke,omitempty"`
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

// ReviewView describes a named architecture review perspective. It is
// resolved server-side into concrete package/component ids before it reaches
// the UI; the UI should not parse package selector rules itself.
type ReviewView struct {
	Title            string          `yaml:"title,omitempty"`
	DefaultScope     string          `yaml:"default_scope,omitempty"`
	DefaultExpansion string          `yaml:"default_expansion,omitempty"`
	GroupBy          string          `yaml:"group_by,omitempty"`
	Packages         PackageSelector `yaml:"packages,omitempty"`
}

// ReviewGroup describes a user-configured package category for the review UI.
// It is resolved server-side into concrete package/component ids. Groups are
// evaluated in lexical key order, and the first matching group owns a package.
type ReviewGroup struct {
	Title    string          `yaml:"title,omitempty"`
	Packages PackageSelector `yaml:"packages,omitempty"`
}

// PackageSelector includes and excludes package paths using archai review
// selector syntax:
//   - "*" matches one package segment.
//   - "pkg/*" matches direct children of pkg.
//   - "pkg/..." matches pkg and all descendants.
//   - exact package paths match only that package.
//
// Exclude rules win over include rules. An empty Include list means "include
// everything" for that review view.
type PackageSelector struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

// PackageOwner describes a server-resolved owner grouping for packages in the
// review UI. It uses the same selector semantics as ReviewView.Packages, and
// the UI receives only resolved package-owner groups.
type PackageOwner struct {
	Name     string          `yaml:"name,omitempty"`
	Packages PackageSelector `yaml:"packages,omitempty"`
}

// ReviewScopes is the closed set of scope ids supported by review views.
var ReviewScopes = []string{
	"top_level_public_api",
	"all_public_api",
	"internal_implementation",
	"everything",
}

// ReviewExpansions is the closed set of initial component expansion policies
// a review view may request. "auto" keeps the UI's compact default, "changed"
// opens packages with local symbol diffs, "expanded" opens every package in the
// view, and "collapsed" opens none.
var ReviewExpansions = []string{
	"auto",
	"changed",
	"expanded",
	"collapsed",
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
