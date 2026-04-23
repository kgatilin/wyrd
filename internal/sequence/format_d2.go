package sequence

import (
	"fmt"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// FormatD2 renders the Node tree as a D2 sequence diagram. Each unique
// actor (package leaf plus optional type for methods) becomes an
// implicit participant; every call becomes an arrow from caller to
// callee labeled with the method (or function) name. Interface
// dispatch edges are annotated with "[via pkg.Iface]".
//
// Example output:
//
//	shape: sequence_diagram
//	service.Service -> golang.reader: Read
//	golang.reader -> service.Service: validate
//
// Cycle, unresolved, and depth-limit leaves are emitted as labeled
// arrows so the diagram still communicates why a branch terminated.
func FormatD2(root *Node) string {
	if root == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("shape: sequence_diagram\n")
	writeD2Edges(&sb, root)
	return sb.String()
}

func writeD2Edges(sb *strings.Builder, n *Node) {
	caller := actorName(n.Symbol)
	for _, c := range n.Children {
		callee := actorName(c.Symbol)
		label := methodLabel(c.Symbol)
		if c.Via != "" {
			label += " [via " + c.Via + "]"
		}
		switch {
		case c.Cycle:
			label += " (cycle)"
		case c.NotFound:
			label += " (unresolved)"
		case c.DepthLimit:
			label += " (depth limit)"
		}
		fmt.Fprintf(sb, "%s -> %s: %s\n",
			d2Ident(caller), d2Ident(callee), label)
		if !c.Cycle && !c.NotFound && !c.DepthLimit {
			writeD2Edges(sb, c)
		}
	}
}

// actorName returns the D2 participant label for a SymbolRef.
//   - Method symbols ("Type.Method") → "pkgleaf.Type"
//   - Function symbols                → "pkgleaf.FuncName" (one actor
//     per function — this keeps the diagram readable even when many
//     package-level functions exist in the same package).
func actorName(ref domain.SymbolRef) string {
	leaf := pkgLeaf(ref.Package)
	typ, _ := splitMethodSymbol(ref.Symbol)
	if typ != "" {
		if leaf == "" {
			return typ
		}
		return leaf + "." + typ
	}
	if leaf == "" {
		return ref.Symbol
	}
	return leaf + "." + ref.Symbol
}

// methodLabel returns the call-arrow label: the method name for a
// "Type.Method" symbol, or the function name otherwise.
func methodLabel(ref domain.SymbolRef) string {
	_, method := splitMethodSymbol(ref.Symbol)
	if method != "" {
		return method
	}
	return ref.Symbol
}

// splitMethodSymbol splits "Type.Method" into ("Type", "Method"). Returns
// ("", "") for plain function symbols.
func splitMethodSymbol(sym string) (typ, method string) {
	dot := strings.Index(sym, ".")
	if dot < 0 {
		return "", ""
	}
	return sym[:dot], sym[dot+1:]
}

// pkgLeaf returns the last path segment of a package path, e.g.
// "internal/service" → "service".
func pkgLeaf(pkg string) string {
	if pkg == "" {
		return ""
	}
	slash := strings.LastIndex(pkg, "/")
	if slash < 0 {
		return pkg
	}
	return pkg[slash+1:]
}

// d2Ident quotes identifiers that contain characters D2 would
// otherwise treat specially. A conservative allow-list: letters,
// digits, underscore, and "." are fine bare; anything else triggers
// quoting.
func d2Ident(s string) string {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '.':
		default:
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
}
