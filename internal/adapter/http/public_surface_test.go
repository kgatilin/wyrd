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

	"github.com/kgatilin/archai/internal/publicapi"
	"github.com/kgatilin/archai/internal/serve"
)

func TestPublicSurfaceAPI_SingleModeReturnsLivePublicSurface(t *testing.T) {
	ts, _, root := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/public-surface")
	if err != nil {
		t.Fatalf("GET /api/public-surface: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}

	var out publicSurfaceResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if out.Schema != publicSurfaceResponseSchema {
		t.Fatalf("Schema = %q, want %q", out.Schema, publicSurfaceResponseSchema)
	}
	if out.Repo.Root != root {
		t.Fatalf("Repo.Root = %q, want %q", out.Repo.Root, root)
	}
	if out.Surface.Schema != publicapi.Schema {
		t.Fatalf("Surface.Schema = %q, want %q", out.Surface.Schema, publicapi.Schema)
	}
	if out.Diff != nil {
		t.Fatalf("Diff = %+v, want nil in single mode", out.Diff)
	}
	if !publicSurfaceHasSymbol(out.Surface, "alpha.New") {
		t.Fatalf("surface missing alpha.New: %+v", out.Surface.Packages)
	}
	if publicSurfaceHasSymbol(out.Surface, "alpha.hidden") {
		t.Fatalf("surface included private symbol alpha.hidden")
	}
}

func TestPublicSurfaceAPI_MultiModeDiffsAgainstMain(t *testing.T) {
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

type Existing interface {
	Do() string
}
`)
	gitRun(t, mainWT, "init", "-q", "-b", "main")
	gitRun(t, mainWT, "add", ".")
	gitRun(t, mainWT, "commit", "-qm", "init")

	featureWT := filepath.Join(parent, "beta")
	gitRun(t, mainWT, "worktree", "add", "-b", "feat/review", featureWT)
	mustWriteFile(t, filepath.Join(featureWT, "api", "api.go"), `package api

type Existing interface {
	Do() int
}

func NewFeature() Existing { return nil }
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

	resp, err := nethttp.Get(ts.URL + "/w/beta/api/public-surface")
	if err != nil {
		t.Fatalf("GET /w/beta/api/public-surface: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}

	var out publicSurfaceResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if out.Repo.ActiveWorktree != "beta" || out.Repo.BaseWorktree != "alpha" {
		t.Fatalf("Repo = %+v, want beta vs alpha", out.Repo)
	}
	if out.Diff == nil {
		t.Fatalf("Diff missing")
	}
	if !publicSurfaceDiffHas(out.Diff, "symbol:function", "api.NewFeature", "added") {
		t.Fatalf("diff missing added api.NewFeature: %+v", out.Diff.Changes)
	}
	if !publicSurfaceDiffHas(out.Diff, "member:method", "api.Existing.Do", "changed") {
		t.Fatalf("diff missing changed api.Existing.Do: %+v", out.Diff.Changes)
	}
}

func publicSurfaceHasSymbol(surface publicapi.Surface, id string) bool {
	for _, pkg := range surface.Packages {
		for _, symbol := range pkg.Symbols {
			if symbol.ID == id {
				return true
			}
		}
	}
	return false
}

func publicSurfaceDiffHas(diff *publicapi.Diff, kind, id, op string) bool {
	for _, change := range diff.Changes {
		if change.Kind == kind && change.ID == id && change.Op == op {
			return true
		}
	}
	return false
}
