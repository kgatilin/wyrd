package sequence

import (
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// Build walks the static call graph rooted at start and returns a Node tree
// capped at maxDepth. Depth 0 means "root only, no children". Calls that
// revisit a symbol already on the current path are marked Cycle and not
// recursed into. When a CallEdge points at a symbol that cannot be
// resolved in models (e.g., stdlib filter already dropped it, or the
// package is not loaded), a NotFound leaf is emitted so renderers can
// still show the edge.
//
// models must be the full set of PackageModels for the analyzed module.
// The builder indexes them by (Package, Symbol) on first call.
func Build(models []domain.PackageModel, start domain.SymbolRef, maxDepth int) *Node {
	idx := buildIndex(models)
	return buildNode(idx, start, "", maxDepth, map[string]bool{})
}

// symbolKey returns the composite key we use to look up (and detect
// cycles over) a symbol. Package + "|" + Symbol is unique because the
// Go reader stores methods as "Type.Method" in Symbol, so there is no
// ambiguity between a function and a method of the same package.
func symbolKey(ref domain.SymbolRef) string {
	return ref.Package + "|" + ref.Symbol
}

// index maps symbol keys to their call lists and back-references to the
// PackageModel/symbol they came from. We store only what the builder
// needs: the outgoing calls for that symbol.
type index struct {
	calls map[string][]domain.CallEdge
}

func buildIndex(models []domain.PackageModel) *index {
	idx := &index{calls: make(map[string][]domain.CallEdge)}
	for _, m := range models {
		for _, fn := range m.Functions {
			ref := domain.SymbolRef{Package: m.Path, Symbol: fn.Name}
			idx.calls[symbolKey(ref)] = fn.Calls
		}
		for _, s := range m.Structs {
			for _, meth := range s.Methods {
				ref := domain.SymbolRef{
					Package: m.Path,
					Symbol:  s.Name + "." + meth.Name,
				}
				idx.calls[symbolKey(ref)] = meth.Calls
			}
		}
	}
	return idx
}

// buildNode constructs the subtree rooted at ref. visited is the set of
// symbol keys on the path from the true root down to (but not including)
// ref. It's copied on each recursion so sibling branches don't falsely
// report each other as cycles.
func buildNode(
	idx *index,
	ref domain.SymbolRef,
	via string,
	depth int,
	visited map[string]bool,
) *Node {
	n := &Node{Symbol: ref, Via: via}

	key := symbolKey(ref)
	if visited[key] {
		n.Cycle = true
		return n
	}

	calls, ok := idx.calls[key]
	if !ok {
		// Only mark as NotFound for non-root nodes — the root is
		// always "found" by definition (it's what the caller asked
		// for). When depth is 0 and the root has no entry, we simply
		// return a childless node.
		if len(visited) > 0 {
			n.NotFound = true
		}
		return n
	}

	if depth <= 0 {
		if len(calls) > 0 {
			n.DepthLimit = true
		}
		return n
	}

	childVisited := copyVisited(visited)
	childVisited[key] = true

	for _, edge := range calls {
		child := buildNode(idx, edge.To, edge.Via, depth-1, childVisited)
		n.Children = append(n.Children, child)
	}
	return n
}

func copyVisited(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// ParseTarget converts a CLI target like
//
//	internal/service.Service.Generate    (method)
//	internal/service.NewService          (function)
//	cmd/archai.main                      (function)
//
// into a SymbolRef. Heuristic: the last dot separates Symbol from
// Package ONLY if the token before it starts lower-case (function) OR
// the token before it is uppercase and the token before that is also
// uppercase-looking (method — "Type.Method"). We keep it simple:
// - Count the dots in the final path segment.
// - If the segment has one dot, treat it as "pkg.Func".
// - If the segment has two dots, treat it as "pkg.Type.Method".
// - More dots: everything after the first dot is the Symbol.
//
// The package portion may contain slashes (e.g. "internal/service").
func ParseTarget(target string) (domain.SymbolRef, bool) {
	// Split off the package prefix at the last slash, if any.
	slash := strings.LastIndex(target, "/")
	var pkgPrefix, rest string
	if slash >= 0 {
		pkgPrefix = target[:slash+1] // includes trailing slash
		rest = target[slash+1:]
	} else {
		rest = target
	}

	// rest is "pkgLeaf.Symbol" or "pkgLeaf.Type.Method".
	firstDot := strings.Index(rest, ".")
	if firstDot < 0 {
		return domain.SymbolRef{}, false
	}
	pkgLeaf := rest[:firstDot]
	sym := rest[firstDot+1:]
	if pkgLeaf == "" || sym == "" {
		return domain.SymbolRef{}, false
	}
	return domain.SymbolRef{
		Package: pkgPrefix + pkgLeaf,
		Symbol:  sym,
	}, true
}
