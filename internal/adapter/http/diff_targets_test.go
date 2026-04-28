package http

import (
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/target"
)

// --- unit tests for summarize / filter / group helpers --------------

func TestSummarize_CountsAddRemoveChange(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "p.F"},
		{Op: diff.OpAdd, Kind: diff.KindStruct, Path: "p.S"},
		{Op: diff.OpRemove, Kind: diff.KindInterface, Path: "p.I"},
		{Op: diff.OpChange, Kind: diff.KindFunction, Path: "p.G"},
	}}
	s := summarize(d)
	if s.Additions != 2 || s.Removals != 1 || s.Changes != 1 || s.Total != 4 {
		t.Errorf("summary = %+v, want {2,1,1,4}", s)
	}
}

func TestSummarize_NilAndEmpty(t *testing.T) {
	if got := summarize(nil); got.Total != 0 {
		t.Errorf("nil diff: total = %d, want 0", got.Total)
	}
	if got := summarize(&diff.Diff{}); got.Total != 0 {
		t.Errorf("empty diff: total = %d, want 0", got.Total)
	}
}

func TestBuildKindFilters_MarksSelectedAndCounts(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "p.F"},
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "p.G"},
		{Op: diff.OpRemove, Kind: diff.KindStruct, Path: "p.S"},
	}}

	filters := buildKindFilters(d, "function")
	// Expect "All" + one per kindOrder entry.
	if len(filters) != len(kindOrder)+1 {
		t.Fatalf("len(filters) = %d, want %d", len(filters), len(kindOrder)+1)
	}
	if filters[0].Label != "All" || filters[0].Count != 3 {
		t.Errorf("all-filter = %+v, want {All,3}", filters[0])
	}

	// Find the function filter and verify selected + count.
	var fn *kindFilter
	for i := range filters {
		if filters[i].Kind == "function" {
			fn = &filters[i]
		}
	}
	if fn == nil {
		t.Fatal("no function filter entry")
	}
	if !fn.Selected {
		t.Error("function filter should be selected")
	}
	if fn.Count != 2 {
		t.Errorf("function count = %d, want 2", fn.Count)
	}
	// All-filter must be unselected when a specific kind is chosen.
	if filters[0].Selected {
		t.Error("All should not be selected when kind=function")
	}
}

func TestBuildKindFilters_DefaultSelectsAll(t *testing.T) {
	filters := buildKindFilters(&diff.Diff{}, "")
	if !filters[0].Selected {
		t.Error("All should be selected when no kind filter set")
	}
}

func TestBuildGroups_OrdersAddChangeRemoveAndFilters(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpRemove, Kind: diff.KindFunction, Path: "p.F"},
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "p.G"},
		{Op: diff.OpChange, Kind: diff.KindStruct, Path: "p.S"},
	}}

	groups := buildGroups(d, "")
	if len(groups) != 3 {
		t.Fatalf("len(groups) = %d, want 3", len(groups))
	}
	wantOps := []string{"add", "change", "remove"}
	for i, w := range wantOps {
		if groups[i].Op != w {
			t.Errorf("groups[%d].Op = %s, want %s", i, groups[i].Op, w)
		}
	}

	// With a filter, only matching kinds survive.
	filtered := buildGroups(d, "function")
	if len(filtered) != 2 {
		t.Fatalf("filtered groups = %d, want 2 (add+remove of function)", len(filtered))
	}
	for _, g := range filtered {
		for _, e := range g.Entries {
			if e.Kind != "function" {
				t.Errorf("filter leaked kind %s", e.Kind)
			}
		}
	}
}

func TestBuildGroups_CSSTagsMatchOps(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "a"},
		{Op: diff.OpRemove, Kind: diff.KindFunction, Path: "b"},
		{Op: diff.OpChange, Kind: diff.KindFunction, Path: "c"},
	}}
	groups := buildGroups(d, "")
	want := map[string]string{"add": "added", "remove": "removed", "change": "changed"}
	for _, g := range groups {
		if got, ok := want[g.Op]; !ok || got != g.CSSTag {
			t.Errorf("group op=%s CSSTag=%q, want %q", g.Op, g.CSSTag, got)
		}
	}
}

// --- handler tests --------------------------------------------------

// newDiffTargetsServer builds an httptest.Server rooted at a temp
// project directory. The caller receives the project root so tests
// can seed .arch/targets/ snapshots.
func newDiffTargetsServer(t *testing.T) (*httptest.Server, string, *serve.State) {
	t.Helper()
	root := t.TempDir()
	state := serve.NewState(root)

	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, root, state
}

// seedTarget creates a minimal locked target on disk with one package
// containing a single function named fnName. The resulting layout is
// enough for yamlAdapter.Read to return a non-empty model.
func seedTarget(t *testing.T, root, id, fnName string) {
	t.Helper()
	pkg := "internal/foo"
	// Seed a per-package .arch so target.Lock has something to freeze.
	archDir := filepath.Join(root, pkg, ".arch")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	spec := `schema: archai/v1
package: ` + pkg + `
name: foo
functions:
  - name: ` + fnName + `
    exported: true
`
	if err := os.WriteFile(filepath.Join(archDir, "pub.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := target.Lock(root, id, target.LockOptions{Description: "test target " + id}); err != nil {
		t.Fatalf("Lock %s: %v", id, err)
	}
	// Remove the temp .arch so subsequent seeds for other targets can
	// reuse the same package path without false stale data. The target
	// copy is already snapshotted under .arch/targets/<id>/model/.
	if err := os.RemoveAll(archDir); err != nil {
		t.Fatalf("cleanup pkg arch: %v", err)
	}
}

func TestTargets_ListRendersTargets(t *testing.T) {
	ts, root, _ := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	seedTarget(t, root, "v2", "Beta")

	resp, err := ts.Client().Get(ts.URL + "/targets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp.Body)
	for _, want := range []string{">v1<", ">v2<", "test target v1", "Use"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n\n%s", want, body)
		}
	}
}

func TestTargets_UseSwitchesActive(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	seedTarget(t, root, "v2", "Beta")
	_ = state // state.Load is not required for disk-only tests

	// Post with HX-Request so we get a fragment back.
	req, _ := nethttp.NewRequest(nethttp.MethodPost, ts.URL+"/targets/use?id=v2", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(b))
	}
	body := readBody(t, resp.Body)
	// The returned fragment should mark v2 as active.
	if !strings.Contains(body, `active-badge`) {
		t.Errorf("fragment missing active badge: %s", body)
	}

	// CURRENT on disk must equal v2.
	got, err := target.Current(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v2" {
		t.Errorf("CURRENT = %q, want v2", got)
	}
	// In-memory state must also be updated.
	if snap := state.Snapshot(); snap.CurrentTarget != "v2" {
		t.Errorf("state.CurrentTarget = %q, want v2", snap.CurrentTarget)
	}
}

func TestTargets_UseMissingIDReturns400(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().PostForm(ts.URL+"/targets/use", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTargets_UseWrongMethodReturns405(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/targets/use?id=v1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestDiff_NoActiveTargetShowsBanner(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "No active target") {
		t.Errorf("missing 'No active target' banner:\n%s", body)
	}
}

func TestDiff_RendersChangesWithColorCoding(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Get(ts.URL + "/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(b))
	}
	body := readBody(t, resp.Body)

	// Target v1 contains function "Alpha"; the current project has no
	// .arch yaml files, so the Alpha function must be flagged as an
	// addition (target has it, current doesn't).
	if !strings.Contains(body, "Alpha") {
		t.Errorf("diff body missing Alpha:\n%s", body)
	}
	// Color-coded group class must appear.
	if !strings.Contains(body, "diff-group added") {
		t.Errorf("missing 'diff-group added' class — color coding lost:\n%s", body)
	}
	// Summary numbers.
	if !strings.Contains(body, "stat add") {
		t.Error("missing add stat in summary")
	}
}

// TestDiff_IncludesCytoscapeOverlay verifies the M8 (#46) refactor
// kept the client-side overlay section on the Diff page: when an
// active target is set the page must render a .cy-graph div pointing
// at /api/diff plus the export links.
func TestDiff_IncludesCytoscapeOverlay(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Get(ts.URL + "/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp.Body)
	for _, want := range []string{
		`class="cy-graph diff-overlay"`,
		`data-api="/api/diff"`,
		`href="/view/diff/d2"`,
		`href="/view/diff/svg"`,
		`data-cy-action="fit"`,
		`data-cy-action="fullscreen"`,
		`data-cy-action="export-png"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("diff page missing %q", want)
		}
	}
}

func TestDiff_KindFilterLimitsRows(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Get(ts.URL + "/diff?kind=struct")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp.Body)
	// Filter is struct but the only change is a function; body must
	// show the empty-state message for that filter.
	if !strings.Contains(body, "No <code>struct</code> changes") {
		t.Errorf("missing filter empty-state: %s", body)
	}
}

func TestDiff_HTMXReturnsFragmentWithoutLayout(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}
	req, _ := nethttp.NewRequest(nethttp.MethodGet, ts.URL+"/diff", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp.Body)
	// HTMX fragment must NOT include the <html> layout.
	if strings.Contains(body, "<!doctype html>") {
		t.Errorf("fragment should not include full html doc:\n%s", body)
	}
	// But it must include the diff body.
	if !strings.Contains(body, "diff-page") {
		t.Errorf("fragment missing diff-page wrapper:\n%s", body)
	}
}

func TestTargetsCompare_CrossTargetDiff(t *testing.T) {
	ts, root, _ := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	seedTarget(t, root, "v2", "Beta")

	resp, err := ts.Client().Get(ts.URL + "/targets/compare?a=v1&b=v2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(b))
	}
	body := readBody(t, resp.Body)
	// v1 has Alpha, v2 has Beta → one add + one remove.
	if !strings.Contains(body, "Alpha") || !strings.Contains(body, "Beta") {
		t.Errorf("compare body missing Alpha or Beta:\n%s", body)
	}
	if !strings.Contains(body, "diff-group added") || !strings.Contains(body, "diff-group removed") {
		t.Errorf("compare body missing color-coded groups:\n%s", body)
	}
}

func TestTargetsCompare_MissingParamsReturnsFragmentBanner(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/targets/compare?a=v1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200 (banner fragment)", resp.StatusCode)
	}
	body := readBody(t, resp.Body)
	if !strings.Contains(body, "both a and b must be provided") {
		t.Errorf("missing error banner:\n%s", body)
	}
}

func TestTargetsCompare_UnknownTargetReturnsBanner(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/targets/compare?a=nope&b=alsonope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp.Body)
	if !strings.Contains(body, "not found") {
		t.Errorf("missing not-found message:\n%s", body)
	}
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// Guard: make sure the new routes don't silently break the 404 contract
// for completely unknown paths under /targets.
func TestTargets_UnknownSubrouteReturns404OrMethodMismatch(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/targets/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// net/http's mux will fall through to / (which 404s unknown paths).
	// Either 404 or a redirect is acceptable as long as it's not a 200.
	if resp.StatusCode == 200 {
		t.Errorf("unknown /targets/* returned 200; expected 404")
	}
}

// Sanity: ensure buildKindFilters + buildGroups handle a huge random
// diff without panicking. This doubles as a smoke test for the filter
// map allocations.
func TestBuildGroups_LargeDiffNoPanic(t *testing.T) {
	d := &diff.Diff{}
	for i := 0; i < 500; i++ {
		d.Changes = append(d.Changes, diff.Change{
			Op:   diff.OpChange,
			Kind: diff.KindFunction,
			Path: "p.F",
		})
	}
	_ = buildKindFilters(d, "")
	_ = buildGroups(d, "")
}

// Guard against slow renders — the diff template has to execute in
// well under a second for realistic inputs.
func TestDiff_RendersQuickly(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	resp, err := ts.Client().Get(ts.URL + "/diff")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("diff render took %v, expected < 2s", d)
	}
}
