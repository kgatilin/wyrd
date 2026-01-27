package domain

import "fmt"

// DependencyKind represents the type of dependency between symbols.
type DependencyKind string

const (
	// DependencyUses indicates a symbol uses another as a parameter type.
	DependencyUses DependencyKind = "uses"

	// DependencyReturns indicates a symbol returns another type.
	DependencyReturns DependencyKind = "returns"

	// DependencyImplements indicates a struct implements an interface.
	DependencyImplements DependencyKind = "implements"
)

// String returns the string representation of the dependency kind.
func (k DependencyKind) String() string {
	return string(k)
}

// SymbolRef is a fully qualified reference to a symbol in the codebase.
type SymbolRef struct {
	// Package is the package path relative to module root, e.g., "internal/service".
	Package string

	// File is the filename where the symbol is defined, e.g., "service.go".
	File string

	// Symbol is the type or function name, e.g., "Service", "NewService".
	Symbol string

	// External indicates if this symbol is outside the module (e.g., "context.Context").
	External bool
}

// String returns a human-readable representation of the symbol reference.
func (r SymbolRef) String() string {
	if r.External {
		return fmt.Sprintf("%s.%s", r.Package, r.Symbol)
	}
	if r.Package == "" {
		return r.Symbol
	}
	return fmt.Sprintf("%s.%s", r.Package, r.Symbol)
}

// QualifiedName returns the fully qualified name for the symbol.
// For external symbols, returns "package.Symbol".
// For internal symbols, returns "package/path.Symbol".
func (r SymbolRef) QualifiedName() string {
	if r.Package == "" {
		return r.Symbol
	}
	return fmt.Sprintf("%s.%s", r.Package, r.Symbol)
}

// Dependency tracks a reference from one symbol to another.
type Dependency struct {
	// From is the symbol that has the dependency.
	From SymbolRef

	// To is the symbol being depended upon.
	To SymbolRef

	// Kind indicates the type of dependency (uses, returns, implements).
	Kind DependencyKind

	// ThroughExported indicates if this dependency is through an exported
	// method or field. Used for filtering public API dependencies.
	ThroughExported bool
}

// String returns a human-readable representation of the dependency.
func (d Dependency) String() string {
	return fmt.Sprintf("%s -> %s: %s", d.From.String(), d.To.String(), d.Kind.String())
}
