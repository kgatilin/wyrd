package domain

// Implementation represents a concrete type implementing an interface.
// It records that a named concrete type (possibly via its pointer) satisfies
// the interface's method set.
type Implementation struct {
	// Concrete is the concrete type that implements the interface.
	// This may be from a different package than the interface.
	Concrete SymbolRef

	// Interface is the interface being implemented.
	Interface SymbolRef

	// IsPointer is true when the pointer type *T implements the interface
	// but the value type T does not (i.e., pointer-receiver methods).
	IsPointer bool
}
