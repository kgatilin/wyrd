package d2_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/domain"
)

func TestReader_Read_HappyPath(t *testing.T) {
	tests := []struct {
		name    string
		d2File  string
		want    []domain.PackageModel
	}{
		{
			name: "parses simple package with interface",
			d2File: `
internal.service: {
  label: "internal/service"

  ModelReader: {
    shape: class
    stereotype: "<<interface>>"

    "+Read(ctx context.Context, paths []string)": "([]domain.PackageModel, error)"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Interfaces: []domain.InterfaceDef{
						{
							Name:       "ModelReader",
							IsExported: true,
							Methods: []domain.MethodDef{
								{
									Name:       "Read",
									IsExported: true,
									Params: []domain.ParamDef{
										{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
										{Name: "paths", Type: domain.TypeRef{Name: "string", IsSlice: true}},
									},
									Returns: []domain.TypeRef{
										{Name: "PackageModel", Package: "domain", IsSlice: true},
										{Name: "error"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "parses package with struct fields and methods",
			d2File: `
internal.domain: {
  label: "internal/domain"

  PackageModel: {
    shape: class
    stereotype: "<<struct>>"

    "+Name string": ""
    "+Path string": ""
    "+SourceFiles()": "[]string"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "domain",
					Path: "internal/domain",
					Structs: []domain.StructDef{
						{
							Name:       "PackageModel",
							IsExported: true,
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
			},
		},
		{
			name: "parses package with factory function",
			d2File: `
internal.service: {
  label: "internal/service"

  NewService: {
    shape: class
    stereotype: "<<factory>>"

    "reader": "ModelReader"
    "writer": "ModelWriter"
    "return": "*Service"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Functions: []domain.FunctionDef{
						{
							Name:       "NewService",
							IsExported: true,
							Stereotype: domain.StereotypeFactory,
							Params: []domain.ParamDef{
								{Name: "reader", Type: domain.TypeRef{Name: "ModelReader"}},
								{Name: "writer", Type: domain.TypeRef{Name: "ModelWriter"}},
							},
							Returns: []domain.TypeRef{{Name: "Service", IsPointer: true}},
						},
					},
				},
			},
		},
		{
			name: "parses package with regular function",
			d2File: `
pkg: {
  label: "pkg"

  Helper: {
    shape: class
    stereotype: "<<function>>"

    "input": "string"
    "return": "error"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Functions: []domain.FunctionDef{
						{
							Name:       "Helper",
							IsExported: true,
							Stereotype: domain.StereotypeNone,
							Params: []domain.ParamDef{
								{Name: "input", Type: domain.TypeRef{Name: "string"}},
							},
							Returns: []domain.TypeRef{{Name: "error"}},
						},
					},
				},
			},
		},
		{
			name: "parses package with typedef/enum",
			d2File: `
internal.domain: {
  label: "internal/domain"

  Stereotype: {
    shape: class
    stereotype: "<<enum>>"

    "type": "string"
    "StereotypeNone": "const"
    "StereotypeService": "const"
    "StereotypeFactory": "const"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "domain",
					Path: "internal/domain",
					TypeDefs: []domain.TypeDef{
						{
							Name:           "Stereotype",
							IsExported:     true,
							Stereotype:     domain.StereotypeEnum,
							UnderlyingType: domain.TypeRef{Name: "string"},
							Constants:      []string{"StereotypeNone", "StereotypeService", "StereotypeFactory"},
						},
					},
				},
			},
		},
		{
			name: "parses multiple packages from combined diagram",
			d2File: `
# Combined Architecture Diagram

legend: {
  label: "Color Legend (DDD)"
  near: top-right
}

# Packages

internal.service: {
  label: "internal/service"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "+Process()": "error"
  }
}

internal.domain: {
  label: "internal/domain"

  Entity: {
    shape: class
    stereotype: "<<struct>>"

    "+ID string": ""
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "service",
					Path: "internal/service",
					Interfaces: []domain.InterfaceDef{
						{
							Name:       "Service",
							IsExported: true,
							Methods: []domain.MethodDef{
								{
									Name:       "Process",
									IsExported: true,
									Returns:    []domain.TypeRef{{Name: "error"}},
								},
							},
						},
					},
				},
				{
					Name: "domain",
					Path: "internal/domain",
					Structs: []domain.StructDef{
						{
							Name:       "Entity",
							IsExported: true,
							Fields: []domain.FieldDef{
								{Name: "ID", Type: domain.TypeRef{Name: "string"}, IsExported: true},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file with D2 content
			tmpDir := t.TempDir()
			d2Path := filepath.Join(tmpDir, "test.d2")
			if err := os.WriteFile(d2Path, []byte(tt.d2File), 0644); err != nil {
				t.Fatalf("Failed to write D2 file: %v", err)
			}

			reader := d2.NewReader()
			got, err := reader.Read(context.Background(), []string{d2Path})
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("Read() got %d packages, want %d", len(got), len(tt.want))
			}

			for i := range got {
				assertPackageEqual(t, got[i], tt.want[i])
			}
		})
	}
}

func TestReader_Read_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		d2File  string
		want    []domain.PackageModel
	}{
		{
			name: "skips legend container",
			d2File: `
legend: {
  label: "Color Legend (DDD)"
  near: top-right

  Service: {
    shape: class
  }
}

internal.service: {
  label: "internal/service"

  Service: {
    shape: class
    stereotype: "<<interface>>"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name:       "service",
					Path:       "internal/service",
					Interfaces: []domain.InterfaceDef{{Name: "Service", IsExported: true}},
				},
			},
		},
		{
			name: "handles methods with multiple parameters",
			d2File: `
pkg: {
  label: "pkg"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "+Process(ctx context.Context, id string, opts Options)": "error"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Interfaces: []domain.InterfaceDef{
						{
							Name:       "Service",
							IsExported: true,
							Methods: []domain.MethodDef{
								{
									Name:       "Process",
									IsExported: true,
									Params: []domain.ParamDef{
										{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
										{Name: "id", Type: domain.TypeRef{Name: "string"}},
										{Name: "opts", Type: domain.TypeRef{Name: "Options"}},
									},
									Returns: []domain.TypeRef{{Name: "error"}},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "handles methods with multiple return types",
			d2File: `
pkg: {
  label: "pkg"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "+Get(id string)": "(Entity, error)"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Interfaces: []domain.InterfaceDef{
						{
							Name:       "Service",
							IsExported: true,
							Methods: []domain.MethodDef{
								{
									Name:       "Get",
									IsExported: true,
									Params: []domain.ParamDef{
										{Name: "id", Type: domain.TypeRef{Name: "string"}},
									},
									Returns: []domain.TypeRef{
										{Name: "Entity"},
										{Name: "error"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "handles pointer types",
			d2File: `
pkg: {
  label: "pkg"

  Service: {
    shape: class
    stereotype: "<<struct>>"

    "+Entity *Entity": ""
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Structs: []domain.StructDef{
						{
							Name:       "Service",
							IsExported: true,
							Fields: []domain.FieldDef{
								{Name: "Entity", Type: domain.TypeRef{Name: "Entity", IsPointer: true}, IsExported: true},
							},
						},
					},
				},
			},
		},
		{
			name: "handles slice types",
			d2File: `
pkg: {
  label: "pkg"

  Service: {
    shape: class
    stereotype: "<<struct>>"

    "+Items []string": ""
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Structs: []domain.StructDef{
						{
							Name:       "Service",
							IsExported: true,
							Fields: []domain.FieldDef{
								{Name: "Items", Type: domain.TypeRef{Name: "string", IsSlice: true}, IsExported: true},
							},
						},
					},
				},
			},
		},
		{
			name: "handles map types",
			d2File: `
pkg: {
  label: "pkg"

  Service: {
    shape: class
    stereotype: "<<struct>>"

    "+Cache map[string]Entity": ""
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Structs: []domain.StructDef{
						{
							Name:       "Service",
							IsExported: true,
							Fields: []domain.FieldDef{
								{
									Name:       "Cache",
									IsExported: true,
									Type: domain.TypeRef{
										Name:      "map",
										IsMap:     true,
										KeyType:   &domain.TypeRef{Name: "string"},
										ValueType: &domain.TypeRef{Name: "Entity"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "handles unexported symbols",
			d2File: `
pkg: {
  label: "pkg"

  helper: {
    shape: class
    stereotype: "<<struct>>"

    "-data string": ""
    "-process()": "error"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Structs: []domain.StructDef{
						{
							Name:       "helper",
							IsExported: false,
							Fields: []domain.FieldDef{
								{Name: "data", Type: domain.TypeRef{Name: "string"}, IsExported: false},
							},
							Methods: []domain.MethodDef{
								{
									Name:       "process",
									IsExported: false,
									Returns:    []domain.TypeRef{{Name: "error"}},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "handles package-qualified types",
			d2File: `
pkg: {
  label: "pkg"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "+Process(ctx context.Context)": "error"
  }
}
`,
			want: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Interfaces: []domain.InterfaceDef{
						{
							Name:       "Service",
							IsExported: true,
							Methods: []domain.MethodDef{
								{
									Name:       "Process",
									IsExported: true,
									Params: []domain.ParamDef{
										{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
									},
									Returns: []domain.TypeRef{{Name: "error"}},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			d2Path := filepath.Join(tmpDir, "test.d2")
			if err := os.WriteFile(d2Path, []byte(tt.d2File), 0644); err != nil {
				t.Fatalf("Failed to write D2 file: %v", err)
			}

			reader := d2.NewReader()
			got, err := reader.Read(context.Background(), []string{d2Path})
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("Read() got %d packages, want %d", len(got), len(tt.want))
			}

			for i := range got {
				assertPackageEqual(t, got[i], tt.want[i])
			}
		})
	}
}

func TestReader_Read_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		d2File  string
		wantErr error
	}{
		{
			name:    "returns ErrNoPackages when diagram has no package containers",
			d2File:  `# Just a comment`,
			wantErr: d2.ErrNoPackages,
		},
		{
			name: "returns ErrNoPackages when only legend exists",
			d2File: `
legend: {
  label: "Color Legend"
}
`,
			wantErr: d2.ErrNoPackages,
		},
		{
			name: "returns ErrEmptyDiagram when diagram has packages but no symbols",
			d2File: `
pkg: {
  style.fill: "#f8f8f8"
}
`,
			wantErr: d2.ErrNoPackages,
		},
		{
			name:    "returns ErrEmptyDiagram for empty file",
			d2File:  "",
			wantErr: d2.ErrEmptyDiagram,
		},
		{
			name:    "returns ErrEmptyDiagram for whitespace-only file",
			d2File:  "   \n\t\n  ",
			wantErr: d2.ErrEmptyDiagram,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			d2Path := filepath.Join(tmpDir, "test.d2")
			if err := os.WriteFile(d2Path, []byte(tt.d2File), 0644); err != nil {
				t.Fatalf("Failed to write D2 file: %v", err)
			}

			reader := d2.NewReader()
			_, err := reader.Read(context.Background(), []string{d2Path})
			if err == nil {
				t.Fatal("Read() expected error, got nil")
			}

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Read() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestReader_Read_ContextCancellation(t *testing.T) {
	d2File := `
pkg: {
  label: "pkg"
  Service: {
    shape: class
    stereotype: "<<interface>>"
  }
}
`
	tmpDir := t.TempDir()
	d2Path := filepath.Join(tmpDir, "test.d2")
	if err := os.WriteFile(d2Path, []byte(d2File), 0644); err != nil {
		t.Fatalf("Failed to write D2 file: %v", err)
	}

	reader := d2.NewReader()

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.Read(ctx, []string{d2Path})
	if err == nil {
		t.Fatal("Read() expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("Read() error = %v, want context.Canceled", err)
	}
}

func TestReader_Read_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first D2 file
	d2File1 := `
internal.service: {
  label: "internal/service"
  Service: {
    shape: class
    stereotype: "<<interface>>"
  }
}
`
	d2Path1 := filepath.Join(tmpDir, "service.d2")
	if err := os.WriteFile(d2Path1, []byte(d2File1), 0644); err != nil {
		t.Fatalf("Failed to write D2 file 1: %v", err)
	}

	// Create second D2 file
	d2File2 := `
internal.domain: {
  label: "internal/domain"
  Entity: {
    shape: class
    stereotype: "<<struct>>"
  }
}
`
	d2Path2 := filepath.Join(tmpDir, "domain.d2")
	if err := os.WriteFile(d2Path2, []byte(d2File2), 0644); err != nil {
		t.Fatalf("Failed to write D2 file 2: %v", err)
	}

	reader := d2.NewReader()
	packages, err := reader.Read(context.Background(), []string{d2Path1, d2Path2})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if len(packages) != 2 {
		t.Fatalf("Read() got %d packages, want 2", len(packages))
	}

	// Verify we got both packages
	gotPaths := make(map[string]bool)
	for _, pkg := range packages {
		gotPaths[pkg.Path] = true
	}

	if !gotPaths["internal/service"] {
		t.Error("Read() missing internal/service package")
	}
	if !gotPaths["internal/domain"] {
		t.Error("Read() missing internal/domain package")
	}
}

func TestReader_Read_FileNotFound(t *testing.T) {
	reader := d2.NewReader()
	_, err := reader.Read(context.Background(), []string{"/nonexistent/file.d2"})
	if err == nil {
		t.Fatal("Read() expected error for nonexistent file")
	}
}

func TestReader_Read_InvalidD2Syntax(t *testing.T) {
	tmpDir := t.TempDir()
	d2Path := filepath.Join(tmpDir, "invalid.d2")

	// Write invalid D2 syntax
	invalidD2 := `
pkg: {
  label: "pkg"
  invalid syntax here }{[
}
`
	if err := os.WriteFile(d2Path, []byte(invalidD2), 0644); err != nil {
		t.Fatalf("Failed to write D2 file: %v", err)
	}

	reader := d2.NewReader()
	_, err := reader.Read(context.Background(), []string{d2Path})
	if err == nil {
		t.Fatal("Read() expected error for invalid D2 syntax")
	}
}

func TestReader_Read_EmptyPaths(t *testing.T) {
	reader := d2.NewReader()
	_, err := reader.Read(context.Background(), []string{})
	if err == nil {
		t.Fatal("Read() expected error for empty paths")
	}
	if !errors.Is(err, d2.ErrEmptyDiagram) {
		t.Errorf("Read() error = %v, want ErrEmptyDiagram", err)
	}
}

func TestReader_Read_SplitMode(t *testing.T) {
	// Split-mode files have file groups with .go labels instead of package containers
	d2File := `
# service package

# Legend
legend: {
  label: "Color Legend (DDD)"
  near: top-right
}

# Files
service: {
  label: "service.go"
  style.fill: "#f0e8fc"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "+Process()": "error"
  }
}

factory: {
  label: "factory.go"
  style.fill: "#e8fce8"

  NewService: {
    shape: class
    stereotype: "<<factory>>"

    "return": "*Service"
  }
}
`
	tmpDir := t.TempDir()

	// Create the file in a .arch directory to simulate real usage
	archDir := filepath.Join(tmpDir, "internal", "service", ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatalf("Failed to create .arch directory: %v", err)
	}
	d2Path := filepath.Join(archDir, "pub.d2")
	if err := os.WriteFile(d2Path, []byte(d2File), 0644); err != nil {
		t.Fatalf("Failed to write D2 file: %v", err)
	}

	reader := d2.NewReader()
	packages, err := reader.Read(context.Background(), []string{d2Path})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Should return exactly 1 package (not 2 file groups)
	if len(packages) != 1 {
		t.Fatalf("Read() got %d packages, want 1", len(packages))
	}

	pkg := packages[0]

	// Package path should be derived from file location
	expectedPath := filepath.Join(tmpDir, "internal", "service")
	if pkg.Path != expectedPath {
		t.Errorf("PackageModel.Path = %q, want %q", pkg.Path, expectedPath)
	}
	if pkg.Name != "service" {
		t.Errorf("PackageModel.Name = %q, want %q", pkg.Name, "service")
	}

	// Should have both the interface and the factory function
	if len(pkg.Interfaces) != 1 {
		t.Errorf("PackageModel.Interfaces count = %d, want 1", len(pkg.Interfaces))
	}
	if len(pkg.Functions) != 1 {
		t.Errorf("PackageModel.Functions count = %d, want 1", len(pkg.Functions))
	}

	// Verify interface
	if len(pkg.Interfaces) > 0 && pkg.Interfaces[0].Name != "Service" {
		t.Errorf("Interface name = %q, want %q", pkg.Interfaces[0].Name, "Service")
	}

	// Verify function
	if len(pkg.Functions) > 0 && pkg.Functions[0].Name != "NewService" {
		t.Errorf("Function name = %q, want %q", pkg.Functions[0].Name, "NewService")
	}
}

// assertPackageEqual compares two PackageModel instances for equality.
func assertPackageEqual(t *testing.T, got, want domain.PackageModel) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("PackageModel.Name = %q, want %q", got.Name, want.Name)
	}
	if got.Path != want.Path {
		t.Errorf("PackageModel.Path = %q, want %q", got.Path, want.Path)
	}

	// Compare interfaces
	if len(got.Interfaces) != len(want.Interfaces) {
		t.Errorf("PackageModel.Interfaces count = %d, want %d", len(got.Interfaces), len(want.Interfaces))
	} else {
		for i := range got.Interfaces {
			assertInterfaceEqual(t, got.Interfaces[i], want.Interfaces[i])
		}
	}

	// Compare structs
	if len(got.Structs) != len(want.Structs) {
		t.Errorf("PackageModel.Structs count = %d, want %d", len(got.Structs), len(want.Structs))
	} else {
		for i := range got.Structs {
			assertStructEqual(t, got.Structs[i], want.Structs[i])
		}
	}

	// Compare functions
	if len(got.Functions) != len(want.Functions) {
		t.Errorf("PackageModel.Functions count = %d, want %d", len(got.Functions), len(want.Functions))
	} else {
		for i := range got.Functions {
			assertFunctionEqual(t, got.Functions[i], want.Functions[i])
		}
	}

	// Compare typedefs
	if len(got.TypeDefs) != len(want.TypeDefs) {
		t.Errorf("PackageModel.TypeDefs count = %d, want %d", len(got.TypeDefs), len(want.TypeDefs))
	} else {
		for i := range got.TypeDefs {
			assertTypeDefEqual(t, got.TypeDefs[i], want.TypeDefs[i])
		}
	}
}

func assertInterfaceEqual(t *testing.T, got, want domain.InterfaceDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("InterfaceDef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.IsExported != want.IsExported {
		t.Errorf("InterfaceDef.IsExported = %v, want %v", got.IsExported, want.IsExported)
	}

	if len(got.Methods) != len(want.Methods) {
		t.Errorf("InterfaceDef.Methods count = %d, want %d", len(got.Methods), len(want.Methods))
	} else {
		for i := range got.Methods {
			assertMethodEqual(t, got.Methods[i], want.Methods[i])
		}
	}
}

func assertStructEqual(t *testing.T, got, want domain.StructDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("StructDef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.IsExported != want.IsExported {
		t.Errorf("StructDef.IsExported = %v, want %v", got.IsExported, want.IsExported)
	}

	if len(got.Fields) != len(want.Fields) {
		t.Errorf("StructDef.Fields count = %d, want %d", len(got.Fields), len(want.Fields))
	} else {
		for i := range got.Fields {
			assertFieldEqual(t, got.Fields[i], want.Fields[i])
		}
	}

	if len(got.Methods) != len(want.Methods) {
		t.Errorf("StructDef.Methods count = %d, want %d", len(got.Methods), len(want.Methods))
	} else {
		for i := range got.Methods {
			assertMethodEqual(t, got.Methods[i], want.Methods[i])
		}
	}
}

func assertFunctionEqual(t *testing.T, got, want domain.FunctionDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("FunctionDef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.IsExported != want.IsExported {
		t.Errorf("FunctionDef.IsExported = %v, want %v", got.IsExported, want.IsExported)
	}
	if got.Stereotype != want.Stereotype {
		t.Errorf("FunctionDef.Stereotype = %v, want %v", got.Stereotype, want.Stereotype)
	}

	if len(got.Params) != len(want.Params) {
		t.Errorf("FunctionDef.Params count = %d, want %d", len(got.Params), len(want.Params))
	} else {
		for i := range got.Params {
			assertParamEqual(t, got.Params[i], want.Params[i])
		}
	}

	if len(got.Returns) != len(want.Returns) {
		t.Errorf("FunctionDef.Returns count = %d, want %d", len(got.Returns), len(want.Returns))
	} else {
		for i := range got.Returns {
			assertTypeRefEqual(t, got.Returns[i], want.Returns[i])
		}
	}
}

func assertTypeDefEqual(t *testing.T, got, want domain.TypeDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("TypeDef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.IsExported != want.IsExported {
		t.Errorf("TypeDef.IsExported = %v, want %v", got.IsExported, want.IsExported)
	}
	if got.Stereotype != want.Stereotype {
		t.Errorf("TypeDef.Stereotype = %v, want %v", got.Stereotype, want.Stereotype)
	}

	assertTypeRefEqual(t, got.UnderlyingType, want.UnderlyingType)

	if len(got.Constants) != len(want.Constants) {
		t.Errorf("TypeDef.Constants count = %d, want %d", len(got.Constants), len(want.Constants))
	} else {
		for i := range got.Constants {
			if got.Constants[i] != want.Constants[i] {
				t.Errorf("TypeDef.Constants[%d] = %q, want %q", i, got.Constants[i], want.Constants[i])
			}
		}
	}
}

func assertMethodEqual(t *testing.T, got, want domain.MethodDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("MethodDef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.IsExported != want.IsExported {
		t.Errorf("MethodDef.IsExported = %v, want %v", got.IsExported, want.IsExported)
	}

	if len(got.Params) != len(want.Params) {
		t.Errorf("MethodDef.Params count = %d, want %d", len(got.Params), len(want.Params))
	} else {
		for i := range got.Params {
			assertParamEqual(t, got.Params[i], want.Params[i])
		}
	}

	if len(got.Returns) != len(want.Returns) {
		t.Errorf("MethodDef.Returns count = %d, want %d", len(got.Returns), len(want.Returns))
	} else {
		for i := range got.Returns {
			assertTypeRefEqual(t, got.Returns[i], want.Returns[i])
		}
	}
}

func assertFieldEqual(t *testing.T, got, want domain.FieldDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("FieldDef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.IsExported != want.IsExported {
		t.Errorf("FieldDef.IsExported = %v, want %v", got.IsExported, want.IsExported)
	}

	assertTypeRefEqual(t, got.Type, want.Type)
}

func assertParamEqual(t *testing.T, got, want domain.ParamDef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("ParamDef.Name = %q, want %q", got.Name, want.Name)
	}

	assertTypeRefEqual(t, got.Type, want.Type)
}

func assertTypeRefEqual(t *testing.T, got, want domain.TypeRef) {
	t.Helper()

	if got.Name != want.Name {
		t.Errorf("TypeRef.Name = %q, want %q", got.Name, want.Name)
	}
	if got.Package != want.Package {
		t.Errorf("TypeRef.Package = %q, want %q", got.Package, want.Package)
	}
	if got.IsPointer != want.IsPointer {
		t.Errorf("TypeRef.IsPointer = %v, want %v", got.IsPointer, want.IsPointer)
	}
	if got.IsSlice != want.IsSlice {
		t.Errorf("TypeRef.IsSlice = %v, want %v", got.IsSlice, want.IsSlice)
	}
	if got.IsMap != want.IsMap {
		t.Errorf("TypeRef.IsMap = %v, want %v", got.IsMap, want.IsMap)
	}

	if got.KeyType != nil && want.KeyType != nil {
		assertTypeRefEqual(t, *got.KeyType, *want.KeyType)
	} else if (got.KeyType == nil) != (want.KeyType == nil) {
		t.Errorf("TypeRef.KeyType presence mismatch: got %v, want %v", got.KeyType != nil, want.KeyType != nil)
	}

	if got.ValueType != nil && want.ValueType != nil {
		assertTypeRefEqual(t, *got.ValueType, *want.ValueType)
	} else if (got.ValueType == nil) != (want.ValueType == nil) {
		t.Errorf("TypeRef.ValueType presence mismatch: got %v, want %v", got.ValueType != nil, want.ValueType != nil)
	}
}
