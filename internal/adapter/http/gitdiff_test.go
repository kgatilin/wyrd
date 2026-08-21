package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/serve"
)

func TestGitDiffJSON_ReturnsChangedFiles(t *testing.T) {
	root := newGitDiffTestRepo(t)
	ts := newGitDiffTestServer(t, root)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/gitdiff?base=main")
	if err != nil {
		t.Fatalf("GET /api/gitdiff: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var payload gitDiffJSON
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Schema != gitDiffSchema {
		t.Errorf("Schema = %q, want %q", payload.Schema, gitDiffSchema)
	}
	if payload.Branch != "feature" || payload.BaseRef != "main" {
		t.Errorf("branch/base = %q/%q, want feature/main", payload.Branch, payload.BaseRef)
	}
	if payload.Stats.Files != len(payload.Files) || payload.Stats.Files == 0 {
		t.Fatalf("stats = %+v for %d files", payload.Stats, len(payload.Files))
	}

	var added *gitDiffFileJSON
	for i := range payload.Files {
		if payload.Files[i].Path == "internal/added.go" {
			added = &payload.Files[i]
		}
	}
	if added == nil {
		t.Fatalf("internal/added.go missing from %+v", payload.Files)
	}
	if added.Status != "A" || !strings.Contains(added.Patch, "func Added()") {
		t.Errorf("added file = %+v", *added)
	}
	if payload.Stats.Insertions < added.Insertions {
		t.Errorf("stats insertions %d < one file's %d", payload.Stats.Insertions, added.Insertions)
	}
}

// The base ref defaults to the same "main" the architecture diff uses, so a
// UI that omits ?base still gets the review diff rather than an error.
func TestGitDiffJSON_DefaultsToReviewBase(t *testing.T) {
	root := newGitDiffTestRepo(t)
	ts := newGitDiffTestServer(t, root)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/gitdiff")
	if err != nil {
		t.Fatalf("GET /api/gitdiff: %v", err)
	}
	defer resp.Body.Close()

	var payload gitDiffJSON
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.BaseRef != defaultReviewBaseRef {
		t.Errorf("BaseRef = %q, want %q", payload.BaseRef, defaultReviewBaseRef)
	}
	if len(payload.Files) == 0 {
		t.Error("no files reported against the default base")
	}
}

func TestGitDiffJSON_RejectsNonGET(t *testing.T) {
	ts := newGitDiffTestServer(t, newGitDiffTestRepo(t))
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL+"/api/gitdiff", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /api/gitdiff: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// newGitDiffTestRepo builds a repo on branch "feature" with one committed
// addition and one committed modification relative to "main".
func newGitDiffTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--initial-branch=main")
	write("base.go", "package base\n")
	git("add", ".")
	git("commit", "-m", "base")
	git("checkout", "-b", "feature")
	write("internal/added.go", "package internal\n\nfunc Added() {}\n")
	write("base.go", "package base\n\nvar Changed = 1\n")
	git("add", ".")
	git("commit", "-m", "feature")
	return root
}

func newGitDiffTestServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	srv, err := NewServer(serve.NewState(root))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	return httptest.NewServer(mux)
}
