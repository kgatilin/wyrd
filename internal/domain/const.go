package domain

// ConstDef represents a package-level constant declaration.
// This captures standalone constants that are not associated with an
// enum-like TypeDef. Enum constants continue to live on their owning
// TypeDef.Constants slice.
type ConstDef struct {
	// Name is the constant name, e.g., "MaxRetries".
	Name string

	// Type is the declared type of the constant. May be a zero-value
	// TypeRef for untyped constants (e.g., `const Pi = 3.14`).
	Type TypeRef

	// Value is the literal value as it appears in source, e.g., `"hello"`,
	// "42", or "iota". Empty if the value cannot be represented as a
	// simple literal.
	Value string

	// IsExported indicates if this constant is exported (starts with uppercase).
	IsExported bool

	// SourceFile is the filename where this constant is defined.
	SourceFile string

	// Doc is the documentation comment for this constant.
	Doc string
}
