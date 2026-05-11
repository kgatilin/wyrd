package archgraph

import (
	"sort"

	"github.com/kgatilin/archai/internal/domain"
)

// NodeKind classifies a Node in the architecture graph.
type NodeKind string

const (
	NodeKindModule    NodeKind = "module"
	NodeKindPackage   NodeKind = "package"
	NodeKindFile      NodeKind = "file"
	NodeKindInterface NodeKind = "interface"
	NodeKindStruct    NodeKind = "struct"
	NodeKindTypeDef   NodeKind = "typedef"
	NodeKindFunction  NodeKind = "function"
	NodeKindMethod    NodeKind = "method"
	NodeKindField     NodeKind = "field"
	NodeKindConst     NodeKind = "const"
	NodeKindVar       NodeKind = "var"
	NodeKindError     NodeKind = "error"
	NodeKindExternal  NodeKind = "external"
)

// EdgeKind classifies an Edge in the architecture graph.
type EdgeKind string

const (
	// Structural containment.
	EdgeKindContains EdgeKind = "contains"

	// Module-level import (package -> package import set).
	EdgeKindImports EdgeKind = "imports"

	// Symbol-level dependency edges. These mirror domain.DependencyKind
	// 1:1 so projection back to []Dependency is lossless.
	EdgeKindUses       EdgeKind = "uses"
	EdgeKindReturns    EdgeKind = "returns"
	EdgeKindImplements EdgeKind = "implements"
	EdgeKindExtends    EdgeKind = "extends"
	EdgeKindNestedIn   EdgeKind = "nested-in"

	// Behavioral / annotation edges.
	EdgeKindCalls           EdgeKind = "calls"
	EdgeKindBelongsToLayer  EdgeKind = "belongs_to_layer"
	EdgeKindBelongsToDomain EdgeKind = "belongs_to_domain"
	EdgeKindHasRole         EdgeKind = "has_role"
)

// Node is a vertex in the architecture graph.
type Node struct {
	// ID is the stable, content-derived identifier (see doc.go).
	ID string

	// Kind classifies what this node represents.
	Kind NodeKind

	// Name is the human-readable label (e.g., the package or symbol
	// name). Not necessarily unique; ID is.
	Name string

	// Package is the owning package path for symbol-kind nodes,
	// empty for module / package / external nodes.
	Package string

	// File is the owning file basename for symbol-kind nodes when
	// known, empty otherwise.
	File string

	// Attrs carries scalar metadata that is also derivable from
	// Payload but kept here for graph-only consumers (browser /
	// diagram code that wants attribute access without a type
	// switch on Payload).
	Attrs map[string]string

	// Payload retains the original domain.* value (InterfaceDef,
	// StructDef, FunctionDef, ...) for symbol-kind nodes so that
	// ProjectPackages can reconstruct the input without fanning out
	// every field into Attrs. For module/package/file/external
	// nodes Payload is nil.
	Payload any
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	// ID is the stable, content-derived identifier (see doc.go).
	ID string

	// Kind classifies the relationship.
	Kind EdgeKind

	// From is the source node id.
	From string

	// To is the target node id.
	To string

	// Attrs carries scalar metadata (e.g., "through_exported": "true"
	// for symbol dependencies, "via": "<interface>" for interface
	// dispatched calls).
	Attrs map[string]string
}

// Graph is the bag of nodes and edges produced by BuildGraph. It is
// immutable by convention (callers may inspect but not mutate). Two
// Graphs are equal iff their node id set, edge id set, and per-node /
// per-edge attribute sets match.
type Graph struct {
	// ModuleID is the id of the single module node, or "" if no
	// module context was supplied to BuildGraph.
	ModuleID string

	// Nodes are sorted by ID for deterministic iteration.
	Nodes []Node

	// Edges are sorted by ID for deterministic iteration.
	Edges []Edge

	// implPayloads / depPayloads / callPayloads are unexported side
	// tables that let ProjectPackages reconstruct the input slices
	// exactly. Graph-native consumers (browser, diagrams, MCP) do
	// not need them; they exist solely to prove the graph carries
	// every field of the input PackageModel. As follow-ups #98 / #99
	// migrate callers, these may shrink or disappear.
	implPayloads map[string]domain.Implementation
	depPayloads  map[string]domain.Dependency
	callPayloads map[string]callPayload
}

// NodeByID returns the node with the given id and a found flag.
// O(log n) over the sorted Nodes slice.
func (g *Graph) NodeByID(id string) (Node, bool) {
	i := sort.Search(len(g.Nodes), func(i int) bool {
		return g.Nodes[i].ID >= id
	})
	if i < len(g.Nodes) && g.Nodes[i].ID == id {
		return g.Nodes[i], true
	}
	return Node{}, false
}

// NodesByKind returns all nodes of the given kind in stable id order.
func (g *Graph) NodesByKind(kind NodeKind) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// EdgesFrom returns all edges originating at the given node id, in
// stable id order.
func (g *Graph) EdgesFrom(id string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

// EdgesByKind returns all edges of the given kind in stable id order.
func (g *Graph) EdgesByKind(kind EdgeKind) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// payloadInterface extracts an InterfaceDef payload from a node.
func payloadInterface(n Node) (domain.InterfaceDef, bool) {
	if v, ok := n.Payload.(domain.InterfaceDef); ok {
		return v, true
	}
	return domain.InterfaceDef{}, false
}

// payloadStruct extracts a StructDef payload from a node.
func payloadStruct(n Node) (domain.StructDef, bool) {
	if v, ok := n.Payload.(domain.StructDef); ok {
		return v, true
	}
	return domain.StructDef{}, false
}

// payloadFunction extracts a FunctionDef payload from a node.
func payloadFunction(n Node) (domain.FunctionDef, bool) {
	if v, ok := n.Payload.(domain.FunctionDef); ok {
		return v, true
	}
	return domain.FunctionDef{}, false
}

// payloadTypeDef extracts a TypeDef payload from a node.
func payloadTypeDef(n Node) (domain.TypeDef, bool) {
	if v, ok := n.Payload.(domain.TypeDef); ok {
		return v, true
	}
	return domain.TypeDef{}, false
}

// payloadConst extracts a ConstDef payload from a node.
func payloadConst(n Node) (domain.ConstDef, bool) {
	if v, ok := n.Payload.(domain.ConstDef); ok {
		return v, true
	}
	return domain.ConstDef{}, false
}

// payloadVar extracts a VarDef payload from a node.
func payloadVar(n Node) (domain.VarDef, bool) {
	if v, ok := n.Payload.(domain.VarDef); ok {
		return v, true
	}
	return domain.VarDef{}, false
}

// payloadError extracts an ErrorDef payload from a node.
func payloadError(n Node) (domain.ErrorDef, bool) {
	if v, ok := n.Payload.(domain.ErrorDef); ok {
		return v, true
	}
	return domain.ErrorDef{}, false
}
