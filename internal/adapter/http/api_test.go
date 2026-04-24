package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/mcp"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
)

// newAPITestServer builds a Server rooted at a tiny Go module with two
// packages so every API endpoint has something to return.
func newAPITestServer(t *testing.T) (*httptest.Server, *serve.State, string) {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module api.test\n\ngo 1.21\n")
	mustWriteFile(t, filepath.Join(root, "alpha", "alpha.go"), `package alpha

type Service interface{ Do() }
type Impl struct{}
func New() *Impl { return &Impl{} }
`)
	mustWriteFile(t, filepath.Join(root, "beta", "beta.go"), `package beta

func Hello() string { return "hi" }
`)
	state := serve.NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("load state: %v", err)
	}
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, state, root
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAPI_ListPackages_ReturnsSummaries(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/mcp/packages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	var summaries []mcp.PackageSummary
	if err := json.Unmarshal(body, &summaries); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, body)
	}
	names := map[string]bool{}
	for _, s := range summaries {
		names[s.Path] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha+beta, got %v", names)
	}
}

func TestAPI_GetPackage_ReturnsPackageModel(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/mcp/packages/beta")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d — body=%s", resp.StatusCode, body)
	}

	var pkg domain.PackageModel
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, body)
	}
	if pkg.Name != "beta" {
		t.Errorf("Name=%q, want beta", pkg.Name)
	}
}

func TestAPI_GetPackage_Unknown_ReturnsError(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/mcp/packages/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Errorf("error message missing: %s", body)
	}
}

func TestAPI_Extract_FilteredByPath(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/mcp/extract?path=alpha")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d body=%s", resp.StatusCode, body)
	}

	var pkgs []domain.PackageModel
	if err := json.Unmarshal(body, &pkgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Path != "alpha" {
		t.Errorf("expected only alpha, got %v", pkgs)
	}
}

func TestAPI_ListTargets_Empty(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/mcp/targets")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d body=%s", resp.StatusCode, body)
	}
	// Empty list is "[]" — the MCP tool emits an empty slice when no
	// targets exist.
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("expected []; got %s", body)
	}
}

func TestAPI_TargetsLock_ThenCurrent_ThenValidate(t *testing.T) {
	ts, state, root := newAPITestServer(t)

	// 1. POST /api/mcp/targets/lock — freeze the current model as "base".
	lockBody := bytes.NewBufferString(`{"id":"base","description":"test"}`)
	resp, err := nethttp.Post(ts.URL+"/api/mcp/targets/lock", "application/json", lockBody)
	if err != nil {
		t.Fatalf("POST lock: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("lock status: %d body=%s", resp.StatusCode, body)
	}

	// 2. POST /api/mcp/targets/current — activate base.
	curBody := bytes.NewBufferString(`{"id":"base"}`)
	resp2, err := nethttp.Post(ts.URL+"/api/mcp/targets/current", "application/json", curBody)
	if err != nil {
		t.Fatalf("POST current: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("current status: %d body=%s", resp2.StatusCode, b)
	}

	// Snapshot should reflect currentTarget.
	if state.Snapshot().CurrentTarget != "base" {
		t.Errorf("state.CurrentTarget=%q, want base", state.Snapshot().CurrentTarget)
	}

	// 3. POST /api/mcp/validate (no body) — expect ok=true because code and
	// target match (we locked right after loading).
	resp3, err := nethttp.Post(ts.URL+"/api/mcp/validate", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST validate: %v", err)
	}
	defer resp3.Body.Close()
	body3, _ := io.ReadAll(resp3.Body)
	if resp3.StatusCode != 200 {
		t.Fatalf("validate status: %d body=%s", resp3.StatusCode, body3)
	}
	var vr mcp.ValidateResult
	if err := json.Unmarshal(body3, &vr); err != nil {
		t.Fatalf("unmarshal validate: %v — body=%s", err, body3)
	}
	if !vr.OK {
		t.Errorf("validate.OK=false, want true — violations=%+v", vr.Violations)
	}
	if vr.Target != "base" {
		t.Errorf("validate.Target=%q, want base", vr.Target)
	}

	// Sanity: a target directory exists on disk.
	if _, err := os.Stat(filepath.Join(root, ".arch", "targets", "base")); err != nil {
		t.Errorf("target dir missing: %v", err)
	}
}

func TestAPI_Diff_NoCurrentTarget_ReturnsError(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := nethttp.Get(ts.URL + "/api/mcp/diff")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "no target") {
		t.Errorf("expected 'no target' hint, got %s", body)
	}
}

func TestAPI_ToolsCall_GenericPassthrough(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	req := `{"name":"list_packages","arguments":{}}`
	resp, err := nethttp.Post(ts.URL+"/api/mcp/tools/call", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d body=%s", resp.StatusCode, body)
	}

	var tr mcp.ToolResult
	if err := json.Unmarshal(body, &tr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tr.IsError {
		t.Errorf("unexpected IsError=true: %+v", tr)
	}
	if len(tr.Content) == 0 {
		t.Fatal("empty content")
	}
	if !strings.Contains(tr.Content[0].Text, "alpha") {
		t.Errorf("expected alpha in payload: %s", tr.Content[0].Text)
	}
}

func TestAPI_ToolsCall_UnknownTool(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	req := `{"name":"does_not_exist","arguments":{}}`
	resp, err := nethttp.Post(ts.URL+"/api/mcp/tools/call", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}
