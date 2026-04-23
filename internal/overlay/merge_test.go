package overlay

import (
	"reflect"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// newMergeConfig returns a representative overlay Config for use by
// Merge tests. Tests mutate fields as needed.
func newMergeConfig() *Config {
	return &Config{
		Module: "github.com/example/app",
		Layers: map[string][]string{
			"domain":   {"internal/domain/..."},
			"service":  {"internal/service/..."},
			"adapter":  {"internal/adapter/..."},
			"entry":    {"cmd/*"},
		},
		LayerRules: map[string][]string{
			"service": {"domain"},
			"adapter": {"domain", "service"},
			"entry":   {"domain", "service", "adapter"},
		},
		Aggregates: map[string]Aggregate{
			"Order": {Root: "github.com/example/app/internal/domain.Order"},
		},
	}
}

func TestMerge_AssignsLayerToPackage(t *testing.T) {
	cfg := newMergeConfig()
	models := []domain.PackageModel{
		{Path: "internal/domain"},
		{Path: "internal/domain/order"},
		{Path: "internal/service"},
		{Path: "internal/adapter/yaml"},
		{Path: "cmd/archai"},
		{Path: "tests/integration"}, // no layer
	}

	merged, _, err := Merge(models, cfg)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}

	want := []string{"domain", "domain", "service", "adapter", "entry", ""}
	for i, m := range merged {
		if m.Layer != want[i] {
			t.Errorf("models[%d] (%s): got layer %q, want %q", i, m.Path, m.Layer, want[i])
		}
	}
}

func TestMerge_AssignsAggregateToPackageContainingRoot(t *testing.T) {
	cfg := newMergeConfig()
	models := []domain.PackageModel{
		{Path: "internal/domain"},
		{Path: "internal/service"},
	}

	merged, _, err := Merge(models, cfg)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}

	if merged[0].Aggregate != "Order" {
		t.Errorf("domain package aggregate: got %q, want %q", merged[0].Aggregate, "Order")
	}
	if merged[1].Aggregate != "" {
		t.Errorf("service package aggregate: got %q, want empty", merged[1].Aggregate)
	}
}

func TestMerge_DetectsForbiddenCrossLayerImport(t *testing.T) {
	cfg := newMergeConfig()
	// domain -> service is forbidden (domain has no outbound rule,
	// so any cross-layer import violates). service -> domain is OK.
	models := []domain.PackageModel{
		{
			Path: "internal/domain",
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "github.com/example/app/internal/domain", Symbol: "Thing"},
					To:   domain.SymbolRef{Package: "github.com/example/app/internal/service", Symbol: "Service"},
					Kind: domain.DependencyUses,
				},
			},
		},
		{
			Path: "internal/service",
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "github.com/example/app/internal/service", Symbol: "Service"},
					To:   domain.SymbolRef{Package: "github.com/example/app/internal/domain", Symbol: "Thing"},
					Kind: domain.DependencyUses,
				},
			},
		},
	}

	_, violations, err := Merge(models, cfg)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	v := violations[0]
	if v.Package != "internal/domain" || v.Layer != "domain" {
		t.Errorf("violation source: got %+v", v)
	}
	if !reflect.DeepEqual(v.Imports, []string{"internal/service"}) {
		t.Errorf("violation imports: got %v, want [internal/service]", v.Imports)
	}
}

func TestMerge_AllowedCrossLayerImportProducesNoViolation(t *testing.T) {
	cfg := newMergeConfig()
	models := []domain.PackageModel{
		{Path: "internal/domain"},
		{
			Path: "internal/service",
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "github.com/example/app/internal/service"},
					To:   domain.SymbolRef{Package: "github.com/example/app/internal/domain"},
					Kind: domain.DependencyUses,
				},
			},
		},
	}

	_, violations, err := Merge(models, cfg)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %+v", violations)
	}
}

func TestMerge_IgnoresExternalDependencies(t *testing.T) {
	cfg := newMergeConfig()
	models := []domain.PackageModel{
		{
			Path: "internal/domain",
			Dependencies: []domain.Dependency{
				{
					From: domain.SymbolRef{Package: "github.com/example/app/internal/domain"},
					To:   domain.SymbolRef{Package: "context", Symbol: "Context", External: true},
					Kind: domain.DependencyUses,
				},
			},
		},
	}
	_, violations, err := Merge(models, cfg)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations for external deps, got %+v", violations)
	}
}

func TestMerge_NilConfigReturnsError(t *testing.T) {
	_, _, err := Merge(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"internal/domain/...", "internal/domain", true},
		{"internal/domain/...", "internal/domain/order", true},
		{"internal/domain/...", "internal/domain/order/sub", true},
		{"internal/domain/...", "internal/service", false},
		{"internal/domain", "internal/domain", true},
		{"internal/domain", "internal/domain/order", false},
		{"cmd/*", "cmd/archai", true},
		{"cmd/*", "cmd/archai/sub", false},
		{"cmd/*", "cmd", false},
	}
	for _, c := range cases {
		got := matchGlob(c.glob, c.path)
		if got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestMerge_FirstMatchingLayerWins(t *testing.T) {
	// Two layers both match: lexically first layer name wins.
	cfg := &Config{
		Module: "github.com/example/app",
		Layers: map[string][]string{
			"alpha": {"internal/..."},
			"beta":  {"internal/service"},
		},
	}
	models := []domain.PackageModel{{Path: "internal/service"}}
	merged, _, err := Merge(models, cfg)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	if merged[0].Layer != "alpha" {
		t.Errorf("got layer %q, want alpha (lexical order)", merged[0].Layer)
	}
}
