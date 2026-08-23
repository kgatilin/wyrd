package mcp

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/retrieval"
)

func TestSplitNodeID(t *testing.T) {
	cases := []struct{ id, pkg, name string }{
		{"internal/adapter/mcp.Dispatch", "internal/adapter/mcp", "Dispatch"},
		{"internal/adapter/mcp.Client.Do", "internal/adapter/mcp", "Client.Do"},
		{"github.com/kgatilin/x/pkg.Foo", "github.com/kgatilin/x/pkg", "Foo"},
		{"main.Foo", "main", "Foo"},
	}
	for _, c := range cases {
		pkg, name := splitNodeID(c.id)
		if pkg != c.pkg || name != c.name {
			t.Errorf("splitNodeID(%q) = (%q,%q), want (%q,%q)", c.id, pkg, name, c.pkg, c.name)
		}
	}
}

func TestSimplifyType(t *testing.T) {
	home := "internal/adapter/mcp"
	in := "(state *internal/serve.State, res internal/adapter/mcp.ToolResult) (encoding/json.RawMessage, *internal/adapter/mcp.RPCError)"
	got := simplifyType(in, home)
	// Home package prefix fully stripped.
	if strings.Contains(got, home+".") {
		t.Errorf("home prefix not stripped: %q", got)
	}
	// Cross-package types shortened to last path segment.
	for _, want := range []string{"*serve.State", "ToolResult", "json.RawMessage", "*RPCError"} {
		if !strings.Contains(got, want) {
			t.Errorf("simplifyType missing %q in %q", want, got)
		}
	}
}

func TestShortArchmotifID(t *testing.T) {
	home := "internal/adapter/mcp"
	cases := []struct{ id, want string }{
		{"fn:internal/adapter/mcp.capMembers", "capMembers"},
		{"type:internal/adapter/mcp.ToolResult", "ToolResult"},
		{"type:internal/serve.State", "serve.State"},
		{"method:internal/domain.PackageModel.Add", "domain.PackageModel.Add"},
	}
	for _, c := range cases {
		if got := shortArchmotifID(c.id, home); got != c.want {
			t.Errorf("shortArchmotifID(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestRenderSubgraph_CompactAndRoundTrippable(t *testing.T) {
	home := "internal/adapter/mcp"
	r := subgraphResult{
		Dense:     true,
		NodeCount: 3,
		EdgeCount: 2,
		Nodes: []nodeInfo{
			{ID: home + ".Dispatch", Kind: "func", Package: home, Name: "Dispatch",
				File: home + "/tools.go", Line: 10,
				Signature: "Dispatch(res internal/adapter/mcp.ToolResult) *internal/adapter/mcp.RPCError",
				Doc:       "Dispatch routes a call.", Score: 0.12013965764586908},
			{ID: home + ".ToolResult", Kind: "struct", Package: home, Name: "ToolResult",
				File: home + "/tools.go", Line: 86, Signature: "type ToolResult struct"},
			{ID: "internal/serve.State", Kind: "struct", Package: "internal/serve", Name: "State",
				File: "internal/serve/state.go", Line: 37},
		},
		Edges: []edgeInfo{
			{From: home + ".Dispatch", To: home + ".ToolResult", Kind: "returns"},
			{From: home + ".Dispatch", To: "internal/serve.State", Kind: "uses"},
		},
	}
	out := renderSubgraph("dispatch", r)

	// The home package path must be declared once (header) and never repeated
	// on node/signature lines.
	if strings.Count(out, home+".") != 0 {
		t.Errorf("home package path repeated in body:\n%s", out)
	}
	if !strings.Contains(out, "home "+home) {
		t.Errorf("missing home header:\n%s", out)
	}
	// Cross-package node keeps its package (round-trip) via its own header.
	if !strings.Contains(out, "internal/serve") {
		t.Errorf("cross-package node lost its package:\n%s", out)
	}
	// Signature type prefixes simplified.
	if !strings.Contains(out, "Dispatch  (res ToolResult) *RPCError") {
		t.Errorf("signature not simplified:\n%s", out)
	}
	// Struct rendered as bare kind after name.
	if !strings.Contains(out, "ToolResult  struct") {
		t.Errorf("struct signature not compacted:\n%s", out)
	}
	// Edges folded with an arrow, endpoints shortened.
	if !strings.Contains(out, "Dispatch --returns--> ToolResult") {
		t.Errorf("edge not folded with arrow:\n%s", out)
	}
	if !strings.Contains(out, "Dispatch --uses--> serve.State") {
		t.Errorf("cross-package edge endpoint not shortened:\n%s", out)
	}
	// No float score noise anywhere.
	assertNoFloatNoise(t, out)
}

func TestRenderSubgraph_ExpandOmitsDiffusion(t *testing.T) {
	// expand walks edges rather than cutting a community, so it has no cut to
	// report and the line must not appear at all.
	out := renderSubgraph("", subgraphResult{
		Nodes: []nodeInfo{{ID: "internal/serve.State", Kind: "struct", Package: "internal/serve", Name: "State"}},
	})
	if strings.Contains(out, "conductance") {
		t.Errorf("expand output carries a cut it never made:\n%s", out)
	}
}

func TestRenderSearch_SeparatesHitsFromRegionAndReportsTheCut(t *testing.T) {
	home := "internal/adapter/mcp"
	r := searchResult{
		Dense:       true,
		Conductance: 0.21384719,
		SeedCount:   2,
		Hits: []searchHit{
			{nodeInfo: nodeInfo{ID: home + ".Dispatch", Kind: "func", Package: home, Name: "Dispatch",
				File: home + "/tools.go", Line: 10,
				Signature: "Dispatch(res internal/adapter/mcp.ToolResult) *internal/adapter/mcp.RPCError",
				Doc:       "Dispatch routes a call.", Score: 0.12013965764586908},
				Seed: true, TextScore: 0.4132876},
			{nodeInfo: nodeInfo{ID: home + ".ToolResult", Kind: "struct", Package: home, Name: "ToolResult",
				File: home + "/tools.go", Line: 86, Signature: "type ToolResult struct"},
				Seed: true, TextScore: 0.2},
			{nodeInfo: nodeInfo{ID: "internal/serve.State", Kind: "struct", Package: "internal/serve", Name: "State",
				File: "internal/serve/state.go", Line: 37, Score: 0.031}},
		},
		Edges: []edgeInfo{
			{From: home + ".Dispatch", To: home + ".ToolResult", Kind: "returns"},
			{From: home + ".Dispatch", To: "internal/serve.State", Kind: "uses"},
		},
	}
	out := renderSearch("dispatch", r)

	// One answer, two sections: what the text matched and what the graph added.
	hits := strings.Index(out, "\nhits\n")
	region := strings.Index(out, "\nregion\n")
	if hits < 0 || region < 0 || hits > region {
		t.Fatalf("hits and region sections missing or out of order:\n%s", out)
	}
	// The section headers carry no legend: the format is in the tool
	// description, which the client already has, not on every response.
	for _, legend := range []string{"matched the query text", "diffuse into", "src --kind--> dst",
		"bare names are in this package", "isolated community"} {
		if strings.Contains(out, legend) {
			t.Errorf("legend %q reprinted in the response body:\n%s", legend, out)
		}
	}
	if strings.Index(out, "Dispatch  (res ToolResult)") > region {
		t.Errorf("a seed was printed under the region:\n%s", out)
	}
	if strings.Index(out, "State") < region {
		t.Errorf("a diffused node was printed under the hits:\n%s", out)
	}
	// The cut quality is reported next to the size, rounded like every other
	// metric in these renders.
	if !strings.Contains(out, "conductance 0.214") || !strings.Contains(out, "2 seeds") {
		t.Errorf("diffusion diagnostics missing from header:\n%s", out)
	}
	if !strings.Contains(out, "2 hits") || !strings.Contains(out, "region 1 nodes / 2 edges") {
		t.Errorf("header counts wrong:\n%s", out)
	}
	// The home package is declared once and never repeated on the body lines.
	if strings.Count(out, home+".") != 0 {
		t.Errorf("home package path repeated in body:\n%s", out)
	}
	if !strings.Contains(out, "home "+home) {
		t.Errorf("missing home header:\n%s", out)
	}
	if !strings.Contains(out, "Dispatch --uses--> serve.State") {
		t.Errorf("cross-package edge endpoint not shortened:\n%s", out)
	}
	assertNoFloatNoise(t, out)
}

func TestRenderSearch_LeadsWithTheTopHitNotTheBiggestPackage(t *testing.T) {
	// One decisive hit, and a tail of weak ones that happen to share a package.
	// Picking home by majority put that tail first and read as a wrong answer,
	// though the ranking underneath was right.
	top := "internal/retrieval"
	tail := "internal/adapter/mcp"
	hit := func(pkg, name string, score float64, seed bool) searchHit {
		return searchHit{
			nodeInfo: nodeInfo{ID: pkg + "." + name, Kind: "func", Package: pkg, Name: name},
			Seed:     seed, TextScore: score,
		}
	}
	out := renderSearch("fuseCalibrated", searchResult{
		SeedCount: 3,
		Hits: []searchHit{
			hit(top, "Service.fuseCalibrated", 0.83, true),
			hit(tail, "trophicVerdict", 0.02, true),
			hit(tail, "expectedMutualInfo", 0.01, true),
		},
	})

	if !strings.Contains(out, "home "+top) {
		t.Errorf("home is not the top hit's package:\n%s", out)
	}
	if strings.Index(out, "fuseCalibrated") > strings.Index(out, "trophicVerdict") {
		t.Errorf("the answer leads with the weak tail:\n%s", out)
	}
}

func TestRenderSearch_NoMatches(t *testing.T) {
	out := renderSearch("nothing", searchResult{})
	if !strings.Contains(out, "no matches") {
		t.Errorf("empty answer must say so:\n%s", out)
	}
	if strings.Contains(out, "conductance") {
		t.Errorf("a search that found nothing reports a cut it never made:\n%s", out)
	}
}

func TestRenderSpectralCore_NoFloatNoise(t *testing.T) {
	home := "internal/adapter/mcp"
	r := spectralClusterResponse{
		NodeCount:  10,
		EdgeCount:  20,
		Modularity: 0.4231897,
		CutAnalysis: spectralCutAnalysis{
			ChosenK: 2, KSource: "eigengap",
			Candidates: []spectralKCandidate{{K: 2, Gap: 0.1734, Modularity: 0.4231897}},
		},
		Eigenvalues: []float64{0, 0.0312, 0.1123},
		Clusters: []spectralClusterInfo{
			{ID: 0, Size: 2, Members: []string{"fn:" + home + ".Dispatch", "type:internal/serve.State"}},
		},
	}
	out := renderSpectralCore("spectral_cluster", r)
	if !strings.Contains(out, "K=2 (eigengap)") || !strings.Contains(out, "Q=0.423") {
		t.Errorf("header metrics wrong:\n%s", out)
	}
	if !strings.Contains(out, "C0  ×2") {
		t.Errorf("cluster line missing:\n%s", out)
	}
	if !strings.Contains(out, "serve.State") || strings.Contains(out, "fn:") || strings.Contains(out, "type:") {
		t.Errorf("member ids not shortened:\n%s", out)
	}
	assertNoFloatNoise(t, out)
}

// assertNoFloatNoise fails if the text contains a float with more than 6
// fractional digits — the 17-digit score/metric noise the compact format drops.
func assertNoFloatNoise(t *testing.T, s string) {
	t.Helper()
	if m := regexp.MustCompile(`\d\.\d{7,}`).FindString(s); m != "" {
		t.Errorf("float-precision noise leaked into output: %q", m)
	}
}

func TestFoldEdges_ConvergingEdgesFoldOntoTheirTarget(t *testing.T) {
	// Every action implements one interface and uses one payload type. Folding
	// by source alone spends a line per action saying the same thing twice.
	var edges []edgeInfo
	names := []string{"AgentAction", "ForeachAction", "IfAction", "MapAction", "SwitchAction"}
	for _, n := range names {
		edges = append(edges,
			edgeInfo{From: "internal/actions." + n, To: "internal/workflow.Action", Kind: "implements"},
			edgeInfo{From: "internal/actions." + n, To: "internal/workflow.ErrorData", Kind: "uses"},
		)
	}
	lines := foldEdges(edges, func(id string) string { return shortEndpoint(id, "internal/actions") })

	if len(lines) != 2 {
		t.Fatalf("10 converging edges should fold to 2 lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "workflow.Action <--implements-- AgentAction, ForeachAction, IfAction, MapAction, SwitchAction") {
		t.Errorf("implements edges not folded onto their target:\n%s", joined)
	}
	if !strings.Contains(joined, "workflow.ErrorData <--uses-- ") {
		t.Errorf("uses edges not folded onto their target:\n%s", joined)
	}
	// Every edge is printed exactly once, on one side of the arrow or the other.
	for _, n := range names {
		if got := strings.Count(joined, n); got != 2 {
			t.Errorf("%s appears %d times, want 2 (once per edge):\n%s", n, got, joined)
		}
	}
}

func TestFoldEdges_FanOutStillFoldsOntoItsSource(t *testing.T) {
	// The other direction has to keep working: one source, many targets.
	var edges []edgeInfo
	for _, n := range []string{"Alpha", "Beta", "Gamma"} {
		edges = append(edges, edgeInfo{From: "pkg.Run", To: "pkg." + n, Kind: "calls"})
	}
	lines := foldEdges(edges, func(id string) string { return shortEndpoint(id, "pkg") })
	if len(lines) != 1 || !strings.Contains(lines[0], "Run --calls--> Alpha, Beta, Gamma") {
		t.Fatalf("fan-out not folded onto its source: %v", lines)
	}
}

func TestRenderSearch_RegionCarriesNoSignatures(t *testing.T) {
	home := "internal/adapter/mcp"
	sig := "streamCompletion(ctx context.Context, params anthropic.MessageNewParams) <-chan event.Event"
	out := renderSearch("stream", searchResult{
		SeedCount: 1,
		Hits: []searchHit{
			{nodeInfo: nodeInfo{ID: home + ".Dispatch", Kind: "func", Package: home, Name: "Dispatch",
				Signature: "Dispatch(a int) error", File: home + "/tools.go", Line: 10}, Seed: true},
			{nodeInfo: nodeInfo{ID: "internal/llm.streamCompletion", Kind: "func", Package: "internal/llm",
				Name: "streamCompletion", Signature: sig, File: "internal/llm/stream.go", Line: 42}},
		},
	})

	// The hit keeps its signature; the region entry is name, kind, location.
	if !strings.Contains(out, "Dispatch  (a int) error") {
		t.Errorf("a hit lost its signature:\n%s", out)
	}
	if strings.Contains(out, "MessageNewParams") {
		t.Errorf("a region entry printed its full signature:\n%s", out)
	}
	if !strings.Contains(out, "streamCompletion  func   stream.go:42") {
		t.Errorf("region entry should be name, kind and location:\n%s", out)
	}
}

func TestRenderSubgraph_NoTrailingWhitespace(t *testing.T) {
	// A node without a location used to leave the padding of the missing
	// column at the end of its line.
	out := renderSubgraph("", subgraphResult{
		Nodes: []nodeInfo{
			{ID: "internal/serve.State", Kind: "struct", Package: "internal/serve", Name: "State"},
			{ID: "internal/serve.Load", Kind: "func", Package: "internal/serve", Name: "Load",
				File: "internal/serve/state.go", Line: 12},
		},
	})
	for _, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line has trailing whitespace: %q\n%s", line, out)
		}
	}
	if !strings.Contains(out, "Load  func   state.go:12") {
		t.Errorf("expand node lost its file:line:\n%s", out)
	}
}

func TestRenderNodeDetail_IncidentEdgesFoldByKind(t *testing.T) {
	home := "internal/adapter/mcp"
	id := home + ".Dispatch"
	var edges []edgeInfo
	for _, c := range []string{"Alpha", "Beta", "Gamma"} {
		edges = append(edges, edgeInfo{From: home + "." + c, To: id, Kind: "calls"})
	}
	edges = append(edges, edgeInfo{From: id, To: home + ".ToolResult", Kind: "returns"})

	out := renderNodeDetail(nodeDetailResult{
		NodeID: id, Kind: "func", Package: home, Name: "Dispatch",
		File: home + "/tools.go", Line: 10, Edges: edges,
	})
	if !strings.Contains(out, "  <--calls-- Alpha, Beta, Gamma\n") {
		t.Errorf("callers not folded onto one line per kind:\n%s", out)
	}
	if !strings.Contains(out, "  --returns--> ToolResult\n") {
		t.Errorf("outgoing edges not folded by kind:\n%s", out)
	}
	if strings.Contains(out, "Dispatch --") || strings.Contains(out, "--> Dispatch") {
		t.Errorf("the section's own node is repeated on its edge lines:\n%s", out)
	}
}

func TestRenderPackageList_SortedByPath(t *testing.T) {
	out := renderPackageList([]PackageSummary{
		{Path: "internal/serve"},
		{Path: "cmd/wyrd"},
		{Path: "internal/adapter/mcp"},
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")[1:]
	got := make([]string, len(lines))
	for i, l := range lines {
		got[i] = strings.Fields(l)[0]
	}
	want := []string{"cmd/wyrd", "internal/adapter/mcp", "internal/serve"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("packages not sorted by path: %v", got)
	}
}

func TestRenderBCList_OneLinePerContext(t *testing.T) {
	out := renderBCList([]BCSummary{{
		Name: "billing", Relationship: "customer-supplier",
		Description: "Handles money movement.\nEverything else is elsewhere.",
		Aggregates:  []string{"invoice", "payment"},
		Upstream:    []string{"catalog"},
	}})
	if !strings.Contains(out, "billing  «customer-supplier»   aggregates invoice, payment   up catalog\n") {
		t.Errorf("context not rendered on one line:\n%s", out)
	}
	if !strings.Contains(out, "· Handles money movement.\n") {
		t.Errorf("description should be the synopsis:\n%s", out)
	}
	if strings.Contains(out, "down ") {
		t.Errorf("empty column printed:\n%s", out)
	}
}

func TestRenderStatus_ReportsThePhaseAsAWord(t *testing.T) {
	out := renderStatus(statusResult{
		Ready: true, DenseAvailable: true, Packages: 267, Nodes: 8412,
		Embedded: 8100, Embeddable: 8100, Embedder: "ollama:nomic", Message: "Ready.",
	})
	if !strings.HasPrefix(out, "status  ready\n") {
		t.Errorf("state word missing:\n%s", out)
	}
	if !strings.Contains(out, "packages 267  nodes 8412  embedded 8100/8100") {
		t.Errorf("counts line wrong:\n%s", out)
	}
	if strings.Contains(out, "{") {
		t.Errorf("status still renders JSON:\n%s", out)
	}

	indexing := renderStatus(statusResult{Indexing: true, Embedded: 10, Embeddable: 100, Pending: 90})
	if !strings.HasPrefix(indexing, "status  indexing\n") || !strings.Contains(indexing, "pending 90") {
		t.Errorf("indexing state not reported:\n%s", indexing)
	}
}

func TestRenderSearch_OneKindVocabularyThroughout(t *testing.T) {
	// The trap this closes: a reader saw `executor  struct` under hits, passed
	// `--kind struct`, and got a silent zero because the region and the filter
	// spoke the graph's `class`. Every surface of one answer now prints the
	// word the filter accepts.
	home := "internal/exec"
	out := renderSearch("executor", searchResult{
		SeedCount: 1,
		Hits: []searchHit{
			{nodeInfo: nodeInfo{ID: home + ".Executor", Kind: "class", Package: home, Name: "Executor",
				Signature: "type Executor struct", File: home + "/exec.go", Line: 10}, Seed: true},
			{nodeInfo: nodeInfo{ID: home + ".Runner", Kind: "iface", Package: home, Name: "Runner",
				Signature: "type Runner interface", File: home + "/exec.go", Line: 3}, Seed: true},
			// Region entries print their kind rather than a signature, which is
			// where the graph's spelling used to leak out.
			{nodeInfo: nodeInfo{ID: home + ".Job", Kind: "class", Package: home, Name: "Job",
				Signature: "type Job struct", File: home + "/job.go", Line: 7}},
			{nodeInfo: nodeInfo{ID: home + ".Sink", Kind: "iface", Package: home, Name: "Sink",
				Signature: "type Sink interface", File: home + "/job.go", Line: 20}},
		},
	})

	for _, leaked := range []string{"class", "iface"} {
		if strings.Contains(out, leaked) {
			t.Errorf("the graph's spelling %q reached the answer:\n%s", leaked, out)
		}
	}
	if !strings.Contains(out, "Executor  struct") || !strings.Contains(out, "Runner  interface") {
		t.Errorf("a hit lost its Go word:\n%s", out)
	}
	if !strings.Contains(out, "Job  struct") || !strings.Contains(out, "Sink  interface") {
		t.Errorf("a region entry lost its Go word:\n%s", out)
	}
	// And the words printed are words the filter resolves back to those nodes.
	if retrieval.CanonicalKind("struct") != "class" || retrieval.CanonicalKind("interface") != "iface" {
		t.Error("the printed vocabulary is not the one the filter accepts")
	}
}

func TestRenderNodeDetail_HeaderUsesTheGoWord(t *testing.T) {
	out := renderNodeDetail(nodeDetailResult{
		NodeID: "internal/exec.Executor", Kind: "class", Package: "internal/exec",
		Name: "Executor", File: "internal/exec/exec.go", Line: 10,
	})
	if !strings.HasPrefix(out, "Executor  ·  struct  ·  internal/exec") {
		t.Errorf("get_node header prints the stored kind:\n%s", out)
	}
}

func TestRenderPackageDigest_CountsDropZeros(t *testing.T) {
	out := renderPackageDigest(packageDigest{
		Path:   "internal/godoc",
		Counts: symbolCounts{Functions: 5, Total: 5},
	})
	if !strings.Contains(out, "counts  = 5   func 5\n") {
		t.Errorf("counts line should carry only what the package has:\n%s", out)
	}
	for _, zero := range []string{"interface 0", "struct 0", "type 0", "const 0", "var 0", "error 0", "iface", "err "} {
		if strings.Contains(out, zero) {
			t.Errorf("counts line still prints %q:\n%s", zero, out)
		}
	}
}

// `impl` is the package name of a dozen packages in a layered repository, so
// the last segment does not name one. Two callers rendered as one string fold
// into one set entry and half the answer is gone, with nothing saying so.
func TestGetNode_CallersInLikeNamedPackagesStayApart(t *testing.T) {
	home := "controllers/processor"
	out := renderNodeDetail(nodeDetailResult{
		NodeID: home + ".DecodeProcessCancel", Kind: "func", Package: home,
		Name: "DecodeProcessCancel", File: home + "/decode.go", Line: 12,
		Edges: []edgeInfo{
			{From: "controllers/entrypoint/impl.reduce", To: home + ".DecodeProcessCancel", Kind: "calls"},
			{From: "controllers/processor/impl.reduce", To: home + ".DecodeProcessCancel", Kind: "calls"},
			{From: "controllers/processor.Decode", To: home + ".DecodeProcessCancel", Kind: "calls"},
		},
	})

	for _, want := range []string{"entrypoint/impl.reduce", "processor/impl.reduce"} {
		if !strings.Contains(out, want) {
			t.Errorf("caller %q is missing:\n%s", want, out)
		}
	}
	// The caller that never collided keeps the short form: disambiguation is
	// paid for where it is needed, not charged to the whole listing.
	if !strings.Contains(out, "Decode,") && !strings.Contains(out, ", Decode") {
		t.Errorf("an unambiguous same-package caller was expanded anyway:\n%s", out)
	}
}

// Expansion stops as soon as the names are distinct, and goes all the way to
// the full path when that is what it takes.
func TestEndpointShortenerExpandsOnlyWhatCollides(t *testing.T) {
	home := "internal/service"
	ids := []string{
		home + ".Run",
		"internal/a/impl.Reduce",
		"internal/b/impl.Reduce",
		"internal/plugins/events.New",
	}
	short := newEndpointShortener(ids, home)
	for id, want := range map[string]string{
		ids[0]: "Run",
		ids[1]: "a/impl.Reduce",
		ids[2]: "b/impl.Reduce",
		ids[3]: "events.New",
	} {
		if got := short(id); got != want {
			t.Errorf("short(%s) = %q, want %q", id, got, want)
		}
	}
}
