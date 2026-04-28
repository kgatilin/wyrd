package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"strings"
	"testing"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
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
	var haveFullPackageLabel bool
	for _, n := range g.Nodes {
		switch n.Kind {
		case "layer":
			haveLayers++
		case "package":
			havePkgs++
			if n.Parent == "" {
				t.Errorf("package node %q missing parent", n.ID)
			}
			if n.ID == "pkg:internal/adapter/yaml" && n.Label == "internal/adapter/yaml" {
				haveFullPackageLabel = true
			}
		}
	}
	if haveLayers != 3 {
		t.Errorf("haveLayers = %d, want 3", haveLayers)
	}
	if havePkgs != 3 {
		t.Errorf("havePkgs = %d, want 3", havePkgs)
	}
	if !haveFullPackageLabel {
		t.Errorf("expected full package path label for internal/adapter/yaml")
	}
	// Exactly one allowed edge (service -> domain).
	var allowed, violations int
	var allowedLabel string
	for _, e := range g.Edges {
		switch e.Kind {
		case "allowed":
			allowed++
			allowedLabel = e.Label
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
	if allowedLabel != "allowed" {
		t.Errorf("allowed edge label = %q, want allowed", allowedLabel)
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
			{
				Name:       "Greeter",
				IsExported: true,
				Methods: []domain.MethodDef{
					{
						Name:       "Greet",
						IsExported: true,
						Params: []domain.ParamDef{
							{Name: "name", Type: domain.TypeRef{Name: "string"}},
						},
						Returns: []domain.TypeRef{{Name: "string"}},
					},
				},
			},
		},
		Structs: []domain.StructDef{
			{
				Name:       "Hello",
				IsExported: true,
				Fields: []domain.FieldDef{
					{Name: "Name", IsExported: true, Type: domain.TypeRef{Name: "string"}},
				},
				Methods: []domain.MethodDef{
					{
						Name:       "Say",
						IsExported: true,
						Returns:    []domain.TypeRef{{Name: "error"}},
					},
				},
			},
		},
		Functions: []domain.FunctionDef{
			{
				Name:       "New",
				IsExported: true,
				Params: []domain.ParamDef{
					{Name: "name", Type: domain.TypeRef{Name: "string"}},
				},
				Returns: []domain.TypeRef{{Name: "Hello", IsPointer: true}},
			},
		},
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{Package: "internal/foo", Symbol: "New"},
				To:   domain.SymbolRef{Package: "internal/foo", Symbol: "Hello"},
				Kind: domain.DependencyReturns, ThroughExported: true,
			},
			{
				From: domain.SymbolRef{Package: "internal/foo", Symbol: "Hello.Say"},
				To:   domain.SymbolRef{Package: "internal/foo", Symbol: "Greeter"},
				Kind: domain.DependencyUses, ThroughExported: true,
			},
			{
				From: domain.SymbolRef{Package: "internal/foo", Symbol: "New"},
				To:   domain.SymbolRef{Package: "internal/bar", Symbol: "Bar"},
				Kind: domain.DependencyReturns, ThroughExported: true,
			},
		},
	}
	bar := domain.PackageModel{
		Path: "internal/bar",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/foo", Symbol: "Hello"}},
		},
	}
	g := buildPackageOverviewGraph(foo, []domain.PackageModel{foo, bar}, d2adapter.OverviewModePublic)
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
	if !hasGraphEdge(g, "fn:internal/foo.New", "type:internal/foo.Hello", "returns") {
		t.Error("missing same-package New -> Hello return edge")
	}
	if !hasGraphEdge(g, "type:internal/foo.Hello", "type:internal/foo.Greeter", "uses") {
		t.Error("missing same-package Hello -> Greeter uses edge")
	}
	for id, want := range map[string][]string{
		"type:internal/foo.Greeter": {"Greeter", "interface", "methods:", "+ Greet(name: string): string"},
		"type:internal/foo.Hello":   {"Hello", "struct", "fields:", "+ Name: string", "methods:", "+ Say(): error"},
		"fn:internal/foo.New":       {"New", "constructor", "args:", "name: string", "returns:", "struct *Hello"},
	} {
		label := nodeLabel(g, id)
		for _, part := range want {
			if !strings.Contains(label, part) {
				t.Errorf("node %s label = %q, missing %q", id, label, part)
			}
		}
	}
}

// M9 (#61): Public mode hides unexported symbols, Full mode includes
// them. Factories tagged with the factory stereotype render as
// kind=entry-point so the front-end can highlight them.
func TestBuildPackageOverviewGraph_ModeFiltering(t *testing.T) {
	foo := domain.PackageModel{
		Path: "internal/foo",
		Name: "foo",
		Interfaces: []domain.InterfaceDef{
			{
				Name:       "PublicAPI",
				IsExported: true,
				Methods: []domain.MethodDef{
					{Name: "Serve", IsExported: true},
					{Name: "debug", IsExported: false},
				},
			},
			{Name: "internalIface", IsExported: false},
		},
		Structs: []domain.StructDef{
			{
				Name:       "Public",
				IsExported: true,
				Fields: []domain.FieldDef{
					{Name: "Name", IsExported: true, Type: domain.TypeRef{Name: "string"}},
					{Name: "secret", IsExported: false, Type: domain.TypeRef{Name: "string"}},
				},
			},
			{Name: "private", IsExported: false},
		},
		Functions: []domain.FunctionDef{
			{
				Name:       "NewService",
				IsExported: true,
				Params: []domain.ParamDef{
					{Name: "cfg", Type: domain.TypeRef{Name: "Config"}},
				},
				Returns:    []domain.TypeRef{{Name: "Service", IsPointer: true}},
				Stereotype: domain.StereotypeFactory,
			},
			{Name: "internalFn", IsExported: false},
		},
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{Package: "internal/foo", Symbol: "NewService"},
				To:   domain.SymbolRef{Package: "internal/foo", Symbol: "PublicAPI"},
				Kind: domain.DependencyUses, ThroughExported: false,
			},
		},
	}

	t.Run("public mode omits unexported", func(t *testing.T) {
		g := buildPackageOverviewGraph(foo, []domain.PackageModel{foo}, d2adapter.OverviewModePublic)
		ids := nodeIDs(g)
		for _, want := range []string{
			"type:internal/foo.PublicAPI",
			"type:internal/foo.Public",
			"fn:internal/foo.NewService",
		} {
			if !ids[want] {
				t.Errorf("missing exported node %q", want)
			}
		}
		for _, banned := range []string{
			"type:internal/foo.internalIface",
			"type:internal/foo.private",
			"fn:internal/foo.internalFn",
		} {
			if ids[banned] {
				t.Errorf("public mode unexpectedly includes %q", banned)
			}
		}
		// Mode must be reflected in meta for client-side reasoning.
		if g.Meta.Mode != string(d2adapter.OverviewModePublic) {
			t.Errorf("meta.mode = %q, want %q", g.Meta.Mode, d2adapter.OverviewModePublic)
		}
		// Factory function is tagged as an entry point.
		var found bool
		for _, n := range g.Nodes {
			if n.ID == "fn:internal/foo.NewService" && n.Kind == "entry-point" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected NewService to render as kind=entry-point")
		}
		if label := nodeLabel(g, "type:internal/foo.PublicAPI"); !strings.Contains(label, "+ Serve()") || strings.Contains(label, "debug") {
			t.Errorf("public interface label = %q, want exported methods only", label)
		}
		if label := nodeLabel(g, "type:internal/foo.Public"); !strings.Contains(label, "+ Name: string") || strings.Contains(label, "secret") {
			t.Errorf("public struct label = %q, want exported fields only", label)
		}
		if label := nodeLabel(g, "fn:internal/foo.NewService"); !strings.Contains(label, "constructor") || !strings.Contains(label, "cfg: Config") || !strings.Contains(label, "returns:") || !strings.Contains(label, "*Service") {
			t.Errorf("factory label = %q, want constructor params and return", label)
		}
		if hasGraphEdge(g, "fn:internal/foo.NewService", "type:internal/foo.PublicAPI", "uses") {
			t.Errorf("public graph leaked non-exported dependency edge")
		}
	})

	t.Run("full mode includes unexported", func(t *testing.T) {
		g := buildPackageOverviewGraph(foo, []domain.PackageModel{foo}, d2adapter.OverviewModeFull)
		ids := nodeIDs(g)
		for _, want := range []string{
			"type:internal/foo.PublicAPI",
			"type:internal/foo.internalIface",
			"type:internal/foo.private",
			"fn:internal/foo.NewService",
			"fn:internal/foo.internalFn",
		} {
			if !ids[want] {
				t.Errorf("full mode missing node %q", want)
			}
		}
		if g.Meta.Mode != string(d2adapter.OverviewModeFull) {
			t.Errorf("meta.mode = %q, want %q", g.Meta.Mode, d2adapter.OverviewModeFull)
		}
		if label := nodeLabel(g, "type:internal/foo.PublicAPI"); !strings.Contains(label, "- debug()") {
			t.Errorf("full interface label = %q, want unexported methods", label)
		}
		if label := nodeLabel(g, "type:internal/foo.Public"); !strings.Contains(label, "- secret: string") {
			t.Errorf("full struct label = %q, want unexported fields", label)
		}
		if !hasGraphEdge(g, "fn:internal/foo.NewService", "type:internal/foo.PublicAPI", "uses") {
			t.Errorf("full graph missing non-exported dependency edge between visible nodes")
		}
	})
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

func nodeLabel(g graphPayload, id string) string {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n.Label
		}
	}
	return ""
}

func hasGraphEdge(g graphPayload, source, target, kind string) bool {
	for _, e := range g.Edges {
		if e.Source == source && e.Target == target && e.Kind == kind {
			return true
		}
	}
	return false
}

// M9 (#61): per-package D2 source must respect mode and stay deterministic.
func TestD2SourceForPackage_ModeAndDeterminism(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Name: "svc",
		Interfaces: []domain.InterfaceDef{
			{Name: "Greeter", IsExported: true},
			{Name: "internalIface", IsExported: false},
		},
		Structs: []domain.StructDef{
			{Name: "Service", IsExported: true},
			{Name: "cache", IsExported: false},
		},
		Functions: []domain.FunctionDef{
			{Name: "NewService", IsExported: true, Stereotype: domain.StereotypeFactory},
			{Name: "Run", IsExported: true},
			{Name: "internalCalc", IsExported: false},
		},
	}

	pub := d2SourceForPackage(pkg, d2adapter.OverviewModePublic)
	full := d2SourceForPackage(pkg, d2adapter.OverviewModeFull)

	// Header reflects mode.
	if !strings.Contains(pub, "# mode: public") {
		t.Errorf("public output missing mode header: %s", pub)
	}
	if !strings.Contains(full, "# mode: full") {
		t.Errorf("full output missing mode header: %s", full)
	}

	// Public hides unexported, full shows them.
	for _, banned := range []string{"internalIface", "cache", "internalCalc"} {
		if strings.Contains(pub, banned) {
			t.Errorf("public mode includes unexported symbol %q", banned)
		}
		if !strings.Contains(full, banned) {
			t.Errorf("full mode missing unexported symbol %q", banned)
		}
	}

	// Entry-point stereotype on the factory in public mode.
	if !strings.Contains(pub, "<<entry-point>>") {
		t.Errorf("public mode missing entry-point stereotype: %s", pub)
	}

	// Determinism: identical inputs produce identical output across runs.
	pub2 := d2SourceForPackage(pkg, d2adapter.OverviewModePublic)
	if pub != pub2 {
		t.Error("public-mode d2 output is non-deterministic across calls")
	}
	full2 := d2SourceForPackage(pkg, d2adapter.OverviewModeFull)
	if full != full2 {
		t.Error("full-mode d2 output is non-deterministic across calls")
	}
}

// --- #89 dedicated package dependency graph -----------------------------

// depsFixture builds a small project where:
//
//	internal/foo  -> internal/bar (project)
//	internal/foo  -> internal/bar (duplicate, must dedup)
//	internal/foo  -> github.com/lib/pq (external)
//	internal/baz  -> internal/foo (inbound)
//	internal/qux  -> internal/foo (inbound)
//
// allowing each mode to be exercised against a known set of edges.
func depsFixture() (foo domain.PackageModel, all []domain.PackageModel) {
	foo = domain.PackageModel{
		Path: "internal/foo",
		Name: "foo",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/bar", Symbol: "Thing"}},
			{To: domain.SymbolRef{Package: "internal/bar", Symbol: "Other"}},
			{To: domain.SymbolRef{Package: "github.com/lib/pq", Symbol: "Open", External: true}},
		},
	}
	bar := domain.PackageModel{Path: "internal/bar"}
	baz := domain.PackageModel{
		Path: "internal/baz",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/foo", Symbol: "New"}},
			{To: domain.SymbolRef{Package: "internal/foo", Symbol: "Hello"}},
		},
	}
	qux := domain.PackageModel{
		Path: "internal/qux",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/foo", Symbol: "Hello"}},
		},
	}
	return foo, []domain.PackageModel{foo, bar, baz, qux}
}

func TestBuildPackageDepsGraph_BothModeIncludesInboundOutbound(t *testing.T) {
	foo, all := depsFixture()
	g := buildPackageDepsGraph(foo, all, depsModeBoth)

	if g.Meta.View != "package-deps" {
		t.Errorf("view = %q, want package-deps", g.Meta.View)
	}
	if g.Meta.Mode != depsModeBoth {
		t.Errorf("mode = %q, want both", g.Meta.Mode)
	}

	ids := nodeIDs(g)
	for _, want := range []string{
		"pkg:internal/foo", // root, preserves pkg: prefix for onTap
		"pkg:internal/bar", // outbound peer
		"pkg:internal/baz", // inbound peer
		"pkg:internal/qux", // inbound peer
	} {
		if !ids[want] {
			t.Errorf("missing node %q in %v", want, ids)
		}
	}
	// Externals must NOT appear as nodes.
	for id := range ids {
		if strings.Contains(id, "github.com/lib/pq") {
			t.Errorf("external dep leaked into graph: %q", id)
		}
	}

	// Edges: 1 outbound (foo->bar), 2 inbound (baz->foo, qux->foo) — and
	// the duplicate foo->bar dependency must dedup to a single edge.
	var out, in int
	var fooBarEdges int
	for _, e := range g.Edges {
		switch e.Kind {
		case "outbound":
			out++
			if e.Source == "pkg:internal/foo" && e.Target == "pkg:internal/bar" {
				fooBarEdges++
			}
		case "inbound":
			in++
		}
	}
	if out != 1 {
		t.Errorf("outbound edges = %d, want 1", out)
	}
	if fooBarEdges != 1 {
		t.Errorf("duplicate foo->bar deps must dedup: got %d edges", fooBarEdges)
	}
	if in != 2 {
		t.Errorf("inbound edges = %d, want 2", in)
	}
}

func TestBuildPackageDepsGraph_OutboundModeOmitsInbound(t *testing.T) {
	foo, all := depsFixture()
	g := buildPackageDepsGraph(foo, all, depsModeOutbound)

	if g.Meta.Mode != depsModeOutbound {
		t.Errorf("mode = %q", g.Meta.Mode)
	}
	for _, e := range g.Edges {
		if e.Kind == "inbound" {
			t.Errorf("outbound mode leaked inbound edge: %+v", e)
		}
	}
	ids := nodeIDs(g)
	for _, banned := range []string{"pkg:internal/baz", "pkg:internal/qux"} {
		if ids[banned] {
			t.Errorf("outbound mode included inbound peer node %q", banned)
		}
	}
	if !ids["pkg:internal/bar"] {
		t.Error("outbound mode missing outbound peer pkg:internal/bar")
	}
}

func TestBuildPackageDepsGraph_InboundModeOmitsOutbound(t *testing.T) {
	foo, all := depsFixture()
	g := buildPackageDepsGraph(foo, all, depsModeInbound)

	if g.Meta.Mode != depsModeInbound {
		t.Errorf("mode = %q", g.Meta.Mode)
	}
	for _, e := range g.Edges {
		if e.Kind == "outbound" {
			t.Errorf("inbound mode leaked outbound edge: %+v", e)
		}
	}
	ids := nodeIDs(g)
	if ids["pkg:internal/bar"] {
		t.Error("inbound mode must not include outbound peer pkg:internal/bar")
	}
	for _, want := range []string{"pkg:internal/baz", "pkg:internal/qux"} {
		if !ids[want] {
			t.Errorf("inbound mode missing peer %q", want)
		}
	}
}

func TestBuildPackageDepsGraph_DefaultsToBothOnUnknownMode(t *testing.T) {
	foo, all := depsFixture()
	g := buildPackageDepsGraph(foo, all, "garbage")
	if g.Meta.Mode != depsModeBoth {
		t.Errorf("unknown mode should default to both, got %q", g.Meta.Mode)
	}
}

func TestBuildPackageDepsGraph_RootMarkedAndIDPreserved(t *testing.T) {
	foo, all := depsFixture()
	g := buildPackageDepsGraph(foo, all, depsModeBoth)
	var rootCount int
	for _, n := range g.Nodes {
		if n.Root {
			rootCount++
			if n.ID != "pkg:internal/foo" {
				t.Errorf("root id = %q, want pkg:internal/foo (preserves prefix for onTap)", n.ID)
			}
		}
	}
	if rootCount != 1 {
		t.Errorf("rootCount = %d, want 1", rootCount)
	}
}

func TestBuildPackageDepsGraph_Deterministic(t *testing.T) {
	foo, all := depsFixture()
	a := buildPackageDepsGraph(foo, all, depsModeBoth)
	b := buildPackageDepsGraph(foo, all, depsModeBoth)
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) != string(jb) {
		t.Errorf("non-deterministic output:\nA=%s\nB=%s", ja, jb)
	}
}

func TestBuildPackageDepsGraph_IgnoresUnknownInternalTargets(t *testing.T) {
	// A dep pointing at a project-shaped path that isn't in the
	// snapshot must be skipped rather than rendered as a phantom node.
	foo := domain.PackageModel{
		Path: "internal/foo",
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "internal/ghost", Symbol: "X"}},
		},
	}
	g := buildPackageDepsGraph(foo, []domain.PackageModel{foo}, depsModeBoth)
	for _, n := range g.Nodes {
		if n.ID == "pkg:internal/ghost" {
			t.Errorf("phantom node for unknown internal dep: %+v", n)
		}
	}
	if len(g.Edges) != 0 {
		t.Errorf("expected no edges for unknown target, got %+v", g.Edges)
	}
}

func TestExternalDepCount_DedupesAndIgnoresInternal(t *testing.T) {
	pkg := domain.PackageModel{
		Dependencies: []domain.Dependency{
			{To: domain.SymbolRef{Package: "github.com/lib/pq", Symbol: "Open", External: true}},
			{To: domain.SymbolRef{Package: "github.com/lib/pq", Symbol: "Close", External: true}},
			{To: domain.SymbolRef{Package: "encoding/json", Symbol: "Marshal", External: true}},
			{To: domain.SymbolRef{Package: "internal/bar", Symbol: "X"}},
		},
	}
	if got := externalDepCount(pkg); got != 2 {
		t.Errorf("externalDepCount = %d, want 2", got)
	}
}

func TestPackageDepsGraphHandler_ServesJSON(t *testing.T) {
	fx := newPackagesTestServer(t)
	// In the fixture, internal/bar imports internal/foo; querying foo
	// (the dependency target) should surface bar as an inbound peer.
	resp, err := fx.ts.Client().Get(fx.ts.URL + "/api/packages/internal/foo/deps/graph?mode=both")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}
	var g graphPayload
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.Meta.View != "package-deps" {
		t.Errorf("view = %q", g.Meta.View)
	}
	var sawInbound bool
	for _, e := range g.Edges {
		if e.Kind == "inbound" && e.Source == "pkg:internal/bar" && e.Target == "pkg:internal/foo" {
			sawInbound = true
		}
	}
	if !sawInbound {
		t.Errorf("expected inbound bar->foo edge, got %+v", g.Edges)
	}
}
