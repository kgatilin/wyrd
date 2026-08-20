package archreview

import (
	"fmt"
	"sort"

	"github.com/kgatilin/archai/internal/diff"
)

// itemLimit caps a section's rows. What is cut is counted in Section.More, so
// a long list says how long it is instead of pretending to be complete.
const itemLimit = 25

// Section ids. Review mode and repo mode answer different questions, so they
// do not share ids even where the underlying measurement is the same.
const (
	SectionGroupCyclesNew = "group_cycles_new"
	SectionEdgesNew       = "edges_new"
	SectionInversionsNew  = "inversions_new"
	SectionUnusedExpNew   = "unused_exports_new"
	SectionImpact         = "impact"
	SectionHotspotGrowth  = "hotspot_growth"
	SectionOrphansNew     = "orphans_new"
	SectionGroupCycles    = "group_cycles"
	SectionInversions     = "inversions"
	SectionGodFiles       = "god_files"
	SectionGodPackages    = "god_packages"
	SectionIslands        = "islands"
	SectionUnusedExports  = "unused_exports"
)

// Build produces the report. It never fails: a measurement that cannot run is
// recorded in Report.Warnings, because a reviewer must be able to tell a
// section that found nothing from a section that never ran.
func Build(in Input) Report {
	report := Report{Schema: Schema, Mode: ModeRepo, Sections: []Section{}}

	head, err := newSide(in.Head, in.Overlay)
	if err != nil {
		report.Warnings = append(report.Warnings, "head: "+err.Error())
		report.Index = indexWhenBlocking(in.Index)
		return report
	}
	report.Totals = Totals{
		Packages:   len(head.pkgs),
		Edges:      len(head.edges),
		Components: head.pkgComps.ComponentCount,
	}

	groups := newGrouping(in.Overlay)
	ports := newPortRules(in.Overlay)

	var base *side
	if len(in.Base) > 0 {
		base, err = newSide(in.Base, in.Overlay)
		if err != nil {
			report.Warnings = append(report.Warnings,
				"base: "+err.Error()+" (falling back to repo mode)")
			base = nil
		}
	}

	// The model diff decides the mode and feeds the impact section. It runs
	// after both graphs are built on purpose: diff.Compute normalizes copies
	// of the symbols it compares, and a regression there would otherwise
	// strip call edges and spans out from under the analysis.
	if base != nil {
		if d := diff.Compute(head.models, base.models); !d.IsEmpty() {
			report.Mode = ModeReview
			report.Base = &Base{Ref: in.BaseRef, Rev: in.BaseRev}
			report.Sections = reviewSections(head, base, groups, ports, d, in.Changed)
		}
	}
	if report.Mode == ModeRepo {
		report.Sections = repoSections(head, groups, ports)
	}

	sortSections(report.Sections)
	report.Index = indexWhenBlocking(in.Index)
	return report
}

// indexWhenBlocking returns the index status only while it stands between the
// reviewer and an answer: still building, or with no embedder configured.
func indexWhenBlocking(status IndexStatus) *IndexStatus {
	if status.Ready {
		return nil
	}
	out := status
	return &out
}

// sortSections orders by severity, most urgent first, with the id as the
// tie-break so the panel never reshuffles between two identical reports.
func sortSections(sections []Section) {
	sort.SliceStable(sections, func(i, j int) bool {
		if sections[i].Severity != sections[j].Severity {
			return sections[i].Severity > sections[j].Severity
		}
		return sections[i].ID < sections[j].ID
	})
}

// makeSection assembles one section. okText is what an untriggered section
// says on its single line; headline is the wording that introduces the list,
// which is where a figure such as the trophic F0 belongs.
func makeSection(id, title string, severity int, okText, headline string, items []Item) Section {
	section := Section{
		ID:       id,
		Title:    title,
		Severity: severity,
		State:    StateOK,
		Count:    len(items),
		Summary:  okText,
		Items:    []Item{},
	}
	if len(items) == 0 {
		return section
	}
	section.State = StateFlag
	section.Summary = headline
	if len(items) > itemLimit {
		section.More = len(items) - itemLimit
		items = items[:itemLimit]
	}
	section.Items = items
	return section
}

// plural renders "1 thing" / "3 things" without the caller reaching for an
// if at every call site.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
