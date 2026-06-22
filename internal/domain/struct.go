package domain

// StructDef represents a Go struct definition.
type StructDef struct {
	// Name is the struct name.
	Name string

	// TypeParams is the list of generic type parameters.
	TypeParams []ParamDef

	// Fields is the list of fields in this struct.
	Fields []FieldDef

	// Methods is the list of methods with this struct as receiver.
	Methods []MethodDef

	// IsExported indicates if this struct is exported (starts with uppercase).
	IsExported bool

	// SourceFile is the filename where this struct is defined, e.g., "model.go".
	SourceFile string

	// Span is the source location of the struct definition.
	Span Span

	// Doc is the documentation comment for this struct.
	Doc string

	// Stereotype is the DDD classification, either detected via heuristics
	// or explicitly set via an archspec annotation.
	Stereotype Stereotype
}
