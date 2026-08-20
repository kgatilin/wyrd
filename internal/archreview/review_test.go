package archreview

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/overlay"
)

// baseModels and headModels share a shape: the branch adds a package, an
// export, a dependency and a few declarations, which is enough to reach every
// review section.
func reviewFixture() (base, head []*pkgBuilder) {
	base = []*pkgBuilder{
		newPkg("internal/domain").strct("Model", "model.go", 1),
		newPkg("internal/service").
			fn("Run", "service.go", 1).
			dep("Run", "internal/domain", "Model"),
		newPkg("internal/api").
			fn("Handle", "api.go", 1).
			dep("Handle", "internal/service", "Run"),
	}
	head = []*pkgBuilder{
		newPkg("internal/domain").strct("Model", "model.go", 1),
		newPkg("internal/service").
			fn("Run", "service.go", 1).
			fn("Helper", "service.go", 20).
			dep("Run", "internal/domain", "Model"),
		newPkg("internal/api").
			fn("Handle", "api.go", 1).
			dep("Handle", "internal/service", "Run").
			dep("Handle", "internal/domain", "Model"),
	}
	return base, head
}

func TestBuildWithBaseIsReviewMode(t *testing.T) {
	base, head := reviewFixture()
	report := Build(Input{
		Head:    models(head...),
		Base:    models(base...),
		BaseRef: "main",
		BaseRev: "abc1234",
	})

	if report.Mode != ModeReview {
		t.Fatalf("Mode = %q, want %q", report.Mode, ModeReview)
	}
	if report.Base == nil || report.Base.Ref != "main" || report.Base.Rev != "abc1234" {
		t.Errorf("Base = %+v, want the ref and rev the comparison used", report.Base)
	}

	want := []string{
		SectionGroupCyclesNew, SectionEdgesNew, SectionInversionsNew,
		SectionUnusedExpNew, SectionImpact, SectionHotspotGrowth, SectionOrphansNew,
	}
	var got []string
	for _, section := range report.Sections {
		got = append(got, section.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("section order = %v, want %v", got, want)
	}
}

// A base whose model is identical to head answers no question about a branch,
// so the report falls back to "what should be refactored next".
func TestIdenticalBaseFallsBackToRepoMode(t *testing.T) {
	_, head := reviewFixture()
	report := Build(Input{
		Head:    models(head...),
		Base:    models(head...),
		BaseRef: "main",
	})
	if report.Mode != ModeRepo {
		t.Errorf("Mode = %q, want %q when the model diff is empty", report.Mode, ModeRepo)
	}
	if report.Base != nil {
		t.Errorf("Base = %+v, want nil outside review mode", report.Base)
	}
}

// A package dependency the branch introduced is reported with its endpoints as
// component ids, so the panel can select the edge without translating anything.
func TestNewCrossPackageEdgeIsReportedWithClickTarget(t *testing.T) {
	base, head := reviewFixture()
	report := Build(Input{Head: models(head...), Base: models(base...)})

	section, ok := sectionByID(report, SectionEdgesNew)
	if !ok {
		t.Fatalf("no %s section", SectionEdgesNew)
	}
	if section.State != StateFlag || len(section.Items) != 1 {
		t.Fatalf("items = %v, want the one added dependency", itemTexts(section))
	}
	item := section.Items[0]
	if item.Text != "internal/api → internal/domain" {
		t.Errorf("Text = %q, want the new edge", item.Text)
	}
	if item.Tag != TagOK {
		t.Errorf("Tag = %q, want %q for an edge no policy forbids", item.Tag, TagOK)
	}
	if item.Target.Edge == nil ||
		item.Target.Edge.From != "internal/api" || item.Target.Edge.To != "internal/domain" {
		t.Errorf("Target.Edge = %+v, want the package edge", item.Target.Edge)
	}
	if item.Target.ComponentID != "internal/api" {
		t.Errorf("Target.ComponentID = %q, want the source package", item.Target.ComponentID)
	}
}

// The policy tag comes from internal/policy, the same evaluator the CI gate
// runs, so the report and the gate cannot disagree about a forbidden edge.
func TestNewEdgeTaggedByThePolicyEvaluator(t *testing.T) {
	cfg := &overlay.Config{
		Module: "example.com/m",
		Layers: map[string][]string{
			"domain":   {"internal/domain/..."},
			"adapters": {"internal/adapter/..."},
		},
		Policy: overlay.PolicyConfig{
			DenyByDefault: new(bool), // false: state the invariant, do not demand an allow-list
			Forbid:        []string{"@domain !-> @adapters"},
		},
	}
	base := models(
		newPkg("internal/domain").layer("domain").strct("Model", "model.go", 1),
		newPkg("internal/adapter/db").layer("adapters").
			fn("Save", "db.go", 1).dep("Save", "internal/domain", "Model"),
	)
	head := models(
		newPkg("internal/domain").layer("domain").
			strct("Model", "model.go", 1).
			fn("Persist", "model.go", 20).
			dep("Persist", "internal/adapter/db", "Save"),
		newPkg("internal/adapter/db").layer("adapters").
			fn("Save", "db.go", 1).dep("Save", "internal/domain", "Model"),
	)

	report := Build(Input{Head: head, Base: base, Overlay: cfg})
	section, _ := sectionByID(report, SectionEdgesNew)
	if len(section.Items) == 0 {
		t.Fatalf("no new edges reported")
	}
	item := section.Items[0]
	if item.Text != "internal/domain → internal/adapter/db" {
		t.Fatalf("first item = %q, want the forbidden edge sorted first", item.Text)
	}
	if item.Tag != TagPolicy {
		t.Errorf("Tag = %q, want %q", item.Tag, TagPolicy)
	}
	if !strings.Contains(item.Detail, "forbidden by") {
		t.Errorf("Detail = %q, want the policy's own message", item.Detail)
	}
}

// Backward means the edge climbs the package-level trophic hierarchy: a
// foundation package depending on one above it.
func TestBackwardEdgeClimbsThePackageLayering(t *testing.T) {
	s, err := newSide(models(
		newPkg("internal/entry").fn("Entry", "e.go", 1).dep("Entry", "internal/hub", "Hub"),
		newPkg("internal/hub").fn("Hub", "h.go", 1).
			dep("Hub", "internal/leafa", "A").dep("Hub", "internal/leafb", "B"),
		newPkg("internal/leafa").fn("A", "a.go", 1),
		newPkg("internal/leafb").fn("B", "b.go", 1),
	), nil)
	if err != nil {
		t.Fatal(err)
	}

	leaf, okLeaf := s.pkgLevel("internal/leafa")
	entry, okEntry := s.pkgLevel("internal/entry")
	if !okLeaf || !okEntry || !(leaf < entry) {
		t.Fatalf("levels leafa=%d(%v) entry=%d(%v), want the leaf below the entry point",
			leaf, okLeaf, entry, okEntry)
	}
	if !isBackwardPackageEdge(s, Edge{From: "internal/leafa", To: "internal/entry"}) {
		t.Error("leafa → entry should be backward: it points up the hierarchy")
	}
	if isBackwardPackageEdge(s, Edge{From: "internal/entry", To: "internal/leafa"}) {
		t.Error("entry → leafa points down the hierarchy and is not backward")
	}
}

// A cycle the branch closed names the edge that closed it, because removing
// that one edge is the smallest change that reopens the loop.
func TestNewGroupCycleNamesTheEdgeThatClosedIt(t *testing.T) {
	base := models(
		newPkg("internal/alpha/one").fn("A1", "a.go", 1).dep("A1", "internal/beta/one", "B1"),
		newPkg("internal/beta/one").fn("B1", "b.go", 1),
		newPkg("internal/alpha/two").fn("A2", "a.go", 1),
	)
	head := models(
		newPkg("internal/alpha/one").fn("A1", "a.go", 1).dep("A1", "internal/beta/one", "B1"),
		newPkg("internal/beta/one").
			fn("B1", "b.go", 1).
			fn("B2", "b.go", 20).
			dep("B2", "internal/alpha/two", "A2"),
		newPkg("internal/alpha/two").fn("A2", "a.go", 1),
	)

	report := Build(Input{Head: head, Base: base})
	section, ok := sectionByID(report, SectionGroupCyclesNew)
	if !ok {
		t.Fatalf("no %s section", SectionGroupCyclesNew)
	}
	if section.State != StateFlag || len(section.Items) != 1 {
		t.Fatalf("items = %v (state %q), want one new cycle", itemTexts(section), section.State)
	}
	item := section.Items[0]
	if item.Tag != TagNew {
		t.Errorf("Tag = %q, want %q", item.Tag, TagNew)
	}
	if !strings.Contains(item.Detail, "closed by internal/beta/one → internal/alpha/two") {
		t.Errorf("Detail = %q, want the branch's own edge named as the closer", item.Detail)
	}
	if item.Target.Edge == nil || item.Target.Edge.To != "internal/alpha/two" {
		t.Errorf("Target.Edge = %+v, want the closing edge", item.Target.Edge)
	}
}

// Inversions are the symbol-level backward edges the branch introduced, headed
// by the trophic incoherence they belong to — never a standalone figure.
func TestNewInversionsAreHeadedByTheIncoherence(t *testing.T) {
	// Base: a straight-line parser. Head: the branch makes it recursive, and
	// the recursion puts a callee above its own caller.
	base := models(
		newPkg("internal/parser").
			fn("parseExpr", "parse.go", 1, callTo("internal/parser", "parseTerm")).
			fn("parseTerm", "parse.go", 20, callTo("internal/parser", "parseFactor")).
			fn("parseFactor", "parse.go", 40),
	)
	head := models(
		newPkg("internal/parser").
			fn("parseExpr", "parse.go", 1, callTo("internal/parser", "parseTerm")).
			fn("parseTerm", "parse.go", 20,
				callTo("internal/parser", "parseFactor"), callTo("internal/parser", "parseCall")).
			fn("parseFactor", "parse.go", 40, callTo("internal/parser", "parseUnary")).
			fn("parseUnary", "parse.go", 60, callTo("internal/parser", "parseExpr")).
			fn("parseCall", "parse.go", 80, callTo("internal/parser", "parseExpr")),
	)

	report := Build(Input{Head: head, Base: base})
	section, ok := sectionByID(report, SectionInversionsNew)
	if !ok {
		t.Fatalf("no %s section", SectionInversionsNew)
	}
	if section.State != StateFlag {
		t.Fatalf("State = %q, want the recursion's inversion reported; summary = %q",
			section.State, section.Summary)
	}
	if !strings.HasPrefix(section.Summary, "F0 ") {
		t.Errorf("Summary = %q, want the incoherence heading the list", section.Summary)
	}
	item := section.Items[0]
	if !strings.Contains(item.Text, "internal/parser.") || !strings.Contains(item.Text, " → ") {
		t.Errorf("Text = %q, want the inverted pair", item.Text)
	}
	if item.Target.InternalID == "" {
		t.Errorf("Target = %+v, want the source symbol for the wiring panel", item.Target)
	}
}

// The two "nothing uses it" states are one symbol apiece: an added export with
// a local caller is unexported, one with no caller at all is deleted or wired.
func TestUnusedExportsAndOrphansDoNotShareASymbol(t *testing.T) {
	base := models(newPkg("internal/lib").fn("Existing", "lib.go", 1))
	head := models(
		newPkg("internal/lib").
			fn("Existing", "lib.go", 1).
			fn("LocallyUsed", "lib.go", 20).
			fn("Untouched", "lib.go", 40).
			fn("localCaller", "lib.go", 60, callTo("internal/lib", "LocallyUsed")),
	)

	report := Build(Input{Head: head, Base: base})

	unused, _ := sectionByID(report, SectionUnusedExpNew)
	orphaned, _ := sectionByID(report, SectionOrphansNew)
	unusedText := strings.Join(itemTexts(unused), ",")
	orphanText := strings.Join(itemTexts(orphaned), ",")

	if !strings.Contains(unusedText, "internal/lib.LocallyUsed") {
		t.Errorf("unused exports = %v, want the locally-used export", itemTexts(unused))
	}
	if strings.Contains(orphanText, "internal/lib.LocallyUsed") {
		t.Errorf("orphans = %v, must not repeat a symbol the export section owns", itemTexts(orphaned))
	}
	if !strings.Contains(orphanText, "internal/lib.Untouched") {
		t.Errorf("orphans = %v, want the symbol nothing references", itemTexts(orphaned))
	}
	if strings.Contains(unusedText, "internal/lib.Untouched") {
		t.Errorf("unused exports = %v, must not repeat an orphan", itemTexts(unused))
	}
	for _, item := range orphaned.Items {
		if !strings.Contains(item.Detail, "0 callers (tests not in graph)") {
			t.Errorf("orphan detail = %q, want the tests-not-in-graph caveat", item.Detail)
		}
	}
	if !strings.Contains(unusedText, "internal/lib.Existing") {
		// Existing was not added by this branch, so review mode leaves it alone.
		t.Log("Existing correctly absent from the added-symbol scan")
	} else {
		t.Errorf("unused exports = %v, want only symbols this branch added", itemTexts(unused))
	}
}

// Impact is about the blast radius outside what the branch edited: a caller in
// a package the branch never opened is a call site nobody looked at.
func TestImpactCountsCallersInUntouchedPackages(t *testing.T) {
	base := models(
		newPkg("internal/core").strct("Handler", "core.go", 1, method("Serve")),
		newPkg("internal/other").fn("Use", "other.go", 1).dep("Use", "internal/core", "Handler"),
	)
	head := models(
		newPkg("internal/core").strct("Handler", "core.go", 1, method("Serve"), method("Close")),
		newPkg("internal/other").fn("Use", "other.go", 1).dep("Use", "internal/core", "Handler"),
	)

	report := Build(Input{
		Head:    head,
		Base:    base,
		Changed: map[string][]LineRange{"internal/core/core.go": {{Start: 1, End: 20}}},
	})

	section, ok := sectionByID(report, SectionImpact)
	if !ok {
		t.Fatalf("no %s section", SectionImpact)
	}
	if section.State != StateFlag {
		t.Fatalf("State = %q, want the changed handler reported; summary = %q", section.State, section.Summary)
	}
	item := section.Items[0]
	if item.Text != "internal/core.Handler" {
		t.Errorf("Text = %q, want the changed symbol", item.Text)
	}
	if !strings.Contains(item.Detail, "internal/other") {
		t.Errorf("Detail = %q, want the untouched package named", item.Detail)
	}
	if item.Target.InternalID != "internal/core.Handler" {
		t.Errorf("Target.InternalID = %q, want the uigraph internal id", item.Target.InternalID)
	}
}

// A file already past the god-file threshold that gained declarations is a
// hotspot growing, and the row opens that file's patch.
func TestHotspotGrowthFlagsAnOverloadedFileThatGrew(t *testing.T) {
	buildPkg := func(n int) *pkgBuilder {
		p := newPkg("internal/big")
		for i := 0; i < n; i++ {
			p.fn("Fn"+string(rune('A'+i)), "huge.go", i*10+1)
		}
		return p
	}
	small := func() *pkgBuilder {
		p := newPkg("internal/small")
		for i := 0; i < 5; i++ {
			p.fn("One"+string(rune('A'+i)), "small"+string(rune('a'+i))+".go", 1)
		}
		return p
	}

	report := Build(Input{
		Head:    models(buildPkg(24), small()),
		Base:    models(buildPkg(21), small()),
		Changed: map[string][]LineRange{"internal/big/huge.go": {{Start: 200, End: 240}}},
	})

	section, ok := sectionByID(report, SectionHotspotGrowth)
	if !ok {
		t.Fatalf("no %s section", SectionHotspotGrowth)
	}
	if section.State != StateFlag {
		t.Fatalf("State = %q, want the grown hotspot; summary = %q", section.State, section.Summary)
	}
	item := section.Items[0]
	if !strings.Contains(item.Text, "internal/big/huge.go — 24 declarations (+3)") {
		t.Errorf("Text = %q, want the file, its count and its growth", item.Text)
	}
	if item.Target.File != "internal/big/huge.go" {
		t.Errorf("Target.File = %q, want the module-relative path", item.Target.File)
	}
}
