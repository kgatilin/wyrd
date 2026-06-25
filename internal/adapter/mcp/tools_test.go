package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

func TestMain(m *testing.M) {
	// Disable retrieval in tests to avoid background goroutines
	// that interfere with temp directory cleanup.
	os.Setenv("ARCHAI_RETRIEVAL_DISABLE", "1")
	os.Exit(m.Run())
}

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
	// 11 original tools + 5 retrieval tools (search, search_graph, expand, get_node, refresh) + spectral_cluster + components + trophic_layers + semantic_cluster + file_hotspots + latent_domains + embedding_coverage + status
	if len(defs) != 24 {
		t.Fatalf("expected 24 tool definitions, got %d", len(defs))
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
	for _, want := range []string{"extract", "list_packages", "get_package", "lock_target", "list_targets", "set_current_target", "diff", "apply_diff", "validate", "list_bounded_contexts", "get_bounded_context", "trophic_layers", "file_hotspots"} {
		if !names[want] {
			t.Errorf("missing tool definition for %q", want)
		}
	}
}

func TestDispatch_TrophicLayers(t *testing.T) {
	state := loadFakeState(t)

	res, rpcErr := Dispatch(state, "trophic_layers", json.RawMessage(`{"package":"alpha"}`))
	if rpcErr != nil {
		t.Fatalf("dispatch error: %v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", res.Content[0].Text)
	}
	if len(res.Content) == 0 {
		t.Fatal("empty content")
	}

	var resp trophicLayersResponse
	if err := json.Unmarshal([]byte(res.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.EdgeKindsUsed) == 0 {
		t.Errorf("edge_kinds_used not echoed")
	}
	if resp.NodeCount == 0 {
		t.Errorf("node_count = 0, want > 0")
	}
	if resp.Coherence.Verdict == "" {
		t.Errorf("coherence verdict missing")
	}
}

func TestDispatch_FileHotspots(t *testing.T) {
	state := loadFakeState(t)

	res, rpcErr := Dispatch(state, "file_hotspots", json.RawMessage(`{"package":"alpha"}`))
	if rpcErr != nil {
		t.Fatalf("dispatch error: %v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", res.Content[0].Text)
	}

	var resp fileHotspotsResponse
	if err := json.Unmarshal([]byte(res.Content[0].Text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// alpha.go holds Service, Impl, New -> one file with 3 top-level decls.
	if resp.FileCount != 1 {
		t.Fatalf("file_count = %d, want 1", resp.FileCount)
	}
	if resp.MaxSymbols != 3 {
		t.Errorf("max_symbols = %d, want 3", resp.MaxSymbols)
	}
	if len(resp.Files) == 0 || resp.Files[0].SymbolCount != 3 {
		t.Errorf("top file = %+v, want symbol_count 3", resp.Files)
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

// loadFakeStateWithOverlay extends loadFakeState with a minimal archai.yaml
// so bounded-context tools have something to query.
func loadFakeStateWithOverlay(t *testing.T) *serve.State {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module fake.test\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "alpha", "alpha.go"), `package alpha
type Service interface{ Do() }
`)
	mustWrite(t, filepath.Join(dir, "beta", "beta.go"), `package beta
type Thing struct{ Name string }
`)
	const overlayYAML = `module: fake.test
aggregates:
  core:
    root: "fake.test/alpha.Service"
  infra:
    root: "fake.test/beta.Thing"
bounded_contexts:
  main:
    description: "Main context"
    aggregates:
      - core
      - infra
  secondary:
    description: "Secondary context"
    aggregates:
      - infra
    upstream:
      - main
`
	mustWrite(t, filepath.Join(dir, "archai.yaml"), overlayYAML)
	state := serve.NewState(dir)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("load state: %v", err)
	}
	return state
}

func TestListBoundedContexts_EmptyStateReturnsEmptyArray(t *testing.T) {
	res, rpcErr := Dispatch(nil, "list_bounded_contexts", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !strings.HasPrefix(strings.TrimSpace(res.Content[0].Text), "[") {
		t.Errorf("expected JSON array, got %q", res.Content[0].Text)
	}
}

func TestListBoundedContexts_ReturnsSortedList(t *testing.T) {
	state := loadFakeStateWithOverlay(t)
	res, rpcErr := Dispatch(state, "list_bounded_contexts", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	var summaries []BCSummary
	if err := json.Unmarshal([]byte(res.Content[0].Text), &summaries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 BCs, got %d: %+v", len(summaries), summaries)
	}
	// Sorted alphabetically: main, secondary.
	if summaries[0].Name != "main" || summaries[1].Name != "secondary" {
		t.Errorf("unexpected order: %v, %v", summaries[0].Name, summaries[1].Name)
	}
	if summaries[0].Description != "Main context" {
		t.Errorf("wrong description: %q", summaries[0].Description)
	}
}

func TestGetBoundedContext_Found(t *testing.T) {
	state := loadFakeStateWithOverlay(t)
	args := json.RawMessage(`{"name":"main"}`)
	res, rpcErr := Dispatch(state, "get_bounded_context", args)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	var detail bcDetail
	if err := json.Unmarshal([]byte(res.Content[0].Text), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.Name != "main" {
		t.Errorf("wrong name: %q", detail.Name)
	}
	if len(detail.Aggregates) != 2 {
		t.Errorf("expected 2 aggregates, got %d", len(detail.Aggregates))
	}
}

func TestGetBoundedContext_NotFound(t *testing.T) {
	state := loadFakeStateWithOverlay(t)
	args := json.RawMessage(`{"name":"ghost"}`)
	res, rpcErr := Dispatch(state, "get_bounded_context", args)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError result for unknown BC")
	}
	if !strings.Contains(res.Content[0].Text, "ghost") {
		t.Errorf("expected error text to mention BC name; got %q", res.Content[0].Text)
	}
}

func TestGetBoundedContext_MissingName(t *testing.T) {
	res, rpcErr := Dispatch(nil, "get_bounded_context", json.RawMessage(`{}`))
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError result for missing name")
	}
}

// TestSemanticCluster_NoState verifies error handling when state is nil.
func TestSemanticCluster_NoState(t *testing.T) {
	res, rpcErr := Dispatch(nil, "semantic_cluster", json.RawMessage(`{"selector":{"package":"alpha"}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError result for nil state")
	}
	if !strings.Contains(res.Content[0].Text, "no state") {
		t.Errorf("expected 'no state' in error, got: %s", res.Content[0].Text)
	}
}

// TestSemanticCluster_NoRetrieval verifies error handling when retrieval is not initialized.
func TestSemanticCluster_NoRetrieval(t *testing.T) {
	// loadFakeState disables retrieval via ARCHAI_RETRIEVAL_DISABLE env var
	state := loadFakeState(t)

	res, rpcErr := Dispatch(state, "semantic_cluster", json.RawMessage(`{"selector":{"package":"alpha"}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !res.IsError {
		t.Fatal("expected IsError result when retrieval not initialized")
	}
	// Should mention retrieval not initialized
	if !strings.Contains(res.Content[0].Text, "retrieval") {
		t.Errorf("expected 'retrieval' in error message, got: %s", res.Content[0].Text)
	}
}

// TestArchmotifIDToRetrievalID tests the ID mapping between archmotif and retrieval.
func TestArchmotifIDToRetrievalID(t *testing.T) {
	cases := []struct {
		amid string
		want string
	}{
		{"type:internal/domain.PackageModel", "internal/domain.PackageModel"},
		{"fn:internal/service.Generate", "internal/service.Generate"},
		{"method:internal/domain.State.Method", ""}, // methods not indexed
		{"field:internal/domain.State.Name", ""},    // fields not indexed
		{"pkg:internal/domain", ""},                 // packages not indexed
		{"file:internal/domain/model.go", ""},       // files not indexed
	}
	for _, tc := range cases {
		got := archmotifIDToRetrievalID(tc.amid)
		if got != tc.want {
			t.Errorf("archmotifIDToRetrievalID(%q) = %q, want %q", tc.amid, got, tc.want)
		}
	}
}

// TestMemberOwnerID verifies the member-to-owner ID mapping for collapse_members.
func TestMemberOwnerID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		// Methods map to their owning type
		{"method:internal/domain.State.DoSomething", "type:internal/domain.State"},
		{"method:pkg.Receiver.Method", "type:pkg.Receiver"},
		// Fields map to their owning type
		{"field:internal/domain.State.Name", "type:internal/domain.State"},
		{"field:pkg.Struct.Field", "type:pkg.Struct"},
		// Non-members return empty string
		{"type:internal/domain.State", ""},
		{"fn:internal/service.NewService", ""},
		{"pkg:internal/domain", ""},
		{"file:internal/domain/model.go", ""},
	}
	for _, tc := range cases {
		got := memberOwnerID(tc.id)
		if got != tc.want {
			t.Errorf("memberOwnerID(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// TestBuildCollapsedGraph verifies that method/field nodes are contracted into
// their owning type and edges are re-pointed correctly.
func TestBuildCollapsedGraph(t *testing.T) {
	// Build a small graph with:
	// - type:pkg.TypeA
	// - type:pkg.TypeB
	// - method:pkg.TypeA.DoSomething with an edge to type:pkg.TypeB
	// After collapse, we expect:
	// - type:pkg.TypeA with edge to type:pkg.TypeB (the method is gone)
	b := archmotifimport.NewBuilder()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("builder error: %v", err)
		}
	}

	must(b.AddPackage("pkg:mypkg", "", ""))
	must(b.AddType("type:mypkg.TypeA", "pkg:mypkg", false, ""))
	must(b.AddType("type:mypkg.TypeB", "pkg:mypkg", false, ""))
	must(b.AddMethod("method:mypkg.TypeA.DoSomething", "type:mypkg.TypeA"))
	// Edge from method to TypeB (the method uses TypeB)
	must(b.AddDependency("method:mypkg.TypeA.DoSomething", "type:mypkg.TypeB", archmotifimport.DependencyUsesType))

	original, err := b.Build()
	if err != nil {
		t.Fatalf("build original graph: %v", err)
	}

	// Selected nodes include the method
	selectedNodeIDs := []string{
		"type:mypkg.TypeA",
		"type:mypkg.TypeB",
		"method:mypkg.TypeA.DoSomething",
	}

	collapsed, survivingNodeIDs, edgeCount, err := buildCollapsedGraph(original, selectedNodeIDs)
	if err != nil {
		t.Fatalf("buildCollapsedGraph failed: %v", err)
	}

	// Verify method node is gone
	for _, id := range survivingNodeIDs {
		if strings.HasPrefix(id, "method:") {
			t.Errorf("method node %q should not survive collapse", id)
		}
	}

	// Verify we have exactly 2 surviving nodes (the two types)
	if len(survivingNodeIDs) != 2 {
		t.Errorf("expected 2 surviving nodes, got %d: %v", len(survivingNodeIDs), survivingNodeIDs)
	}

	// Verify edge was re-pointed: TypeA -> TypeB
	if edgeCount != 1 {
		t.Errorf("expected 1 edge after collapse, got %d", edgeCount)
	}

	// Verify graph can be used (has nodes and edges)
	if collapsed.NodeCount() == 0 {
		t.Error("collapsed graph has no nodes")
	}
	if collapsed.EdgeCount() == 0 {
		t.Error("collapsed graph has no edges")
	}
}

// TestBuildCollapsedGraph_SelfLoopRemoval verifies that edges that become self-loops
// after collapse are removed.
func TestBuildCollapsedGraph_SelfLoopRemoval(t *testing.T) {
	// Build a graph where a method has an edge to its own type (self-loop after collapse)
	b := archmotifimport.NewBuilder()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("builder error: %v", err)
		}
	}

	must(b.AddPackage("pkg:mypkg", "", ""))
	must(b.AddType("type:mypkg.TypeA", "pkg:mypkg", false, ""))
	must(b.AddMethod("method:mypkg.TypeA.Method", "type:mypkg.TypeA"))
	// This edge should become TypeA -> TypeA (self-loop) and be removed
	must(b.AddDependency("method:mypkg.TypeA.Method", "type:mypkg.TypeA", archmotifimport.DependencyUsesType))

	original, err := b.Build()
	if err != nil {
		t.Fatalf("build original graph: %v", err)
	}

	selectedNodeIDs := []string{
		"type:mypkg.TypeA",
		"method:mypkg.TypeA.Method",
	}

	collapsed, _, edgeCount, err := buildCollapsedGraph(original, selectedNodeIDs)
	if err != nil {
		t.Fatalf("buildCollapsedGraph failed: %v", err)
	}

	// Self-loop should be removed
	if edgeCount != 0 {
		t.Errorf("expected 0 edges (self-loop removed), got %d", edgeCount)
	}

	// Graph should still be valid
	if collapsed == nil {
		t.Fatal("collapsed graph is nil")
	}
}

// TestBuildSemanticKNNGraph_RegistersPackages verifies that buildSemanticKNNGraph
// correctly registers package nodes before adding symbol nodes. This test exercises
// the same code path the MCP handler uses (real archmotifimport.NewBuilder construction).
// It would fail with "unknown packageID" if packages aren't registered first.
func TestBuildSemanticKNNGraph_RegistersPackages(t *testing.T) {
	// Create test nodes spanning two packages with mock embeddings.
	// The node IDs use archmotif format: "type:pkg/path.Name" and "fn:pkg/path.Name"
	nodes := []semanticNode{
		{
			archmotifID: "type:internal/domain.PackageModel",
			retrievalID: "internal/domain.PackageModel",
			vec:         []float32{1.0, 0.0, 0.0, 0.0},
		},
		{
			archmotifID: "type:internal/domain.InterfaceDef",
			retrievalID: "internal/domain.InterfaceDef",
			vec:         []float32{0.9, 0.1, 0.0, 0.0},
		},
		{
			archmotifID: "fn:internal/service.Generate",
			retrievalID: "internal/service.Generate",
			vec:         []float32{0.0, 1.0, 0.0, 0.0},
		},
		{
			archmotifID: "fn:internal/service.NewService",
			retrievalID: "internal/service.NewService",
			vec:         []float32{0.0, 0.9, 0.1, 0.0},
		},
	}

	// Build the semantic kNN graph. This MUST NOT fail — if it does, the package
	// registration order is wrong.
	graph, edgeCount, err := buildSemanticKNNGraph(nodes, 2, 0.0)
	if err != nil {
		t.Fatalf("buildSemanticKNNGraph failed: %v", err)
	}

	// Verify we got the expected structure.
	if graph == nil {
		t.Fatal("graph is nil")
	}

	// With 4 nodes and knn=2, we expect 8 directed edges (each node connects to 2 neighbors).
	if edgeCount != 8 {
		t.Errorf("expected 8 edges, got %d", edgeCount)
	}

	// Verify nodes are present in the graph: 4 symbol nodes + 2 package nodes.
	// (buildSemanticKNNGraph registers package nodes before symbols)
	if graph.NodeCount() != 6 {
		t.Errorf("expected 6 nodes (4 symbols + 2 packages), got %d", graph.NodeCount())
	}

	// Verify the graph can be used with spectral clustering (the actual consumer).
	// This implicitly validates that the graph structure is correct.
	if graph.EdgeCount() == 0 {
		t.Error("graph has no edges — semantic edges were not added")
	}
}

// TestBuildClusterInfos_CapsLargeClusters verifies that cluster membership is
// returned in full for normal (package-sized) clusters but degrades to a sized
// sample once a cluster exceeds clusterMembersFullLimit — so the lens output
// never grows unbounded in node-id strings.
func TestBuildClusterInfos_CapsLargeClusters(t *testing.T) {
	ids := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("fn:pkg.%s%d", prefix, i)
		}
		return out
	}

	small := spectralcluster.Cluster{ID: 0, Members: ids("small", 5)}
	big := spectralcluster.Cluster{ID: 1, Members: ids("big", clusterMembersFullLimit+50)}

	infos := buildClusterInfos([]spectralcluster.Cluster{small, big})

	// Small cluster: full members, no sample, not truncated.
	if got := infos[0]; len(got.Members) != 5 || got.Truncated || got.MembersSample != nil {
		t.Errorf("small cluster not returned in full: %+v", got)
	}
	// Big cluster: sample only, truncated, but Size reflects the true count.
	bigInfo := infos[1]
	if !bigInfo.Truncated {
		t.Errorf("big cluster should be truncated")
	}
	if bigInfo.Members != nil {
		t.Errorf("big cluster should not carry full members, got %d", len(bigInfo.Members))
	}
	if len(bigInfo.MembersSample) != clusterMembersSample {
		t.Errorf("sample len = %d, want %d", len(bigInfo.MembersSample), clusterMembersSample)
	}
	if bigInfo.Size != clusterMembersFullLimit+50 {
		t.Errorf("size = %d, want %d (true count)", bigInfo.Size, clusterMembersFullLimit+50)
	}
}

// TestCapBoundary_CapsAndCounts verifies boundary symbols are capped to a fixed
// ceiling while the true total is still reported.
func TestCapBoundary_CapsAndCounts(t *testing.T) {
	syms := make([]string, boundarySymbolLimit+30)
	for i := range syms {
		syms[i] = fmt.Sprintf("fn:pkg.b%d", i)
	}

	capped, total := capBoundary(syms)
	if total != boundarySymbolLimit+30 {
		t.Errorf("total = %d, want %d", total, boundarySymbolLimit+30)
	}
	if len(capped) != boundarySymbolLimit {
		t.Errorf("capped len = %d, want %d", len(capped), boundarySymbolLimit)
	}

	// Short lists pass through unchanged.
	short := []string{"fn:pkg.a", "fn:pkg.b"}
	capped, total = capBoundary(short)
	if total != 2 || len(capped) != 2 {
		t.Errorf("short list mangled: capped=%d total=%d", len(capped), total)
	}
}
