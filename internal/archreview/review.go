package archreview

import (
	"fmt"
	"sort"
	"strings"

	archmotifadapter "github.com/kgatilin/wyrd/internal/adapter/archmotif"
	"github.com/kgatilin/wyrd/internal/diff"
)

// Review-mode severities. The order is the order a reviewer should read them
// in: a cycle costs more than a new edge, a new edge more than an inversion,
// and an orphan is the cheapest thing on the list to fix.
const (
	severityGroupCyclesNew = 70
	severityEdgesNew       = 60
	severityInversionsNew  = 50
	severityUnusedExpNew   = 40
	severityImpact         = 30
	severityHotspotGrowth  = 20
	severityOrphansNew     = 10
)

// reviewSections answers "did this branch make it worse, and where".
func reviewSections(
	head, base *side,
	groups *grouping,
	ports portRules,
	modelDiff *diff.Diff,
	changed map[string][]LineRange,
) []Section {
	added := addedSymbols(head, base)
	return []Section{
		reviewGroupCycles(head, base, groups),
		reviewNewEdges(head, base, groups),
		reviewInversions(head, base),
		reviewUnusedExports(head, ports, added),
		reviewImpact(head, modelDiff, changed),
		reviewHotspotGrowth(head, base, changed),
		reviewOrphans(head, ports, added),
	}
}

// addedSymbols returns a predicate for the declarations this branch added: a
// straight set difference over the symbols the graph carries a node for, so a
// new method on an existing type counts as added rather than disappearing into
// its receiver's change entry.
func addedSymbols(head, base *side) func(symbol) bool {
	return func(sym symbol) bool {
		_, existed := base.symbols[sym.node]
		return !existed
	}
}

// newPackageEdges lists the package edges head has and base did not.
func newPackageEdges(head, base *side) map[Edge]bool {
	out := map[Edge]bool{}
	for edge := range head.edges {
		if _, existed := base.edges[edge]; !existed {
			out[edge] = true
		}
	}
	return out
}

func reviewGroupCycles(head, base *side, groups *grouping) Section {
	headCycles := cyclesOf(head, groups)
	baseCycles := cyclesOf(base, groups)
	fresh := newPackageEdges(head, base)

	var items []Item
	for _, cycle := range headCycles {
		gained, isNew := cycleDelta(cycle, baseCycles)
		if !isNew && len(gained) == 0 {
			continue
		}

		item := Item{
			Text:   cycleLabel(cycle),
			Tag:    TagNew,
			Target: Target{Edges: cycle.pkgEdges()},
		}
		if !isNew {
			item.Tag = TagGrew
		}

		var reason []string
		if isNew {
			reason = append(reason, "new in this branch")
		} else {
			reason = append(reason, "gained "+strings.Join(gained, ", "))
		}
		if closing, ok := closingEdge(cycle, fresh); ok {
			reason = append(reason, "closed by "+closing.From+" → "+closing.To)
			edge := closing
			item.Target.Edge = &edge
			item.Target.ComponentID = closing.From
		}
		reason = append(reason, "break it at that edge")
		item.Detail = strings.Join(reason, "; ")
		items = append(items, item)
	}

	return makeSection(
		SectionGroupCyclesNew, "New group cycles", severityGroupCyclesNew,
		"no new cycles between groups",
		plural(len(items), "group cycle is new or grew", "group cycles are new or grew"),
		items,
	)
}

// cycleDelta compares a head cycle with the base's cycles: it is new when no
// base cycle shares a group with it, and it grew when the best-overlapping
// base cycle is missing some of its members.
func cycleDelta(cycle groupCycle, baseCycles []groupCycle) (gained []string, isNew bool) {
	best := map[string]bool{}
	bestOverlap := 0
	for _, candidate := range baseCycles {
		members := map[string]bool{}
		overlap := 0
		for _, g := range candidate.Groups {
			members[g] = true
		}
		for _, g := range cycle.Groups {
			if members[g] {
				overlap++
			}
		}
		if overlap > bestOverlap {
			bestOverlap, best = overlap, members
		}
	}
	if bestOverlap == 0 {
		return nil, true
	}
	for _, g := range cycle.Groups {
		if !best[g] {
			gained = append(gained, g)
		}
	}
	return gained, false
}

// closingEdge picks the package edge inside the cycle that the branch added —
// the one to cut, because removing it is the smallest change that reopens the
// loop.
func closingEdge(cycle groupCycle, fresh map[Edge]bool) (Edge, bool) {
	var candidates []Edge
	for _, edge := range cycle.pkgEdges() {
		if fresh[edge] {
			candidates = append(candidates, edge)
		}
	}
	if len(candidates) == 0 {
		return Edge{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].From != candidates[j].From {
			return candidates[i].From < candidates[j].From
		}
		return candidates[i].To < candidates[j].To
	})
	return candidates[0], true
}

func cycleLabel(cycle groupCycle) string {
	if len(cycle.Groups) == 2 {
		return cycle.Groups[0] + " ↔ " + cycle.Groups[1]
	}
	return fmt.Sprintf("%s (%d groups)", strings.Join(cycle.Groups, ", "), len(cycle.Groups))
}

func reviewNewEdges(head, base *side, groups *grouping) Section {
	fresh := newPackageEdges(head, base)
	violations := policyViolations(head)
	backward := backwardGroupEdges(head, groups)

	edges := make([]Edge, 0, len(fresh))
	for edge := range fresh {
		edges = append(edges, edge)
	}

	type tagged struct {
		edge   Edge
		tag    string
		detail string
	}
	rank := map[string]int{TagPolicy: 0, TagBackward: 1, TagOK: 2}
	list := make([]tagged, 0, len(edges))
	for _, edge := range edges {
		switch {
		case violations[edge] != "":
			list = append(list, tagged{edge, TagPolicy,
				violations[edge] + " — fix it, or allow the edge explicitly in wyrd.yaml"})
		case isBackwardEdge(groups, backward, edge):
			list = append(list, tagged{edge, TagBackward,
				groups.of(edge.From) + " sits below " + groups.of(edge.To) +
					" — invert it through an interface in the lower group"})
		default:
			list = append(list, tagged{edge, TagOK,
				"new dependency — confirm it was intended"})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].tag != list[j].tag {
			return rank[list[i].tag] < rank[list[j].tag]
		}
		if list[i].edge.From != list[j].edge.From {
			return list[i].edge.From < list[j].edge.From
		}
		return list[i].edge.To < list[j].edge.To
	})

	items := make([]Item, 0, len(list))
	for _, entry := range list {
		edge := entry.edge
		items = append(items, Item{
			Text:   edge.From + " → " + edge.To,
			Detail: entry.detail,
			Tag:    entry.tag,
			Target: Target{ComponentID: edge.From, Edge: &edge},
		})
	}

	return makeSection(
		SectionEdgesNew, "New cross-package edges", severityEdgesNew,
		"no new package dependencies",
		plural(len(items), "new package dependency", "new package dependencies"),
		items,
	)
}

// isBackwardEdge reports whether the package edge is carried by a group edge
// that climbs the group hierarchy — a foundation depending on something above
// it. An edge inside a single group has no direction to contradict.
func isBackwardEdge(g *grouping, backward map[Edge]bool, edge Edge) bool {
	from, to := g.of(edge.From), g.of(edge.To)
	if from == to {
		return false
	}
	return backward[Edge{From: from, To: to}]
}

func reviewInversions(head, base *side) Section {
	baseline := base.backwardEdges()

	type inversion struct {
		from, to string
		span     float64
	}
	var fresh []inversion
	for _, backward := range head.symTrophic.BackwardEdges {
		if _, existed := baseline[Edge{From: backward.From, To: backward.To}]; existed {
			continue
		}
		fresh = append(fresh, inversion{backward.From, backward.To, backward.Span})
	}
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].span != fresh[j].span {
			return fresh[i].span > fresh[j].span
		}
		if fresh[i].from != fresh[j].from {
			return fresh[i].from < fresh[j].from
		}
		return fresh[i].to < fresh[j].to
	})

	items := make([]Item, 0, len(fresh))
	for _, inv := range fresh {
		from, okFrom := head.symbols[inv.from]
		to, okTo := head.symbols[inv.to]
		if !okFrom || !okTo {
			continue
		}
		items = append(items, Item{
			Text: from.label() + " → " + to.label(),
			Detail: fmt.Sprintf("reaches %.1f levels up — a candidate for a port or callback in the lower layer",
				inv.span),
			Target: from.target(),
		})
	}

	f0 := head.symTrophic.IncoherenceF0
	return makeSection(
		SectionInversionsNew, "New inversions", severityInversionsNew,
		fmt.Sprintf("no new inversions (F0 %.2f, %s)", f0, trophicVerdict(f0)),
		fmt.Sprintf("F0 %.2f (%s) — %s", f0, trophicVerdict(f0),
			plural(len(items), "new inversion", "new inversions")),
		items,
	)
}

func reviewUnusedExports(head *side, ports portRules, added func(symbol) bool) Section {
	var items []Item
	for _, finding := range unusedExports(head, ports, added) {
		// A symbol nothing references at all is an orphan, not an export to
		// hide: the action there is to delete or wire it, so it belongs to
		// exactly one section and gets exactly one instruction.
		if finding.dead {
			continue
		}
		items = append(items, Item{
			Text:   finding.sym.label(),
			Detail: "0 callers (tests not in graph) outside its package — unexport it",
			Target: finding.sym.target(),
		})
	}

	return makeSection(
		SectionUnusedExpNew, "New unused exports", severityUnusedExpNew,
		"every export this branch added has a caller",
		plural(len(items), "added export has no caller outside its package",
			"added exports have no caller outside their package"),
		items,
	)
}

func reviewImpact(head *side, modelDiff *diff.Diff, changed map[string][]LineRange) Section {
	touched := touchedPackages(head, modelDiff, changed)

	type impacted struct {
		sym      symbol
		callers  int
		packages []string
	}
	var found []impacted
	for _, node := range head.order {
		sym := head.symbols[node]
		if !shapeChanged(modelDiff, sym) && !bodyChanged(sym, changed) {
			continue
		}
		callers := 0
		var from []string
		for pkg, count := range head.inFrom[node] {
			if touched[pkg] {
				continue
			}
			callers += count
			from = append(from, pkg)
		}
		if callers == 0 {
			continue
		}
		sort.Strings(from)
		found = append(found, impacted{sym: sym, callers: callers, packages: from})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].callers != found[j].callers {
			return found[i].callers > found[j].callers
		}
		return found[i].sym.label() < found[j].sym.label()
	})

	items := make([]Item, 0, len(found))
	for _, entry := range found {
		items = append(items, Item{
			Text: entry.sym.label(),
			Detail: fmt.Sprintf("%s in %s the branch did not touch (%s) — a signature change is compile-checked, a behavioural one is not",
				plural(entry.callers, "caller", "callers"),
				plural(len(entry.packages), "package", "packages"),
				strings.Join(entry.packages, ", ")),
			Target: entry.sym.target(),
		})
	}

	return makeSection(
		SectionImpact, "Impact", severityImpact,
		"nothing this branch changed is used by an untouched package",
		plural(len(items), "changed symbol reaches untouched packages",
			"changed symbols reach untouched packages"),
		items,
	)
}

// shapeChanged reports whether the model diff recorded a change to the
// symbol's declared shape. Methods roll up into their receiver, which is how
// the diff records them and how the wiring panel presents them.
func shapeChanged(modelDiff *diff.Diff, sym symbol) bool {
	if modelDiff == nil || sym.isMember() {
		return false
	}
	path := sym.pkg + "." + sym.name
	for _, change := range modelDiff.Changes {
		if change.Op == diff.OpChange && change.Path == path {
			return true
		}
	}
	return false
}

// bodyChanged reports whether the symbol's source span overlaps a changed hunk.
func bodyChanged(sym symbol, changed map[string][]LineRange) bool {
	if len(changed) == 0 || !sym.span.IsValid() {
		return false
	}
	for _, r := range changed[sym.span.File] {
		if r.Start <= sym.span.EndLine && sym.span.StartLine <= r.End {
			return true
		}
	}
	return false
}

// touchedPackages is every package the branch edited, by either measure: a
// changed file inside it, or a model change naming it.
func touchedPackages(head *side, modelDiff *diff.Diff, changed map[string][]LineRange) map[string]bool {
	touched := map[string]bool{}
	for path := range changed {
		if pkg := head.packageOfFile(path); pkg != "" {
			touched[pkg] = true
		}
	}
	if modelDiff != nil {
		for _, change := range modelDiff.Changes {
			for _, pkg := range head.pkgs {
				if change.Path == pkg || strings.HasPrefix(change.Path, pkg+".") {
					touched[pkg] = true
				}
			}
		}
	}
	return touched
}

func reviewHotspotGrowth(head, base *side, changed map[string][]LineRange) Section {
	var items []Item

	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		now, ok := head.declsByFile[path]
		if !ok || float64(now) < head.fileCutoff {
			continue
		}
		before := base.declsByFile[path]
		if now <= before {
			continue
		}
		items = append(items, Item{
			Text: fmt.Sprintf("%s — %s (+%d)", path, plural(now, "declaration", "declarations"), now-before),
			Detail: fmt.Sprintf("already at or past the god-file threshold of %d — put the new declarations elsewhere",
				head.cutoffDeclarations()),
			Target: Target{File: path, ComponentID: head.packageOfFile(path)},
		})
	}

	headDegree := head.packageDegree()
	baseDegree := base.packageDegree()
	for _, pkg := range godPackages(head, headDegree) {
		grew := headDegree[pkg] - baseDegree[pkg]
		if grew <= 0 {
			continue
		}
		items = append(items, Item{
			Text:   fmt.Sprintf("%s — degree %d (+%d)", pkg, headDegree[pkg], grew),
			Detail: "already one of the most connected packages — put the new dependencies elsewhere",
			Target: Target{ComponentID: pkg},
		})
	}

	return makeSection(
		SectionHotspotGrowth, "Hotspot growth", severityHotspotGrowth,
		"no overloaded file or package grew",
		plural(len(items), "hotspot grew", "hotspots grew"),
		items,
	)
}

func reviewOrphans(head *side, ports portRules, added func(symbol) bool) Section {
	var items []Item
	for _, sym := range orphans(head, ports, added) {
		items = append(items, Item{
			Text:   sym.label(),
			Detail: "0 callers (tests not in graph) anywhere — delete it or wire it up",
			Target: sym.target(),
		})
	}

	return makeSection(
		SectionOrphansNew, "Orphans", severityOrphansNew,
		"everything this branch added is referenced",
		plural(len(items), "added symbol has no incoming edge",
			"added symbols have no incoming edge"),
		items,
	)
}

// trophicVerdict names an incoherence value in the report's voice: the
// trophic_layers lens's own vocabulary with its underscores opened up, so the
// panel and the lens cannot call the same number by two different words.
func trophicVerdict(f0 float64) string {
	return strings.ReplaceAll(archmotifadapter.TrophicVerdict(f0), "_", " ")
}
