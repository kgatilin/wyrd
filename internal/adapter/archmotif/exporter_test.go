package archmotif

import (
	"reflect"
	"sort"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// TestToArchmotifGraph_NodeAndEdgeCounts checks that the adapter
// emits exactly the nodes and edges implied by a small fixture.
func TestToArchmotifGraph_NodeAndEdgeCounts(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path:  "internal/domain",
			Name:  "domain",
			Layer: "domain",
			Structs: []domain.StructDef{
				{
					Name:       "Order",
					IsExported: true,
					Stereotype: domain.StereotypeAggregate,
					Fields: []domain.FieldDef{
						{Name: "ID", Type: domain.TypeRef{Name: "string"}, IsExported: true},
						{Name: "Total", Type: domain.TypeRef{Name: "int"}, IsExported: true},
					},
					Methods: []domain.MethodDef{
						{Name: "Submit", IsExported: true},
					},
				},
			},
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "OrderRepo",
					IsExported: true,
					Stereotype: domain.StereotypeRepository,
					Methods: []domain.MethodDef{
						{Name: "Save", IsExported: true},
					},
				},
			},
		},
		{
			Path:  "internal/service",
			Name:  "service",
			Layer: "service",
			Structs: []domain.StructDef{
				{
					Name:       "OrderService",
					IsExported: true,
					Stereotype: domain.StereotypeService,
				},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewOrderService", IsExported: true, Stereotype: domain.StereotypeFactory},
			},
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "internal/service", Symbol: "OrderService"},
					To:   domain.SymbolRef{Package: "internal/domain", Symbol: "Order"},
					Kind: domain.DependencyUses,
				},
			},
		},
	}

	// The domain package owns the implements declaration.
	models[0].Implementations = []domain.Implementation{
		{
			Concrete:  domain.SymbolRef{Package: "internal/service", Symbol: "OrderService"},
			Interface: domain.SymbolRef{Package: "internal/domain", Symbol: "OrderRepo"},
		},
	}

	g, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}

	// Expected nodes: 2 packages + 3 types (Order, OrderRepo, OrderService)
	// + 2 fields + 2 methods (Order.Submit, OrderRepo.Save) + 1 function.
	wantNodes := 2 + 3 + 2 + 2 + 1
	if g.NodeCount() != wantNodes {
		t.Errorf("node count: got %d, want %d", g.NodeCount(), wantNodes)
	}

	// Expected edges:
	//   contains: pkg domain -> Order, pkg domain -> OrderRepo,
	//             pkg service -> OrderService, pkg service -> NewOrderService,
	//             Order -> ID, Order -> Total, Order -> Submit, OrderRepo -> Save (8)
	//   implements: OrderService -> OrderRepo (1)
	//   usesType: OrderService -> Order (1)
	//   dependsOn pkg-level: service -> domain (1)
	wantEdges := 8 + 1 + 1 + 1
	if g.EdgeCount() != wantEdges {
		t.Errorf("edge count: got %d, want %d", g.EdgeCount(), wantEdges)
	}

	// Spot-check role attribute on the aggregate.
	if n, ok := g.Node("type:internal/domain.Order"); !ok {
		t.Error("missing type:internal/domain.Order")
	} else if n.Attrs["role"] != "aggregate" {
		t.Errorf("Order.role: got %v, want aggregate", n.Attrs["role"])
	}

	// Spot-check the layer attribute on a package node.
	if n, ok := g.Node("pkg:internal/domain"); !ok {
		t.Error("missing pkg:internal/domain")
	} else if n.Attrs["layer"] != "domain" {
		t.Errorf("domain.layer: got %v, want domain", n.Attrs["layer"])
	}
}

// TestToArchmotifGraph_StableIDs checks that two runs over the same
// input produce identical node-id sets and that permuting the input
// slice ordering does not change the output id set.
func TestToArchmotifGraph_StableIDs(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "a",
			Structs: []domain.StructDef{
				{Name: "Z", IsExported: true},
				{Name: "A", IsExported: true},
				{Name: "M", IsExported: true},
			},
			Interfaces: []domain.InterfaceDef{
				{Name: "P", IsExported: true},
				{Name: "B", IsExported: true},
			},
			Functions: []domain.FunctionDef{
				{Name: "FZ", IsExported: true},
				{Name: "FA", IsExported: true},
			},
		},
	}

	g1, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	g2, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	ids1 := nodeIDs(g1)
	ids2 := nodeIDs(g2)
	if !reflect.DeepEqual(ids1, ids2) {
		t.Errorf("node ids not stable across runs:\nrun1=%v\nrun2=%v", ids1, ids2)
	}

	// Permuting input slices must not change the output id set.
	models2 := []domain.PackageModel{{
		Path: "a",
		Structs: []domain.StructDef{
			{Name: "A", IsExported: true},
			{Name: "M", IsExported: true},
			{Name: "Z", IsExported: true},
		},
		Interfaces: []domain.InterfaceDef{
			{Name: "B", IsExported: true},
			{Name: "P", IsExported: true},
		},
		Functions: []domain.FunctionDef{
			{Name: "FA", IsExported: true},
			{Name: "FZ", IsExported: true},
		},
	}}
	g3, err := ToArchmotifGraph(models2, nil)
	if err != nil {
		t.Fatalf("permuted run: %v", err)
	}
	ids3 := nodeIDs(g3)
	if !reflect.DeepEqual(ids1, ids3) {
		t.Errorf("node ids not stable under input permutation:\noriginal=%v\npermuted=%v", ids1, ids3)
	}
}

// TestToArchmotifGraph_ExternalDependenciesSkipped checks that
// dependencies pointing outside the loaded package set are dropped.
func TestToArchmotifGraph_ExternalDependenciesSkipped(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "internal/service",
			Structs: []domain.StructDef{
				{Name: "S", IsExported: true},
			},
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "internal/service", Symbol: "S"},
					To:   domain.SymbolRef{Package: "context", Symbol: "Context", External: true},
					Kind: domain.DependencyUses,
				},
			},
		},
	}
	g, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}
	// 1 package + 1 type, 1 contains edge — no external dep edges.
	if g.NodeCount() != 2 {
		t.Errorf("node count: got %d, want 2", g.NodeCount())
	}
	if g.EdgeCount() != 1 {
		t.Errorf("edge count: got %d, want 1", g.EdgeCount())
	}
}

// TestToArchmotifGraph_StereotypeRoles verifies every archai
// stereotype is mapped to the documented role string.
func TestToArchmotifGraph_StereotypeRoles(t *testing.T) {
	cases := []struct {
		stereotype domain.Stereotype
		wantRole   string
	}{
		{domain.StereotypeService, "service"},
		{domain.StereotypeRepository, "repository"},
		{domain.StereotypePort, "port"},
		{domain.StereotypeFactory, "factory"},
		{domain.StereotypeAggregate, "aggregate"},
		{domain.StereotypeEntity, "entity"},
		{domain.StereotypeValue, "value"},
		{domain.StereotypeEnum, "enum"},
	}
	for _, tc := range cases {
		t.Run(string(tc.stereotype), func(t *testing.T) {
			models := []domain.PackageModel{{
				Path: "p",
				Structs: []domain.StructDef{
					{Name: "T", IsExported: true, Stereotype: tc.stereotype},
				},
			}}
			g, err := ToArchmotifGraph(models, nil)
			if err != nil {
				t.Fatalf("ToArchmotifGraph: %v", err)
			}
			n, ok := g.Node("type:p.T")
			if !ok {
				t.Fatal("type node not found")
			}
			if got := n.Attrs["role"]; got != tc.wantRole {
				t.Errorf("role: got %v, want %s", got, tc.wantRole)
			}
		})
	}
}

// TestToArchmotifGraph_ExtendsBecomesEmbeds checks that the Java
// 'extends' kind is mapped to archmotif's embeds edge.
func TestToArchmotifGraph_ExtendsBecomesEmbeds(t *testing.T) {
	models := []domain.PackageModel{{
		Path: "p",
		Structs: []domain.StructDef{
			{Name: "Parent", IsExported: true},
			{Name: "Child", IsExported: true},
		},
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{Package: "p", Symbol: "Child"},
				To:   domain.SymbolRef{Package: "p", Symbol: "Parent"},
				Kind: domain.DependencyExtends,
			},
		},
	}}
	g, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}
	// 1 package + 2 types + 2 contains + 1 embeds = 3 nodes + 3 edges.
	if g.NodeCount() != 3 {
		t.Errorf("node count: got %d, want 3", g.NodeCount())
	}
	if g.EdgeCount() != 3 {
		t.Errorf("edge count: got %d, want 3", g.EdgeCount())
	}
	embeds := 0
	for _, e := range g.Edges() {
		if string(e.Kind) == "embeds" {
			embeds++
		}
	}
	if embeds != 1 {
		t.Errorf("embeds edges: got %d, want 1", embeds)
	}
}

// TestToArchmotifGraph_CallToUnregisteredMethod is a regression test for
// the "unknown to-node" crash: a call edge whose target method does not
// appear in the receiver type's captured method set must be skipped, not
// emitted as a dangling edge that aborts the entire graph build.
//
// Mirrors the field report: fn registerPluginCommands "calls"
// agentAPIPlugin.Tools, the agentAPIPlugin struct exists but the reader's
// call-extraction surfaced a Tools method that was never registered as a
// node (e.g. promoted/embedded or partial file coverage).
func TestToArchmotifGraph_CallToUnregisteredMethod(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "internal/plugins/agent_api",
			Name: "agent_api",
			Structs: []domain.StructDef{
				// Struct exists, but its captured method set does NOT
				// include "Tools" — exactly the dangling-target condition.
				{Name: "agentAPIPlugin"},
			},
		},
		{
			Path: "internal/adapters/cli",
			Name: "cli",
			Functions: []domain.FunctionDef{
				{
					Name: "registerPluginCommands",
					Calls: []domain.CallEdge{
						{To: domain.SymbolRef{
							Package: "internal/plugins/agent_api",
							Symbol:  "agentAPIPlugin.Tools",
						}},
					},
				},
			},
		},
	}

	g, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph must not crash on a call to an "+
			"unregistered method: %v", err)
	}

	// The phantom method node must not have been created, and no call
	// edge to it should exist.
	if _, ok := g.Node("method:internal/plugins/agent_api.agentAPIPlugin.Tools"); ok {
		t.Error("phantom method node was created; expected it to be absent")
	}
	for _, e := range g.Edges() {
		if string(e.Kind) == "calls" {
			t.Errorf("unexpected dangling call edge emitted: %s -> %s", e.From, e.To)
		}
	}
}

// TestToArchmotifGraph_CallToRegisteredMethod is the positive companion:
// when the called method IS in the receiver type's method set, the call
// edge must be emitted to the registered method node.
func TestToArchmotifGraph_CallToRegisteredMethod(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "internal/plugins/agent_api",
			Name: "agent_api",
			Structs: []domain.StructDef{
				{
					Name:    "agentAPIPlugin",
					Methods: []domain.MethodDef{{Name: "Tools"}},
				},
			},
		},
		{
			Path: "internal/adapters/cli",
			Name: "cli",
			Functions: []domain.FunctionDef{
				{
					Name: "registerPluginCommands",
					Calls: []domain.CallEdge{
						{To: domain.SymbolRef{
							Package: "internal/plugins/agent_api",
							Symbol:  "agentAPIPlugin.Tools",
						}},
					},
				},
			},
		},
	}

	g, err := ToArchmotifGraph(models, nil)
	if err != nil {
		t.Fatalf("ToArchmotifGraph: %v", err)
	}

	var calls int
	for _, e := range g.Edges() {
		if string(e.Kind) == "calls" {
			calls++
			if e.From != "fn:internal/adapters/cli.registerPluginCommands" ||
				e.To != "method:internal/plugins/agent_api.agentAPIPlugin.Tools" {
				t.Errorf("unexpected call edge: %s -> %s", e.From, e.To)
			}
		}
	}
	if calls != 1 {
		t.Errorf("calls edges: got %d, want 1", calls)
	}
}

// nodeIDs returns sorted node ids from an archmotifimport.Graph.
func nodeIDs(g *archmotifimport.Graph) []string {
	out := make([]string, 0, g.NodeCount())
	for _, n := range g.Nodes() {
		out = append(out, n.ID)
	}
	sort.Strings(out)
	return out
}
