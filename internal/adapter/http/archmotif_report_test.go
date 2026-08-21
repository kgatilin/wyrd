package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/archreview"
	"github.com/kgatilin/wyrd/internal/serve"
)

func TestArchMotifReportAPIServesTheReport(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := ts.Client().Get(ts.URL + "/api/archmotif/report")
	if err != nil {
		t.Fatalf("GET /api/archmotif/report: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}

	var report archreview.Report
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, body)
	}
	if report.Schema != archreview.Schema {
		t.Errorf("schema = %q, want %q", report.Schema, archreview.Schema)
	}
	// Single-worktree mode has no review base, so the report answers the
	// repo-mode question instead of comparing against nothing.
	if report.Mode != archreview.ModeRepo {
		t.Errorf("mode = %q, want %q without a base", report.Mode, archreview.ModeRepo)
	}
	if len(report.Sections) == 0 {
		t.Fatal("no sections in the report")
	}
	if report.Totals.Packages == 0 {
		t.Errorf("totals = %+v, want the package count", report.Totals)
	}
}

func TestArchMotifReportAPIRejectsNonGET(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := ts.Client().Post(ts.URL+"/api/archmotif/report", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/archmotif/report: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func newReportTestServer(t *testing.T) (*httptest.Server, *serve.State, *Server) {
	t.Helper()
	ts, state, srv, _ := newAPIFixture(t)
	return ts, state, srv
}

// cachedReport returns the cache entry backing a key, so a test can tell one
// build from the next without reaching for wall-clock timing.
func cachedReport(srv *Server, key reportKey) *reportEntry {
	srv.reports.mu.Lock()
	defer srv.reports.mu.Unlock()
	return srv.reports.entries[key]
}

func getReport(t *testing.T, ts *httptest.Server, header string) {
	t.Helper()
	req, err := nethttp.NewRequest(nethttp.MethodGet, ts.URL+"/api/archmotif/report", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if header != "" {
		req.Header.Set("Cache-Control", header)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/archmotif/report: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
}

// The endpoint caches the report, so it has to be wired to the one signal the
// daemon has that the working tree moved. A cache that keeps answering across a
// model change would put the panel and the canvas on different working trees —
// the trap the file diff already documents.
func TestArchMotifReportRebuildsAfterAModelEvent(t *testing.T) {
	ts, state, srv := newReportTestServer(t)
	key := reportKey{baseRef: defaultReviewBaseRef}

	getReport(t, ts, "")
	first := cachedReport(srv, key)
	if first == nil {
		t.Fatal("the report was not cached, so nothing was saved for the next open")
	}

	state.PublishPackageReload([]string{"alpha"})

	deadline := time.Now().Add(5 * time.Second)
	for cachedReport(srv, key) == first {
		if time.Now().After(deadline) {
			t.Fatal("a model event left the cached report in place; the next read would be stale")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Refresh has to reach past the cache: the changes it exists for — an edited
// comment, a base branch that moved underneath — produce no model event at all,
// so answering it from the store would make the button a lie.
func TestArchMotifReportRebuildsWhenTheClientAsksForAFreshOne(t *testing.T) {
	ts, _, srv := newReportTestServer(t)
	key := reportKey{baseRef: defaultReviewBaseRef}

	getReport(t, ts, "")
	first := cachedReport(srv, key)

	getReport(t, ts, "")
	if cachedReport(srv, key) != first {
		t.Fatal("an ordinary read rebuilt the report instead of reading the cached one")
	}

	getReport(t, ts, "no-cache")
	if cachedReport(srv, key) == first {
		t.Error("Cache-Control: no-cache did not rebuild the report")
	}
}

// The report keys changed lines by module-relative path, which is the same key
// a symbol's span carries — otherwise no hunk would ever meet a symbol.
func TestChangedLinesKeysByModuleRelativePath(t *testing.T) {
	got := changedLines(git.Result{Files: []git.FileStat{
		{
			Path:  "internal/core/core.go",
			Patch: "@@ -1,2 +1,4 @@\n+added()\n+added()\n context\n",
		},
		{Path: "logo.png", Binary: true, Patch: "Binary files differ\n"},
	}})

	ranges, ok := got["internal/core/core.go"]
	if !ok {
		t.Fatalf("changedLines = %+v, want the module-relative path as key", got)
	}
	if len(ranges) != 1 || ranges[0].Start != 1 || ranges[0].End != 4 {
		t.Errorf("ranges = %+v, want the new-side hunk", ranges)
	}
	if _, ok := got["logo.png"]; ok {
		t.Error("a binary file has no line ranges and should not be listed")
	}
}
