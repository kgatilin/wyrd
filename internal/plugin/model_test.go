package plugin

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

func TestBuildModel_PassesThroughPackages(t *testing.T) {
	pkgs := []domain.PackageModel{
		{Path: "internal/a", Name: "a", Layer: "domain"},
		{Path: "internal/b", Name: "b", Layer: "service"},
	}
	got := BuildModel("acme.io/x", pkgs, nil)
	if got.Module != "acme.io/x" {
		t.Errorf("Module = %q, want acme.io/x", got.Module)
	}
	if len(got.Packages) != 2 {
		t.Fatalf("Packages len = %d, want 2", len(got.Packages))
	}
	if got.Packages[0].Path != "internal/a" || got.Packages[0].Layer != "domain" {
		t.Errorf("Packages[0] = %+v", *got.Packages[0])
	}
}

func TestBuildModel_LayersAndRulesFromOverlay(t *testing.T) {
	cfg := &overlay.Config{
		Module: "acme.io/x",
		Layers: map[string][]string{
			"domain":  {"internal/domain/..."},
			"service": {"internal/service/..."},
		},
		LayerRules: map[string][]string{
			"service": {"domain"},
		},
		Aggregates: map[string]overlay.Aggregate{
			"User": {Root: "acme.io/x/internal/domain.User"},
		},
		Configs: []string{"acme.io/x/internal/config.AppConfig"},
	}
	got := BuildModel("", nil, cfg)
	if got.Module != "acme.io/x" {
		t.Errorf("Module = %q, want acme.io/x", got.Module)
	}
	if len(got.Layers) != 2 {
		t.Errorf("Layers len = %d, want 2", len(got.Layers))
	}
	// Layers should be sorted.
	if got.Layers[0].Name != "domain" {
		t.Errorf("Layers[0].Name = %q, want domain", got.Layers[0].Name)
	}
	if len(got.LayerRules) != 1 || got.LayerRules[0].Layer != "service" {
		t.Errorf("LayerRules = %+v", got.LayerRules)
	}
	if len(got.Aggregates) != 1 || got.Aggregates[0].Name != "User" {
		t.Errorf("Aggregates = %+v", got.Aggregates)
	}
	if len(got.Configs) != 1 || got.Configs[0].FQTypeName != "acme.io/x/internal/config.AppConfig" {
		t.Errorf("Configs = %+v", got.Configs)
	}
}

func TestModel_FindPackage(t *testing.T) {
	pkgs := []domain.PackageModel{
		{Path: "internal/a"},
		{Path: "internal/b"},
	}
	m := BuildModel("acme.io/x", pkgs, nil)
	if got := m.FindPackage("internal/b"); got == nil || got.Path != "internal/b" {
		t.Errorf("FindPackage missed internal/b: %+v", got)
	}
	if got := m.FindPackage("missing"); got != nil {
		t.Errorf("FindPackage(missing) = %+v, want nil", got)
	}
}

func TestModel_PackagesInLayer(t *testing.T) {
	pkgs := []domain.PackageModel{
		{Path: "internal/a", Layer: "domain"},
		{Path: "internal/b", Layer: "service"},
		{Path: "internal/c", Layer: "domain"},
	}
	m := BuildModel("acme.io/x", pkgs, nil)
	got := m.PackagesInLayer("domain")
	if len(got) != 2 {
		t.Fatalf("PackagesInLayer(domain) len = %d, want 2", len(got))
	}
	if got[0].Path != "internal/a" || got[1].Path != "internal/c" {
		t.Errorf("PackagesInLayer(domain) paths = %s, %s", got[0].Path, got[1].Path)
	}
}
