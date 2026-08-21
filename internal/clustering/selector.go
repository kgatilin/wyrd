// Package clustering holds the clustering analyses over the code graph,
// independent of any transport. The MCP lenses (spectral_cluster,
// semantic_cluster, latent_domains) and the review UI's domains endpoint are
// callers; the maths, the node selection and the partition encoding live here,
// so a lens answers the same thing whichever surface asked.
package clustering

import (
	"sort"
	"strings"

	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

// Selector says which nodes of the code graph an analysis runs over.
type Selector struct {
	// Package is a package path; empty means every package.
	Package string
	// IncludeSubpackages extends Package to its subtree. Nil means true.
	IncludeSubpackages *bool
	// NodeKinds restricts the archmotif node kinds. Empty means every symbol
	// kind, with the package and file containers dropped.
	NodeKinds []string
	// Diff scopes the analysis to the region of the graph the worktree's
	// changes (vs the review base) pull on, replacing the package scoping.
	Diff bool
}

// SelectNodes returns the archmotif node ids matching sel, sorted so a repeated
// call over an unchanged model produces the same node order — the label arrays
// a partition is reported as are indexed by that order.
func SelectNodes(graph *spectralcluster.Graph, packages []domain.PackageModel, sel Selector) []string {
	includeSubpkgs := true
	if sel.IncludeSubpackages != nil {
		includeSubpkgs = *sel.IncludeSubpackages
	}

	matchingPkgs := make(map[string]bool, len(packages))
	for _, pkg := range packages {
		switch {
		case sel.Package == "":
			matchingPkgs[pkg.Path] = true
		case pkg.Path == sel.Package:
			matchingPkgs[pkg.Path] = true
		case includeSubpkgs && strings.HasPrefix(pkg.Path, sel.Package+"/"):
			matchingPkgs[pkg.Path] = true
		}
	}

	nodeKindFilter := make(map[string]bool, len(sel.NodeKinds))
	for _, k := range sel.NodeKinds {
		nodeKindFilter[k] = true
	}

	var nodeIDs []string
	for _, n := range graph.Nodes() {
		if !matchingPkgs[PackagePathOf(n.ID)] {
			continue
		}
		if len(nodeKindFilter) > 0 {
			if !nodeKindFilter[string(n.Kind)] {
				continue
			}
			nodeIDs = append(nodeIDs, n.ID)
			continue
		}
		// Package and file containers are the structural layout, not symbols;
		// an analysis that clusters symbols drops them unless asked for by kind.
		if n.Kind == "package" || n.Kind == "file" {
			continue
		}
		nodeIDs = append(nodeIDs, n.ID)
	}

	sort.Strings(nodeIDs)
	return nodeIDs
}

// PackagePathOf extracts the package path from an archmotif node id.
//
//	pkg:<path>                    -> <path>
//	type:<path>.<Name>            -> <path>
//	fn:<path>.<Name>              -> <path>
//	method:<path>.<Recv>.<Method> -> <path>
//	field:<path>.<Struct>.<Field> -> <path>
//	file:<path>/<basename>        -> <path>
func PackagePathOf(id string) string {
	switch {
	case strings.HasPrefix(id, "pkg:"):
		return strings.TrimPrefix(id, "pkg:")
	case strings.HasPrefix(id, "type:"):
		return trimLastDots(strings.TrimPrefix(id, "type:"), 1)
	case strings.HasPrefix(id, "fn:"):
		return trimLastDots(strings.TrimPrefix(id, "fn:"), 1)
	case strings.HasPrefix(id, "method:"):
		return trimLastDots(strings.TrimPrefix(id, "method:"), 2)
	case strings.HasPrefix(id, "field:"):
		return trimLastDots(strings.TrimPrefix(id, "field:"), 2)
	case strings.HasPrefix(id, "file:"):
		rest := strings.TrimPrefix(id, "file:")
		if i := strings.LastIndex(rest, "/"); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	return ""
}

func trimLastDots(s string, n int) string {
	for i := 0; i < n; i++ {
		idx := strings.LastIndex(s, ".")
		if idx < 0 {
			return s
		}
		s = s[:idx]
	}
	return s
}
