package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	nethttptest "net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
)

func TestParseTypeID(t *testing.T) {
	cases := []struct {
		in      string
		wantPkg string
		wantNam string
		ok      bool
	}{
		{"internal/service.Service", "internal/service", "Service", true},
		{"internal/adapter/golang.Reader", "internal/adapter/golang", "Reader", true},
		{"fmt.Stringer", "fmt", "Stringer", true},
		{"noDot", "", "", false},
		{"", "", "", false},
		{"pkg.", "", "", false},
		{".Foo", "", "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseTypeID(tc.in)
			if ok != tc.ok {
				t.Fatalf("parseTypeID(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if !ok {
				return
			}
			if got.Package != tc.wantPkg || got.Name != tc.wantNam {
				t.Fatalf("parseTypeID(%q) = %+v, want {%q, %q}", tc.in, got, tc.wantPkg, tc.wantNam)
			}
		})
	}
}

func TestBuildTypePage_StructWithImplementsAndUsedBy(t *testing.T) {
	pkgs := []domain.PackageModel{
		{
			Path: "internal/service",
			Structs: []domain.StructDef{
				{
					Name:       "Service",
					IsExported: true,
					SourceFile: "service.go",
					Fields: []domain.FieldDef{
						{Name: "reader", Type: domain.TypeRef{Name: "ModelReader"}},
					},
					Methods: []domain.MethodDef{
						{Name: "Generate", IsExported: true, Returns: []domain.TypeRef{{Name: "error"}}},
					},
				},
			},
			Implementations: []domain.Implementation{
				{
					Concrete:  domain.SymbolRef{Package: "internal/service", Symbol: "Service"},
					Interface: domain.SymbolRef{Package: "internal/api", Symbol: "Generator"},
				},
			},
		},
		{
			Path: "cmd/archai",
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "cmd/archai", Symbol: "main"},
					To:   domain.SymbolRef{Package: "internal/service", Symbol: "Service"},
					Kind: domain.DependencyUses,
				},
			},
		},
	}
	ref := typeRef{Package: "internal/service", Name: "Service"}
	data, ok := buildTypePage(pkgs, ref)
	if !ok {
		t.Fatal("expected Service to be found")
	}
	if data.Kind != typeKindStruct {
		t.Fatalf("Kind = %q, want struct", data.Kind)
	}
	if len(data.Fields) != 1 || data.Fields[0].Name != "reader" {
		t.Fatalf("fields = %+v", data.Fields)
	}
	if len(data.Methods) != 1 || data.Methods[0].Name != "Generate" {
		t.Fatalf("methods = %+v", data.Methods)
	}
	if len(data.Implements) != 1 || data.Implements[0].Name != "Generator" {
		t.Fatalf("implements = %+v", data.Implements)
	}
	if len(data.UsedBy) != 1 || data.UsedBy[0].Package != "cmd/archai" {
		t.Fatalf("usedBy = %+v", data.UsedBy)
	}
	if data.UsedBy[0].Href != "/packages/cmd/archai" {
		t.Fatalf("usedBy href = %q", data.UsedBy[0].Href)
	}
	if data.PackageHref != "/packages/internal/service" {
		t.Fatalf("packageHref = %q", data.PackageHref)
	}
}

func TestBuildTypePage_InterfaceImplementedBy(t *testing.T) {
	pkgs := []domain.PackageModel{
		{
			Path: "internal/api",
			Interfaces: []domain.InterfaceDef{
				{Name: "Generator", IsExported: true, SourceFile: "api.go"},
			},
			Implementations: []domain.Implementation{
				{
					Concrete:  domain.SymbolRef{Package: "internal/service", Symbol: "Service"},
					Interface: domain.SymbolRef{Package: "internal/api", Symbol: "Generator"},
				},
			},
		},
	}
	ref := typeRef{Package: "internal/api", Name: "Generator"}
	data, ok := buildTypePage(pkgs, ref)
	if !ok {
		t.Fatal("expected Generator to be found")
	}
	if data.Kind != typeKindInterface {
		t.Fatalf("Kind = %q, want interface", data.Kind)
	}
	if len(data.ImplementedBy) != 1 || data.ImplementedBy[0].Name != "Service" {
		t.Fatalf("implementedBy = %+v", data.ImplementedBy)
	}
	if data.ImplementedBy[0].Href != "/types/internal/service.Service" {
		t.Fatalf("href = %q", data.ImplementedBy[0].Href)
	}
}

func TestBuildTypePage_MissingType(t *testing.T) {
	pkgs := []domain.PackageModel{{Path: "internal/foo"}}
	if _, ok := buildTypePage(pkgs, typeRef{Package: "internal/foo", Name: "Nope"}); ok {
		t.Fatal("expected missing type to return ok=false")
	}
}

func TestBuildRelationshipGraph_RootAndEdges(t *testing.T) {
	ref := typeRef{Package: "internal/service", Name: "Service"}
	impls := []relatedView{{Package: "internal/api", Name: "Generator", Href: "/types/internal/api.Generator"}}
	implBy := []relatedView{{Package: "internal/other", Name: "Other"}}
	usedBy := []usedByView{{Package: "cmd/archai", Count: 3}}

	g := buildRelationshipGraph(ref, impls, implBy, usedBy)
	if len(g.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	if g.Nodes[0].Root == false {
		t.Fatalf("root node not flagged: %+v", g.Nodes[0])
	}
	if len(g.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %d: %+v", len(g.Edges), g.Edges)
	}
}

func TestBuildRelationshipGraph_Empty(t *testing.T) {
	ref := typeRef{Package: "internal/service", Name: "Service"}
	g := buildRelationshipGraph(ref, nil, nil, nil)
	if len(g.Nodes) != 1 {
		t.Fatalf("want 1 root node, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Fatalf("want 0 edges, got %d", len(g.Edges))
	}
}

func TestHandleType_RoundTripAndSections(t *testing.T) {
	ts := newTypesFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/types/internal/svc.Widget")
	if err != nil {
		t.Fatalf("GET /types/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{
		"Widget",                 // type name
		"Fields",                 // fields section
		"Methods",                // methods section
		"Relationships",          // relationships section
		"Graph",                  // graph section
		`id="type-graph"`,        // cytoscape target
		`cytoscape.min.js`,       // vendored cytoscape script
		`/packages/internal/svc`, // package backlink
	} {
		if !strings.Contains(s, want) {
			t.Errorf("/types page missing %q: %s", want, truncate(s, 500))
		}
	}
}

func TestHandleType_NotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/types/internal/nope.Thing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleTypeGraph_JSON(t *testing.T) {
	ts := newTypesFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/types/internal/svc.Widget/graph")
	if err != nil {
		t.Fatalf("GET /api/types/.../graph: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var payload graphJSON
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Nodes) < 1 {
		t.Fatalf("expected at least the root node, got %+v", payload.Nodes)
	}
	if !payload.Nodes[0].Root {
		t.Errorf("first node should be flagged Root")
	}
}

// newTypesFixtureServer builds a minimal fixture with a struct that has
// fields and an exported method + a dependency from another package so
// the type detail page has something to render in every section.
func newTypesFixtureServer(t *testing.T) *nethttptest.Server {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"),
		"module example.com/typesfix\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(root, "internal", "svc", "widget.go"), `package svc

// Widget is a fixture type used by the /types handler test.
type Widget struct {
	Name string
	Size int
}

// Describe returns a short description.
func (w *Widget) Describe() string { return w.Name }
`)
	mustWrite(t, filepath.Join(root, "internal", "user", "user.go"), `package user

import svc "example.com/typesfix/internal/svc"

// Use references Widget so the search and used-by collectors have data.
func Use(w *svc.Widget) string { return w.Describe() }
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
	return nethttptest.NewServer(mux)
}

func TestBuildPackageSequenceEntries_PublicMode(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Functions: []domain.FunctionDef{
			{
				Name: "NewService", IsExported: true, Stereotype: domain.StereotypeFactory,
				Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Helper"}}},
			},
			{
				Name: "Helper", IsExported: true,
				Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "internal"}}},
			},
			{Name: "internal", IsExported: false},
		},
		Structs: []domain.StructDef{
			{
				Name: "Service", IsExported: true,
				Methods: []domain.MethodDef{
					{
						Name: "Run", IsExported: true,
						Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Helper"}}},
					},
					{Name: "step", IsExported: false},
				},
			},
			{Name: "hidden", IsExported: false, Methods: []domain.MethodDef{
				{Name: "Do", IsExported: true},
			}},
		},
	}
	got := buildPackageSequenceEntries([]domain.PackageModel{pkg}, pkg, "")
	if len(got) != 3 {
		t.Fatalf("public mode entries = %d, want 3: %+v", len(got), got)
	}
	if got[0].Label != "NewService" {
		t.Fatalf("constructor not first: %+v", got)
	}
	labels := []string{got[0].Label, got[1].Label, got[2].Label}
	wantSet := map[string]bool{"NewService": true, "Helper": true, "Service.Run": true}
	for _, l := range labels {
		if !wantSet[l] {
			t.Fatalf("unexpected label %q in %+v", l, labels)
		}
	}
	for _, entry := range got {
		if !strings.Contains(entry.D2, "shape: sequence_diagram") {
			t.Fatalf("%s missing D2 sequence source: %q", entry.Label, entry.D2)
		}
	}
}

func TestBuildPackageSequenceEntries_FullMode(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Functions: []domain.FunctionDef{
			{
				Name: "Helper", IsExported: true,
				Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "internal"}}},
			},
			{
				Name: "internal", IsExported: false,
				Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Helper"}}},
			},
		},
		Structs: []domain.StructDef{
			{Name: "hidden", IsExported: false, Methods: []domain.MethodDef{
				{
					Name: "Do", IsExported: true,
					Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "Helper"}}},
				},
				{
					Name: "step", IsExported: false,
					Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "internal"}}},
				},
			}},
		},
	}
	got := buildPackageSequenceEntries([]domain.PackageModel{pkg}, pkg, "full")
	if len(got) != 4 {
		t.Fatalf("full mode entries = %d, want 4: %+v", len(got), got)
	}
}

func TestBuildPackageSequenceEntries_Empty(t *testing.T) {
	pkg := domain.PackageModel{Path: "internal/svc"}
	if got := buildPackageSequenceEntries([]domain.PackageModel{pkg}, pkg, ""); len(got) != 0 {
		t.Fatalf("expected 0 entries, got %+v", got)
	}
}

func TestBuildPackageSequenceEntries_SkipsRootOnlyEntries(t *testing.T) {
	pkg := domain.PackageModel{
		Path: "internal/svc",
		Functions: []domain.FunctionDef{
			{
				Name: "Run", IsExported: true,
				Calls: []domain.CallEdge{
					{To: domain.SymbolRef{Package: "internal/svc", Symbol: "helper"}},
				},
			},
			{Name: "Bare", IsExported: true},
			{Name: "helper", IsExported: false},
		},
	}
	got := buildPackageSequenceEntries([]domain.PackageModel{pkg}, pkg, "")
	by := map[string]sequenceEntry{}
	for _, e := range got {
		by[e.Label] = e
	}
	if !by["Run"].HasM6 {
		t.Fatalf("Run should have HasM6=true: %+v", by["Run"])
	}
	if _, ok := by["Bare"]; ok {
		t.Fatalf("Bare should be skipped because it has no recorded calls: %+v", by["Bare"])
	}
	if !strings.Contains(by["Run"].D2, "svc.Run -> svc.helper: helper") {
		t.Fatalf("Run D2 does not look like a sequence edge: %q", by["Run"].D2)
	}
}

func TestSequenceLinkResolver_HrefFor(t *testing.T) {
	pkgs := []domain.PackageModel{
		{
			Path: "internal/svc",
			Structs: []domain.StructDef{
				{Name: "Service", IsExported: true},
			},
			Functions: []domain.FunctionDef{
				{Name: "Helper", IsExported: true},
			},
		},
	}
	r := newSequenceLinkResolver(pkgs)

	cases := []struct {
		name string
		ref  domain.SymbolRef
		want string
	}{
		{"method on known type", domain.SymbolRef{Package: "internal/svc", Symbol: "Service.Run"}, "/types/internal/svc.Service"},
		{"plain function", domain.SymbolRef{Package: "internal/svc", Symbol: "Helper"}, "/packages/internal/svc"},
		{"unknown type method", domain.SymbolRef{Package: "internal/svc", Symbol: "Other.Run"}, ""},
		{"unknown function", domain.SymbolRef{Package: "internal/svc", Symbol: "missing"}, ""},
		{"external symbol", domain.SymbolRef{Package: "fmt", Symbol: "Println", External: true}, ""},
		{"unloaded package", domain.SymbolRef{Package: "other/pkg", Symbol: "Foo"}, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := r.hrefFor(tc.ref); got != tc.want {
				t.Fatalf("hrefFor(%+v) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}

	// nil resolver tolerates calls.
	var nilR *sequenceLinkResolver
	if got := nilR.hrefFor(domain.SymbolRef{Package: "x", Symbol: "Y"}); got != "" {
		t.Fatalf("nil resolver hrefFor = %q, want empty", got)
	}
}

func TestSequenceNodeToGraph_PopulatesHref(t *testing.T) {
	pkgs := []domain.PackageModel{
		{
			Path:      "internal/svc",
			Structs:   []domain.StructDef{{Name: "Service", IsExported: true, Methods: []domain.MethodDef{{Name: "Run", IsExported: true, Calls: []domain.CallEdge{{To: domain.SymbolRef{Package: "internal/svc", Symbol: "helper"}}}}}}},
			Functions: []domain.FunctionDef{{Name: "helper"}},
		},
	}
	got := buildPackageSequenceEntries(pkgs, pkgs[0], "")
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	g := got[0].Graph
	if len(g.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes, got %+v", g.Nodes)
	}
	if g.Nodes[0].Href != "/types/internal/svc.Service" {
		t.Fatalf("root href = %q", g.Nodes[0].Href)
	}
	if g.Nodes[1].Href != "/packages/internal/svc" {
		t.Fatalf("child href = %q", g.Nodes[1].Href)
	}
}

func TestRenderSequenceSVGs_RendersD2Sequence(t *testing.T) {
	entries := []sequenceEntry{{
		Label: "Run",
		D2:    "shape: sequence_diagram\na -> b: call\n",
		HasM6: true,
	}}
	renderSequenceSVGs(context.Background(), entries)
	if entries[0].SVGError != "" {
		t.Fatalf("unexpected render error: %s", entries[0].SVGError)
	}
	if !strings.Contains(string(entries[0].SVG), "<svg") {
		t.Fatalf("rendered SVG missing <svg: %s", entries[0].SVG)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
