// Package archgraph defines a graph-shaped read model of an
// archai-analyzed codebase. It is the internal counterpart to the
// existing package-centric domain.PackageModel: every node and edge
// is a first-class data point with a stable, human-readable id, so
// downstream surfaces (browser, diagrams, MCP) can consume the
// architecture as a graph instead of as a denormalized aggregate.
//
// Issue: kgatilin/archai#97. This package is additive and is not
// wired into any reader, writer, or service path in this change;
// follow-ups #98 and #99 will migrate callers onto graph projections.
//
// # Dependency direction
//
// archgraph imports domain. domain does NOT import archgraph. The
// graph is a derived view; the original PackageModel slice remains
// authoritative input. Projection back to PackageModel exists to
// prove the graph carries every field of the input model.
//
// # Stable IDs
//
// IDs are derived purely from package path + symbol name so the
// same input always produces byte-identical id sets. The scheme
// mirrors the one used by internal/adapter/archmotif so a single
// vocabulary covers both internal projections and the external
// archmotif export.
//
//	module     mod:<module-path>
//	package    pkg:<package-path>
//	file       file:<package-path>/<base>
//	interface  type:<package-path>.<Name>
//	struct     type:<package-path>.<Name>
//	typedef    type:<package-path>.<Name>
//	function   fn:<package-path>.<Name>
//	method     method:<package-path>.<Recv>.<Name>
//	field      field:<package-path>.<Struct>.<Field>
//	const      const:<package-path>.<Name>
//	var        var:<package-path>.<Name>
//	error      err:<package-path>.<Name>
//	external   ext:<package>.<Symbol>
//
// Edge ids are derived from kind + endpoint ids:
//
//	<kind>:<from>-><to>
//
// # Round-trip
//
// BuildGraph(models, mod) produces a Graph that, when fed to
// Graph.ProjectPackages(), reconstructs the input []PackageModel
// modulo deterministic sort order. The graph achieves this by
// retaining typed payloads on symbol-kind nodes; later changes
// may shrink payloads to attribute maps as graph-native consumers
// arrive in #98 / #99.
package archgraph
