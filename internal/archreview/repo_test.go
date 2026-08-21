package archreview

import (
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/overlay"
)

func TestBuildWithoutBaseIsRepoMode(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("internal/domain").strct("Model", "model.go", 1),
		newPkg("internal/service").
			fn("Run", "service.go", 1).
			dep("Run", "internal/domain", "Model"),
	)})

	if report.Mode != ModeRepo {
		t.Fatalf("Mode = %q, want %q", report.Mode, ModeRepo)
	}
	if report.Base != nil {
		t.Errorf("Base = %+v, want nil in repo mode", report.Base)
	}
	if report.Schema != Schema {
		t.Errorf("Schema = %q, want %q", report.Schema, Schema)
	}

	want := []string{
		SectionGroupCycles, SectionInversions, SectionGodFiles,
		SectionGodPackages, SectionIslands, SectionUnusedExports,
	}
	var got []string
	for _, section := range report.Sections {
		got = append(got, section.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("section order = %v, want %v", got, want)
	}

	if report.Totals.Packages != 2 {
		t.Errorf("Totals.Packages = %d, want 2", report.Totals.Packages)
	}
	if report.Totals.Edges != 1 {
		t.Errorf("Totals.Edges = %d, want 1", report.Totals.Edges)
	}
	if report.Totals.Components != 1 {
		t.Errorf("Totals.Components = %d, want 1", report.Totals.Components)
	}
}

// A section that did not trigger renders as one "ok" line with no rows: a
// clean repository must not produce a grid of figures.
func TestUntriggeredSectionsCollapseToOK(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("internal/domain").strct("Model", "model.go", 1),
		newPkg("internal/service").
			fn("Run", "service.go", 1).
			dep("Run", "internal/domain", "Model"),
	)})

	section, ok := sectionByID(report, SectionGroupCycles)
	if !ok {
		t.Fatalf("no %s section", SectionGroupCycles)
	}
	if section.State != StateOK {
		t.Errorf("State = %q, want %q", section.State, StateOK)
	}
	if len(section.Items) != 0 {
		t.Errorf("Items = %v, want none", itemTexts(section))
	}
	if section.Summary != "no cycles between groups" {
		t.Errorf("Summary = %q", section.Summary)
	}
}

// Go forbids package import cycles, so a cycle only exists once packages are
// collapsed into groups. The row must name the weakest group edge and the
// symbol dependency holding it, because that is where cutting costs least.
func TestGroupCycleCollapsesPackagesAndNamesWeakestEdge(t *testing.T) {
	report := Build(Input{Head: models(
		// internal/alpha depends on internal/beta twice over…
		newPkg("internal/alpha/one").
			fn("A1", "a.go", 1).
			dep("A1", "internal/beta/one", "B1"),
		newPkg("internal/alpha/two").
			fn("A2", "a.go", 1).
			dep("A2", "internal/beta/one", "B1"),
		// …and internal/beta depends back on internal/alpha once, which is
		// the weak link that closes the group cycle.
		newPkg("internal/beta/one").
			fn("B1", "b.go", 1).
			dep("B1", "internal/alpha/three", "A3"),
		newPkg("internal/alpha/three").fn("A3", "a.go", 1),
	)})

	section, ok := sectionByID(report, SectionGroupCycles)
	if !ok {
		t.Fatalf("no %s section", SectionGroupCycles)
	}
	if section.State != StateFlag || len(section.Items) != 1 {
		t.Fatalf("State = %q with %d items, want one flagged cycle", section.State, len(section.Items))
	}

	item := section.Items[0]
	if item.Text != "internal/alpha ↔ internal/beta" {
		t.Errorf("Text = %q, want the two groups", item.Text)
	}
	if !strings.Contains(item.Detail, "weakest link internal/beta → internal/alpha") {
		t.Errorf("Detail = %q, want the single-dependency edge named as weakest", item.Detail)
	}
	if !strings.Contains(item.Detail, "internal/beta/one.B1 → internal/alpha/three.A3") {
		t.Errorf("Detail = %q, want the symbol holding the weak edge", item.Detail)
	}
	if len(item.Target.Edges) != 3 {
		t.Errorf("Target.Edges = %v, want the cycle's three package edges", item.Target.Edges)
	}
	if item.Target.Edge == nil || item.Target.Edge.From != "internal/beta/one" {
		t.Errorf("Target.Edge = %+v, want the weakest package edge", item.Target.Edge)
	}
}

// Configured review groups own the collapse, so a cycle the report names is a
// cycle between the categories the reviewer already sees on the canvas.
func TestGroupCycleUsesOverlayReviewGroups(t *testing.T) {
	cfg := &overlay.Config{
		ReviewGroups: map[string]overlay.ReviewGroup{
			"core": {Packages: overlay.PackageSelector{Include: []string{"internal/alpha/..."}}},
			"edge": {Packages: overlay.PackageSelector{Include: []string{"internal/beta/..."}}},
		},
	}
	report := Build(Input{
		Overlay: cfg,
		Head: models(
			newPkg("internal/alpha/one").fn("A1", "a.go", 1).dep("A1", "internal/beta/one", "B1"),
			newPkg("internal/beta/one").fn("B1", "b.go", 1).dep("B1", "internal/alpha/two", "A2"),
			newPkg("internal/alpha/two").fn("A2", "a.go", 1),
		),
	})

	section, _ := sectionByID(report, SectionGroupCycles)
	if len(section.Items) != 1 {
		t.Fatalf("items = %v, want one cycle", itemTexts(section))
	}
	if got := section.Items[0].Text; got != "core ↔ edge" {
		t.Errorf("Text = %q, want the configured group names", got)
	}
}

// Without configured groups the fallback is the depth-2 directory prefix, so
// two packages under one prefix are one group and cannot cycle with each other.
func TestDirectoryGroupingIsDepthTwo(t *testing.T) {
	cases := map[string]string{
		"internal/adapter/http": "internal/adapter",
		"internal/serve":        "internal/serve",
		"cmd/wyrd":            "cmd",
		"internal/plugins/x/y":  "plugins",
		".":                     "(root)",
	}
	for pkg, want := range cases {
		if got := directoryGroup(pkg); got != want {
			t.Errorf("directoryGroup(%q) = %q, want %q", pkg, got, want)
		}
	}
}

// The caveat is said out loud on the row, not hidden: the graph has no test
// files in it, so "no callers" may mean "used only by tests" — itself a
// finding, just a different one.
func TestUnusedExportsSayTestsAreNotInTheGraph(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("internal/lib").
			fn("Helper", "lib.go", 1).
			fn("caller", "lib.go", 10, callTo("internal/lib", "Helper")),
		newPkg("internal/other").fn("Run", "other.go", 1),
	)})

	section, ok := sectionByID(report, SectionUnusedExports)
	if !ok {
		t.Fatalf("no %s section", SectionUnusedExports)
	}
	var helper *Item
	for i := range section.Items {
		if section.Items[i].Text == "internal/lib.Helper" {
			helper = &section.Items[i]
		}
	}
	if helper == nil {
		t.Fatalf("internal/lib.Helper not reported; items = %v", itemTexts(section))
	}
	if !strings.Contains(helper.Detail, "0 callers (tests not in graph)") {
		t.Errorf("Detail = %q, want the exact tests-not-in-graph caveat", helper.Detail)
	}
	if helper.Tag == TagDead {
		t.Errorf("Tag = %q, want no dead tag: Helper has a caller inside its package", helper.Tag)
	}
	if helper.Target.InternalID != "internal/lib.Helper" {
		t.Errorf("Target.InternalID = %q, want the uigraph internal id", helper.Target.InternalID)
	}
	if helper.Target.ComponentID != "internal/lib" {
		t.Errorf("Target.ComponentID = %q, want the package path", helper.Target.ComponentID)
	}
}

// An export nothing references at all is tagged dead, because the action is
// to delete it rather than to hide it.
func TestUnusedExportTagsDeadWhenNothingReferencesIt(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("internal/lib").fn("Helper", "lib.go", 1),
		newPkg("internal/other").fn("Run", "other.go", 1),
	)})

	section, _ := sectionByID(report, SectionUnusedExports)
	for _, item := range section.Items {
		if item.Text != "internal/lib.Helper" {
			continue
		}
		if item.Tag != TagDead {
			t.Errorf("Tag = %q, want %q", item.Tag, TagDead)
		}
		if !strings.Contains(item.Detail, "0 callers (tests not in graph)") {
			t.Errorf("Detail = %q, want the caveat", item.Detail)
		}
		return
	}
	t.Fatalf("internal/lib.Helper not reported; items = %v", itemTexts(section))
}

// Rule 1 — language visibility. Only packages Go itself hides are checked;
// an export of any other package is importable by the world and is a port by
// Go's own rule, not by a guess about intent.
func TestPortRuleLanguageVisibility(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("pkg/api").fn("Helper", "api.go", 1),
		newPkg("internal/lib").fn("Helper", "lib.go", 1),
	)})

	section, _ := sectionByID(report, SectionUnusedExports)
	got := strings.Join(itemTexts(section), ",")
	if strings.Contains(got, "pkg/api.Helper") {
		t.Errorf("pkg/api.Helper reported, but its package is importable by the world: %v", itemTexts(section))
	}
	if !strings.Contains(got, "internal/lib.Helper") {
		t.Errorf("internal/lib.Helper not reported; items = %v", itemTexts(section))
	}
}

// Still rule 1 — the runtime calls Go's own entry points, so no edge in the
// graph ever will. Reporting main or init as unreferenced would be reporting
// the language.
func TestGoEntryPointsAreNeverOrphans(t *testing.T) {
	cmd := newPkg("cmd/app").fn("main", "main.go", 1).fn("init", "main.go", 20)
	cmd.model.Name = "main"
	report := Build(Input{Head: models(
		cmd,
		newPkg("internal/lib").fn("unreferenced", "lib.go", 1),
	)})

	section, _ := sectionByID(report, SectionIslands)
	got := strings.Join(itemTexts(section), ",")
	for _, gone := range []string{"cmd/app.main", "cmd/app.init"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s reported as an island; items = %v", gone, itemTexts(section))
		}
	}
	if !strings.Contains(got, "internal/lib.unreferenced") {
		t.Errorf("an ordinary unreferenced function should still be reported; items = %v", itemTexts(section))
	}
}

// Rule 2 — overlay declaration, in all three selector forms.
func TestPortRuleOverlayDeclaration(t *testing.T) {
	head := models(
		newPkg("internal/plugins/events").strct("Plugin", "plugin.go", 1),
		newPkg("internal/adapter/mcp").fn("Dispatch", "mcp.go", 1),
		newPkg("internal/serve").strct("State", "state.go", 1, method("Snapshot"), method("Reload")),
		newPkg("internal/lib").fn("Helper", "lib.go", 1),
	)

	bare := Build(Input{Head: head})
	section, _ := sectionByID(bare, SectionUnusedExports)
	for _, want := range []string{
		"internal/plugins/events.Plugin",
		"internal/adapter/mcp.Dispatch",
		"internal/serve.State.Snapshot",
	} {
		if !strings.Contains(strings.Join(itemTexts(section), ","), want) {
			t.Fatalf("undeclared %s was not reported; items = %v", want, itemTexts(section))
		}
	}

	declared := Build(Input{
		Head: head,
		Overlay: &overlay.Config{Ports: overlay.Ports{External: []string{
			"internal/plugins/...",          // package glob
			"internal/adapter/mcp.Dispatch", // one symbol
			"internal/serve.State.Snapshot", // one member
		}}},
	})
	section, _ = sectionByID(declared, SectionUnusedExports)
	got := strings.Join(itemTexts(section), ",")
	for _, gone := range []string{
		"internal/plugins/events.Plugin",
		"internal/adapter/mcp.Dispatch",
		"internal/serve.State.Snapshot",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("%s still reported after being declared an external port; items = %v", gone, itemTexts(section))
		}
	}
	// A member selector covers that member alone: declaring State.Snapshot
	// is not a licence for every other method of State.
	if !strings.Contains(got, "internal/serve.State.Reload") {
		t.Errorf("internal/serve.State.Reload should still be reported; items = %v", itemTexts(section))
	}
	if !strings.Contains(got, "internal/lib.Helper") {
		t.Errorf("internal/lib.Helper should still be reported; items = %v", itemTexts(section))
	}
}

// Rule 3 — graph evidence. A method of a type that implements an interface
// declaring the same method is used through the interface, and the implements
// edge proves it.
func TestPortRuleGraphEvidenceThroughInterface(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("internal/port").
			iface("Store", "port.go", 1, "Save", "Load").
			implements("internal/adapter/db", "Postgres", "Store"),
		newPkg("internal/adapter/db").
			strct("Postgres", "db.go", 1, method("Save"), method("Load"), method("Vacuum")),
	)})

	section, _ := sectionByID(report, SectionUnusedExports)
	got := strings.Join(itemTexts(section), ",")
	for _, gone := range []string{
		"internal/adapter/db.Postgres.Save",
		"internal/adapter/db.Postgres.Load",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("%s reported, but the implements edge shows it is used through Store; items = %v",
				gone, itemTexts(section))
		}
	}
	// Vacuum is not on the interface, so nothing in the graph reaches it.
	if !strings.Contains(got, "internal/adapter/db.Postgres.Vacuum") {
		t.Errorf("internal/adapter/db.Postgres.Vacuum not reported; items = %v", itemTexts(section))
	}
}

// A god file is flagged at the lens's own threshold — three times the median,
// never below twenty — so the report and file_hotspots cannot disagree.
func TestGodFilesUseTheLensThreshold(t *testing.T) {
	big := newPkg("internal/big")
	for i := 0; i < 24; i++ {
		big.fn("Fn"+string(rune('A'+i)), "huge.go", i*10+1)
	}
	// Enough ordinary files that the median stays at one declaration, so the
	// threshold is the floor of twenty rather than three times a large median.
	small := newPkg("internal/small")
	for i := 0; i < 5; i++ {
		small.fn("One"+string(rune('A'+i)), "small"+string(rune('a'+i))+".go", 1)
	}
	report := Build(Input{Head: models(big, small)})

	section, ok := sectionByID(report, SectionGodFiles)
	if !ok {
		t.Fatalf("no %s section", SectionGodFiles)
	}
	if section.State != StateFlag {
		t.Fatalf("State = %q, want a flagged god file; summary = %q", section.State, section.Summary)
	}
	item := section.Items[0]
	if !strings.HasPrefix(item.Text, "internal/big/huge.go — 24 declarations") {
		t.Errorf("Text = %q, want the file and its declaration count", item.Text)
	}
	if item.Target.File != "internal/big/huge.go" {
		t.Errorf("Target.File = %q, want the module-relative path", item.Target.File)
	}
	if item.Target.ComponentID != "internal/big" {
		t.Errorf("Target.ComponentID = %q, want the owning package", item.Target.ComponentID)
	}
}

// A package tree nothing depends on and that depends on nothing is an island.
func TestIslandsReportDisconnectedPackages(t *testing.T) {
	report := Build(Input{Head: models(
		newPkg("internal/a").fn("A", "a.go", 1).dep("A", "internal/b", "B"),
		newPkg("internal/b").fn("B", "b.go", 1),
		newPkg("internal/lonely").fn("L", "l.go", 1),
	)})

	section, ok := sectionByID(report, SectionIslands)
	if !ok {
		t.Fatalf("no %s section", SectionIslands)
	}
	if !strings.Contains(strings.Join(itemTexts(section), ","), "internal/lonely") {
		t.Errorf("internal/lonely not reported as an island; items = %v", itemTexts(section))
	}
}

// The index is only reported while it stands in the reviewer's way.
func TestIndexReportedOnlyWhenNotReady(t *testing.T) {
	head := models(newPkg("internal/a").fn("A", "a.go", 1))

	ready := Build(Input{Head: head, Index: IndexStatus{Ready: true, DenseAvailable: true}})
	if ready.Index != nil {
		t.Errorf("Index = %+v, want nil once the index is ready", ready.Index)
	}

	indexing := Build(Input{Head: head, Index: IndexStatus{Indexing: true, Embedded: 3, Embeddable: 9}})
	if indexing.Index == nil {
		t.Fatal("Index = nil while indexing, want progress reported")
	}
	if indexing.Index.Embedded != 3 || indexing.Index.Embeddable != 9 {
		t.Errorf("Index = %+v, want the embedding progress carried through", indexing.Index)
	}
}
