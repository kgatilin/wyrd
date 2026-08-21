package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/uigraph"
	"github.com/kgatilin/wyrd/internal/serve"
)

func TestUIGraphAPI_SingleModeReturnsLiveGraph(t *testing.T) {
	ts, _, root := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/uigraph")
	if err != nil {
		t.Fatalf("GET /api/uigraph: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}

	var graph uigraph.UIGraph
	if err := json.Unmarshal(body, &graph); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if graph.Schema != uigraph.Schema {
		t.Fatalf("schema = %q, want %q", graph.Schema, uigraph.Schema)
	}
	if graph.Repo == nil || graph.Repo.Root != root {
		t.Fatalf("repo = %+v, want root %q", graph.Repo, root)
	}
	if len(graph.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(graph.Components))
	}
	if len(graph.ReviewScopes) == 0 || len(graph.ReviewViews) == 0 {
		t.Fatalf("review metadata missing: scopes=%d views=%d", len(graph.ReviewScopes), len(graph.ReviewViews))
	}
}

// TestUIGraphAPI_MultiModeDiffsAgainstMergeBase pins the semantics the file
// diff has always had and the model diff used to lack: a branch is reviewed
// against the commit it forked from, not against wherever the base worktree
// has got to. Without it, everything that landed on main after the branch
// point is reported as this branch *removing* it — a confident, backwards
// diff that made the two review surfaces contradict each other.
func TestUIGraphAPI_MultiModeDiffsAgainstMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("WYRD_HOME", t.TempDir()) // materialized base trees stay in the sandbox

	parent := t.TempDir()
	mainWT := filepath.Join(parent, "alpha")
	if err := os.MkdirAll(filepath.Join(mainWT, "api"), 0o755); err != nil {
		t.Fatalf("mkdir main worktree: %v", err)
	}
	mustWriteFile(t, filepath.Join(mainWT, "go.mod"), "module example.com/review\n\ngo 1.21\n")
	mustWriteFile(t, filepath.Join(mainWT, "api", "api.go"), `package api

type Existing struct {
	Name string
}
`)
	gitRun(t, mainWT, "init", "-q", "-b", "main")
	gitRun(t, mainWT, "add", ".")
	gitRun(t, mainWT, "commit", "-qm", "init")

	featureWT := filepath.Join(parent, "beta")
	gitRun(t, mainWT, "worktree", "add", "-q", "-b", "feat/review", featureWT)
	mustWriteFile(t, filepath.Join(featureWT, "api", "api.go"), `package api

type Existing struct {
	Name string
}

type Feature struct {
	Enabled bool
}
`)
	gitRun(t, featureWT, "add", ".")
	gitRun(t, featureWT, "commit", "-qm", "feature")

	// main moves on after the branch point.
	mustWriteFile(t, filepath.Join(mainWT, "api", "later.go"), `package api

type LandedOnMainLater struct {
	Field string
}
`)
	gitRun(t, mainWT, "add", ".")
	gitRun(t, mainWT, "commit", "-qm", "main moves on")

	loader := func(ctx context.Context, _, path string) (*serve.State, error) {
		state := serve.NewState(path)
		if err := state.Load(ctx); err != nil {
			return nil, err
		}
		return state, nil
	}
	multi := serve.NewMultiState(mainWT, loader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	srv, err := NewMultiServer(multi)
	if err != nil {
		t.Fatalf("NewMultiServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/w/beta/api/uigraph")
	if err != nil {
		t.Fatalf("GET /w/beta/api/uigraph: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var graph uigraph.UIGraph
	if err := json.Unmarshal(body, &graph); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}

	if graph.Repo == nil || graph.Repo.BaseRev == "" {
		t.Fatalf("repo does not report the base revision: %+v", graph.Repo)
	}
	mainHead := gitOutput(t, mainWT, "rev-parse", "HEAD")
	if graph.Repo.BaseRev == mainHead {
		t.Errorf("BaseRev = main's tip %s; the diff is still two-dot", mainHead)
	}

	apiComp := findUIGraphComponent(graph.Components, "api")
	if apiComp == nil {
		t.Fatalf("api component missing: %+v", graph.Components)
	}
	for _, internal := range apiComp.Internals {
		if internal.Name == "LandedOnMainLater" {
			t.Fatalf("a type added on main after the branch point appears in the branch diff as %q", internal.Diff)
		}
		if internal.Name == "Feature" && internal.Diff != "added" {
			t.Errorf("Feature diff = %q, want added", internal.Diff)
		}
	}
	if graph.PR != nil && graph.PR.Stats.Removed != 0 {
		t.Errorf("PR stats report %d removals; the branch removed nothing", graph.PR.Stats.Removed)
	}
}

func TestUIGraphAPI_MultiModeComparesWorktreeAgainstMain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	parent := t.TempDir()
	mainWT := filepath.Join(parent, "alpha")
	if err := os.MkdirAll(filepath.Join(mainWT, "api"), 0o755); err != nil {
		t.Fatalf("mkdir main worktree: %v", err)
	}
	mustWriteFile(t, filepath.Join(mainWT, "go.mod"), "module example.com/review\n\ngo 1.21\n")
	mustWriteFile(t, filepath.Join(mainWT, "api", "api.go"), `package api

type Existing struct {
	Name string
}
`)
	gitRun(t, mainWT, "init", "-q", "-b", "main")
	gitRun(t, mainWT, "add", ".")
	gitRun(t, mainWT, "commit", "-qm", "init")

	featureWT := filepath.Join(parent, "beta")
	gitRun(t, mainWT, "worktree", "add", "-b", "feat/review", featureWT)
	mustWriteFile(t, filepath.Join(featureWT, "api", "api.go"), `package api

type Existing struct {
	Name string
}

type Feature struct {
	Enabled bool
}
`)

	loader := func(ctx context.Context, _, path string) (*serve.State, error) {
		state := serve.NewState(path)
		if err := state.Load(ctx); err != nil {
			return nil, err
		}
		return state, nil
	}
	multi := serve.NewMultiState(mainWT, loader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	srv, err := NewMultiServer(multi)
	if err != nil {
		t.Fatalf("NewMultiServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/w/beta/api/uigraph")
	if err != nil {
		t.Fatalf("GET /w/beta/api/uigraph: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}

	var graph uigraph.UIGraph
	if err := json.Unmarshal(body, &graph); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if graph.Repo == nil {
		t.Fatalf("repo missing")
	}
	if graph.Repo.ActiveWorktree != "beta" || graph.Repo.BaseWorktree != "alpha" {
		t.Fatalf("repo = %+v, want beta vs alpha", graph.Repo)
	}
	if graph.PR == nil || graph.PR.Stats.Added == 0 {
		t.Fatalf("expected added diff in PR, got %+v", graph.PR)
	}
	if len(graph.Worktrees) != 2 {
		t.Fatalf("worktrees = %d, want 2", len(graph.Worktrees))
	}

	apiComp := findUIGraphComponent(graph.Components, "api")
	if apiComp == nil {
		t.Fatalf("api component missing: %+v", graph.Components)
	}
	foundFeature := false
	for _, internal := range apiComp.Internals {
		if internal.Name == "Feature" {
			foundFeature = true
			if internal.Diff != "added" {
				t.Fatalf("Feature diff = %q, want added", internal.Diff)
			}
		}
	}
	if !foundFeature {
		t.Fatalf("Feature internal missing: %+v", apiComp.Internals)
	}
}

func findUIGraphComponent(components []uigraph.Component, id string) *uigraph.Component {
	for i := range components {
		if components[i].ID == id {
			return &components[i]
		}
	}
	return nil
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
