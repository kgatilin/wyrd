package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/git"
	"github.com/kgatilin/archai/internal/archreview"
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
