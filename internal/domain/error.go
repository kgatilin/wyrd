package domain

// ErrorDef represents a sentinel error declaration such as
// `var ErrNotFound = errors.New("not found")` or
// `var ErrBadArg = fmt.Errorf("bad argument: %w", ErrInput)`.
type ErrorDef struct {
	// Name is the error variable name, e.g., "ErrNotFound".
	Name string

	// Message is the error message string extracted from the constructor
	// call (errors.New or fmt.Errorf). Empty if the message is not a
	// simple string literal.
	Message string

	// IsExported indicates if this error is exported (starts with uppercase).
	IsExported bool

	// SourceFile is the filename where this error is defined.
	SourceFile string

	// Doc is the documentation comment for this error.
	Doc string
}
