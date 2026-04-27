package http

import (
	"strings"

	"github.com/kgatilin/archai/internal/diff"
)

// buildDiffGraph produces a cytoscape overlay of current vs. target.
// Nodes are identified by their diff.Change.Path, colored by operation
// (add=green / remove=red / change=yellow). Edges connect parent
// packages to their changed children so the graph reveals the impact
// surface per package.
func buildDiffGraph(d *diff.Diff, filter string) graphPayload {
	out := graphPayload{
		Meta: graphMeta{
			View:   "diff-overlay",
			Layout: "dagre",
			Title:  "Diff",
		},
	}
	if d == nil || len(d.Changes) == 0 {
		return out
	}

	seen := make(map[string]struct{})
	addNode := func(n graphNode) {
		if _, ok := seen[n.ID]; ok {
			return
		}
		seen[n.ID] = struct{}{}
		out.Nodes = append(out.Nodes, n)
	}

	for _, c := range d.Changes {
		if filter != "" && string(c.Kind) != filter {
			continue
		}
		parent := diffParentID(c.Path)
		if parent != "" {
			addNode(graphNode{
				ID:    parent,
				Label: parent,
				Kind:  "package",
			})
		}
		// The change itself is a child of the parent, if any.
		nodeID := string(c.Kind) + ":" + c.Path
		addNode(graphNode{
			ID:     nodeID,
			Label:  shortNameForDiff(c.Path),
			Kind:   string(c.Kind),
			Parent: parent,
			Op:     string(c.Op),
		})
		if parent != "" {
			out.Edges = append(out.Edges, graphEdge{
				Source: parent,
				Target: nodeID,
				Kind:   string(c.Op),
			})
		}
	}
	return out
}

// diffParentID returns the "package:" parent id for a diff change path.
// Paths look like "pkg.Symbol" or "pkg.Symbol.Field"; the substring up
// to the first dot is taken as the package.
func diffParentID(path string) string {
	if path == "" {
		return ""
	}
	// diff paths use '.' but packages may contain '/' — so the first dot
	// after the last slash is the package/type boundary.
	cut := strings.LastIndex(path, "/")
	tail := path
	prefix := ""
	if cut >= 0 {
		prefix = path[:cut+1]
		tail = path[cut+1:]
	}
	dot := strings.Index(tail, ".")
	if dot < 0 {
		return ""
	}
	return "pkg:" + prefix + tail[:dot]
}

// shortNameForDiff returns the trailing component of a diff path
// (everything after the final dot), suitable as a short node label.
func shortNameForDiff(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 && i < len(path)-1 {
		return path[i+1:]
	}
	return path
}
