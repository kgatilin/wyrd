package http

import (
	"context"
	"encoding/json"
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

// sampleBCOverlay returns an overlay with two bounded contexts sharing an
// aggregate, for use in unit tests.
func sampleBCOverlay() *overlay.Config {
	return &overlay.Config{
		Module: "example.com/app",
		Aggregates: map[string]overlay.Aggregate{
			"core":  {Root: "example.com/app/internal/domain.Order"},
			"svc":   {Root: "example.com/app/internal/service.Service"},
			"infra": {Root: "example.com/app/internal/adapter.Adapter"},
		},
		BoundedContexts: map[string]overlay.BoundedContext{
			"core_ctx": {
				Description: "Core domain",
				Aggregates:  []string{"core", "svc"},
			},
			"infra_ctx": {
				Description: "Infrastructure",
				Aggregates:  []string{"infra"},
				Upstream:    []string{"core_ctx"},
			},
		},
	}
}

// sampleBCPackages returns packages that match the globs in sampleBCOverlay.
func sampleBCPackages() []domain.PackageModel {
	return []domain.PackageModel{
		{Path: "internal/domain/order", Aggregate: "core"},
		{Path: "internal/service/order", Aggregate: "svc"},
		{Path: "internal/adapter/yaml", Aggregate: "infra"},
		{Path: "internal/adapter/http", Aggregate: "infra"},
	}
}

// --- unit tests for buildBCGraph -----------------------------------------

func TestBuildBCGraph_NodesAndEdges(t *testing.T) {
	cfg := sampleBCOverlay()
	payload := buildBCGraph(cfg, false)
	if payload.Meta.View != "bc-map" {
		t.Errorf("meta.view = %q, want bc-map", payload.Meta.View)
	}
	if payload.Meta.Layout != "elk" {
		t.Errorf("meta.layout = %q, want elk", payload.Meta.Layout)
	}

	// Both BCs must appear as nodes.
	nodeIDs := map[string]bool{}
	for _, n := range payload.Nodes {
		nodeIDs[n.ID] = true
		if n.Kind != "bc" {
			t.Errorf("node %q: want kind=bc, got %q", n.ID, n.Kind)
		}
		// Label must NOT contain a newline — name and description
		// are exposed as separate fields so the front-end can lay
		// them out without clipping (#81).
		if strings.Contains(n.Label, "\n") {
			t.Errorf("node %q: label contains newline (%q); name and description must be separate fields", n.ID, n.Label)
		}
	}
	for _, want := range []string{"bc:core_ctx", "bc:infra_ctx"} {
		if !nodeIDs[want] {
			t.Errorf("missing node %q; nodes: %v", want, nodeIDs)
		}
	}

	// Description must be carried on the node, not concatenated into
	// the label.
	for _, n := range payload.Nodes {
		switch n.ID {
		case "bc:core_ctx":
			if n.Label != "core_ctx" {
				t.Errorf("core_ctx label = %q, want %q", n.Label, "core_ctx")
			}
			if n.Description != "Core domain" {
				t.Errorf("core_ctx description = %q, want %q", n.Description, "Core domain")
			}
		case "bc:infra_ctx":
			if n.Label != "infra_ctx" {
				t.Errorf("infra_ctx label = %q, want %q", n.Label, "infra_ctx")
			}
			if n.Description != "Infrastructure" {
				t.Errorf("infra_ctx description = %q, want %q", n.Description, "Infrastructure")
			}
		}
	}

	// Must have exactly one upstream edge: core_ctx -> infra_ctx.
	if len(payload.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(payload.Edges), payload.Edges)
	}
	e := payload.Edges[0]
	if e.Source != "bc:core_ctx" || e.Target != "bc:infra_ctx" {
		t.Errorf("edge: got %s -> %s, want bc:core_ctx -> bc:infra_ctx", e.Source, e.Target)
	}
	if e.Kind != "upstream" {
		t.Errorf("edge kind = %q, want upstream", e.Kind)
	}
}

func TestBuildBCGraph_NoDuplicateEdges(t *testing.T) {
	// Give both BCs mutual upstream/downstream declarations and verify
	// the deduplication logic only emits one edge.
	cfg := &overlay.Config{
		Module: "example.com/app",
		BoundedContexts: map[string]overlay.BoundedContext{
			"a": {Upstream: []string{"b"}},
			"b": {Upstream: []string{"a"}},
		},
	}
	payload := buildBCGraph(cfg, false)
	if len(payload.Edges) > 2 {
		t.Errorf("expected at most 2 edges (one per direction), got %d", len(payload.Edges))
	}
}

// TestBuildBCGraph_DownstreamOnlyEdges verifies that an edge is
// derived from a Downstream declaration even when no Upstream
// counterpart exists. A.Downstream contains B  ->  A -> B.
func TestBuildBCGraph_DownstreamOnlyEdges(t *testing.T) {
	cfg := &overlay.Config{
		Module: "example.com/app",
		BoundedContexts: map[string]overlay.BoundedContext{
			"a": {Downstream: []string{"b"}},
			"b": {},
		},
	}
	payload := buildBCGraph(cfg, false)
	if len(payload.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(payload.Edges), payload.Edges)
	}
	e := payload.Edges[0]
	if e.Source != "bc:a" || e.Target != "bc:b" {
		t.Errorf("edge: got %s -> %s, want bc:a -> bc:b", e.Source, e.Target)
	}
}

// TestBuildBCGraph_MixedUpstreamDownstreamDedup verifies that when
// both sides declare the same edge symmetrically (A.Downstream=[B]
// AND B.Upstream=[A]), only one edge is emitted (same relationship).
func TestBuildBCGraph_MixedUpstreamDownstreamDedup(t *testing.T) {
	cfg := &overlay.Config{
		Module: "example.com/app",
		BoundedContexts: map[string]overlay.BoundedContext{
			"a": {Downstream: []string{"b"}, Relationship: "customer-supplier"},
			"b": {Upstream: []string{"a"}, Relationship: "customer-supplier"},
		},
	}
	payload := buildBCGraph(cfg, false)
	if len(payload.Edges) != 1 {
		t.Fatalf("expected 1 edge after dedup, got %d: %+v", len(payload.Edges), payload.Edges)
	}
	e := payload.Edges[0]
	if e.Source != "bc:a" || e.Target != "bc:b" {
		t.Errorf("edge: got %s -> %s, want bc:a -> bc:b", e.Source, e.Target)
	}
	if e.Relationship != "customer-supplier" {
		t.Errorf("edge.relationship = %q, want customer-supplier", e.Relationship)
	}
}

// TestBuildBCGraph_RelationshipQualifierPreserved verifies that the
// relationship qualifier of the BC declaring the link is attached
// to the edge.
func TestBuildBCGraph_RelationshipQualifierPreserved(t *testing.T) {
	// Each edge has exactly one declarer — no symmetrical declarations
	// — so the relationship attached to each edge is unambiguous.
	cfg := &overlay.Config{
		Module: "example.com/app",
		BoundedContexts: map[string]overlay.BoundedContext{
			"core":   {Downstream: []string{"infra"}, Relationship: "open-host"},
			"infra":  {Relationship: "conformist"},
			"shared": {Downstream: []string{"core"}, Relationship: "shared-kernel"},
		},
	}
	payload := buildBCGraph(cfg, false)

	// Build a lookup of relationships per (source, target).
	rels := map[string]string{}
	for _, e := range payload.Edges {
		rels[e.Source+"->"+e.Target] = e.Relationship
	}

	if got := rels["bc:core->bc:infra"]; got != "open-host" {
		t.Errorf("core->infra relationship = %q, want open-host", got)
	}
	if got := rels["bc:shared->bc:core"]; got != "shared-kernel" {
		t.Errorf("shared->core relationship = %q, want shared-kernel", got)
	}
}

// TestBuildBCGraph_MiniViewTag verifies that mini=true flips the
// view tag to "bc-map-mini".
func TestBuildBCGraph_MiniViewTag(t *testing.T) {
	cfg := sampleBCOverlay()
	payload := buildBCGraph(cfg, true)
	if payload.Meta.View != "bc-map-mini" {
		t.Errorf("meta.view = %q, want bc-map-mini", payload.Meta.View)
	}
}

// --- unit tests for buildBCSummaries -------------------------------------

func TestBuildBCSummaries_CountsAndLinks(t *testing.T) {
	cfg := sampleBCOverlay()
	pkgs := sampleBCPackages()
	summaries := buildBCSummaries(cfg.BoundedContexts, pkgs)

	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	// Sorted alphabetically: core_ctx, infra_ctx.
	if summaries[0].Name != "core_ctx" || summaries[1].Name != "infra_ctx" {
		t.Errorf("unexpected order: %v, %v", summaries[0].Name, summaries[1].Name)
	}

	byName := map[string]bcSummaryView{}
	for _, s := range summaries {
		byName[s.Name] = s
	}

	core := byName["core_ctx"]
	if core.AggCount != 2 {
		t.Errorf("core_ctx: want 2 aggs, got %d", core.AggCount)
	}
	if core.PkgCount != 2 {
		t.Errorf("core_ctx: want 2 pkgs, got %d", core.PkgCount)
	}
	if core.Href != "/bc/core_ctx" {
		t.Errorf("core_ctx href = %q, want /bc/core_ctx", core.Href)
	}

	infra := byName["infra_ctx"]
	if infra.PkgCount != 2 {
		t.Errorf("infra_ctx: want 2 pkgs, got %d", infra.PkgCount)
	}
}

// --- unit tests for packagesInBC -----------------------------------------

func TestPackagesInBC_FiltersCorrectly(t *testing.T) {
	pkgs := sampleBCPackages()
	got := packagesInBC("core_ctx", []string{"core", "svc"}, pkgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 packages, got %d: %+v", len(got), got)
	}
	paths := map[string]bool{}
	for _, p := range got {
		paths[p.Path] = true
	}
	for _, want := range []string{"internal/domain/order", "internal/service/order"} {
		if !paths[want] {
			t.Errorf("missing package %q", want)
		}
	}
}

func TestPackagesInBC_EmptyAggregatesReturnsNil(t *testing.T) {
	pkgs := sampleBCPackages()
	got := packagesInBC("none", []string{}, pkgs)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestPackagesInBC_SortedByPath(t *testing.T) {
	pkgs := []domain.PackageModel{
		{Path: "z/pkg", Aggregate: "a"},
		{Path: "a/pkg", Aggregate: "a"},
		{Path: "m/pkg", Aggregate: "a"},
	}
	got := packagesInBC("bc", []string{"a"}, pkgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(got))
	}
	if got[0].Path != "a/pkg" || got[1].Path != "m/pkg" || got[2].Path != "z/pkg" {
		t.Errorf("unexpected order: %v", got)
	}
}

// --- HTTP handler tests --------------------------------------------------

// newBCFixtureServer builds an httptest.Server with an overlay that
// includes a bounded_contexts block so the /bc routes have data.
func newBCFixtureServer(t *testing.T) (*httptest.Server, *serve.State) {
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
aggregates:
  core:
    root: "example.com/fixture/internal/domain.Thing"
bounded_contexts:
  core_ctx:
    description: "Core domain"
    aggregates:
      - core
  secondary_ctx:
    description: "Secondary"
    aggregates: []
    upstream:
      - core_ctx
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

func TestAPIBCGraph_ReturnsJSONPayload(t *testing.T) {
	ts, _ := newBCFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/bc/graph")
	if err != nil {
		t.Fatalf("GET /api/bc/graph: %v", err)
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
	if p.Meta.View != "bc-map" {
		t.Errorf("meta.view = %q, want bc-map", p.Meta.View)
	}
	if len(p.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes (one per BC), got %d", len(p.Nodes))
	}
}

func TestAPIBCGraphMini_ReturnsMiniViewTag(t *testing.T) {
	ts, _ := newBCFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/bc/mini")
	if err != nil {
		t.Fatalf("GET /api/bc/mini: %v", err)
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
	if p.Meta.View != "bc-map-mini" {
		t.Errorf("meta.view = %q, want bc-map-mini", p.Meta.View)
	}
	if len(p.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes (one per BC), got %d", len(p.Nodes))
	}
}

func TestAPIBCGraph_NoOverlayReturnsEmptyPayload(t *testing.T) {
	// newFixtureServer has no bounded_contexts block.
	ts, _ := newFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/bc/graph")
	if err != nil {
		t.Fatalf("GET /api/bc/graph: %v", err)
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
	if len(p.Nodes) != 0 {
		t.Errorf("expected 0 nodes for overlay without BCs, got %d", len(p.Nodes))
	}
	if len(p.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(p.Edges))
	}
}

func TestBCList_RendersPage(t *testing.T) {
	ts, _ := newBCFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/bc")
	if err != nil {
		t.Fatalf("GET /bc: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(b))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "core_ctx") {
		t.Errorf("expected BC name in response body; got %q", string(body)[:200])
	}
}

func TestBCDetail_RendersPage(t *testing.T) {
	ts, _ := newBCFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/bc/core_ctx")
	if err != nil {
		t.Fatalf("GET /bc/core_ctx: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, string(b))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "core_ctx") {
		t.Errorf("expected BC name in detail page; got body=%q", string(body)[:200])
	}
}

func TestBCDetail_NotFoundReturns404(t *testing.T) {
	ts, _ := newBCFixtureServer(t)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/bc/ghost_bc")
	if err != nil {
		t.Fatalf("GET /bc/ghost_bc: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
