package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/serve"
)

// sampleOverlay is the three-layer overlay shared by most layers_test
// cases (domain → service → adapter hexagonal style).
func sampleOverlay() *overlay.Config {
	return &overlay.Config{
		Module: "example.com/app",
		Layers: map[string][]string{
			"domain":  {"internal/domain/..."},
			"service": {"internal/service/..."},
			"adapter": {"internal/adapter/..."},
		},
		LayerRules: map[string][]string{
			"service": {"domain"},
			"adapter": {"domain", "service"},
			"domain":  {},
		},
	}
}

func TestComputePackageLayers_AssignsFromGlobs(t *testing.T) {
	cfg := sampleOverlay()
	pkgs := []domain.PackageModel{
		{Path: "internal/domain"},
		{Path: "internal/domain/order"},
		{Path: "internal/service"},
		{Path: "internal/adapter/yaml"},
		{Path: "tests/integration"},
	}
	got := computePackageLayers(cfg, pkgs)
	want := map[string]string{
		"internal/domain":       "domain",
		"internal/domain/order": "domain",
		"internal/service":      "service",
		"internal/adapter/yaml": "adapter",
	}
	for path, layer := range want {
		if got[path] != layer {
			t.Errorf("%s: got layer %q, want %q", path, got[path], layer)
		}
	}
	if _, ok := got["tests/integration"]; ok {
		t.Errorf("tests/integration should not be assigned a layer, got %q", got["tests/integration"])
	}
}

func TestBuildLayerEdges_SplitsAllowedAndViolations(t *testing.T) {
	cfg := sampleOverlay()
	pkgs := []domain.PackageModel{
		{
			Path: "internal/service",
			Dependencies: []domain.Dependency{
				// service -> domain: allowed.
				{To: domain.SymbolRef{Package: "example.com/app/internal/domain", Symbol: "Thing"}},
			},
		},
		{
			Path: "internal/domain",
			Dependencies: []domain.Dependency{
				// domain -> service: violation (domain has empty rules).
				{To: domain.SymbolRef{Package: "example.com/app/internal/service", Symbol: "Svc"}},
			},
		},
		{Path: "internal/adapter/yaml"},
	}

	violations, allowed, declared := buildLayerEdges(cfg, pkgs)

	if len(violations) != 1 {
		t.Fatalf("violations: got %d, want 1 (%+v)", len(violations), violations)
	}
	if violations[0].From != "domain" || violations[0].To != "service" {
		t.Errorf("violation edge: got %s -> %s, want domain -> service", violations[0].From, violations[0].To)
	}
	if violations[0].Color != "red" {
		t.Errorf("violation color: got %q, want red", violations[0].Color)
	}
	if len(violations[0].Details) != 1 || !strings.Contains(violations[0].Details[0], "internal/domain -> internal/service") {
		t.Errorf("violation details unexpected: %v", violations[0].Details)
	}

	if len(allowed) != 1 {
		t.Fatalf("allowed: got %d, want 1 (%+v)", len(allowed), allowed)
	}
	if allowed[0].From != "service" || allowed[0].To != "domain" {
		t.Errorf("allowed edge: got %s -> %s, want service -> domain", allowed[0].From, allowed[0].To)
	}
	if allowed[0].Color != "green" {
		t.Errorf("allowed color: got %q, want green", allowed[0].Color)
	}

	// adapter -> {domain, service} is declared but unused here.
	declaredSet := make(map[string]bool)
	for _, e := range declared {
		declaredSet[e.From+"->"+e.To] = true
	}
	for _, key := range []string{"adapter->domain", "adapter->service"} {
		if !declaredSet[key] {
			t.Errorf("missing declared edge %q in %v", key, declaredSet)
		}
	}
}

func TestBuildLayerEdges_SkipsExternalAndSelfDeps(t *testing.T) {
	cfg := sampleOverlay()
	pkgs := []domain.PackageModel{
		{
			Path: "internal/service",
			Dependencies: []domain.Dependency{
				{To: domain.SymbolRef{Package: "context", Symbol: "Context", External: true}},
				{To: domain.SymbolRef{Package: "example.com/app/internal/service", Symbol: "Self"}},
			},
		},
		{Path: "internal/domain"},
	}
	violations, allowed, _ := buildLayerEdges(cfg, pkgs)
	if len(violations) != 0 {
		t.Errorf("violations should be empty, got %+v", violations)
	}
	if len(allowed) != 0 {
		t.Errorf("allowed should be empty, got %+v", allowed)
	}
}

func TestBuildLayerMapD2_RendersNodesAndEdges(t *testing.T) {
	cfg := sampleOverlay()
	pkgs := []domain.PackageModel{
		{
			Path: "internal/service",
			Dependencies: []domain.Dependency{
				{To: domain.SymbolRef{Package: "example.com/app/internal/domain", Symbol: "Thing"}},
			},
		},
		{Path: "internal/domain"},
	}
	src := buildLayerMapD2(cfg, pkgs, true)

	// Every layer must appear as a node.
	for _, expect := range []string{"domain:", "service:", "adapter:"} {
		if !strings.Contains(src, expect) {
			t.Errorf("expected D2 source to contain %q, got:\n%s", expect, src)
		}
	}
	// The allowed green edge must be present.
	if !strings.Contains(src, "service -> domain") {
		t.Errorf("expected edge service -> domain in D2 source:\n%s", src)
	}
	// And the D2 source must actually render to SVG.
	svg, err := renderD2(context.Background(), src)
	if err != nil {
		t.Fatalf("renderD2: %v\nsource:\n%s", err, src)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Fatalf("rendered output missing <svg tag")
	}
}

func TestMatchLayerGlobs(t *testing.T) {
	cases := []struct {
		name  string
		globs []string
		path  string
		want  bool
	}{
		{"recursive match", []string{"internal/foo/..."}, "internal/foo/bar", true},
		{"recursive match self", []string{"internal/foo/..."}, "internal/foo", true},
		{"recursive miss", []string{"internal/foo/..."}, "internal/bar", false},
		{"single-wildcard match", []string{"cmd/*"}, "cmd/archai", true},
		{"single-wildcard no nested match", []string{"cmd/*"}, "cmd/archai/sub", false},
		{"exact match", []string{"internal/foo"}, "internal/foo", true},
		{"exact miss", []string{"internal/foo"}, "internal/bar", false},
		{"ellipsis root", []string{"..."}, "anything", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchLayerGlobs(tc.globs, tc.path)
			if got != tc.want {
				t.Errorf("matchLayerGlobs(%v, %q) = %v, want %v", tc.globs, tc.path, got, tc.want)
			}
		})
	}
}

func TestModuleRel(t *testing.T) {
	mod := "example.com/app"
	cases := map[string]string{
		"example.com/app":              "",
		"example.com/app/internal/foo": "internal/foo",
		"internal/foo":                 "internal/foo", // already relative
		"other.com/lib":                "other.com/lib",
	}
	for in, want := range cases {
		if got := moduleRel(mod, in); got != want {
			t.Errorf("moduleRel(%q, %q) = %q, want %q", mod, in, got, want)
		}
	}
}

// TestHandleLayers_NoOverlay confirms the page renders a no-overlay
// empty state when the state carries no archai.yaml.
func TestHandleLayers_NoOverlay(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/layers")
	if err != nil {
		t.Fatalf("GET /layers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "No overlay") {
		t.Errorf("expected empty-state message, got: %s", truncate(string(body), 400))
	}
}

// TestHandleLayers_WithOverlay boots a real Server backed by a fixture
// project tree so the full handler path (snapshot → build views → D2
// render) runs end-to-end.
func TestHandleLayers_WithOverlay(t *testing.T) {
	ts, _ := newFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/layers")
	if err != nil {
		t.Fatalf("GET /layers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	// Layer names, diagram and legend pills must all be present.
	for _, want := range []string{"domain", "Layer map", "allowed", "violation"} {
		if !strings.Contains(s, want) {
			t.Errorf("/layers body missing %q: %s", want, truncate(s, 400))
		}
	}
	// M8 (#46): the server-rendered SVG is gone — confirm the page now
	// carries the client-side cytoscape div and export toolbar.
	for _, want := range []string{
		`class="cy-graph layer-map"`,
		`data-api="/api/layers"`,
		`href="/view/layers/d2"`,
		`href="/view/layers/svg"`,
		`data-cy-action="fit"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("/layers body missing %q", want)
		}
	}
}

// newFixtureServer builds a tiny Go fixture project with an overlay,
// loads it into a serve.State, wires up a Server, and returns an
// httptest.Server. The caller must Close() the returned server.
func newFixtureServer(t *testing.T) (*httptest.Server, *serve.State) {
	t.Helper()
	root := t.TempDir()

	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	writeFile(filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.21\n")
	writeFile(filepath.Join(root, "internal", "domain", "thing.go"),
		"package domain\n\ntype Thing struct{}\n")
	writeFile(filepath.Join(root, "archai.yaml"), `module: example.com/fixture
layers:
  domain:
    - "internal/domain/..."
layer_rules:
  domain: []
`)

	state := serve.NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	return httptest.NewServer(mux), state
}
