// Package mermaid renders call-sequence trees (built by internal/sequence)
// as Mermaid `sequenceDiagram` source. It is a sibling output adapter to
// internal/adapter/d2: same input Node tree, different target syntax, so
// the diagram can be rendered directly in a browser/front-end that speaks
// Mermaid without a D2 toolchain.
package mermaid

import (
	"fmt"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/sequence"
)

// BuildSequenceSource renders a call tree as a Mermaid sequenceDiagram.
//
// Lifelines (participants) are projected to the type level: a method maps
// to its receiver type, a package-level function maps to itself. Messages
// follow the actual source-order call flow and are annotated with
// interface dispatch ("[via Iface]"), multiplicity ("×N"), and
// cycle/depth/unresolved markers so the reader can see how types,
// interfaces, and functions are wired together.
func BuildSequenceSource(root *sequence.Node) string {
	if root == nil {
		return ""
	}
	b := &builder{ids: map[string]string{}}
	b.collect(root)

	var sb strings.Builder
	sb.WriteString("sequenceDiagram\n")
	for _, name := range b.order {
		fmt.Fprintf(&sb, "    participant %s as %s\n", b.ids[name], mermaidLabel(name))
	}
	b.writeEdges(&sb, root)
	return sb.String()
}

// builder interns participant display names into stable Mermaid-safe ids,
// preserving first-seen order for a deterministic left-to-right layout.
type builder struct {
	ids   map[string]string
	order []string
}

func (b *builder) intern(name string) string {
	if id, ok := b.ids[name]; ok {
		return id
	}
	id := fmt.Sprintf("p%d", len(b.order))
	b.ids[name] = id
	b.order = append(b.order, name)
	return id
}

// collect walks the tree in the same child order as writeEdges so that
// participants are declared in the order they first appear in the flow.
func (b *builder) collect(n *sequence.Node) {
	b.intern(actorName(n.Symbol))
	for _, c := range n.Children {
		b.intern(actorName(c.Symbol))
		if expands(c) {
			b.collect(c)
		}
	}
}

func (b *builder) writeEdges(sb *strings.Builder, n *sequence.Node) {
	caller := b.ids[actorName(n.Symbol)]
	for _, c := range n.Children {
		callee := b.ids[actorName(c.Symbol)]
		fmt.Fprintf(sb, "    %s->>%s: %s\n", caller, callee, messageLabel(c))
		if expands(c) {
			b.writeEdges(sb, c)
		}
	}
}

// expands reports whether a node's children should be recursed into.
// Terminal nodes (cycle, depth cutoff, unresolved) are drawn but not
// walked further.
func expands(n *sequence.Node) bool {
	return !n.Cycle && !n.DepthLimit && !n.NotFound
}

// messageLabel is the text on the call arrow: the called method/function
// name plus dispatch, multiplicity, and terminal-state annotations.
func messageLabel(n *sequence.Node) string {
	label := methodLeaf(n.Symbol) + "()"
	if n.Via != "" {
		label += " [via " + leafName(n.Via) + "]"
	}
	if n.Count > 1 {
		label += fmt.Sprintf(" ×%d", n.Count)
	}
	switch {
	case n.Cycle:
		label += " (cycle)"
	case n.NotFound:
		label += " (unresolved)"
	case n.DepthLimit:
		label += " (depth limit)"
	}
	return label
}

// actorName projects a symbol to its lifeline name: "pkgLeaf.Type" for a
// method, "pkgLeaf.Func" for a package-level function.
func actorName(ref domain.SymbolRef) string {
	leaf := pkgLeaf(ref.Package)
	typ, _ := splitMethodSymbol(ref.Symbol)
	name := ref.Symbol
	if typ != "" {
		name = typ
	}
	if leaf == "" {
		if name == "" {
			return ref.Package
		}
		return name
	}
	return leaf + "." + name
}

// methodLeaf is the message name for a call: the method name for a method
// symbol ("Type.Method" -> "Method"), or the function name otherwise.
func methodLeaf(ref domain.SymbolRef) string {
	_, method := splitMethodSymbol(ref.Symbol)
	if method != "" {
		return method
	}
	return ref.Symbol
}

func splitMethodSymbol(sym string) (typ, method string) {
	dot := strings.Index(sym, ".")
	if dot < 0 {
		return "", ""
	}
	return sym[:dot], sym[dot+1:]
}

func pkgLeaf(pkg string) string {
	return leafName(pkg)
}

// leafName returns the segment after the last "/" (for package paths) and
// then the segment after the last "." (for qualified interface names like
// "io.Reader" -> "Reader").
func leafName(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// mermaidLabel quotes a participant label when it contains characters that
// would otherwise confuse the Mermaid parser.
func mermaidLabel(s string) string {
	if s == "" {
		return `"participant"`
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
		default:
			return `"` + strings.ReplaceAll(s, `"`, `#quot;`) + `"`
		}
	}
	return s
}
