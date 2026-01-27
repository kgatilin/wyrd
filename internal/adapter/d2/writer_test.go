package d2_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/domain"
)

func TestWriter_Write(t *testing.T) {
	tests := []struct {
		name       string
		model      domain.PackageModel
		publicOnly bool
		wantParts  []string // Parts that should be in the output
	}{
		{
			name: "writes basic package with interface",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Interfaces: []domain.InterfaceDef{
					{
						Name:       "Service",
						IsExported: true,
						SourceFile: "service.go",
						Stereotype: domain.StereotypeService,
						Methods: []domain.MethodDef{
							{
								Name:       "Generate",
								IsExported: true,
								Params: []domain.ParamDef{
									{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
								},
								Returns: []domain.TypeRef{
									{Name: "error"},
								},
							},
						},
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				"# service package",
				"# Legend",
				"legend:",
				"# Files",
				"service: {",
				`label: "service.go"`,
				"Service: {",
				"shape: class",
				`stereotype: "<<interface>>"`,
				`"+Generate(ctx context.Context)"`,
				`"error"`,
			},
		},
		{
			name: "writes struct with fields and methods",
			model: domain.PackageModel{
				Name: "domain",
				Path: "internal/domain",
				Structs: []domain.StructDef{
					{
						Name:       "PackageModel",
						IsExported: true,
						SourceFile: "package.go",
						Stereotype: domain.StereotypeAggregate,
						Fields: []domain.FieldDef{
							{Name: "Name", Type: domain.TypeRef{Name: "string"}, IsExported: true},
							{Name: "Path", Type: domain.TypeRef{Name: "string"}, IsExported: true},
						},
						Methods: []domain.MethodDef{
							{
								Name:       "SourceFiles",
								IsExported: true,
								Returns:    []domain.TypeRef{{Name: "string", IsSlice: true}},
							},
						},
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				"# domain package",
				"PackageModel: {",
				"shape: class",
				`stereotype: "<<struct>>"`,
				`"+Name string"`,
				`"+Path string"`,
				`"+SourceFiles()"`,
				`"[]string"`,
			},
		},
		{
			name: "writes functions including factories",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Functions: []domain.FunctionDef{
					{
						Name:       "NewService",
						IsExported: true,
						SourceFile: "factory.go",
						Stereotype: domain.StereotypeFactory,
						Returns:    []domain.TypeRef{{Name: "Service", IsPointer: true}},
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				"NewService: {",
				"shape: class",
				`stereotype: "<<factory>>"`,
				`"return": "*Service"`,
			},
		},
		{
			name: "writes type definitions with constants",
			model: domain.PackageModel{
				Name: "domain",
				Path: "internal/domain",
				TypeDefs: []domain.TypeDef{
					{
						Name:           "Stereotype",
						IsExported:     true,
						SourceFile:     "stereotype.go",
						UnderlyingType: domain.TypeRef{Name: "string"},
						Stereotype:     domain.StereotypeEnum,
						Constants:      []string{"StereotypeNone", "StereotypeService"},
					},
				},
			},
			publicOnly: false,
			wantParts: []string{
				"Stereotype: {",
				"shape: class",
				`stereotype: "<<enum>>"`,
				`"type": "string"`,
				`"StereotypeNone": "const"`,
				`"StereotypeService": "const"`,
			},
		},
		{
			name: "filters unexported symbols in public only mode",
			model: domain.PackageModel{
				Name: "service",
				Path: "internal/service",
				Interfaces: []domain.InterfaceDef{
					{
						Name:       "Service",
						IsExported: true,
						SourceFile: "service.go",
						Stereotype: domain.StereotypeService,
						Methods: []domain.MethodDef{
							{Name: "Generate", IsExported: true},
							{Name: "validate", IsExported: false},
						},
					},
					{
						Name:       "helper",
						IsExported: false,
						SourceFile: "helper.go",
					},
				},
			},
			publicOnly: true,
			wantParts: []string{
				"Service: {",
				`"+Generate()"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for output
			tmpDir := t.TempDir()
			outputPath := filepath.Join(tmpDir, ".arch", "test.d2")

			writer := d2.NewWriter()
			opts := domain.WriteOptions{
				OutputPath: outputPath,
				PublicOnly: tt.publicOnly,
			}

			err := writer.Write(context.Background(), tt.model, opts)
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			// Read the generated file
			content, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("Failed to read output file: %v", err)
			}

			contentStr := string(content)

			// Check for expected parts
			for _, part := range tt.wantParts {
				if !strings.Contains(contentStr, part) {
					t.Errorf("Output missing expected part: %q\nGot:\n%s", part, contentStr)
				}
			}
		})
	}
}

func TestWriter_Write_ToStdout(t *testing.T) {
	model := domain.PackageModel{
		Name: "test",
		Path: "test",
		Interfaces: []domain.InterfaceDef{
			{
				Name:       "TestInterface",
				IsExported: true,
				SourceFile: "test.go",
			},
		},
	}

	writer := d2.NewWriter()
	opts := domain.WriteOptions{
		ToStdout: true,
	}

	// Should not error when writing to stdout
	err := writer.Write(context.Background(), model, opts)
	if err != nil {
		t.Fatalf("Write() to stdout error = %v", err)
	}
}

func TestWriter_Write_ContextCancellation(t *testing.T) {
	model := domain.PackageModel{
		Name: "test",
		Path: "test",
	}

	writer := d2.NewWriter()

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := domain.WriteOptions{
		ToStdout: true,
	}

	err := writer.Write(ctx, model, opts)
	if err == nil {
		t.Fatal("Write() expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("Write() error = %v, want context.Canceled", err)
	}
}

func TestWriter_Write_MissingOutputPath(t *testing.T) {
	model := domain.PackageModel{
		Name: "test",
		Path: "test",
	}

	writer := d2.NewWriter()
	opts := domain.WriteOptions{
		OutputPath: "",
		ToStdout:   false,
	}

	err := writer.Write(context.Background(), model, opts)
	if err == nil {
		t.Fatal("Write() expected error for missing output path")
	}
	if !strings.Contains(err.Error(), "output path is required") {
		t.Errorf("Write() error = %v, want error about output path", err)
	}
}

func TestWriter_Write_Dependencies(t *testing.T) {
	model := domain.PackageModel{
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
		Structs: []domain.StructDef{
			{
				Name:       "Options",
				IsExported: true,
				SourceFile: "options.go",
				Stereotype: domain.StereotypeValue,
			},
		},
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{
					Package: "internal/service",
					File:    "service.go",
					Symbol:  "Service",
				},
				To: domain.SymbolRef{
					Package: "internal/service",
					File:    "options.go",
					Symbol:  "Options",
				},
				Kind: domain.DependencyUses,
			},
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test.d2")

	writer := d2.NewWriter()
	opts := domain.WriteOptions{
		OutputPath: outputPath,
		PublicOnly: false,
	}

	err := writer.Write(context.Background(), model, opts)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	contentStr := string(content)

	// Check for dependency
	wantDep := `service.Service -> options.Options: "uses"`
	if !strings.Contains(contentStr, wantDep) {
		t.Errorf("Output missing dependency: %q\nGot:\n%s", wantDep, contentStr)
	}
}

func TestWriter_WriteCombined(t *testing.T) {
	tests := []struct {
		name      string
		packages  []domain.PackageModel
		wantParts []string
	}{
		{
			name: "writes combined diagram for multiple packages",
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
				"# Combined Architecture Diagram",
				"# Legend",
				"# Packages",
				"internal.service: {",
				"internal.domain: {",
				"Service: {",
				"Entity: {",
			},
		},
		{
			name: "writes empty diagram for empty package list",
			packages: []domain.PackageModel{},
			wantParts: []string{
				"# Combined Architecture Diagram",
				"# Legend",
				"# Packages",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			outputPath := filepath.Join(tmpDir, "combined.d2")

			writer := d2.NewWriter()

			err := writer.WriteCombined(context.Background(), tt.packages, outputPath)
			if err != nil {
				t.Fatalf("WriteCombined() error = %v", err)
			}

			content, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("Failed to read output file: %v", err)
			}

			contentStr := string(content)
			for _, part := range tt.wantParts {
				if !strings.Contains(contentStr, part) {
					t.Errorf("Output missing expected part: %q\nGot:\n%s", part, contentStr)
				}
			}
		})
	}
}

func TestWriter_WriteCombined_ContextCancellation(t *testing.T) {
	packages := []domain.PackageModel{
		{Name: "test", Path: "test"},
	}

	writer := d2.NewWriter()

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "combined.d2")

	err := writer.WriteCombined(ctx, packages, outputPath)
	if err == nil {
		t.Fatal("WriteCombined() expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("WriteCombined() error = %v, want context.Canceled", err)
	}
}

func TestWriter_WriteCombined_CreatesDirectory(t *testing.T) {
	packages := []domain.PackageModel{
		{
			Name: "test",
			Path: "test",
			Interfaces: []domain.InterfaceDef{
				{Name: "Service", IsExported: true, SourceFile: "service.go"},
			},
		},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nested", "dirs", "combined.d2")

	writer := d2.NewWriter()

	err := writer.WriteCombined(context.Background(), packages, outputPath)
	if err != nil {
		t.Fatalf("WriteCombined() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("WriteCombined() did not create output file")
	}
}
