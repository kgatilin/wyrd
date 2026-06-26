package domain

// CallEdge represents a behavioral edge from one function/method to another,
// captured via static analysis of call expressions in the source AST.
//
// Direct calls (concrete function or method targets) have an empty Via.
// Calls dispatched through an interface value have Via set to the
// fully-qualified interface name (e.g., "io.Reader") and one edge per
// known implementation of that interface.
type CallEdge struct {
	// To is a reference to the called function or method.
	To SymbolRef

	// Via is the interface name when the call is dispatched through an
	// interface, in the form "pkg.Interface". Empty for direct calls.
	Via string

	// Order is the rank of this edge in source order within the enclosing
	// body, assigned by first occurrence of the (To, Via) target. The
	// reader emits Calls sorted by Order so renderers (sequence diagrams)
	// can follow the actual flow instead of an alphabetical set. Repeated
	// calls to the same target keep the rank of their first occurrence.
	Order int

	// Count is how many times this exact target is called within the body
	// (>= 1). Repeated call sites collapse onto one edge whose Count
	// records the multiplicity, so a renderer can annotate "xN" without
	// the edge list exploding.
	Count int
}
