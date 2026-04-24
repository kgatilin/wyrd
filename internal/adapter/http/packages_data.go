package http

import (
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// packageFilter captures the user-selectable filters on the packages
// list page. Zero values mean "no filter".
type packageFilter struct {
	// Layer restricts the list to packages whose overlay-assigned
	// Layer exactly equals this value.
	Layer string
	// Stereotype restricts the list to packages that contain at least
	// one symbol with a Stereotype equal to this value.
	Stereotype string
	// Search is a case-insensitive substring match against package path,
	// package name, or any symbol name in the package.
	Search string
}

// packageSummary is a compact per-package view-model used in the list
// page. It holds everything the list template needs without exposing
// the full domain model.
type packageSummary struct {
	Path        string
	Name        string
	Layer       string
	SymbolCount int
	// Counts broken down per symbol kind so the list can show a small
	// histogram next to each package without the template doing math.
	Interfaces int
	Structs    int
	Functions  int
	Constants  int
	Variables  int
	Errors     int
	TypeDefs   int
	// Stereotypes is a sorted, de-duplicated list of stereotype strings
	// attached to symbols in this package. Empty if none.
	Stereotypes []string
}

// packageNode is one node of the directory-style package tree. Name is
// the last path segment (used for rendering), FullPath is the
// module-relative package path used to hyperlink to the detail page.
// Package is non-nil when there is an actual package at this node;
// intermediate directories (e.g. "internal/adapter" when only
// "internal/adapter/golang" is a real package) have a nil Package and
// are rendered as plain groups.
type packageNode struct {
	Name     string
	FullPath string
	Package  *packageSummary
	Children []*packageNode
}

// packageListData is the view-model passed to packages.html. It carries
// both the filtered flat list (for simple counts / empty-state logic)
// and the directory tree (for the hierarchical display).
type packageListData struct {
	pageData
	Filter         packageFilter
	Packages       []packageSummary
	Tree           []*packageNode
	LayerOptions   []string
	StereotypeOpts []string
	TotalCount     int
	// Partial indicates the template is being rendered into an HTMX
	// swap target (only the tree fragment) rather than a full page.
	Partial bool
}

// buildPackageSummaries converts the snapshot's packages into summaries,
// applying the given filter. The output is sorted by Path for stable
// ordering.
func buildPackageSummaries(pkgs []domain.PackageModel, f packageFilter) []packageSummary {
	out := make([]packageSummary, 0, len(pkgs))
	for _, p := range pkgs {
		if !matchesFilter(p, f) {
			continue
		}
		out = append(out, pkgSummarize(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// pkgSummarize collapses a PackageModel into its list-page view-model.
func pkgSummarize(p domain.PackageModel) packageSummary {
	s := packageSummary{
		Path:       p.Path,
		Name:       p.Name,
		Layer:      p.Layer,
		Interfaces: len(p.Interfaces),
		Structs:    len(p.Structs),
		Functions:  len(p.Functions),
		Constants:  len(p.Constants),
		Variables:  len(p.Variables),
		Errors:     len(p.Errors),
		TypeDefs:   len(p.TypeDefs),
	}
	s.SymbolCount = s.Interfaces + s.Structs + s.Functions + s.Constants +
		s.Variables + s.Errors + s.TypeDefs
	s.Stereotypes = collectStereotypes(p)
	return s
}

// collectStereotypes returns a sorted, de-duplicated slice of
// stereotype strings seen on any symbol in p.
func collectStereotypes(p domain.PackageModel) []string {
	seen := make(map[string]struct{})
	add := func(s domain.Stereotype) {
		if s.IsEmpty() {
			return
		}
		seen[s.String()] = struct{}{}
	}
	for _, iface := range p.Interfaces {
		add(iface.Stereotype)
	}
	for _, st := range p.Structs {
		add(st.Stereotype)
	}
	for _, fn := range p.Functions {
		add(fn.Stereotype)
	}
	for _, td := range p.TypeDefs {
		add(td.Stereotype)
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// matchesFilter reports whether pkg passes the given filter. Empty
// filter fields are treated as "accept anything".
func matchesFilter(pkg domain.PackageModel, f packageFilter) bool {
	if f.Layer != "" && pkg.Layer != f.Layer {
		return false
	}
	if f.Stereotype != "" && !hasStereotype(pkg, f.Stereotype) {
		return false
	}
	if f.Search != "" {
		needle := strings.ToLower(f.Search)
		if !packageMatchesSearch(pkg, needle) {
			return false
		}
	}
	return true
}

// hasStereotype reports whether pkg contains any symbol with the given
// stereotype.
func hasStereotype(pkg domain.PackageModel, want string) bool {
	for _, iface := range pkg.Interfaces {
		if iface.Stereotype.String() == want {
			return true
		}
	}
	for _, s := range pkg.Structs {
		if s.Stereotype.String() == want {
			return true
		}
	}
	for _, fn := range pkg.Functions {
		if fn.Stereotype.String() == want {
			return true
		}
	}
	for _, td := range pkg.TypeDefs {
		if td.Stereotype.String() == want {
			return true
		}
	}
	return false
}

// packageMatchesSearch performs a case-insensitive substring match of
// needle (already lowercased) against the package path, name, and any
// symbol name in the package. Dependencies are not scanned — those are
// noise for a "find a package" query.
func packageMatchesSearch(pkg domain.PackageModel, needle string) bool {
	if strings.Contains(strings.ToLower(pkg.Path), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(pkg.Name), needle) {
		return true
	}
	for _, iface := range pkg.Interfaces {
		if strings.Contains(strings.ToLower(iface.Name), needle) {
			return true
		}
	}
	for _, s := range pkg.Structs {
		if strings.Contains(strings.ToLower(s.Name), needle) {
			return true
		}
	}
	for _, fn := range pkg.Functions {
		if strings.Contains(strings.ToLower(fn.Name), needle) {
			return true
		}
	}
	for _, td := range pkg.TypeDefs {
		if strings.Contains(strings.ToLower(td.Name), needle) {
			return true
		}
	}
	for _, c := range pkg.Constants {
		if strings.Contains(strings.ToLower(c.Name), needle) {
			return true
		}
	}
	for _, v := range pkg.Variables {
		if strings.Contains(strings.ToLower(v.Name), needle) {
			return true
		}
	}
	for _, e := range pkg.Errors {
		if strings.Contains(strings.ToLower(e.Name), needle) {
			return true
		}
	}
	return false
}

// buildPackageTree organizes a flat list of summaries into a directory
// tree keyed by package path segments. Root-level packages (Path=".")
// become an entry named ".".
func buildPackageTree(summaries []packageSummary) []*packageNode {
	root := &packageNode{}
	for i := range summaries {
		s := summaries[i] // capture
		segments := splitPath(s.Path)
		insertPackage(root, segments, &s)
	}
	sortTree(root)
	return root.Children
}

// splitPath splits a module-relative package path into segments. The
// root package "." becomes the single segment ".".
func splitPath(p string) []string {
	if p == "." || p == "" {
		return []string{"."}
	}
	return strings.Split(p, "/")
}

// insertPackage walks/creates nodes along segments from root, attaching
// the package summary to the terminal node.
func insertPackage(root *packageNode, segments []string, pkg *packageSummary) {
	cur := root
	for i, seg := range segments {
		child := findChild(cur, seg)
		if child == nil {
			child = &packageNode{
				Name:     seg,
				FullPath: joinSegments(segments[:i+1]),
			}
			cur.Children = append(cur.Children, child)
		}
		if i == len(segments)-1 {
			child.Package = pkg
		}
		cur = child
	}
}

// findChild looks up an immediate child of parent by name; nil if
// absent.
func findChild(parent *packageNode, name string) *packageNode {
	for _, c := range parent.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// joinSegments rebuilds a path string from a slice of segments. The
// special root segment "." is passed through unchanged.
func joinSegments(segs []string) string {
	if len(segs) == 1 && segs[0] == "." {
		return "."
	}
	return strings.Join(segs, "/")
}

// sortTree sorts the tree depth-first by node name so the rendered
// output is deterministic.
func sortTree(n *packageNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		return n.Children[i].Name < n.Children[j].Name
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// collectLayerOptions returns a sorted list of all distinct non-empty
// Layer values seen across the snapshot. Used to populate the layer
// filter dropdown.
func collectLayerOptions(pkgs []domain.PackageModel) []string {
	seen := make(map[string]struct{})
	for _, p := range pkgs {
		if p.Layer != "" {
			seen[p.Layer] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// collectStereotypeOptions returns a sorted list of all distinct
// non-empty stereotype strings seen across the snapshot.
func collectStereotypeOptions(pkgs []domain.PackageModel) []string {
	seen := make(map[string]struct{})
	for _, p := range pkgs {
		for _, s := range collectStereotypes(p) {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
