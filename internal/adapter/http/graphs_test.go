package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/target"
)

// --- unit tests for graph builders --------------------------------------

func TestBuildLayerGraph_FullViewIncludesPackagesAndEdges(t *testing.T) {
	cfg := sampleOverlay()
	pkgs := []domain.PackageModel{
		{
			Path: "internal/service",
			Dependencies: []domain.Dependency{
				{To: domain.SymbolRef{Package: "example.com/app/internal/domain", Symbol: "Thing"}},
			},
		},
		{Path: "internal/domain"},
		{Path: "internal/adapter/yaml"},
	}
	g := buildLayerGraph(cfg, pkgs, false)
	if g.Meta.View != "layer-map" {
		t.Errorf("view = %q, want layer-map", g.Meta.View)
	}
	if g.Meta.Layout != "elk" {
		t.Errorf("layout = %q, want elk", g.Meta.Layout)
	}
	// Every layer appears as a compound node.
	var haveLayers, havePkgs int
	for _, n := range g.Nodes {
		switch n.Kind {
		case "layer":
			haveLayers++
		case "package":
			havePkgs++
			if n.Parent == "" {
				t.Errorf("package node %q missing parent", n.ID)
			}
		}
	}
	if haveLayers != 3 {
		t.Errorf("haveLayers = %d, want 3", haveLayers)
	}
	if havePkgs != 3 {
		t.Errorf("havePkgs = %d, want 3", havePkgs)
	}
	// Exactly one allowed edge (service -> domain).
	var allowed, violations int
	for _, e := range g.Edges {
		switch e.Kind {
		case "allowed":
			allowed++
		case "violation":
			violations++
		}
	}
	if allowed != 1 {
		t.Errorf("allowed edges = %d, want 1", allowed)
	}
	if violations != 0 {
		t.Errorf("violation edges = %d, want 0 (none forbidden in fixture)", violations)
	}
}

func TestBuildLayerGraph_MiniStripsPackagesAndViolations(t *testing.T) {
	cfg := sampleOverlay()
	pkgs := []domain.PackageModel{
		{
			Path: "internal/domain",
			Dependencies: []domain.Dependency{
				// domain->service is a violation in sampleOverlay.
				{To: domain.SymbolRef{Package: "example.com/app/internal/service", Symbol: "Svc"}},
			},
		},
		{Path: "internal/service"},
	}
	g := buildLayerGraph(cfg, pkgs, true)
	if g.Meta.View != "layer-map-mini" {
		t.Errorf("mini view = %q", g.Meta.View)
	}
	for _, n := range g.Nodes {
		if n.Kind == "package" {
			t.Errorf("mini view should not include package children: %+v", n)
		}
	}
	for _, e := range g.Edges {
		if e.Kind == "violation" || e.Kind == "declared" {
			t.Errorf("mini view should exclude %s edges: %+v", e.Kind, e)
		}
	}
}

func TestBuildLayerGraph_NilOverlayReturnsEmpty(t *testing.T) {
	g := buildLayerGraph(nil, nil, false)
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Errorf("expected empty graph, got %+v", g)
	}
}

func TestBuildPackageOverviewGraph_IncludesTypesAndInternalDeps(t *testing.T) {
	foo := domain.PackageModel{
		Path: "internal/foo",
		Name: "foo",
		Interfaces: []domain.InterfaceDef{
			{Name: "Greeter", IsExported: true},
		},
		Structs: []domain.StructDef{
			{Name: "Hello", IsExported: true},
		},
		Functions: []domain.FunctionDef{
			{Name: "New", IsExported: true},
		},
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/bar", Symbol: "Bar"}},
		},
	}
	bar := domain.PackageModel{
		Path: "internal/bar",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/foo", Symbol: "Hello"}},
		},
	}
	g := buildPackageOverviewGraph(foo, []domain.PackageModel{foo, bar})
	if g.Meta.View != "package-overview" {
		t.Errorf("view = %q", g.Meta.View)
	}
	ids := nodeIDs(g)
	for _, want := range []string{
		"pkg:internal/foo",
		"type:internal/foo.Greeter",
		"type:internal/foo.Hello",
		"fn:internal/foo.New",
		"pkg:internal/bar",
	} {
		if !ids[want] {
			t.Errorf("missing node %q in %v", want, ids)
		}
	}
	// Must have an outbound edge foo->bar and inbound bar->foo.
	var out, in int
	for _, e := range g.Edges {
		if e.Source == "pkg:internal/foo" && e.Target == "pkg:internal/bar" {
			out++
		}
		if e.Source == "pkg:internal/bar" && e.Target == "pkg:internal/foo" {
			in++
		}
	}
	if out == 0 {
		t.Error("missing outbound edge foo -> bar")
	}
	if in == 0 {
		t.Error("missing inbound edge bar -> foo")
	}
}

func TestBuildDiffGraph_ColoursByOp(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "internal/foo.F"},
		{Op: diff.OpRemove, Kind: diff.KindStruct, Path: "internal/foo.S"},
		{Op: diff.OpChange, Kind: diff.KindFunction, Path: "internal/bar.G"},
	}}
	g := buildDiffGraph(d, "")
	if g.Meta.View != "diff-overlay" {
		t.Errorf("view = %q", g.Meta.View)
	}
	ops := make(map[string]string)
	for _, n := range g.Nodes {
		if n.Op != "" {
			ops[n.ID] = n.Op
		}
	}
	if ops["function:internal/foo.F"] != "add" {
		t.Errorf("foo.F op = %q, want add", ops["function:internal/foo.F"])
	}
	if ops["struct:internal/foo.S"] != "remove" {
		t.Errorf("foo.S op = %q, want remove", ops["struct:internal/foo.S"])
	}
	if ops["function:internal/bar.G"] != "change" {
		t.Errorf("bar.G op = %q, want change", ops["function:internal/bar.G"])
	}
	// Parents should be materialized.
	var foo, bar bool
	for _, n := range g.Nodes {
		switch n.ID {
		case "pkg:internal/foo":
			foo = true
		case "pkg:internal/bar":
			bar = true
		}
	}
	if !foo || !bar {
		t.Errorf("missing parent package nodes foo=%v bar=%v", foo, bar)
	}
}

func TestBuildDiffGraph_KindFilter(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "internal/foo.F"},
		{Op: diff.OpRemove, Kind: diff.KindStruct, Path: "internal/foo.S"},
	}}
	g := buildDiffGraph(d, "function")
	for _, n := range g.Nodes {
		if n.Kind == "struct" {
			t.Errorf("filter leaked struct node: %+v", n)
		}
	}
}

func TestDiffParentID_CoversPathsWithSlashes(t *testing.T) {
	cases := map[string]string{
		"internal/foo.Bar": "pkg:internal/foo",
		"foo.Bar":          "pkg:foo",
		"a/b/c.Type.Field": "pkg:a/b/c",
		"only-package":     "",
		"":                 "",
	}
	for in, want := range cases {
		if got := diffParentID(in); got != want {
			t.Errorf("diffParentID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderDiffD2_ProducesCompilableSource(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "internal/foo.F"},
		{Op: diff.OpRemove, Kind: diff.KindStruct, Path: "internal/foo.S"},
	}}
	src := renderDiffD2(d, "")
	if !strings.Contains(src, "Diff overlay") {
		t.Errorf("d2 source missing title: %s", src)
	}
	// We don't re-run renderD2 here (it's already covered by
	// layers_test); just guard against empty / missing nodes.
	if !strings.Contains(src, "internal/foo.F") {
		t.Errorf("d2 missing change node: %s", src)
	}
}

// --- handler tests -------------------------------------------------------

func TestAPILayers_ReturnsJSONPayload(t *testing.T) {
	ts, _ := newFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/layers")
	if err != nil {
		t.Fatalf("GET /api/layers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(b))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json*", ct)
	}
	var p graphPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Meta.View != "layer-map" {
		t.Errorf("meta.view = %q, want layer-map", p.Meta.View)
	}
	if len(p.Nodes) == 0 {
		t.Errorf("expected at least one node, got 0")
	}
}

func TestAPILayersMini_ReturnsMiniPayload(t *testing.T) {
	ts, _ := newFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/layers/mini")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var p graphPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Meta.View != "layer-map-mini" {
		t.Errorf("meta.view = %q, want layer-map-mini", p.Meta.View)
	}
	for _, n := range p.Nodes {
		if n.Kind == "package" {
			t.Errorf("mini payload should not include package children: %+v", n)
		}
	}
}

func TestAPIPackageGraph_ReturnsPackageOverview(t *testing.T) {
	fx := newPackagesTestServer(t)

	resp, err := fx.ts.Client().Get(fx.ts.URL + "/api/packages/internal/foo/graph")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(b))
	}
	var p graphPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Meta.View != "package-overview" {
		t.Errorf("meta.view = %q", p.Meta.View)
	}
	ids := nodeIDs(p)
	if !ids["pkg:internal/foo"] {
		t.Errorf("missing subject package node in %v", ids)
	}
	if !ids["type:internal/foo.Thing"] {
		t.Errorf("missing exported struct node in %v", ids)
	}
}

func TestAPIPackageGraph_UnknownPackageReturns404(t *testing.T) {
	fx := newPackagesTestServer(t)

	resp, err := fx.ts.Client().Get(fx.ts.URL + "/api/packages/does/not/exist/graph")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAPIPackageGraph_MissingGraphSuffixReturns404(t *testing.T) {
	fx := newPackagesTestServer(t)

	resp, err := fx.ts.Client().Get(fx.ts.URL + "/api/packages/internal/foo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAPIDiff_NoTargetReturnsEmptyPayload(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/api/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var p graphPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Meta.View != "diff-overlay" {
		t.Errorf("view = %q", p.Meta.View)
	}
	if len(p.Nodes) != 0 {
		t.Errorf("expected empty payload, got %d nodes", len(p.Nodes))
	}
}

func TestAPIDiff_WithActiveTarget_ReturnsChanges(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Get(ts.URL + "/api/diff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(b))
	}
	var p graphPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Nodes) == 0 {
		t.Errorf("expected non-empty payload for active target diff")
	}
}

// --- export endpoint tests ----------------------------------------------

func TestViewLayersD2_ReturnsAttachment(t *testing.T) {
	ts, _ := newFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/view/layers/d2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "layers.d2") {
		t.Errorf("Content-Disposition = %q, want layers.d2 attachment", cd)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("empty body")
	}
}

func TestViewLayersSVG_ReturnsSVG(t *testing.T) {
	ts, _ := newFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/view/layers/svg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/svg") {
		t.Errorf("Content-Type = %q, want image/svg*", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<svg") {
		t.Errorf("body missing <svg: %s", string(body))
	}
}

func TestViewLayersD2_NoOverlayReturns404(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + "/view/layers/d2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestViewPackage_D2AndSVG(t *testing.T) {
	fx := newPackagesTestServer(t)

	// D2 export.
	resp, err := fx.ts.Client().Get(fx.ts.URL + "/view/packages/internal/foo/d2")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("d2 status = %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	resp.Body.Close()
	if !strings.Contains(cd, "internal_foo") {
		t.Errorf("d2 filename encoding lost slashes: %q", cd)
	}

	// SVG export.
	resp, err = fx.ts.Client().Get(fx.ts.URL + "/view/packages/internal/foo/svg")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("svg status = %d body=%s", resp.StatusCode, string(b))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<svg") {
		t.Errorf("svg body missing <svg tag")
	}
}

func TestViewPackage_UnknownFormatReturns404(t *testing.T) {
	fx := newPackagesTestServer(t)
	resp, err := fx.ts.Client().Get(fx.ts.URL + "/view/packages/internal/foo/png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestViewDiffD2_NoTargetReturns400(t *testing.T) {
	ts, _, _ := newDiffTargetsServer(t)
	resp, err := ts.Client().Get(ts.URL + "/view/diff/d2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestViewDiffD2_WithTargetReturnsSource(t *testing.T) {
	ts, root, state := newDiffTargetsServer(t)
	seedTarget(t, root, "v1", "Alpha")
	if err := target.Use(root, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := state.SwitchTarget("v1"); err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Get(ts.URL + "/view/diff/d2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Diff overlay") {
		t.Errorf("diff d2 missing title: %s", body)
	}
}

// --- helpers -------------------------------------------------------------

// nodeIDs collects the set of node ids from a graph payload.
func nodeIDs(g graphPayload) map[string]bool {
	out := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		out[n.ID] = true
	}
	return out
}
