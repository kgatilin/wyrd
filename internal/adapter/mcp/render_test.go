package mcp

import (
	"regexp"
	"strings"
	"testing"
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
		Diffusion: &diffusionStats{Conductance: 0.21384719, SeedCount: 5},
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
	// The cut quality is reported next to the size, rounded like every other
	// metric in these renders.
	if !strings.Contains(out, "conductance 0.214") || !strings.Contains(out, "5 seeds") {
		t.Errorf("diffusion diagnostics missing from header:\n%s", out)
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
