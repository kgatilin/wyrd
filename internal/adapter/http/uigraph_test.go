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
	"testing"

	"github.com/kgatilin/archai/internal/adapter/uigraph"
	"github.com/kgatilin/archai/internal/serve"
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
