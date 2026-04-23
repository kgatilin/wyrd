package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
)

// loadFakeState creates a tiny Go module on disk with two packages
// and loads it into a serve.State. Slow-ish (calls go/packages) but
// it exercises the full integration.
func loadFakeState(t *testing.T) *serve.State {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module fake.test\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "alpha", "alpha.go"), `package alpha

type Service interface{ Do() }

type Impl struct{}

func New() *Impl { return &Impl{} }
`)
	mustWrite(t, filepath.Join(dir, "beta", "beta.go"), `package beta

type Thing struct{ Name string }

func Hello() string { return "hi" }
`)
	state := serve.NewState(dir)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("load state: %v", err)
	}
	return state
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestToolDefinitions(t *testing.T) {
	defs := ToolDefinitions()
	if len(defs) != 9 {
		t.Fatalf("expected 9 tool definitions, got %d", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
		if d.Description == "" {
			t.Errorf("tool %q missing description", d.Name)
		}
		if d.InputSchema == nil {
			t.Errorf("tool %q missing input schema", d.Name)
		}
	}
	for _, want := range []string{"extract", "list_packages", "get_package", "lock_target", "list_targets", "set_current_target", "diff", "apply_diff", "validate"} {
		if !names[want] {
			t.Errorf("missing tool definition for %q", want)
		}
	}
}

func TestDispatchUnknownTool(t *testing.T) {
	_, rpcErr := Dispatch(nil, "does_not_exist", nil)
	if rpcErr == nil {
		t.Fatal("expected RPC error for unknown tool")
	}
	if rpcErr.Code != ErrMethodNotFound {
		t.Errorf("want ErrMethodNotFound, got %d", rpcErr.Code)
	}
}

func TestExtract_EmptyStateReturnsEmptyArray(t *testing.T) {
	res, rpcErr := Dispatch(nil, "extract", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", res.Content)
	}
	var pkgs []domain.PackageModel
	if err := json.Unmarshal([]byte(res.Content[0].Text), &pkgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestExtract_ReturnsAllPackages(t *testing.T) {
	state := loadFakeState(t)
	res, rpcErr := Dispatch(state, "extract", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	var pkgs []domain.PackageModel
	if err := json.Unmarshal([]byte(res.Content[0].Text), &pkgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d: %+v", len(pkgs), pkgs)
	}
}

func TestExtract_FilterByPath(t *testing.T) {
	state := loadFakeState(t)
	args := json.RawMessage(`{"paths":["alpha"]}`)
	res, rpcErr := Dispatch(state, "extract", args)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	var pkgs []domain.PackageModel
	if err := json.Unmarshal([]byte(res.Content[0].Text), &pkgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Path != "alpha" {
		t.Fatalf("expected single 'alpha' package, got %+v", pkgs)
	}
}

func TestExtract_InvalidArguments(t *testing.T) {
	// paths should be an array; pass a string to trigger schema error.
	args := json.RawMessage(`{"paths":"alpha"}`)
	_, rpcErr := Dispatch(nil, "extract", args)
	if rpcErr == nil {
		t.Fatal("expected invalid-params error")
	}
	if rpcErr.Code != ErrInvalidParams {
		t.Errorf("want ErrInvalidParams, got %d", rpcErr.Code)
	}
}

func TestListPackages(t *testing.T) {
	state := loadFakeState(t)
	res, rpcErr := Dispatch(state, "list_packages", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	var summaries []PackageSummary
	if err := json.Unmarshal([]byte(res.Content[0].Text), &summaries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	byPath := map[string]PackageSummary{}
	for _, s := range summaries {
		byPath[s.Path] = s
	}
	alpha, ok := byPath["alpha"]
	if !ok {
		t.Fatalf("no alpha in summaries: %+v", summaries)
	}
	if alpha.InterfaceCount != 1 || alpha.StructCount != 1 || alpha.FunctionCount != 1 {
		t.Errorf("alpha counts wrong: %+v", alpha)
	}
}

func TestListPackages_EmptyStateReturnsEmptyArray(t *testing.T) {
	res, rpcErr := Dispatch(nil, "list_packages", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !strings.HasPrefix(strings.TrimSpace(res.Content[0].Text), "[") {
		t.Errorf("expected JSON array, got %q", res.Content[0].Text)
	}
}

func TestGetPackage_Found(t *testing.T) {
	state := loadFakeState(t)
	args := json.RawMessage(`{"path":"beta"}`)
	res, rpcErr := Dispatch(state, "get_package", args)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("expected non-error result, got %+v", res)
	}
	var pkg domain.PackageModel
	if err := json.Unmarshal([]byte(res.Content[0].Text), &pkg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pkg.Path != "beta" || pkg.Name != "beta" {
		t.Errorf("unexpected package: %+v", pkg)
	}
}

func TestGetPackage_NotFound(t *testing.T) {
	state := loadFakeState(t)
	args := json.RawMessage(`{"path":"gamma"}`)
	res, rpcErr := Dispatch(state, "get_package", args)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError result for unknown package")
	}
	if !strings.Contains(res.Content[0].Text, "gamma") {
		t.Errorf("expected error text to mention path; got %q", res.Content[0].Text)
	}
}

func TestGetPackage_MissingPath(t *testing.T) {
	res, rpcErr := Dispatch(nil, "get_package", json.RawMessage(`{}`))
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError result for missing path")
	}
}
