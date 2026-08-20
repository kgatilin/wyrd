package retrieval

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// methodModel is the shape the Go reader produces for one struct with one
// method: the method's calls on the method, and the same dependency reported
// twice — once against the struct, once against the method — which is what the
// reader does on purpose so both granularities are available.
func methodModel() domain.PackageModel {
	return domain.PackageModel{
		Path: "pkg",
		Structs: []domain.StructDef{
			{
				Name: "Service",
				Methods: []domain.MethodDef{
					{
						Name: "Search",
						Calls: []domain.CallEdge{
							{To: domain.SymbolRef{Package: "pkg", Symbol: "fuse"}},
						},
					},
				},
			},
		},
		Functions: []domain.FunctionDef{{Name: "fuse"}},
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{Package: "pkg", Symbol: "Service"},
				To:   domain.SymbolRef{Package: "pkg", Symbol: "Options"},
				Kind: domain.DependencyUses,
			},
			{
				From: domain.SymbolRef{Package: "pkg", Symbol: "Service.Search"},
				To:   domain.SymbolRef{Package: "pkg", Symbol: "Options"},
				Kind: domain.DependencyUses,
			},
		},
		TypeDefs: []domain.TypeDef{{Name: "Options"}},
	}
}

func edgesOf(g *Graph, from string) map[string]EdgeKind {
	out := make(map[string]EdgeKind)
	for _, e := range g.Outgoing[from] {
		out[e.To] = e.Kind
	}
	return out
}

func TestBuildGraph_MethodCallsBelongToTheMethod(t *testing.T) {
	_, g := BuildGraph([]domain.PackageModel{methodModel()})

	if kind, ok := edgesOf(g, "pkg.Service.Search")["pkg.fuse"]; !ok || kind != EdgeCalls {
		t.Errorf("method does not call fuse: %v", edgesOf(g, "pkg.Service.Search"))
	}
	// Attributing a method's calls to its receiver answers "who calls fuse"
	// with a type that does not call it.
	if _, ok := edgesOf(g, "pkg.Service")["pkg.fuse"]; ok {
		t.Error("the call is still attributed to the receiver")
	}
}

func TestBuildGraph_ContainmentKeepsTheTypeReachable(t *testing.T) {
	_, g := BuildGraph([]domain.PackageModel{methodModel()})

	if kind, ok := edgesOf(g, "pkg.Service")["pkg.Service.Search"]; !ok || kind != EdgeContains {
		t.Fatalf("no containment edge: %v", edgesOf(g, "pkg.Service"))
	}
	// Moving calls onto methods costs the aggregate view nothing: a traversal
	// that takes all edges still reaches the call from the type, one hop later.
	reached := g.NeighborNodes([]string{"pkg.Service"}, 2, nil, 0)
	if !reached["pkg.fuse"] {
		t.Errorf("fuse unreachable from the type in 2 hops: %v", reached)
	}
}

func TestBuildGraph_ContainmentStaysOutOfTheDiffusion(t *testing.T) {
	_, g := BuildGraph([]domain.PackageModel{methodModel()})

	for _, e := range diffusionEdges(g, DefaultParams().EdgeKindWeights) {
		if e.From == "pkg.Service" && e.To == "pkg.Service.Search" {
			t.Fatal("containment is diffusing; a type would inherit its methods' relevance")
		}
	}
}

func TestBuildGraph_DuplicateRelationsAreRecordedOnce(t *testing.T) {
	_, g := BuildGraph([]domain.PackageModel{methodModel()})

	count := 0
	for _, e := range g.Outgoing["pkg.Service.Search"] {
		if e.To == "pkg.Options" && e.Kind == EdgeUses {
			count++
		}
	}
	if count != 1 {
		t.Errorf("uses edge recorded %d times; the diffusion sums duplicate weights", count)
	}
}

func TestBuildGraph_MemberEndpointsFoldOntoTheirOwner(t *testing.T) {
	// An interface method has no node, so a relation the reader recorded
	// against it has to land on the interface rather than on a phantom id no
	// answer could ever resolve.
	models := []domain.PackageModel{{
		Path:       "pkg",
		Interfaces: []domain.InterfaceDef{{Name: "Reader", Methods: []domain.MethodDef{{Name: "Read"}}}},
		TypeDefs:   []domain.TypeDef{{Name: "Options"}},
		Dependencies: []domain.Dependency{{
			From: domain.SymbolRef{Package: "pkg", Symbol: "Reader.Read"},
			To:   domain.SymbolRef{Package: "pkg", Symbol: "Options"},
			Kind: domain.DependencyUses,
		}},
	}}

	_, g := BuildGraph(models)

	if _, ok := g.Outgoing["pkg.Reader.Read"]; ok {
		t.Error("edge kept an endpoint that is not a node")
	}
	if _, ok := edgesOf(g, "pkg.Reader")["pkg.Options"]; !ok {
		t.Errorf("relation lost instead of folding onto the interface: %v", edgesOf(g, "pkg.Reader"))
	}
}

func TestBuildGraph_UnresolvableEndpointsAreDropped(t *testing.T) {
	models := []domain.PackageModel{{
		Path:      "pkg",
		Functions: []domain.FunctionDef{{Name: "fuse"}},
		Dependencies: []domain.Dependency{{
			From: domain.SymbolRef{Package: "pkg", Symbol: "fuse"},
			To:   domain.SymbolRef{Package: "pkg", Symbol: "Gone"},
			Kind: domain.DependencyUses,
		}},
	}}

	_, g := BuildGraph(models)

	if len(g.Outgoing["pkg.fuse"]) != 0 {
		t.Errorf("edge into a symbol the graph cannot name survived: %v", g.Outgoing["pkg.fuse"])
	}
}
