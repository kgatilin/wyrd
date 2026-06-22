package retrieval

import (
	"github.com/kgatilin/archai/internal/domain"
)

// Node represents a retrieval unit — a code symbol that can be searched
// and embedded. ID follows the uigraph convention: {PackagePath}.{SymbolName}.
type Node struct {
	// ID is the unique identifier matching uigraph's Internal.ID scheme:
	// "{PackagePath}.{SymbolName}" (e.g., "internal/serve.State").
	ID string

	// Kind identifies the symbol type. Values: iface, class, func, type,
	// const, var, error. Matches uigraph's Internal.Kind.
	Kind string

	// Package is the package path relative to the module root.
	Package string

	// Name is the symbol name without package prefix.
	Name string

	// Signature is the human-readable type signature (for funcs, methods,
	// types). Empty for consts/vars/errors.
	Signature string

	// Doc is the documentation comment for the symbol.
	Doc string

	// Span locates the symbol's source code for body extraction.
	Span domain.Span

	// Embeddable indicates whether this node should be embedded for
	// dense search. Determined by the embeddable predicate based on kind.
	Embeddable bool
}

// BuildNodes projects all symbols from the given package models into a
// flat slice of retrieval nodes. IDs are constructed using the same scheme
// as uigraph ({PackagePath}.{SymbolName}) to ensure consistency between
// search results and graph views.
func BuildNodes(models []domain.PackageModel) []Node {
	var nodes []Node

	for _, model := range models {
		// Interfaces
		for _, iface := range model.Interfaces {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, iface.Name),
				Kind:       "iface",
				Package:    model.Path,
				Name:       iface.Name,
				Signature:  interfaceSignature(iface),
				Doc:        iface.Doc,
				Span:       iface.Span,
				Embeddable: isEmbeddable("iface"),
			})
		}

		// Structs
		for _, s := range model.Structs {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, s.Name),
				Kind:       "class",
				Package:    model.Path,
				Name:       s.Name,
				Signature:  structSignature(s),
				Doc:        s.Doc,
				Span:       s.Span,
				Embeddable: isEmbeddable("class"),
			})
		}

		// Functions
		for _, fn := range model.Functions {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, fn.Name),
				Kind:       "func",
				Package:    model.Path,
				Name:       fn.Name,
				Signature:  fn.Signature(),
				Doc:        fn.Doc,
				Span:       fn.Span,
				Embeddable: isEmbeddable("func"),
			})
		}

		// Type definitions
		for _, td := range model.TypeDefs {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, td.Name),
				Kind:       "type",
				Package:    model.Path,
				Name:       td.Name,
				Signature:  typeDefSignature(td),
				Doc:        td.Doc,
				Span:       td.Span,
				Embeddable: isEmbeddable("type"),
			})
		}

		// Constants
		for _, c := range model.Constants {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, c.Name),
				Kind:       "const",
				Package:    model.Path,
				Name:       c.Name,
				Signature:  constSignature(c),
				Doc:        c.Doc,
				Span:       c.Span,
				Embeddable: isEmbeddable("const"),
			})
		}

		// Variables
		for _, v := range model.Variables {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, v.Name),
				Kind:       "var",
				Package:    model.Path,
				Name:       v.Name,
				Signature:  varSignature(v),
				Doc:        v.Doc,
				Span:       v.Span,
				Embeddable: isEmbeddable("var"),
			})
		}

		// Errors
		for _, e := range model.Errors {
			nodes = append(nodes, Node{
				ID:         nodeID(model.Path, e.Name),
				Kind:       "error",
				Package:    model.Path,
				Name:       e.Name,
				Signature:  "",
				Doc:        e.Doc,
				Span:       e.Span,
				Embeddable: isEmbeddable("error"),
			})
		}
	}

	return nodes
}

// nodeID constructs the canonical node identifier matching uigraph's scheme.
func nodeID(pkgPath, symbolName string) string {
	return pkgPath + "." + symbolName
}

// isEmbeddable returns true if nodes of the given kind should be embedded
// for dense search. This is the default predicate; it can be made
// configurable in the future.
//
// Embeddable: func, method (captured as part of struct), iface, class, type
// Not embeddable: const, var, error (typically too small to benefit)
func isEmbeddable(kind string) bool {
	switch kind {
	case "func", "iface", "class", "type":
		return true
	case "const", "var", "error":
		return false
	default:
		return false
	}
}

// Signature helpers to generate human-readable signatures

func interfaceSignature(iface domain.InterfaceDef) string {
	return "type " + domain.NameWithTypeParams(iface.Name, iface.TypeParams) + " interface"
}

func structSignature(s domain.StructDef) string {
	return "type " + domain.NameWithTypeParams(s.Name, s.TypeParams) + " struct"
}

func typeDefSignature(td domain.TypeDef) string {
	name := domain.NameWithTypeParams(td.Name, td.TypeParams)
	if td.UnderlyingType.Name == "" && td.UnderlyingType.Package == "" {
		return "type " + name
	}
	return "type " + name + " " + td.UnderlyingType.String()
}

func constSignature(c domain.ConstDef) string {
	sig := c.Name
	if c.Type.Name != "" || c.Type.Package != "" {
		sig += " " + c.Type.String()
	}
	if c.Value != "" {
		sig += " = " + c.Value
	}
	return sig
}

func varSignature(v domain.VarDef) string {
	if v.Type.Name == "" && v.Type.Package == "" {
		return v.Name
	}
	return v.Name + " " + v.Type.String()
}
