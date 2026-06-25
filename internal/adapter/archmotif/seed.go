package archmotif

import (
	"sort"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
)

// SeedIDsFromDiff computes the structural diff between a base and a worktree
// snapshot and maps it to the archmotif node ids that the change touches —
// the seed set for a diff-scoped local partition.
//
// The seed is: every changed *node* (function/method/type/typedef/field/
// package that was added, removed, or changed) ∪ both endpoints of every
// changed *dependency edge*. This is exactly the "changed nodes ∪ endpoints
// of changed edges" rule: the diff tells us where the change is, ACL then
// tells us what region that change pulls on.
//
// Node ids are resolved against the *worktree* model (the graph callers
// expand), so symbols that the diff reports as removed — and therefore absent
// from the worktree graph — naturally drop out: there is no node to seed from
// in the new graph. Their effect surfaces instead through the caller-side
// changes that the removal forced, which are themselves in the diff.
//
// const/var/error/layer-rule changes are skipped: archmotif's graph has no
// node for them (the exporter does not emit const/var/error nodes, and a
// layer rule is policy, not a symbol).
//
// The returned ids are sorted and de-duplicated. An empty or nil diff yields
// an empty seed.
func SeedIDsFromDiff(base, worktree []domain.PackageModel) []string {
	d := diff.Compute(base, worktree)
	if d.IsEmpty() {
		return nil
	}

	pkgByPath := make(map[string]*domain.PackageModel, len(worktree))
	for i := range worktree {
		pkgByPath[worktree[i].Path] = &worktree[i]
	}

	seen := map[string]bool{}
	add := func(id string) {
		if id != "" {
			seen[id] = true
		}
	}

	for _, c := range d.Changes {
		switch c.Kind {
		case diff.KindDep:
			// Endpoints live in the Dependency carried by Before/After.
			for _, dep := range dependenciesOfChange(c) {
				if id, ok := resolveSymbolID(dep.From, pkgByPath); ok {
					add(id)
				}
				if id, ok := resolveSymbolID(dep.To, pkgByPath); ok {
					add(id)
				}
			}
		default:
			// Path ≡ node id minus the kind prefix, by construction:
			// diff Path is "pkg.Name" / "pkg.Recv.Method" and the exporter
			// ids are "<prefix>:" + that same dotted path.
			add(nodeIDForChange(c))
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// nodeIDForChange maps a node-kind change to its archmotif node id by
// prefixing the diff Path. Returns "" for kinds that have no graph node
// (const/var/error/layer-rule, and dep which is handled by the caller).
func nodeIDForChange(c diff.Change) string {
	switch c.Kind {
	case diff.KindPackage:
		return "pkg:" + c.Path
	case diff.KindInterface, diff.KindStruct, diff.KindTypeDef:
		return "type:" + c.Path
	case diff.KindFunction:
		return "fn:" + c.Path
	case diff.KindMethod:
		return "method:" + c.Path
	case diff.KindField:
		return "field:" + c.Path
	default:
		return ""
	}
}

// dependenciesOfChange extracts the domain.Dependency values a KindDep change
// carries. OpChange has both Before and After (the edge's old and new form);
// OpAdd/OpRemove carry one side. Both endpoints of each are seeded, so an
// edge that moved between symbols seeds all four.
func dependenciesOfChange(c diff.Change) []domain.Dependency {
	var out []domain.Dependency
	if dep, ok := c.Before.(domain.Dependency); ok {
		out = append(out, dep)
	}
	if dep, ok := c.After.(domain.Dependency); ok {
		out = append(out, dep)
	}
	return out
}
