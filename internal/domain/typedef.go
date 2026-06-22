package domain

// TypeDef represents a type alias or type definition.
// This is used primarily for detecting enum patterns where a type
// is defined with associated constants.
type TypeDef struct {
	// Name is the type name.
	Name string

	// TypeParams is the list of generic type parameters.
	TypeParams []ParamDef

	// UnderlyingType is the underlying type (e.g., "string" for "type Status string").
	UnderlyingType TypeRef

	// Constants is the list of constant names associated with this type.
	// Used to detect enum patterns (type with associated const values).
	Constants []string

	// IsExported indicates if this type is exported (starts with uppercase).
	IsExported bool

	// SourceFile is the filename where this type is defined, e.g., "status.go".
	SourceFile string

	// Span is the source location of the type definition.
	Span Span

	// Doc is the documentation comment for this type.
	Doc string

	// Stereotype is the DDD classification, either detected via heuristics
	// or explicitly set via an archspec annotation.
	Stereotype Stereotype
}

// IsEnum returns true if this type definition has associated constants,
// which is a common pattern for enums in Go.
func (t TypeDef) IsEnum() bool {
	return len(t.Constants) > 0
}
