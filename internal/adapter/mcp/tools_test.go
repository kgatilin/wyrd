package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/serve"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

func TestMain(m *testing.M) {
	// Disable retrieval in tests to avoid background goroutines
	// that interfere with temp directory cleanup.
	os.Setenv("WYRD_RETRIEVAL_DISABLE", "1")
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
	// 11 original tools + 4 retrieval tools (search, expand, get_node, refresh) + spectral_cluster + components + trophic_layers + semantic_cluster + file_hotspots + latent_domains + embedding_coverage + status + read_file + search_files
	if len(defs) != 25 {
		t.Fatalf("expected 25 tool definitions, got %d", len(defs))
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

	out := res.Content[0].Text
	if !strings.Contains(out, "trophic_layers  alpha") {
		t.Errorf("missing lens header: %q", out)
	}
	if !strings.Contains(out, "coherence  F0=") {
		t.Errorf("coherence line missing: %q", out)
	}
	if !strings.Contains(out, "edges ") {
		t.Errorf("edge kinds not echoed: %q", out)
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

	// alpha.go holds Service, Impl, New -> one file with 3 top-level decls.
	out := res.Content[0].Text
	if !strings.Contains(out, "file_hotspots  alpha") {
		t.Errorf("missing lens header: %q", out)
	}
	if !strings.Contains(out, "1 files / 3 symbols") {
		t.Errorf("file/symbol counts wrong: %q", out)
	}
	if !strings.Contains(out, "max 3") {
		t.Errorf("max_symbols wrong: %q", out)
	}
	if !strings.Contains(out, "alpha.go") || !strings.Contains(out, "×3") {
		t.Errorf("top file line missing: %q", out)
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

func TestExtract_EmptyStateReturnsEmptyIndex(t *testing.T) {
	res, rpcErr := Dispatch(nil, "extract", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", res.Content)
	}
	if got := res.Content[0].Text; !strings.HasPrefix(got, "extract index  ·  0 packages") {
		t.Errorf("expected an empty index header, got %q", got)
	}
}

func TestExtract_NoPathsReturnsIndex(t *testing.T) {
	state := loadFakeState(t)
	res, rpcErr := Dispatch(state, "extract", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	out := res.Content[0].Text
	if !strings.Contains(out, "extract index  ·  2 packages") {
		t.Fatalf("expected a 2-package index header, got %q", out)
	}
	// Index mode: a census line per package, no symbol surface.
	if !strings.Contains(out, "alpha   = ") || !strings.Contains(out, "beta   = ") {
		t.Errorf("index mode must carry per-package counts: %q", out)
	}
	if strings.Contains(out, "\ninterfaces\n") || strings.Contains(out, "\nfuncs\n") {
		t.Errorf("index mode must carry no symbols: %q", out)
	}
	// Zero-valued kinds are dropped rather than printed as `const 0  var 0`.
	if strings.Contains(out, " 0  ") || strings.HasSuffix(strings.TrimSpace(out), " 0") {
		t.Errorf("index mode printed a zero kind count: %q", out)
	}
}

func TestExtract_FilterByPathReturnsDigest(t *testing.T) {
	state := loadFakeState(t)
	args := json.RawMessage(`{"paths":["alpha"]}`)
	res, rpcErr := Dispatch(state, "extract", args)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	out := res.Content[0].Text
	if !strings.Contains(out, "package alpha") {
		t.Fatalf("expected the alpha digest, got %q", out)
	}
	if strings.Contains(out, "package beta") {
		t.Fatalf("path filter leaked another package: %q", out)
	}
	// With explicit paths the digest carries the symbol surface, rendered the
	// same way get_package renders it.
	if !strings.Contains(out, "counts  ") || !strings.Contains(out, "\ninterfaces\n") {
		t.Errorf("expected symbol digest for alpha, got %q", out)
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
	out := res.Content[0].Text
	if !strings.Contains(out, "packages  ·  2") {
		t.Fatalf("expected 2 packages in header, got %q", out)
	}
	// alpha declares one interface, one struct, one func. The labels are the
	// words `kinds` accepts, so a count can be filtered on as printed.
	if !strings.Contains(out, "alpha   interface 1  struct 1  func 1") {
		t.Errorf("alpha counts line missing/wrong: %q", out)
	}
}

func TestListPackages_EmptyState(t *testing.T) {
	res, rpcErr := Dispatch(nil, "list_packages", nil)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if got := strings.TrimSpace(res.Content[0].Text); got != "packages  ·  0" {
		t.Errorf("expected empty-package header, got %q", got)
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
	out := res.Content[0].Text
	if !strings.Contains(out, "package beta") {
		t.Errorf("expected package header for beta, got %q", out)
	}
	// beta declares Thing (struct) and Hello (func).
	if !strings.Contains(out, "struct 1") || !strings.Contains(out, "func 1") {
		t.Errorf("unexpected counts line: %q", out)
	}
	if !strings.Contains(out, "Thing") || !strings.Contains(out, "Hello") {
		t.Errorf("missing expected symbols in digest: %q", out)
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

// loadFakeStateWithOverlay extends loadFakeState with a minimal wyrd.yaml
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
	mustWrite(t, filepath.Join(dir, "wyrd.yaml"), overlayYAML)
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
	if got := strings.TrimSpace(res.Content[0].Text); got != "bounded_contexts  ·  0" {
		t.Errorf("expected an empty context listing, got %q", got)
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
	out := res.Content[0].Text
	if !strings.HasPrefix(out, "bounded_contexts  ·  2\n") {
		t.Fatalf("expected a 2-context header, got %q", out)
	}
	// Sorted alphabetically: main, secondary.
	if i, j := strings.Index(out, "main"), strings.Index(out, "secondary"); i < 0 || j < 0 || i > j {
		t.Errorf("unexpected order: %q", out)
	}
	if !strings.Contains(out, "· Main context") {
		t.Errorf("description missing: %q", out)
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
	out := res.Content[0].Text
	if !strings.HasPrefix(out, "bounded_context  main") {
		t.Errorf("wrong header: %q", out)
	}
	if !strings.Contains(out, "aggregates  ") || strings.Count(out, ",") < 1 {
		t.Errorf("expected the aggregate list: %q", out)
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
	// loadFakeState disables retrieval via WYRD_RETRIEVAL_DISABLE env var
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
