package publicapi

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestProjectKeepsOnlyExportedPackageSurface(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Client",
					IsExported: true,
					Methods: []domain.MethodDef{
						{Name: "Do", IsExported: true, Params: []domain.ParamDef{{Name: "ctx", Type: domain.TypeRef{Package: "context", Name: "Context"}}}},
						{Name: "internalHook"},
					},
				},
				{Name: "hidden"},
			},
			Structs: []domain.StructDef{
				{
					Name:       "Config",
					IsExported: true,
					Fields: []domain.FieldDef{
						{Name: "Endpoint", Type: domain.TypeRef{Name: "string"}, IsExported: true},
						{Name: "token", Type: domain.TypeRef{Name: "string"}},
					},
					Methods: []domain.MethodDef{{Name: "Validate", IsExported: true}},
				},
				{Name: "privateConfig"},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewClient", IsExported: true, Returns: []domain.TypeRef{{Name: "Client"}}},
				{Name: "helper"},
			},
			TypeDefs: []domain.TypeDef{
				{Name: "Mode", UnderlyingType: domain.TypeRef{Name: "string"}, Constants: []string{"ModeFast", "modeSlow"}, IsExported: true},
			},
			Constants: []domain.ConstDef{
				{Name: "DefaultPort", Type: domain.TypeRef{Name: "int"}, Value: "8080", IsExported: true},
				{Name: "privateConst"},
			},
			Variables: []domain.VarDef{
				{Name: "DefaultClient", Type: domain.TypeRef{Name: "Client"}, IsExported: true},
				{Name: "privateVar"},
			},
			Errors: []domain.ErrorDef{
				{Name: "ErrMissing", IsExported: true},
				{Name: "errHidden"},
			},
		},
	}

	surface := Project(models)

	if surface.Schema != Schema {
		t.Fatalf("Schema = %q, want %q", surface.Schema, Schema)
	}
	if len(surface.Packages) != 1 {
		t.Fatalf("Packages = %+v, want one public package", surface.Packages)
	}

	got := map[string]Symbol{}
	for _, symbol := range surface.Packages[0].Symbols {
		got[symbol.ID] = symbol
	}
	for _, id := range []string{
		"api.Client",
		"api.Config",
		"api.DefaultClient",
		"api.DefaultPort",
		"api.ErrMissing",
		"api.Mode",
		"api.NewClient",
	} {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing public symbol %q in %+v", id, surface.Packages[0].Symbols)
		}
	}
	for _, id := range []string{"api.hidden", "api.privateConfig", "api.helper", "api.privateConst", "api.privateVar", "api.errHidden"} {
		if _, ok := got[id]; ok {
			t.Fatalf("unexpected private symbol %q in public surface", id)
		}
	}

	client := got["api.Client"]
	if len(client.Members) != 1 || client.Members[0].ID != "api.Client.Do" {
		t.Fatalf("Client members = %+v, want only exported Do", client.Members)
	}

	config := got["api.Config"]
	configMembers := map[string]struct{}{}
	for _, member := range config.Members {
		configMembers[member.ID] = struct{}{}
	}
	for _, id := range []string{"api.Config.Endpoint", "api.Config.Validate"} {
		if _, ok := configMembers[id]; !ok {
			t.Fatalf("missing Config public member %q in %+v", id, config.Members)
		}
	}
	if _, ok := configMembers["api.Config.token"]; ok {
		t.Fatalf("unexpected private field in public Config members: %+v", config.Members)
	}

	mode := got["api.Mode"]
	if len(mode.Members) != 1 || mode.Members[0].ID != "api.Mode.ModeFast" {
		t.Fatalf("Mode members = %+v, want only exported ModeFast", mode.Members)
	}
}

func TestProjectKeepsOnlyPublicPackageDependencies(t *testing.T) {
	models := []domain.PackageModel{
		{
			Path: "api",
			Name: "api",
			Functions: []domain.FunctionDef{
				{Name: "NewRepo", IsExported: true},
			},
			Dependencies: []domain.Dependency{
				{
					From:            domain.SymbolRef{Package: "api", Symbol: "NewRepo"},
					To:              domain.SymbolRef{Package: "storage", Symbol: "Repository", External: true},
					Kind:            domain.DependencyReturns,
					ThroughExported: true,
				},
				{
					From: domain.SymbolRef{Package: "api", Symbol: "helper"},
					To:   domain.SymbolRef{Package: "internal/cache", Symbol: "Cache"},
					Kind: domain.DependencyUses,
				},
				{
					From:            domain.SymbolRef{Package: "api", Symbol: "NewRepo"},
					To:              domain.SymbolRef{Package: "external/mod", Symbol: "Client", External: true},
					Kind:            domain.DependencyUses,
					ThroughExported: true,
				},
			},
		},
		{Path: "storage", Name: "storage", Interfaces: []domain.InterfaceDef{{Name: "Repository", IsExported: true}}},
		{Path: "internal/cache", Name: "cache"},
	}

	surface := Project(models)

	if len(surface.PackageDeps) != 1 {
		t.Fatalf("PackageDeps = %+v, want one public repo dep", surface.PackageDeps)
	}
	dep := surface.PackageDeps[0]
	if dep.ID != "e:api->storage" || dep.FromPackage != "api" || dep.ToPackage != "storage" {
		t.Fatalf("PackageDependency = %+v, want api -> storage", dep)
	}
	if len(dep.Kinds) != 1 || dep.Kinds[0] != "returns" {
		t.Fatalf("PackageDependency.Kinds = %+v, want [returns]", dep.Kinds)
	}

	idx := surface.Index()
	if !idx.HasSymbolID("api.NewRepo") {
		t.Fatalf("public index missing api.NewRepo")
	}
	if !idx.HasPackageDependency("api", "storage") {
		t.Fatalf("public index missing api -> storage")
	}
	if idx.HasPackageDependency("api", "internal/cache") {
		t.Fatalf("public index included private api -> internal/cache")
	}
}
