package sequence

import (
	"strings"
)

// FormatText renders a Node tree as an indented outline. Example:
//
//	internal/service.Service.Generate
//	  └─ internal/adapter/golang.reader.Read
//	     └─ internal/service.Service.validate
//	  └─ internal/service.Service.Generate (cycle)
//	  └─ ... (depth limit)
//
// The root is printed at column 0; each level of children is indented
// two spaces and prefixed with "└─ ". Via annotations are shown inline
// when non-empty.
func FormatText(root *Node) string {
	if root == nil {
		return ""
	}
	var sb strings.Builder
	writeTextNode(&sb, root, "", true)
	return sb.String()
}

func writeTextNode(sb *strings.Builder, n *Node, indent string, isRoot bool) {
	if isRoot {
		sb.WriteString(n.Symbol.String())
		writeAnnotations(sb, n)
		sb.WriteString("\n")
	} else {
		sb.WriteString(indent)
		sb.WriteString("└─ ")
		sb.WriteString(n.Symbol.String())
		writeAnnotations(sb, n)
		sb.WriteString("\n")
	}

	// Depth-limit / cycle leaves never have children, but if the node is
	// a non-leaf that hit the depth limit we already marked it with
	// DepthLimit — children is empty in that case anyway.
	childIndent := indent
	if !isRoot {
		childIndent = indent + "   "
	}
	for _, c := range n.Children {
		writeTextNode(sb, c, childIndent, false)
	}
}

func writeAnnotations(sb *strings.Builder, n *Node) {
	if n.Via != "" {
		sb.WriteString(" [via ")
		sb.WriteString(n.Via)
		sb.WriteString("]")
	}
	if n.Cycle {
		sb.WriteString(" (cycle)")
	}
	if n.NotFound {
		sb.WriteString(" (unresolved)")
	}
	if n.DepthLimit {
		sb.WriteString(" (depth limit)")
	}
}
