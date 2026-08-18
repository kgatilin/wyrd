package check

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// stubReader returns a fixed model, standing in for the Go source or the
// .arch/*.yaml spec reader depending on which slot it is wired into.
type stubReader struct {
	models []domain.PackageModel
	paths  []string
	err    error
}

func (s *stubReader) Read(_ context.Context, paths []string) ([]domain.PackageModel, error) {
	s.paths = paths
	return s.models, s.err
}

// writeProject lays out a minimal module with an archai.yaml overlay and
// returns the overlay and go.mod paths.
func writeProject(t *testing.T, module, overlayYAML string) (overlayPath, goModPath string) {
	t.Helper()
	dir := t.TempDir()
	overlayPath = filepath.Join(dir, "archai.yaml")
	goModPath = filepath.Join(dir, "go.mod")
	if err := os.WriteFile(overlayPath, []byte(overlayYAML), 0o644); err != nil {
		t.Fatalf("writing overlay: %v", err)
	}
	if err := os.WriteFile(goModPath, []byte("module "+module+"\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	return overlayPath, goModPath
}

const testModule = "example.com/proj"

const layeredOverlay = `module: example.com/proj
layers:
  domain:
    - internal/domain/...
  adapter:
    - internal/adapter/...
layer_rules:
  adapter:
    - domain
  domain: []
`

// layeredModels returns a domain package and an adapter package. When
// domainImportsAdapter is set, the domain package depends on the adapter —
// the inversion the layer rules forbid.
func layeredModels(domainImportsAdapter bool) []domain.PackageModel {
	// Path is module-relative; dependency refs carry the full module path,
	// the shape the Go reader produces.
	domainPkg := domain.PackageModel{Path: "internal/domain", Name: "domain"}
	adapterPkg := domain.PackageModel{
		Path: "internal/adapter",
		Name: "adapter",
		Dependencies: []domain.Dependency{{
			From: domain.SymbolRef{Package: testModule + "/internal/adapter", Symbol: "Reader"},
			To:   domain.SymbolRef{Package: testModule + "/internal/domain", Symbol: "Model"},
			Kind: domain.DependencyUses,
		}},
	}
	if domainImportsAdapter {
		domainPkg.Dependencies = []domain.Dependency{{
			From: domain.SymbolRef{Package: testModule + "/internal/domain", Symbol: "Model"},
			To:   domain.SymbolRef{Package: testModule + "/internal/adapter", Symbol: "Reader"},
			Kind: domain.DependencyUses,
		}}
	}
	return []domain.PackageModel{domainPkg, adapterPkg}
}

func TestRunOverlay_CleanModelReportsOK(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, layeredOverlay)
	source := &stubReader{models: layeredModels(false)}
	c := New(source, &stubReader{})

	var out, errOut bytes.Buffer
	if err := c.RunOverlay(context.Background(), OverlayOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
	}, &out, &errOut); err != nil {
		t.Fatalf("RunOverlay returned error: %v", err)
	}
	if !strings.Contains(out.String(), "OK: overlay is valid and no layer-rule violations found.") {
		t.Errorf("unexpected report:\n%s", out.String())
	}
	if got, want := source.paths, []string{"./..."}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("reader paths = %v, want %v", got, want)
	}
}

func TestRunOverlay_ViolationFailsAndNamesBothLayers(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, layeredOverlay)
	c := New(&stubReader{models: layeredModels(true)}, &stubReader{})

	var out, errOut bytes.Buffer
	err := c.RunOverlay(context.Background(), OverlayOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("RunOverlay succeeded; want a layer-rule failure")
	}
	if got, want := err.Error(), "1 layer-rule violation(s) found"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	want := "VIOLATION: package internal/domain (layer domain) imports package internal/adapter (layer adapter) — not allowed"
	if !strings.Contains(out.String(), want) {
		t.Errorf("report missing %q:\n%s", want, out.String())
	}
}

func TestRunOverlay_InvalidOverlayReportsDetailOnErrOut(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, `module: example.com/other
layers:
  domain:
    - internal/domain/...
`)
	source := &stubReader{models: layeredModels(false)}
	c := New(source, &stubReader{})

	var out, errOut bytes.Buffer
	err := c.RunOverlay(context.Background(), OverlayOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("RunOverlay succeeded; want an overlay-validation failure")
	}
	if got, want := err.Error(), "overlay validation failed"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if !strings.Contains(errOut.String(), "Overlay validation failed:") ||
		!strings.Contains(errOut.String(), "module mismatch") {
		t.Errorf("errOut missing validation detail:\n%s", errOut.String())
	}
	if source.paths != nil {
		t.Error("model was read despite an invalid overlay; the gate must fail first")
	}
}

func TestRunPolicy_NoPolicyBlockIsNotAFailure(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, layeredOverlay)
	c := New(&stubReader{models: layeredModels(false)}, &stubReader{})

	var out, errOut bytes.Buffer
	if err := c.RunPolicy(context.Background(), PolicyOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
	}, &out, &errOut); err != nil {
		t.Fatalf("RunPolicy returned error: %v", err)
	}
	if !strings.Contains(out.String(), "No policy defined in") {
		t.Errorf("unexpected report:\n%s", out.String())
	}
}

const policyOverlay = layeredOverlay + `policy:
  allow:
    - "internal/adapter/... -> internal/domain/..."
`

func TestRunPolicy_ForbiddenEdgeFailsWithJSONReport(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, policyOverlay)
	c := New(&stubReader{models: layeredModels(true)}, &stubReader{})

	var out, errOut bytes.Buffer
	err := c.RunPolicy(context.Background(), PolicyOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
		Format:      "json",
	}, &out, &errOut)
	if err == nil {
		t.Fatal("RunPolicy succeeded; want a dependency-policy failure")
	}

	var payload struct {
		OK         bool `json:"ok"`
		Count      int  `json:"count"`
		Violations []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, out.String())
	}
	if payload.OK || payload.Count == 0 {
		t.Errorf("payload = %+v, want ok=false with violations", payload)
	}
}

func TestRunPolicy_WarnKeepsExitCodeZero(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, policyOverlay)
	c := New(&stubReader{models: layeredModels(true)}, &stubReader{})

	var out, errOut bytes.Buffer
	if err := c.RunPolicy(context.Background(), PolicyOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
		Warn:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("RunPolicy with --warn returned error: %v", err)
	}
	if !strings.Contains(out.String(), "dependency-policy violation(s)") {
		t.Errorf("violations not reported under --warn:\n%s", out.String())
	}
}

func TestRunPolicy_RejectsUnknownFormat(t *testing.T) {
	c := New(&stubReader{}, &stubReader{})
	var out, errOut bytes.Buffer
	err := c.RunPolicy(context.Background(), PolicyOptions{Format: "xml"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "--format must be text or json") {
		t.Fatalf("error = %v, want a format complaint", err)
	}
}

func TestRunDrift_MatchingModelReportsMatch(t *testing.T) {
	root := t.TempDir()
	writeTargetModel(t, root, "baseline")
	specs := &stubReader{models: layeredModels(false)}
	c := New(&stubReader{models: layeredModels(false)}, specs)

	var out bytes.Buffer
	if err := c.RunDrift(context.Background(), DriftOptions{
		ProjectRoot: root,
		TargetID:    "baseline",
	}, &out); err != nil {
		t.Fatalf("RunDrift returned error: %v", err)
	}
	if !strings.Contains(out.String(), `code matches target "baseline"`) {
		t.Errorf("unexpected report:\n%s", out.String())
	}
}

func TestRunDrift_MissingTargetIsAnError(t *testing.T) {
	c := New(&stubReader{}, &stubReader{})
	var out bytes.Buffer
	err := c.RunDrift(context.Background(), DriftOptions{
		ProjectRoot: t.TempDir(),
		TargetID:    "nope",
	}, &out)
	if err == nil || !strings.Contains(err.Error(), `target "nope" not found`) {
		t.Fatalf("error = %v, want a missing-target failure", err)
	}
}

// writeTargetModel creates .arch/targets/<id>/model/ with one spec file so
// LoadTargetModel has something to read; the stub reader supplies the model.
func writeTargetModel(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, ".arch", "targets", id, "model")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.yaml"), []byte("path: internal/domain\n"), 0o644); err != nil {
		t.Fatalf("writing target model: %v", err)
	}
}

func TestResolveOverlay_AutoDetectsAdjacentGoMod(t *testing.T) {
	overlayPath, _ := writeProject(t, testModule, layeredOverlay)

	gotOverlay, gotGoMod := ResolveOverlay(overlayPath)
	if gotOverlay != overlayPath {
		t.Errorf("overlay = %q, want %q", gotOverlay, overlayPath)
	}
	if want := filepath.Join(filepath.Dir(overlayPath), "go.mod"); gotGoMod != want {
		t.Errorf("go.mod = %q, want %q", gotGoMod, want)
	}
}

func TestResolveOverlay_NoOverlayReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	if overlayPath, goModPath := ResolveOverlay(""); overlayPath != "" || goModPath != "" {
		t.Errorf("ResolveOverlay = (%q, %q), want empty", overlayPath, goModPath)
	}
}

func TestRunAll_ReadsTheModelOnceAndRunsBothGates(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, policyOverlay)
	source := &countingReader{stubReader: stubReader{models: layeredModels(true)}}
	c := New(source, &stubReader{})

	var out, errOut bytes.Buffer
	err := c.RunAll(context.Background(), AllOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("RunAll succeeded; want both gates to fail")
	}
	if got, want := err.Error(), "overlay and dependency-policy gates failed"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if source.reads != 1 {
		t.Errorf("model was read %d times, want 1 — the go/packages load is the expensive part", source.reads)
	}
	for _, want := range []string{"layer-rule violation(s)", "dependency-policy violation(s)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunAll_CleanModelPasses(t *testing.T) {
	overlayPath, goModPath := writeProject(t, testModule, policyOverlay)
	c := New(&stubReader{models: layeredModels(false)}, &stubReader{})

	var out, errOut bytes.Buffer
	if err := c.RunAll(context.Background(), AllOptions{
		OverlayPath: overlayPath,
		GoModPath:   goModPath,
	}, &out, &errOut); err != nil {
		t.Fatalf("RunAll returned error: %v\n%s", err, out.String())
	}
	for _, want := range []string{"no layer-rule violations found", "no dependency-policy violations"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
}

// countingReader records how many times a gate asked for the model.
type countingReader struct {
	stubReader
	reads int
}

func (c *countingReader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	c.reads++
	return c.stubReader.Read(ctx, paths)
}
