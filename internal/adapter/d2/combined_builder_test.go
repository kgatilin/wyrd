package d2

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestCombinedBuilder_Build(t *testing.T) {
	tests := []struct {
		name       string
		packages   []domain.PackageModel
		wantParts  []string
		wantAbsent []string
	}{
		{
			name: "generates header and legend",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Interfaces: []domain.InterfaceDef{
						{Name: "Service", IsExported: true, SourceFile: "service.go"},
					},
				},
			},
			wantParts: []string{
				"# Combined Architecture Diagram",
				"# Legend",
				"legend: {",
				`label: "Color Legend (DDD)"`,
				"near: top-right",
				"# Packages",
			},
		},
		{
			name: "creates package-level containers",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Interfaces: []domain.InterfaceDef{
						{Name: "Service", IsExported: true, SourceFile: "service.go"},
					},
				},
				{
					Name: "domain",
					Path: "internal/domain",
					Structs: []domain.StructDef{
						{Name: "Entity", IsExported: true, SourceFile: "entity.go"},
					},
				},
			},
			wantParts: []string{
				"internal.service: {",
				`label: "internal/service"`,
				"Service: {",
				"internal.domain: {",
				`label: "internal/domain"`,
				"Entity: {",
			},
		},
		{
			name: "includes only exported symbols",
			packages: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Interfaces: []domain.InterfaceDef{
						{Name: "PublicInterface", IsExported: true, SourceFile: "public.go"},
						{Name: "privateInterface", IsExported: false, SourceFile: "private.go"},
					},
					Structs: []domain.StructDef{
						{Name: "PublicStruct", IsExported: true, SourceFile: "public.go"},
						{Name: "privateStruct", IsExported: false, SourceFile: "private.go"},
					},
				},
			},
			wantParts: []string{
				"PublicInterface: {",
				"PublicStruct: {",
			},
			wantAbsent: []string{
				"privateInterface",
				"privateStruct",
			},
		},
		{
			name: "renders functions with correct stereotypes",
			packages: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Functions: []domain.FunctionDef{
						{
							Name:       "NewService",
							IsExported: true,
							SourceFile: "factory.go",
							Stereotype: domain.StereotypeFactory,
							Returns:    []domain.TypeRef{{Name: "Service", IsPointer: true}},
						},
						{
							Name:       "Helper",
							IsExported: true,
							SourceFile: "helper.go",
							Stereotype: domain.StereotypeNone,
						},
					},
				},
			},
			wantParts: []string{
				"NewService: {",
				`stereotype: "<<factory>>"`,
				"Helper: {",
				`stereotype: "<<function>>"`,
			},
		},
		{
			name: "renders type definitions with constants",
			packages: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					TypeDefs: []domain.TypeDef{
						{
							Name:           "Status",
							IsExported:     true,
							SourceFile:     "status.go",
							Stereotype:     domain.StereotypeEnum,
							UnderlyingType: domain.TypeRef{Name: "string"},
							Constants:      []string{"StatusActive", "StatusInactive"},
						},
					},
				},
			},
			wantParts: []string{
				"Status: {",
				`stereotype: "<<enum>>"`,
				`"type": "string"`,
				`"StatusActive": "const"`,
				`"StatusInactive": "const"`,
			},
		},
		{
			name: "sanitizes package paths for D2 identifiers",
			packages: []domain.PackageModel{
				{
					Name: "golang",
					Path: "internal/adapter/golang",
					Interfaces: []domain.InterfaceDef{
						{Name: "Reader", IsExported: true, SourceFile: "reader.go"},
					},
				},
			},
			wantParts: []string{
				"internal.adapter.golang: {",
				`label: "internal/adapter/golang"`,
			},
		},
		{
			name: "handles root package",
			packages: []domain.PackageModel{
				{
					Name: "main",
					Path: "",
					Functions: []domain.FunctionDef{
						{Name: "Main", IsExported: true, SourceFile: "main.go"},
					},
				},
			},
			wantParts: []string{
				"root: {",
				`label: "main"`,
				"Main: {",
			},
		},
		{
			name: "renders interface methods",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "service",
					Interfaces: []domain.InterfaceDef{
						{
							Name:       "Service",
							IsExported: true,
							SourceFile: "service.go",
							Methods: []domain.MethodDef{
								{
									Name:       "Process",
									IsExported: true,
									Params:     []domain.ParamDef{{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}}},
									Returns:    []domain.TypeRef{{Name: "error"}},
								},
								{Name: "helper", IsExported: false},
							},
						},
					},
				},
			},
			wantParts: []string{
				"Service: {",
				`stereotype: "<<interface>>"`,
				`"+Process(ctx context.Context)"`,
				`"error"`,
			},
			wantAbsent: []string{
				"helper",
			},
		},
		{
			name: "renders struct fields and methods",
			packages: []domain.PackageModel{
				{
					Name: "domain",
					Path: "domain",
					Structs: []domain.StructDef{
						{
							Name:       "Entity",
							IsExported: true,
							SourceFile: "entity.go",
							Fields: []domain.FieldDef{
								{Name: "ID", Type: domain.TypeRef{Name: "string"}, IsExported: true},
								{Name: "secret", Type: domain.TypeRef{Name: "string"}, IsExported: false},
							},
							Methods: []domain.MethodDef{
								{Name: "GetID", IsExported: true, Returns: []domain.TypeRef{{Name: "string"}}},
								{Name: "internal", IsExported: false},
							},
						},
					},
				},
			},
			wantParts: []string{
				"Entity: {",
				`stereotype: "<<struct>>"`,
				`"+ID string"`,
				`"+GetID()"`,
			},
			wantAbsent: []string{
				"secret",
				"internal",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newCombinedBuilder()
			got := builder.Build(tt.packages)

			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("Build() output missing expected part: %q\n\nGot:\n%s", part, got)
				}
			}

			for _, part := range tt.wantAbsent {
				if strings.Contains(got, part) {
					t.Errorf("Build() output contains unexpected part: %q\n\nGot:\n%s", part, got)
				}
			}
		})
	}
}

func TestCombinedBuilder_IntraPackageDependencies(t *testing.T) {
	packages := []domain.PackageModel{
		{
			Name: "service",
			Path: "internal/service",
			Interfaces: []domain.InterfaceDef{
				{Name: "Service", IsExported: true, SourceFile: "service.go"},
			},
			Structs: []domain.StructDef{
				{Name: "Options", IsExported: true, SourceFile: "options.go"},
			},
			Functions: []domain.FunctionDef{
				{Name: "NewService", IsExported: true, SourceFile: "factory.go", Stereotype: domain.StereotypeFactory},
			},
			Dependencies: []domain.Dependency{
				{
					From:            domain.SymbolRef{Package: "internal/service", Symbol: "NewService"},
					To:              domain.SymbolRef{Package: "internal/service", Symbol: "Service"},
					Kind:            domain.DependencyReturns,
					ThroughExported: true,
				},
				{
					From:            domain.SymbolRef{Package: "internal/service", Symbol: "NewService"},
					To:              domain.SymbolRef{Package: "internal/service", Symbol: "Options"},
					Kind:            domain.DependencyUses,
					ThroughExported: true,
				},
			},
		},
	}

	builder := newCombinedBuilder()
	got := builder.Build(packages)

	// Should contain intra-package dependencies
	wantParts := []string{
		"# Dependencies",
		`NewService -> Service: "returns"`,
		`NewService -> Options: "uses"`,
	}

	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Errorf("Build() output missing expected part: %q\n\nGot:\n%s", part, got)
		}
	}
}

func TestCombinedBuilder_CrossPackageDependencies(t *testing.T) {
	tests := []struct {
		name       string
		packages   []domain.PackageModel
		wantParts  []string
		wantAbsent []string
	}{
		{
			name: "renders cross-package dependencies",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Interfaces: []domain.InterfaceDef{
						{Name: "Service", IsExported: true, SourceFile: "service.go"},
					},
					Functions: []domain.FunctionDef{
						{Name: "NewService", IsExported: true, SourceFile: "factory.go"},
					},
					Dependencies: []domain.Dependency{
						{
							From:            domain.SymbolRef{Package: "internal/service", Symbol: "NewService"},
							To:              domain.SymbolRef{Package: "internal/domain", Symbol: "Entity"},
							Kind:            domain.DependencyReturns,
							ThroughExported: true,
						},
					},
				},
				{
					Name: "domain",
					Path: "internal/domain",
					Structs: []domain.StructDef{
						{Name: "Entity", IsExported: true, SourceFile: "entity.go"},
					},
				},
			},
			wantParts: []string{
				"# Cross-package dependencies",
				`internal.service.NewService -> internal.domain.Entity: "returns"`,
			},
		},
		{
			name: "skips dependencies to packages not in diagram",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Functions: []domain.FunctionDef{
						{Name: "NewService", IsExported: true, SourceFile: "factory.go"},
					},
					Dependencies: []domain.Dependency{
						{
							From:            domain.SymbolRef{Package: "internal/service", Symbol: "NewService"},
							To:              domain.SymbolRef{Package: "external/pkg", Symbol: "ExternalType"},
							Kind:            domain.DependencyUses,
							ThroughExported: true, // Even exported, target package not in diagram
						},
					},
				},
			},
			wantAbsent: []string{
				"# Cross-package dependencies",
				"external.pkg",
			},
		},
		{
			name: "skips dependencies from unexported methods",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Functions: []domain.FunctionDef{
						{Name: "helper", IsExported: false, SourceFile: "helper.go"},
					},
					Dependencies: []domain.Dependency{
						{
							From:            domain.SymbolRef{Package: "internal/service", Symbol: "helper"},
							To:              domain.SymbolRef{Package: "internal/domain", Symbol: "Entity"},
							Kind:            domain.DependencyUses,
							ThroughExported: false, // Not through exported method
						},
					},
				},
				{
					Name: "domain",
					Path: "internal/domain",
					Structs: []domain.StructDef{
						{Name: "Entity", IsExported: true, SourceFile: "entity.go"},
					},
				},
			},
			wantAbsent: []string{
				"# Cross-package dependencies",
				"helper",
			},
		},
		{
			name: "deduplicates dependencies",
			packages: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Functions: []domain.FunctionDef{
						{Name: "NewService", IsExported: true, SourceFile: "factory.go"},
					},
					Dependencies: []domain.Dependency{
						{
							From:            domain.SymbolRef{Package: "internal/service", Symbol: "NewService"},
							To:              domain.SymbolRef{Package: "internal/domain", Symbol: "Entity"},
							Kind:            domain.DependencyReturns,
							ThroughExported: true,
						},
						{
							From:            domain.SymbolRef{Package: "internal/service", Symbol: "NewService"},
							To:              domain.SymbolRef{Package: "internal/domain", Symbol: "Entity"},
							Kind:            domain.DependencyReturns,
							ThroughExported: true,
						},
					},
				},
				{
					Name: "domain",
					Path: "internal/domain",
					Structs: []domain.StructDef{
						{Name: "Entity", IsExported: true, SourceFile: "entity.go"},
					},
				},
			},
			wantParts: []string{
				`internal.service.NewService -> internal.domain.Entity: "returns"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newCombinedBuilder()
			got := builder.Build(tt.packages)

			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("Build() output missing expected part: %q\n\nGot:\n%s", part, got)
				}
			}

			for _, part := range tt.wantAbsent {
				if strings.Contains(got, part) {
					t.Errorf("Build() output contains unexpected part: %q\n\nGot:\n%s", part, got)
				}
			}

			// Count occurrences to verify deduplication
			if tt.name == "deduplicates dependencies" {
				count := strings.Count(got, `internal.service.NewService -> internal.domain.Entity`)
				if count != 1 {
					t.Errorf("Expected exactly 1 dependency arrow, found %d", count)
				}
			}
		})
	}
}

func TestCombinedBuilder_DeterministicOutput(t *testing.T) {
	// Create packages in different orders to verify sorting
	packagesOrder1 := []domain.PackageModel{
		{Name: "pkg1", Path: "pkg1", Interfaces: []domain.InterfaceDef{{Name: "A", IsExported: true, SourceFile: "a.go"}}},
		{Name: "pkg2", Path: "pkg2", Interfaces: []domain.InterfaceDef{{Name: "B", IsExported: true, SourceFile: "b.go"}}},
		{Name: "pkg3", Path: "pkg3", Interfaces: []domain.InterfaceDef{{Name: "C", IsExported: true, SourceFile: "c.go"}}},
	}

	packagesOrder2 := []domain.PackageModel{
		{Name: "pkg3", Path: "pkg3", Interfaces: []domain.InterfaceDef{{Name: "C", IsExported: true, SourceFile: "c.go"}}},
		{Name: "pkg1", Path: "pkg1", Interfaces: []domain.InterfaceDef{{Name: "A", IsExported: true, SourceFile: "a.go"}}},
		{Name: "pkg2", Path: "pkg2", Interfaces: []domain.InterfaceDef{{Name: "B", IsExported: true, SourceFile: "b.go"}}},
	}

	builder := newCombinedBuilder()
	output1 := builder.Build(packagesOrder1)
	output2 := builder.Build(packagesOrder2)

	if output1 != output2 {
		t.Error("Build() should produce deterministic output regardless of package order")
		t.Logf("Output1:\n%s", output1)
		t.Logf("Output2:\n%s", output2)
	}
}
