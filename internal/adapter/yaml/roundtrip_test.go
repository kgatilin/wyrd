package yaml

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// testPackageModel creates a representative PackageModel with all field types.
func testPackageModel() domain.PackageModel {
	return domain.PackageModel{
		Path:      "github.com/example/project/internal/service",
		Name:      "service",
		Layer:     "service",
		Aggregate: "Order",
		Interfaces: []domain.InterfaceDef{
			{
				Name:       "Repository",
				IsExported: true,
				SourceFile: "repository.go",
				Doc:        "Repository defines data access operations.",
				Stereotype: domain.StereotypeInterface,
				Methods: []domain.MethodDef{
					{
						Name:       "FindByID",
						IsExported: true,
						Params: []domain.ParamDef{
							{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
							{Name: "id", Type: domain.TypeRef{Name: "string"}},
						},
						Returns: []domain.TypeRef{
							{Name: "Entity", IsPointer: true},
							{Name: "error"},
						},
					},
				},
			},
		},
		Structs: []domain.StructDef{
			{
				Name:       "Handler",
				IsExported: true,
				SourceFile: "handler.go",
				Doc:        "Handler processes requests.",
				Stereotype: domain.StereotypeNone,
				Fields: []domain.FieldDef{
					{Name: "repo", Type: domain.TypeRef{Name: "Repository"}, IsExported: false},
					{Name: "Logger", Type: domain.TypeRef{Name: "Logger", Package: "log", IsPointer: true}, IsExported: true},
				},
				Methods: []domain.MethodDef{
					{
						Name:       "Handle",
						IsExported: true,
						Params: []domain.ParamDef{
							{Name: "req", Type: domain.TypeRef{Name: "Request", IsPointer: true}},
						},
						Returns: []domain.TypeRef{
							{Name: "Response", IsPointer: true},
							{Name: "error"},
						},
						Calls: []domain.CallEdge{
							{
								To: domain.SymbolRef{
									Package: "github.com/example/project/internal/service",
									File:    "repository.go",
									Symbol:  "MemRepo.FindByID",
								},
								Via: "service.Repository",
							},
						},
					},
				},
			},
		},
		Functions: []domain.FunctionDef{
			{
				Name:       "NewHandler",
				IsExported: true,
				SourceFile: "handler.go",
				Doc:        "NewHandler creates a new Handler.",
				Stereotype: domain.StereotypeFactory,
				Params: []domain.ParamDef{
					{Name: "repo", Type: domain.TypeRef{Name: "Repository"}},
				},
				Returns: []domain.TypeRef{
					{Name: "Handler", IsPointer: true},
				},
				Calls: []domain.CallEdge{
					{
						To: domain.SymbolRef{
							Package: "github.com/example/project/internal/service",
							File:    "handler.go",
							Symbol:  "validate",
						},
					},
				},
			},
		},
		TypeDefs: []domain.TypeDef{
			{
				Name:           "Status",
				UnderlyingType: domain.TypeRef{Name: "string"},
				Constants:      []string{"Active", "Inactive", "Pending"},
				IsExported:     true,
				SourceFile:     "status.go",
				Doc:            "Status represents entity state.",
				Stereotype:     domain.StereotypeEnum,
			},
		},
		Constants: []domain.ConstDef{
			{
				Name:       "MaxRetries",
				Type:       domain.TypeRef{Name: "int"},
				Value:      "5",
				IsExported: true,
				SourceFile: "config.go",
				Doc:        "MaxRetries caps retries.",
			},
		},
		Variables: []domain.VarDef{
			{
				Name:       "Version",
				Type:       domain.TypeRef{Name: "string"},
				IsExported: true,
				SourceFile: "version.go",
				Doc:        "Version is the build version.",
			},
		},
		Errors: []domain.ErrorDef{
			{
				Name:       "ErrNotFound",
				Message:    "not found",
				IsExported: true,
				SourceFile: "errors.go",
				Doc:        "ErrNotFound is returned when a lookup fails.",
			},
		},
		Implementations: []domain.Implementation{
			{
				Concrete: domain.SymbolRef{
					Package: "github.com/example/project/internal/service",
					File:    "handler.go",
					Symbol:  "Handler",
				},
				Interface: domain.SymbolRef{
					Package: "github.com/example/project/internal/service",
					File:    "repository.go",
					Symbol:  "Repository",
				},
				IsPointer: true,
			},
		},
		Dependencies: []domain.Dependency{
			{
				From: domain.SymbolRef{
					Package: "github.com/example/project/internal/service",
					File:    "handler.go",
					Symbol:  "NewHandler",
				},
				To: domain.SymbolRef{
					Package: "github.com/example/project/internal/service",
					File:    "repository.go",
					Symbol:  "Repository",
				},
				Kind:            domain.DependencyUses,
				ThroughExported: true,
			},
			{
				From: domain.SymbolRef{
					Package:  "github.com/example/project/internal/service",
					File:     "handler.go",
					Symbol:   "Handler",
					External: false,
				},
				To: domain.SymbolRef{
					Package:  "github.com/example/project/external/logger",
					Symbol:   "Logger",
					External: true,
				},
				Kind:            domain.DependencyUses,
				ThroughExported: true,
			},
		},
	}
}

// TestRoundtrip_SinglePackage writes a model to YAML and reads it back,
// verifying all fields survive the roundtrip.
func TestRoundtrip_SinglePackage(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "test.yaml")
	ctx := context.Background()

	original := testPackageModel()

	// Write
	w := NewWriter()
	err := w.Write(ctx, original, domain.WriteOptions{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read back
	r := NewReader()
	models, err := r.Read(ctx, []string{outputPath})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	got := models[0]

	// Verify top-level fields
	if got.Path != original.Path {
		t.Errorf("Path: got %q, want %q", got.Path, original.Path)
	}
	if got.Name != original.Name {
		t.Errorf("Name: got %q, want %q", got.Name, original.Name)
	}
	if got.Layer != original.Layer {
		t.Errorf("Layer: got %q, want %q", got.Layer, original.Layer)
	}
	if got.Aggregate != original.Aggregate {
		t.Errorf("Aggregate: got %q, want %q", got.Aggregate, original.Aggregate)
	}

	// Verify interfaces
	if len(got.Interfaces) != len(original.Interfaces) {
		t.Fatalf("Interfaces count: got %d, want %d", len(got.Interfaces), len(original.Interfaces))
	}
	iface := got.Interfaces[0]
	if iface.Name != "Repository" || !iface.IsExported || iface.SourceFile != "repository.go" {
		t.Errorf("Interface mismatch: %+v", iface)
	}
	if len(iface.Methods) != 1 || iface.Methods[0].Name != "FindByID" {
		t.Errorf("Interface methods mismatch: %+v", iface.Methods)
	}
	if len(iface.Methods[0].Params) != 2 {
		t.Errorf("Interface method params: got %d, want 2", len(iface.Methods[0].Params))
	}
	if iface.Methods[0].Params[0].Type.Package != "context" {
		t.Errorf("Param type package: got %q, want %q", iface.Methods[0].Params[0].Type.Package, "context")
	}

	// Verify structs
	if len(got.Structs) != len(original.Structs) {
		t.Fatalf("Structs count: got %d, want %d", len(got.Structs), len(original.Structs))
	}
	s := got.Structs[0]
	if s.Name != "Handler" || len(s.Fields) != 2 || len(s.Methods) != 1 {
		t.Errorf("Struct mismatch: name=%s fields=%d methods=%d", s.Name, len(s.Fields), len(s.Methods))
	}
	if s.Fields[1].Type.IsPointer != true || s.Fields[1].Type.Package != "log" {
		t.Errorf("Field type mismatch: %+v", s.Fields[1].Type)
	}

	// Verify functions
	if len(got.Functions) != len(original.Functions) {
		t.Fatalf("Functions count: got %d, want %d", len(got.Functions), len(original.Functions))
	}
	fn := got.Functions[0]
	if fn.Name != "NewHandler" || fn.Stereotype != domain.StereotypeFactory {
		t.Errorf("Function mismatch: %+v", fn)
	}

	// Verify type defs
	if len(got.TypeDefs) != len(original.TypeDefs) {
		t.Fatalf("TypeDefs count: got %d, want %d", len(got.TypeDefs), len(original.TypeDefs))
	}
	td := got.TypeDefs[0]
	if td.Name != "Status" || len(td.Constants) != 3 {
		t.Errorf("TypeDef mismatch: name=%s constants=%v", td.Name, td.Constants)
	}

	// Verify constants
	if len(got.Constants) != len(original.Constants) {
		t.Fatalf("Constants count: got %d, want %d", len(got.Constants), len(original.Constants))
	}
	c := got.Constants[0]
	if c.Name != "MaxRetries" || c.Value != "5" || c.Type.Name != "int" || !c.IsExported {
		t.Errorf("Constant mismatch: %+v", c)
	}

	// Verify variables
	if len(got.Variables) != len(original.Variables) {
		t.Fatalf("Variables count: got %d, want %d", len(got.Variables), len(original.Variables))
	}
	v := got.Variables[0]
	if v.Name != "Version" || v.Type.Name != "string" {
		t.Errorf("Variable mismatch: %+v", v)
	}

	// Verify errors
	if len(got.Errors) != len(original.Errors) {
		t.Fatalf("Errors count: got %d, want %d", len(got.Errors), len(original.Errors))
	}
	e := got.Errors[0]
	if e.Name != "ErrNotFound" || e.Message != "not found" {
		t.Errorf("Error mismatch: %+v", e)
	}

	// Verify dependencies
	if len(got.Dependencies) != len(original.Dependencies) {
		t.Fatalf("Dependencies count: got %d, want %d", len(got.Dependencies), len(original.Dependencies))
	}
	dep := got.Dependencies[0]
	if dep.From.Symbol != "NewHandler" || dep.To.Symbol != "Repository" || dep.Kind != domain.DependencyUses {
		t.Errorf("Dependency mismatch: %+v", dep)
	}
	dep2 := got.Dependencies[1]
	if !dep2.To.External {
		t.Errorf("Expected external dependency, got: %+v", dep2.To)
	}

	// Verify calls on function
	if len(fn.Calls) != 1 {
		t.Fatalf("Function Calls count: got %d, want 1", len(fn.Calls))
	}
	if fn.Calls[0].To.Symbol != "validate" || fn.Calls[0].Via != "" {
		t.Errorf("Function call mismatch: %+v", fn.Calls[0])
	}

	// Verify calls on method
	if len(s.Methods) != 1 || len(s.Methods[0].Calls) != 1 {
		t.Fatalf("Method Calls count: got %d, want 1", len(s.Methods[0].Calls))
	}
	mc := s.Methods[0].Calls[0]
	if mc.To.Symbol != "MemRepo.FindByID" || mc.Via != "service.Repository" {
		t.Errorf("Method call mismatch: %+v", mc)
	}

	// Verify implementations
	if len(got.Implementations) != len(original.Implementations) {
		t.Fatalf("Implementations count: got %d, want %d", len(got.Implementations), len(original.Implementations))
	}
	impl := got.Implementations[0]
	if impl.Concrete.Symbol != "Handler" || impl.Interface.Symbol != "Repository" {
		t.Errorf("Implementation symbols mismatch: %+v", impl)
	}
	if !impl.IsPointer {
		t.Errorf("Implementation IsPointer: got false, want true")
	}
}

// TestRoundtrip_YAML_Stability verifies that write→read→write produces identical YAML.
func TestRoundtrip_YAML_Stability(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "first.yaml")
	path2 := filepath.Join(dir, "second.yaml")
	ctx := context.Background()

	original := testPackageModel()
	w := NewWriter()
	r := NewReader()

	// First write
	if err := w.Write(ctx, original, domain.WriteOptions{OutputPath: path1}); err != nil {
		t.Fatalf("First write failed: %v", err)
	}

	// Read back
	models, err := r.Read(ctx, []string{path1})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Second write from read-back model
	if err := w.Write(ctx, models[0], domain.WriteOptions{OutputPath: path2}); err != nil {
		t.Fatalf("Second write failed: %v", err)
	}

	// Compare YAML output
	data1, _ := os.ReadFile(path1)
	data2, _ := os.ReadFile(path2)
	if string(data1) != string(data2) {
		t.Errorf("YAML not stable across roundtrip.\nFirst:\n%s\nSecond:\n%s", data1, data2)
	}
}

// TestRoundtrip_CombinedFormat tests write/read of multi-package combined format.
func TestRoundtrip_CombinedFormat(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "combined.yaml")
	ctx := context.Background()

	pkg1 := domain.PackageModel{
		Path: "github.com/example/pkg1",
		Name: "pkg1",
		Interfaces: []domain.InterfaceDef{
			{Name: "Service", IsExported: true, SourceFile: "service.go"},
		},
	}
	pkg2 := domain.PackageModel{
		Path: "github.com/example/pkg2",
		Name: "pkg2",
		Functions: []domain.FunctionDef{
			{Name: "NewThing", IsExported: true, SourceFile: "thing.go", Stereotype: domain.StereotypeFactory},
		},
	}

	w := NewWriter()
	if err := w.WriteCombined(ctx, []domain.PackageModel{pkg1, pkg2}, outputPath); err != nil {
		t.Fatalf("WriteCombined failed: %v", err)
	}

	r := NewReader()
	models, err := r.Read(ctx, []string{outputPath})
	if err != nil {
		t.Fatalf("Read combined failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// Packages are sorted by path in WriteCombined
	if models[0].Path != "github.com/example/pkg1" {
		t.Errorf("first package: got %q, want pkg1", models[0].Path)
	}
	if models[1].Path != "github.com/example/pkg2" {
		t.Errorf("second package: got %q, want pkg2", models[1].Path)
	}
	if len(models[0].Interfaces) != 1 {
		t.Errorf("pkg1 interfaces: got %d, want 1", len(models[0].Interfaces))
	}
	if len(models[1].Functions) != 1 {
		t.Errorf("pkg2 functions: got %d, want 1", len(models[1].Functions))
	}
}

// TestRoundtrip_MapAndSliceTypes verifies complex type references survive roundtrip.
func TestRoundtrip_MapAndSliceTypes(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "complex.yaml")
	ctx := context.Background()

	original := domain.PackageModel{
		Path: "github.com/example/types",
		Name: "types",
		Structs: []domain.StructDef{
			{
				Name:       "Cache",
				IsExported: true,
				SourceFile: "cache.go",
				Fields: []domain.FieldDef{
					{
						Name: "Items",
						Type: domain.TypeRef{
							Name:  "map",
							IsMap: true,
							KeyType: &domain.TypeRef{
								Name: "string",
							},
							ValueType: &domain.TypeRef{
								Name:    "Item",
								IsSlice: true,
							},
						},
						IsExported: true,
					},
				},
			},
		},
	}

	w := NewWriter()
	if err := w.Write(ctx, original, domain.WriteOptions{OutputPath: outputPath}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	r := NewReader()
	models, err := r.Read(ctx, []string{outputPath})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	field := models[0].Structs[0].Fields[0]
	if !field.Type.IsMap {
		t.Error("expected map type")
	}
	if field.Type.KeyType == nil || field.Type.KeyType.Name != "string" {
		t.Errorf("key type mismatch: %+v", field.Type.KeyType)
	}
	if field.Type.ValueType == nil || !field.Type.ValueType.IsSlice || field.Type.ValueType.Name != "Item" {
		t.Errorf("value type mismatch: %+v", field.Type.ValueType)
	}
}

// TestRoundtrip_DirectoryRead tests reading YAML files from .arch directories.
func TestRoundtrip_DirectoryRead(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, "mypackage", ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	model := domain.PackageModel{
		Path: "github.com/example/mypackage",
		Name: "mypackage",
		Functions: []domain.FunctionDef{
			{Name: "Run", IsExported: true, SourceFile: "run.go"},
		},
	}

	w := NewWriter()
	if err := w.Write(ctx, model, domain.WriteOptions{OutputPath: filepath.Join(archDir, "pub.yaml")}); err != nil {
		t.Fatal(err)
	}

	// Read using directory scan
	r := NewReader()
	models, err := r.Read(ctx, []string{dir})
	if err != nil {
		t.Fatalf("Directory read failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model from directory scan, got %d", len(models))
	}
	if models[0].Name != "mypackage" {
		t.Errorf("got name %q, want mypackage", models[0].Name)
	}
}

// TestRoundtrip_PublicOnly verifies that PublicOnly option filters unexported symbols.
func TestRoundtrip_PublicOnly(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "pub.yaml")
	ctx := context.Background()

	model := domain.PackageModel{
		Path: "github.com/example/mixed",
		Name: "mixed",
		Functions: []domain.FunctionDef{
			{Name: "PublicFunc", IsExported: true, SourceFile: "funcs.go"},
			{Name: "privateFunc", IsExported: false, SourceFile: "funcs.go"},
		},
		Structs: []domain.StructDef{
			{Name: "PublicStruct", IsExported: true, SourceFile: "types.go"},
			{Name: "privateStruct", IsExported: false, SourceFile: "types.go"},
		},
	}

	w := NewWriter()
	if err := w.Write(ctx, model, domain.WriteOptions{OutputPath: outputPath, PublicOnly: true}); err != nil {
		t.Fatal(err)
	}

	r := NewReader()
	models, err := r.Read(ctx, []string{outputPath})
	if err != nil {
		t.Fatal(err)
	}

	got := models[0]
	if len(got.Functions) != 1 || got.Functions[0].Name != "PublicFunc" {
		t.Errorf("expected only PublicFunc, got %+v", got.Functions)
	}
	if len(got.Structs) != 1 || got.Structs[0].Name != "PublicStruct" {
		t.Errorf("expected only PublicStruct, got %+v", got.Structs)
	}
}
