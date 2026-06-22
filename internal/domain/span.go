package domain

// Span represents the source code location of a symbol definition.
// File is a path relative to the module root (e.g., "internal/serve/state.go").
// Byte and line offsets are 0-based.
type Span struct {
	// File is the path relative to the module root.
	File string

	// StartByte is the byte offset of the first character of the definition.
	StartByte int

	// EndByte is the byte offset past the last character of the definition.
	EndByte int

	// StartLine is the 1-based line number where the definition starts.
	StartLine int

	// EndLine is the 1-based line number where the definition ends.
	EndLine int
}

// IsValid returns true if the span has been populated with valid position data.
func (s Span) IsValid() bool {
	return s.File != "" && s.EndByte > s.StartByte
}
