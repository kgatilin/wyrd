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
}
