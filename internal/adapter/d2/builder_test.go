package d2

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestD2TextBuilder_Build(t *testing.T) {
	tests := []struct {
		name       string
		model      domain.PackageModel
		publicOnly bool
		wantParts  []string
		wantAbsent []string
	}{
		{
			name: "generates header and legend",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
			},
			publicOnly: false,
			wantParts: []string{
				"# service package",
				"# Legend",
				"legend: {",
				`label: "Color Legend (DDD)"`,
				"near: top-right",
				"# Files",
				"# Dependencies",
			},
		},
		{
			name: "groups symbols by file",
			model: domain.PackageModel{
				Name: "domain",
				Path: "internal/domain",
				Interfaces: []domain.InterfaceDef{
					{Name: "Model", IsExported: true, SourceFile: "model.go"},
				},
				Structs: []domain.StructDef{
					{Name: "Entity", IsExported: true, SourceFile: "entity.go"},
				},
			},
			publicOnly: false,
			wantParts: []string{
				"model: {",
				`label: "model.go"`,
				"Model: {",
				"entity: {",
				`label: "entity.go"`,
				"Entity: {",
			},
		},
		{
			name: "filters unexported in public mode",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Interfaces: []domain.InterfaceDef{
					{Name: "PublicService", IsExported: true, SourceFile: "service.go"},
					{Name: "privateHelper", IsExported: false, SourceFile: "helper.go"},
				},
				Structs: []domain.StructDef{
					{Name: "PublicStruct", IsExported: true, SourceFile: "struct.go"},
					{Name: "privateStruct", IsExported: false, SourceFile: "struct.go"},
				},
			},
			publicOnly: true,
			wantParts: []string{
				"PublicService: {",
				"PublicStruct: {",
			},
			wantAbsent: []string{
				"privateHelper",
				"privateStruct",
			},
		},
		{
			name: "includes method visibility prefixes",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Interfaces: []domain.InterfaceDef{
					{
						Name:       "Service",
						IsExported: true,
						SourceFile: "service.go",
						Methods: []domain.MethodDef{
							{Name: "PublicMethod", IsExported: true},
							{Name: "privateMethod", IsExported: false},
						},
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				`"+PublicMethod()"`,
				`"-privateMethod()"`,
			},
		},
		{
			name: "formats method parameters",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Interfaces: []domain.InterfaceDef{
					{
						Name:       "Service",
						IsExported: true,
						SourceFile: "service.go",
						Methods: []domain.MethodDef{
							{
								Name:       "Generate",
								IsExported: true,
								Params: []domain.ParamDef{
									{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
									{Name: "opts", Type: domain.TypeRef{Name: "Options"}},
								},
								Returns: []domain.TypeRef{
									{Name: "Result", IsSlice: true},
									{Name: "error"},
								},
							},
						},
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				`"+Generate(ctx context.Context, opts Options)"`,
				`"([]Result, error)"`,
			},
		},
		{
			name: "applies stereotype colors to file containers",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Interfaces: []domain.InterfaceDef{
					{
						Name:       "Service",
						IsExported: true,
						SourceFile: "service.go",
						Stereotype: domain.StereotypeService,
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				`style.fill: "#f0e8fc"`, // Purple for service
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newD2TextBuilder()
			got := builder.Build(tt.model, tt.publicOnly)

			// Check expected parts are present
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("Build() output missing expected part: %q\n\nGot:\n%s", part, got)
				}
			}

			// Check unexpected parts are absent
			for _, part := range tt.wantAbsent {
				if strings.Contains(got, part) {
					t.Errorf("Build() output contains unexpected part: %q\n\nGot:\n%s", part, got)
				}
			}
		})
	}
}

func TestD2TextBuilder_MethodSignatures(t *testing.T) {
	tests := []struct {
		name   string
		method domain.MethodDef
		want   string
	}{
		{
			name: "simple method no params no returns",
			method: domain.MethodDef{
				Name:       "DoSomething",
				IsExported: true,
			},
			want: `"+DoSomething()"`,
		},
		{
			name: "method with single return",
			method: domain.MethodDef{
				Name:       "GetValue",
				IsExported: true,
				Returns:    []domain.TypeRef{{Name: "string"}},
			},
			want: `"string"`,
		},
		{
			name: "method with multiple returns",
			method: domain.MethodDef{
				Name:       "GetValueWithError",
				IsExported: true,
				Returns:    []domain.TypeRef{{Name: "string"}, {Name: "error"}},
			},
			want: `"(string, error)"`,
		},
		{
			name: "method with pointer return",
			method: domain.MethodDef{
				Name:       "GetModel",
				IsExported: true,
				Returns:    []domain.TypeRef{{Name: "Model", IsPointer: true}},
			},
			want: `"*Model"`,
		},
		{
			name: "method with slice return",
			method: domain.MethodDef{
				Name:       "GetModels",
				IsExported: true,
				Returns:    []domain.TypeRef{{Name: "Model", IsSlice: true}},
			},
			want: `"[]Model"`,
		},
		{
			name: "unexported method",
			method: domain.MethodDef{
				Name:       "internalMethod",
				IsExported: false,
			},
			want: `"-internalMethod()"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := domain.PackageModel{
				Name: "test",
				Path: "test",
				Interfaces: []domain.InterfaceDef{
					{
						Name:       "TestInterface",
						IsExported: true,
						SourceFile: "test.go",
						Methods:    []domain.MethodDef{tt.method},
					},
				},
			}

			builder := newD2TextBuilder()
			got := builder.Build(model, false)

			if !strings.Contains(got, tt.want) {
				t.Errorf("Build() output missing expected signature part: %q\n\nGot:\n%s", tt.want, got)
			}
		})
	}
}

func TestD2TextBuilder_RendersImplementations(t *testing.T) {
	model := domain.PackageModel{
		Name: "svc",
		Path: "internal/svc",
		Interfaces: []domain.InterfaceDef{
			{Name: "Greeter", IsExported: true, SourceFile: "greeter.go"},
		},
		Structs: []domain.StructDef{
			{Name: "Hello", IsExported: true, SourceFile: "hello.go"},
		},
		Implementations: []domain.Implementation{
			{
				Concrete:  domain.SymbolRef{Package: "internal/svc", File: "hello.go", Symbol: "Hello"},
				Interface: domain.SymbolRef{Package: "internal/svc", File: "greeter.go", Symbol: "Greeter"},
				IsPointer: false,
			},
		},
	}

	builder := newD2TextBuilder()
	got := builder.Build(model, false)

	wantParts := []string{
		"# Implementations",
		`hello.Hello -> greeter.Greeter: "implements"`,
		"style.stroke-dash",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Errorf("Build() output missing %q\n\nGot:\n%s", want, got)
		}
	}
}

func TestD2TextBuilder_SkipsCrossPackageImplementations(t *testing.T) {
	model := domain.PackageModel{
		Name: "api",
		Path: "internal/api",
		Interfaces: []domain.InterfaceDef{
			{Name: "Service", IsExported: true, SourceFile: "api.go"},
		},
		Implementations: []domain.Implementation{
			{
				// Concrete from different package — can't be rendered here.
				Concrete:  domain.SymbolRef{Package: "internal/impl", File: "impl.go", Symbol: "Worker"},
				Interface: domain.SymbolRef{Package: "internal/api", File: "api.go", Symbol: "Service"},
			},
		},
	}

	builder := newD2TextBuilder()
	got := builder.Build(model, false)

	if strings.Contains(got, "# Implementations") {
		t.Errorf("expected no Implementations section for cross-package impl\n\nGot:\n%s", got)
	}
}
