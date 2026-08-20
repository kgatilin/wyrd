package archreview

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archmotif/pkg/components"
)

// Repo-mode severities, in the order the refactoring is worth doing.
const (
	severityGroupCycles   = 60
	severityInversions    = 50
	severityGodFiles      = 40
	severityGodPackages   = 30
	severityIslands       = 20
	severityUnusedExports = 10
)

// repoSections answers "what to refactor next" — the report a branch-less
// checkout, or a branch that changed nothing structural, gets instead of a
// comparison it has no base for.
func repoSections(head *side, groups *grouping, ports portRules) []Section {
	return []Section{
		repoGroupCycles(head, groups),
		repoInversions(head),
		repoGodFiles(head),
		repoGodPackages(head),
		repoIslands(head, ports),
		repoUnusedExports(head, ports),
	}
}

func repoGroupCycles(head *side, groups *grouping) Section {
	var items []Item
	for _, cycle := range cyclesOf(head, groups) {
		item := Item{
			Text:   cycleLabel(cycle),
			Target: Target{Edges: cycle.pkgEdges()},
		}
		if len(cycle.Internal) > 0 {
			// Internal is sorted weakest first, and the weakest link is
			// where cutting costs least.
			weakest := cycle.Internal[0]
			detail := fmt.Sprintf("weakest link %s → %s, %s",
				weakest.From, weakest.To,
				plural(weakest.Weight, "dependency", "dependencies"))
			if len(weakest.Samples) > 0 {
				detail += ": " + strings.Join(weakest.Samples, ", ")
			}
			item.Detail = detail + " — cut there"
			if len(weakest.PkgEdges) > 0 {
				edge := weakest.PkgEdges[0]
				item.Target.Edge = &edge
				item.Target.ComponentID = edge.From
			}
		}
		items = append(items, item)
	}

	return makeSection(
		SectionGroupCycles, "Group cycles", severityGroupCycles,
		"no cycles between groups",
		plural(len(items), "group cycle", "group cycles"),
		items,
	)
}

func repoInversions(head *side) Section {
	backward := make([]struct {
		from, to string
		span     float64
	}, 0, len(head.symTrophic.BackwardEdges))
	for _, be := range head.symTrophic.BackwardEdges {
		backward = append(backward, struct {
			from, to string
			span     float64
		}{be.From, be.To, be.Span})
	}
	sort.Slice(backward, func(i, j int) bool {
		if backward[i].span != backward[j].span {
			return backward[i].span > backward[j].span
		}
		if backward[i].from != backward[j].from {
			return backward[i].from < backward[j].from
		}
		return backward[i].to < backward[j].to
	})

	items := make([]Item, 0, len(backward))
	for _, inv := range backward {
		from, okFrom := head.symbols[inv.from]
		to, okTo := head.symbols[inv.to]
		if !okFrom || !okTo {
			continue
		}
		items = append(items, Item{
			Text: from.label() + " → " + to.label(),
			Detail: fmt.Sprintf("reaches %.1f levels up — put an interface in the lower layer and depend on that",
				inv.span),
			Target: from.target(),
		})
	}

	f0 := head.symTrophic.IncoherenceF0
	return makeSection(
		SectionInversions, "Inversions", severityInversions,
		fmt.Sprintf("no inversions (F0 %.2f, %s)", f0, trophicVerdict(f0)),
		fmt.Sprintf("F0 %.2f (%s) — %s", f0, trophicVerdict(f0),
			plural(len(items), "inversion", "inversions")),
		items,
	)
}

func repoGodFiles(head *side) Section {
	var items []Item
	for _, file := range head.files.Files {
		if !file.Outlier {
			continue
		}
		path := strings.TrimPrefix(file.File, kindPrefixFile)
		items = append(items, Item{
			Text: fmt.Sprintf("%s — %s", path, plural(file.SymbolCount, "declaration", "declarations")),
			Detail: fmt.Sprintf("past the threshold of %d (three times the median of %.0f) — split the file",
				head.cutoffDeclarations(), head.files.MedianSymbols),
			Target: Target{File: path, ComponentID: head.packageOfFile(path)},
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Text < items[j].Text })

	return makeSection(
		SectionGodFiles, "God files", severityGodFiles,
		"no file is structurally overloaded",
		plural(len(items), "file is structurally overloaded", "files are structurally overloaded"),
		items,
	)
}

func repoGodPackages(head *side) Section {
	degree := head.packageDegree()
	fanIn, fanOut := map[string]int{}, map[string]int{}
	for edge := range head.edges {
		fanOut[edge.From]++
		fanIn[edge.To]++
	}

	var items []Item
	for _, pkg := range godPackages(head, degree) {
		items = append(items, Item{
			Text: fmt.Sprintf("%s — degree %d", pkg, degree[pkg]),
			Detail: fmt.Sprintf("%d in, %d out — ask for its latent domains and pull the glue to a thin boundary",
				fanIn[pkg], fanOut[pkg]),
			Target: Target{ComponentID: pkg},
		})
	}

	return makeSection(
		SectionGodPackages, "God packages", severityGodPackages,
		"no package is an outlier for coupling",
		plural(len(items), "package is an outlier for coupling", "packages are outliers for coupling"),
		items,
	)
}

func repoIslands(head *side, ports portRules) Section {
	var items []Item

	// Package level: more than one component means a package tree nothing
	// connects to the rest of the module.
	if head.pkgComps.ComponentCount > 1 {
		islands := make([]components.Component, len(head.pkgComps.Components))
		copy(islands, head.pkgComps.Components)
		sort.Slice(islands, func(i, j int) bool {
			if islands[i].Size != islands[j].Size {
				return islands[i].Size > islands[j].Size
			}
			return islands[i].CenterNodeID < islands[j].CenterNodeID
		})
		for _, island := range islands[1:] {
			pkgs := make([]string, 0, len(island.Members))
			for _, member := range island.Members {
				pkgs = append(pkgs, strings.TrimPrefix(member, kindPrefixPkg))
			}
			sort.Strings(pkgs)
			if len(pkgs) == 0 {
				continue
			}
			items = append(items, Item{
				Text:   strings.Join(pkgs, ", "),
				Detail: "no dependency joins this to the rest of the module — delete it, or add the missing edge",
				Target: Target{ComponentID: pkgs[0]},
			})
		}
	}

	// Symbol level: a declaration with no edge in or out at all.
	if len(head.symbolNodes) > 0 {
		result := components.Analyze(head.graph, head.symbolNodes)
		var singletons []symbol
		for _, component := range result.Components {
			if component.Size != 1 || len(component.Members) != 1 {
				continue
			}
			sym, ok := head.symbols[component.Members[0]]
			if !ok || ports.isPort(sym, head) {
				continue
			}
			singletons = append(singletons, sym)
		}
		sort.Slice(singletons, func(i, j int) bool { return singletons[i].label() < singletons[j].label() })
		for _, sym := range singletons {
			items = append(items, Item{
				Text:   sym.label(),
				Detail: "no edge in or out (tests not in graph) — delete it, or wire it up",
				Target: sym.target(),
			})
		}
	}

	return makeSection(
		SectionIslands, "Islands", severityIslands,
		"the graph is connected",
		plural(len(items), "island", "islands"),
		items,
	)
}

func repoUnusedExports(head *side, ports portRules) Section {
	var items []Item
	for _, finding := range unusedExports(head, ports, nil) {
		item := Item{Text: finding.sym.label(), Target: finding.sym.target()}
		if finding.dead {
			item.Tag = TagDead
			item.Detail = "0 callers (tests not in graph) anywhere — delete it"
		} else {
			item.Detail = "0 callers (tests not in graph) outside its package — unexport it"
		}
		items = append(items, item)
	}

	return makeSection(
		SectionUnusedExports, "Unused exports", severityUnusedExports,
		"every export has a caller outside its package",
		plural(len(items), "export has no caller outside its package",
			"exports have no caller outside their package"),
		items,
	)
}
