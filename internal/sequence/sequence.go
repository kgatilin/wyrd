// Package sequence builds and renders static call-sequence trees rooted at
// a given function or method. Input is a set of PackageModels populated by
// the Go reader (M6a); output is a Node tree that can be rendered as a
// text outline (via this package) or D2/SVG diagrams (via the D2 adapter).
package sequence

import "github.com/kgatilin/archai/internal/domain"

// Node is a single step in a rendered call chain. Root nodes represent the
// starting symbol; Children are the callees resolved from Symbol's Calls.
// Nodes flagged with Cycle are leaves — the caller should not recurse
// further, but the node is kept so renderers can show "(cycle)" markers.
// Nodes flagged with DepthLimit are leaves that were cut off because the
// --depth budget was exhausted; they indicate the sequence may have more
// structure that simply wasn't expanded.
type Node struct {
	// Symbol identifies the function/method at this step. For methods
	// Symbol.Symbol is "Type.Method"; for functions it is just the name.
	Symbol domain.SymbolRef

	// Via is the interface through which this node was reached from its
	// parent (empty for direct calls). Copied from CallEdge.Via and
	// useful for renderers that want to annotate interface dispatch.
	Via string

	// Children are the resolved callees.
	Children []*Node

	// Cycle is true when this node revisits an ancestor in the call
	// chain. Children is always empty when Cycle is true.
	Cycle bool

	// DepthLimit is true when this node was the depth-N cutoff — the
	// real call graph may continue, but the builder stopped recursing.
	DepthLimit bool

	// NotFound is true when the parent's CallEdge pointed at a symbol
	// that could not be resolved against the loaded models (e.g., the
	// symbol's package is not loaded). Children is empty.
	NotFound bool
}
