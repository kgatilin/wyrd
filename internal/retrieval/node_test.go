package retrieval

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestBuildNodes_CountsAndKinds(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "internal/serve",
			Interfaces: []domain.InterfaceDef{
				{Name: "Handler"},
			},
			Structs: []domain.StructDef{
				{Name: "State"},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewState"},
			},
			TypeDefs: []domain.TypeDef{
				{Name: "StatusCode"},
			},
			Constants: []domain.ConstDef{
				{Name: "MaxRetries"},
			},
			Variables: []domain.VarDef{
				{Name: "DefaultTimeout"},
			},
			Errors: []domain.ErrorDef{
				{Name: "ErrNotFound"},
			},
		},
	}

	nodes := BuildNodes(models)

	if len(nodes) != 7 {
		t.Errorf("expected 7 nodes, got %d", len(nodes))
	}

	kindCounts := make(map[string]int)
	for _, n := range nodes {
		kindCounts[n.Kind]++
	}

	expectedKinds := map[string]int{
		"iface": 1,
		"class": 1,
		"func":  1,
		"type":  1,
		"const": 1,
		"var":   1,
		"error": 1,
	}

	for kind, expected := range expectedKinds {
		if kindCounts[kind] != expected {
			t.Errorf("expected %d nodes of kind %q, got %d", expected, kind, kindCounts[kind])
		}
	}
}

func TestBuildNodes_IDMatchesUIGraph(t *testing.T) {
	// The uigraph ID scheme is: {PackagePath}.{SymbolName}
	// (see internal/adapter/uigraph/uigraph.go buildComponent)
	models := []domain.PackageModel{
		{
			Path: "internal/serve",
			Interfaces: []domain.InterfaceDef{
				{Name: "Handler"},
			},
			Structs: []domain.StructDef{
				{Name: "State"},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewState"},
			},
		},
	}

	nodes := BuildNodes(models)

	expectedIDs := map[string]bool{
		"internal/serve.Handler":  true,
		"internal/serve.State":    true,
		"internal/serve.NewState": true,
	}

	for _, n := range nodes {
		if !expectedIDs[n.ID] {
			t.Errorf("unexpected node ID: %s", n.ID)
		}
		delete(expectedIDs, n.ID)
	}

	for id := range expectedIDs {
		t.Errorf("missing expected node ID: %s", id)
	}
}

func TestBuildNodes_EmbeddablePredicate(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "pkg",
			Interfaces: []domain.InterfaceDef{
				{Name: "Iface"},
			},
			Structs: []domain.StructDef{
				{Name: "Struct"},
			},
			Functions: []domain.FunctionDef{
				{Name: "Func"},
			},
			TypeDefs: []domain.TypeDef{
				{Name: "Type"},
			},
			Constants: []domain.ConstDef{
				{Name: "Const"},
			},
			Variables: []domain.VarDef{
				{Name: "Var"},
			},
			Errors: []domain.ErrorDef{
				{Name: "Err"},
			},
		},
	}

	nodes := BuildNodes(models)

	expected := map[string]bool{
		"pkg.Iface":  true,  // iface -> embeddable
		"pkg.Struct": true,  // class -> embeddable
		"pkg.Func":   true,  // func -> embeddable
		"pkg.Type":   true,  // type -> embeddable
		"pkg.Const":  false, // const -> not embeddable
		"pkg.Var":    false, // var -> not embeddable
		"pkg.Err":    false, // error -> not embeddable
	}

	for _, n := range nodes {
		if want, ok := expected[n.ID]; ok {
			if n.Embeddable != want {
				t.Errorf("node %s: expected Embeddable=%v, got %v", n.ID, want, n.Embeddable)
			}
		}
	}
}

func TestBuildNodes_Signatures(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "pkg",
			Interfaces: []domain.InterfaceDef{
				{Name: "Handler"},
			},
			Structs: []domain.StructDef{
				{Name: "State"},
			},
			Functions: []domain.FunctionDef{
				{
					Name: "NewState",
					Params: []domain.ParamDef{
						{Name: "cfg", Type: domain.TypeRef{Name: "Config"}},
					},
					Returns: []domain.TypeRef{
						{Name: "State", IsPointer: true},
					},
				},
			},
			TypeDefs: []domain.TypeDef{
				{
					Name:           "StatusCode",
					UnderlyingType: domain.TypeRef{Name: "int"},
				},
			},
		},
	}

	nodes := BuildNodes(models)

	signatures := make(map[string]string)
	for _, n := range nodes {
		signatures[n.ID] = n.Signature
	}

	if sig := signatures["pkg.Handler"]; sig != "type Handler interface" {
		t.Errorf("interface signature: got %q", sig)
	}
	if sig := signatures["pkg.State"]; sig != "type State struct" {
		t.Errorf("struct signature: got %q", sig)
	}
	if sig := signatures["pkg.NewState"]; sig != "NewState(cfg Config) *State" {
		t.Errorf("function signature: got %q", sig)
	}
	if sig := signatures["pkg.StatusCode"]; sig != "type StatusCode int" {
		t.Errorf("typedef signature: got %q", sig)
	}
}

func TestIsEmbeddable(t *testing.T) {
	tests := []struct {
		kind     string
		expected bool
	}{
		{"func", true},
		{"iface", true},
		{"class", true},
		{"type", true},
		{"const", false},
		{"var", false},
		{"error", false},
		{"unknown", false},
	}

	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			if got := isEmbeddable(tc.kind); got != tc.expected {
				t.Errorf("isEmbeddable(%q) = %v, want %v", tc.kind, got, tc.expected)
			}
		})
	}
}
