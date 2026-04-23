package domain

// VarDef represents a package-level variable declaration.
// Sentinel error variables (e.g. `var ErrNotFound = errors.New(...)`) are
// captured separately as ErrorDef rather than as VarDef.
type VarDef struct {
	// Name is the variable name.
	Name string

	// Type is the declared type of the variable. May be a zero-value
	// TypeRef if the type is inferred and cannot be resolved statically.
	Type TypeRef

	// IsExported indicates if this variable is exported (starts with uppercase).
	IsExported bool

	// SourceFile is the filename where this variable is defined.
	SourceFile string

	// Doc is the documentation comment for this variable.
	Doc string
}
