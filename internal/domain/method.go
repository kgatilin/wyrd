package domain

import (
	"fmt"
	"strings"
)

// TypeRef represents a reference to a type, capturing whether it's
// a pointer, slice, map, and which package it belongs to.
type TypeRef struct {
	// Name is the type name, e.g., "PackageModel", "string", "error".
	Name string

	// Package is the package path for non-builtin types.
	// Empty for local types or builtins like "string", "error".
	Package string

	// IsPointer indicates if this is a pointer type (*T).
	IsPointer bool

	// IsSlice indicates if this is a slice type ([]T).
	IsSlice bool

	// IsMap indicates if this is a map type (map[K]V).
	// When true, KeyType and ValueType should be populated.
	IsMap bool

	// KeyType is the key type for maps (only set when IsMap is true).
	KeyType *TypeRef

	// ValueType is the value type for maps (only set when IsMap is true).
	ValueType *TypeRef
}

// String returns a human-readable representation of the type reference.
func (t TypeRef) String() string {
	var sb strings.Builder

	if t.IsSlice {
		sb.WriteString("[]")
	}

	if t.IsMap {
		sb.WriteString("map[")
		if t.KeyType != nil {
			sb.WriteString(t.KeyType.String())
		}
		sb.WriteString("]")
		if t.ValueType != nil {
			sb.WriteString(t.ValueType.String())
		}
		return sb.String()
	}

	if t.IsPointer {
		sb.WriteString("*")
	}

	if t.Package != "" {
		sb.WriteString(t.Package)
		sb.WriteString(".")
	}

	sb.WriteString(t.Name)

	return sb.String()
}

// ParamDef represents a function or method parameter.
type ParamDef struct {
	// Name is the parameter name. May be empty for unnamed parameters.
	Name string

	// Type is the type reference for this parameter.
	Type TypeRef
}

// String returns a human-readable representation of the parameter.
func (p ParamDef) String() string {
	if p.Name == "" {
		return p.Type.String()
	}
	return fmt.Sprintf("%s %s", p.Name, p.Type.String())
}

// MethodDef represents a method signature (for interfaces or structs).
type MethodDef struct {
	// Name is the method name.
	Name string

	// Params is the list of method parameters.
	Params []ParamDef

	// Returns is the list of return types.
	Returns []TypeRef

	// IsExported indicates if this method is exported (starts with uppercase).
	IsExported bool

	// Calls is the list of static call edges from this method's body to
	// other functions/methods within the loaded package set. Populated by
	// the Go reader's call-extraction pass.
	Calls []CallEdge
}

// Signature returns a formatted method signature string.
// Format: "MethodName(param1 Type1, param2 Type2) (ReturnType1, ReturnType2)"
func (m MethodDef) Signature() string {
	var sb strings.Builder

	sb.WriteString(m.Name)
	sb.WriteString("(")

	// Write parameters
	for i, p := range m.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.String())
	}

	sb.WriteString(")")

	// Write return types
	if len(m.Returns) > 0 {
		sb.WriteString(" ")
		if len(m.Returns) == 1 {
			sb.WriteString(m.Returns[0].String())
		} else {
			sb.WriteString("(")
			for i, r := range m.Returns {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(r.String())
			}
			sb.WriteString(")")
		}
	}

	return sb.String()
}

// SignatureWithVisibility returns the method signature with a visibility prefix.
// Exported methods get "+" prefix, unexported get "-" prefix.
func (m MethodDef) SignatureWithVisibility() string {
	prefix := "-"
	if m.IsExported {
		prefix = "+"
	}
	return prefix + m.Signature()
}
