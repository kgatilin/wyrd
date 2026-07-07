package policy

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// graph is the package-level dependency graph the policy evaluates over. All
// package identifiers are module-relative paths (the form overlay layer globs
// match against). It is built once per Check from the (already layer-merged)
// package models.
type graph struct {
	module  string
	pkgs    []string                   // all package ids, sorted
	layerOf map[string]string          // package id -> layer (from overlay.Merge)
	adj     map[string]map[string]bool // src -> set of direct internal deps
}

// edge is a directed package-to-package dependency.
type edge struct {
	from string
	to   string
}

// newGraph builds the dependency graph from models. An edge is kept only when
// its target is one of the packages actually parsed (a member of models) — the
// robust in-module test, since the Go reader stores dependency package paths
// module-relative and does not reliably flag stdlib/third-party targets as
// External. This mirrors overlay.Merge, which likewise only counts a dependency
// whose target resolves to a known package. Self edges are dropped. Layers are
// taken from the models, which the caller must have run through overlay.Merge.
//
// Because edges are membership-gated, scoping a scan to a subset of packages
// makes edges leaving that subset invisible; callers wanting full coverage
// should scan the whole module (the CLI default of ./...).
func newGraph(models []domain.PackageModel, module string) *graph {
	g := &graph{
		module:  module,
		layerOf: make(map[string]string),
		adj:     make(map[string]map[string]bool),
	}
	known := make(map[string]bool)
	for _, m := range models {
		src := relPath(module, m.Path)
		if !known[src] {
			known[src] = true
			g.pkgs = append(g.pkgs, src)
		}
		if m.Layer != "" {
			g.layerOf[src] = m.Layer
		}
	}
	for _, m := range models {
		src := relPath(module, m.Path)
		for _, dep := range m.Dependencies {
			dst := relPath(module, dep.To.Package)
			if dst == src || !known[dst] {
				continue
			}
			if g.adj[src] == nil {
				g.adj[src] = make(map[string]bool)
			}
			g.adj[src][dst] = true
		}
	}
	sort.Strings(g.pkgs)
	return g
}

// edges returns all observed edges in deterministic (from, to) order.
func (g *graph) edges() []edge {
	var es []edge
	for from, tos := range g.adj {
		for to := range tos {
			es = append(es, edge{from: from, to: to})
		}
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].from != es[j].from {
			return es[i].from < es[j].from
		}
		return es[i].to < es[j].to
	})
	return es
}

// resolve expands a selector list to the set of package ids it matches. A
// layer selector (@name) matches every package assigned to that layer; a glob
// selector matches by overlay glob semantics.
func (g *graph) resolve(sels []Selector) map[string]bool {
	out := make(map[string]bool)
	for _, s := range sels {
		if s.Layer != "" {
			for _, p := range g.pkgs {
				if g.layerOf[p] == s.Layer {
					out[p] = true
				}
			}
			continue
		}
		for _, p := range g.pkgs {
			if matchGlob(s.Glob, p) {
				out[p] = true
			}
		}
	}
	return out
}

// findPath returns an example path from src to any node in targets, never
// visiting a node in excluded (excluded also blocks src itself). The target
// must be a node other than src, so only real paths of length >= 1 count.
// Returns nil when no such path exists. Neighbors are visited in sorted order
// so the reported path is deterministic.
func (g *graph) findPath(src string, targets, excluded map[string]bool) []string {
	if excluded[src] {
		return nil
	}
	prev := make(map[string]string)
	visited := map[string]bool{src: true}
	queue := []string{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		nbrs := make([]string, 0, len(g.adj[cur]))
		for n := range g.adj[cur] {
			nbrs = append(nbrs, n)
		}
		sort.Strings(nbrs)
		for _, n := range nbrs {
			if visited[n] || excluded[n] {
				continue
			}
			visited[n] = true
			prev[n] = cur
			if n != src && targets[n] {
				return reconstruct(prev, src, n)
			}
			queue = append(queue, n)
		}
	}
	return nil
}

// reconstruct walks prev from dst back to src and returns the forward path.
func reconstruct(prev map[string]string, src, dst string) []string {
	var rev []string
	for cur := dst; ; cur = prev[cur] {
		rev = append(rev, cur)
		if cur == src {
			break
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// relPath strips the module prefix from a package path, returning the
// module-relative form used by overlay globs. It mirrors overlay.modulePath:
// a path equal to the module maps to "", an already-relative path is returned
// unchanged.
func relPath(module, p string) string {
	if module == "" {
		return p
	}
	if p == module {
		return ""
	}
	if rel, ok := strings.CutPrefix(p, module+"/"); ok {
		return rel
	}
	return p
}

// matchGlob mirrors overlay.matchGlob for module-relative package paths:
//   - "pkg/..." matches pkg and any sub-package.
//   - "pkg/*"   matches exactly one extra segment (path.Match semantics).
//   - "pkg"     exact match.
//
// It is re-implemented here (rather than exporting overlay's unexported
// helper) to keep the two packages decoupled; the semantics are small and
// stable.
func matchGlob(glob, pkg string) bool {
	if prefix, ok := strings.CutSuffix(glob, "/..."); ok {
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
	}
	if glob == "..." {
		return true
	}
	if strings.Contains(glob, "*") {
		if ok, _ := path.Match(glob, pkg); ok {
			return true
		}
		ok2, _ := filepath.Match(glob, pkg)
		return ok2
	}
	return glob == pkg
}
