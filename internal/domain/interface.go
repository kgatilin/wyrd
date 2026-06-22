package domain

// InterfaceDef represents a Go interface definition.
type InterfaceDef struct {
	// Name is the interface name.
	Name string

	// TypeParams is the list of generic type parameters.
	TypeParams []ParamDef

	// Methods is the list of methods declared in this interface.
	Methods []MethodDef

	// IsExported indicates if this interface is exported (starts with uppercase).
	IsExported bool

	// SourceFile is the filename where this interface is defined, e.g., "service.go".
	SourceFile string

	// Span is the source location of the interface definition.
	Span Span

	// Doc is the documentation comment for this interface.
	Doc string

	// Stereotype is the DDD classification, either detected via heuristics
	// or explicitly set via an archspec annotation.
	Stereotype Stereotype
}
