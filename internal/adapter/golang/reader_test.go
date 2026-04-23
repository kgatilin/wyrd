package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestReader_Read(t *testing.T) {
	// Create a temporary test package
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test.example/testpkg

go 1.21
`
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create a simple test file
	testCode := `package testpkg

import "context"

// Service is a test service interface.
// archspec:service
type Service interface {
	// Generate creates something.
	Generate(ctx context.Context, opts Options) (Result, error)

	// internal helper method
	validate(data string) error
}

// Options configures the service.
// archspec:value
type Options struct {
	Name string
	Count int
}

// Result represents the output.
type Result struct {
	Success bool
	Message string
}

// NewService creates a new Service.
// archspec:factory
func NewService() *ServiceImpl {
	return &ServiceImpl{}
}

// ServiceImpl implements Service.
type ServiceImpl struct {
	config Options
}

// Generate implements Service.Generate.
func (s *ServiceImpl) Generate(ctx context.Context, opts Options) (Result, error) {
	return Result{Success: true}, nil
}

func (s *ServiceImpl) validate(data string) error {
	return nil
}
`

	err = os.WriteFile(filepath.Join(tmpDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Test reading the package
	reader := NewReader()

	// Change to tmpDir to load the package
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	models, err := reader.Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Read() returned %d models, want 1", len(models))
	}

	model := models[0]

	// Verify package metadata
	if model.Name != "testpkg" {
		t.Errorf("PackageModel.Name = %q, want %q", model.Name, "testpkg")
	}

	// Verify interfaces
	if len(model.Interfaces) != 1 {
		t.Fatalf("PackageModel has %d interfaces, want 1", len(model.Interfaces))
	}

	serviceIface := model.Interfaces[0]
	if serviceIface.Name != "Service" {
		t.Errorf("Interface.Name = %q, want %q", serviceIface.Name, "Service")
	}
	if !serviceIface.IsExported {
		t.Errorf("Service interface should be exported")
	}
	if serviceIface.Stereotype != domain.StereotypeService {
		t.Errorf("Service.Stereotype = %q, want %q", serviceIface.Stereotype, domain.StereotypeService)
	}
	if len(serviceIface.Methods) != 2 {
		t.Errorf("Service has %d methods, want 2", len(serviceIface.Methods))
	}

	// Verify methods
	generateMethod := serviceIface.Methods[0]
	if generateMethod.Name != "Generate" {
		t.Errorf("Method.Name = %q, want %q", generateMethod.Name, "Generate")
	}
	if !generateMethod.IsExported {
		t.Errorf("Generate method should be exported")
	}
	if len(generateMethod.Params) != 2 {
		t.Errorf("Generate has %d params, want 2", len(generateMethod.Params))
	}
	if len(generateMethod.Returns) != 2 {
		t.Errorf("Generate has %d returns, want 2", len(generateMethod.Returns))
	}

	validateMethod := serviceIface.Methods[1]
	if validateMethod.Name != "validate" {
		t.Errorf("Method.Name = %q, want %q", validateMethod.Name, "validate")
	}
	if validateMethod.IsExported {
		t.Errorf("validate method should not be exported")
	}

	// Verify structs
	if len(model.Structs) < 2 {
		t.Fatalf("PackageModel has %d structs, want at least 2", len(model.Structs))
	}

	var optionsStruct, resultStruct *domain.StructDef
	for i := range model.Structs {
		if model.Structs[i].Name == "Options" {
			optionsStruct = &model.Structs[i]
		}
		if model.Structs[i].Name == "Result" {
			resultStruct = &model.Structs[i]
		}
	}

	if optionsStruct == nil {
		t.Fatal("Options struct not found")
	}
	if optionsStruct.Stereotype != domain.StereotypeValue {
		t.Errorf("Options.Stereotype = %q, want %q", optionsStruct.Stereotype, domain.StereotypeValue)
	}
	if len(optionsStruct.Fields) != 2 {
		t.Errorf("Options has %d fields, want 2", len(optionsStruct.Fields))
	}

	if resultStruct == nil {
		t.Fatal("Result struct not found")
	}
	if len(resultStruct.Fields) != 2 {
		t.Errorf("Result has %d fields, want 2", len(resultStruct.Fields))
	}

	// Verify functions
	if len(model.Functions) < 1 {
		t.Fatalf("PackageModel has %d functions, want at least 1", len(model.Functions))
	}

	var newServiceFunc *domain.FunctionDef
	for i := range model.Functions {
		if model.Functions[i].Name == "NewService" {
			newServiceFunc = &model.Functions[i]
			break
		}
	}

	if newServiceFunc == nil {
		t.Fatal("NewService function not found")
	}
	if newServiceFunc.Stereotype != domain.StereotypeFactory {
		t.Errorf("NewService.Stereotype = %q, want %q", newServiceFunc.Stereotype, domain.StereotypeFactory)
	}
	if !newServiceFunc.IsExported {
		t.Errorf("NewService should be exported")
	}
}

func TestReader_Read_ContextCancellation(t *testing.T) {
	reader := NewReader()

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reader.Read(ctx, []string{"."})
	if err == nil {
		t.Fatal("Read() expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("Read() error = %v, want context.Canceled", err)
	}
}

func TestReader_Read_InvalidPath(t *testing.T) {
	reader := NewReader()

	_, err := reader.Read(context.Background(), []string{"/nonexistent/path/that/does/not/exist"})
	if err == nil {
		t.Fatal("Read() expected error for invalid path")
	}
}

func TestReader_Read_MultiplePackages(t *testing.T) {
	// This test uses the actual project structure
	reader := NewReader()

	// Try to read internal/domain which should exist
	models, err := reader.Read(context.Background(), []string{"../../domain"})
	if err != nil {
		t.Skipf("Skipping test - could not read domain package: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("Read() returned no models")
	}

	// Verify we got at least some types
	model := models[0]
	totalSymbols := len(model.Interfaces) + len(model.Structs) + len(model.Functions) + len(model.TypeDefs)
	if totalSymbols == 0 {
		t.Errorf("Expected to find some symbols in domain package, got 0")
	}
}

func TestReader_ExportsVsUnexports(t *testing.T) {
	tmpDir := t.TempDir()

	goMod := `module test.example/exporttest

go 1.21
`
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package exporttest

// PublicInterface is exported.
type PublicInterface interface {
	PublicMethod() string
	privateMethod() int
}

// privateInterface is not exported.
type privateInterface interface {
	method() string
}

// PublicStruct is exported.
type PublicStruct struct {
	PublicField  string
	privateField int
}

// privateStruct is not exported.
type privateStruct struct {
	field string
}

// PublicFunc is exported.
func PublicFunc() {}

// privateFunc is not exported.
func privateFunc() {}
`

	err = os.WriteFile(filepath.Join(tmpDir, "exports.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create exports.go: %v", err)
	}

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	reader := NewReader()
	models, err := reader.Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Read() returned %d models, want 1", len(models))
	}

	model := models[0]

	// Check interfaces
	if len(model.Interfaces) != 2 {
		t.Errorf("Expected 2 interfaces (both exported and unexported), got %d", len(model.Interfaces))
	}

	publicIfaceCount := 0
	privateIfaceCount := 0
	for _, iface := range model.Interfaces {
		if iface.IsExported {
			publicIfaceCount++
			if iface.Name != "PublicInterface" {
				t.Errorf("Unexpected exported interface: %s", iface.Name)
			}
			// Check methods
			if len(iface.Methods) != 2 {
				t.Errorf("PublicInterface should have 2 methods, got %d", len(iface.Methods))
			}
		} else {
			privateIfaceCount++
		}
	}

	if publicIfaceCount != 1 {
		t.Errorf("Expected 1 exported interface, got %d", publicIfaceCount)
	}
	if privateIfaceCount != 1 {
		t.Errorf("Expected 1 unexported interface, got %d", privateIfaceCount)
	}

	// Check structs
	if len(model.Structs) < 2 {
		t.Errorf("Expected at least 2 structs, got %d", len(model.Structs))
	}

	publicStructCount := 0
	for _, s := range model.Structs {
		if s.IsExported && s.Name == "PublicStruct" {
			publicStructCount++
			// Check fields
			if len(s.Fields) != 2 {
				t.Errorf("PublicStruct should have 2 fields, got %d", len(s.Fields))
			}
			exportedFields := 0
			for _, f := range s.Fields {
				if f.IsExported {
					exportedFields++
				}
			}
			if exportedFields != 1 {
				t.Errorf("PublicStruct should have 1 exported field, got %d", exportedFields)
			}
		}
	}

	if publicStructCount != 1 {
		t.Errorf("Expected 1 PublicStruct, got %d", publicStructCount)
	}

	// Check functions
	if len(model.Functions) < 2 {
		t.Errorf("Expected at least 2 functions, got %d", len(model.Functions))
	}

	publicFuncCount := 0
	for _, f := range model.Functions {
		if f.IsExported && f.Name == "PublicFunc" {
			publicFuncCount++
		}
	}

	if publicFuncCount != 1 {
		t.Errorf("Expected 1 PublicFunc, got %d", publicFuncCount)
	}
}

// loadSingleModelFromSources writes the given files into a fresh Go module
// under a temp directory and loads the resulting package. It returns the
// single extracted PackageModel.
func loadSingleModelFromSources(t *testing.T, sources map[string]string) domain.PackageModel {
	t.Helper()
	tmpDir := t.TempDir()

	goMod := "module test.example/m2a\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	for name, content := range sources {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	r := NewReader()
	models, err := r.Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 model, got %d", len(models))
	}
	return models[0]
}

// TestReader_ExtractsStandaloneConstants verifies that plain package-level
// constants surface in PackageModel.Constants and that enum constants are
// left on their owning TypeDef rather than duplicated.
func TestReader_ExtractsStandaloneConstants(t *testing.T) {
	model := loadSingleModelFromSources(t, map[string]string{
		"p.go": `package p

// MaxRetries caps the number of retries.
const MaxRetries = 5

// DefaultName is the default name.
const DefaultName = "bob"

// Pi is an untyped float const.
const Pi = 3.14

// internalConst is unexported.
const internalConst = 42

// Status is an enum-like type.
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)
`,
	})

	got := make(map[string]domain.ConstDef)
	for _, c := range model.Constants {
		got[c.Name] = c
	}

	for _, want := range []string{"MaxRetries", "DefaultName", "Pi", "internalConst"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected const %q in Constants, have %v", want, keys(got))
		}
	}
	for _, excl := range []string{"StatusActive", "StatusInactive"} {
		if _, ok := got[excl]; ok {
			t.Errorf("enum const %q must not appear in Constants", excl)
		}
	}

	if c := got["MaxRetries"]; c.Value != "5" || !c.IsExported {
		t.Errorf("MaxRetries: %+v", c)
	}
	if c := got["DefaultName"]; c.Value != `"bob"` {
		t.Errorf("DefaultName value: got %q, want %q", c.Value, `"bob"`)
	}
	if c := got["internalConst"]; c.IsExported {
		t.Errorf("internalConst should not be exported")
	}

	// TypeDef should still carry its enum constants.
	var status *domain.TypeDef
	for i := range model.TypeDefs {
		if model.TypeDefs[i].Name == "Status" {
			status = &model.TypeDefs[i]
			break
		}
	}
	if status == nil {
		t.Fatalf("Status TypeDef missing")
	}
	if len(status.Constants) != 2 {
		t.Errorf("Status enum constants: got %v, want 2", status.Constants)
	}
}

// TestReader_SplitsVarsAndErrors verifies that sentinel-error variables land
// in Errors while plain package-level vars land in Variables.
func TestReader_SplitsVarsAndErrors(t *testing.T) {
	model := loadSingleModelFromSources(t, map[string]string{
		"p.go": `package p

import (
	"errors"
	"fmt"
)

// ErrNotFound indicates a missing record.
var ErrNotFound = errors.New("not found")

// ErrBadArg is another sentinel error.
var ErrBadArg = fmt.Errorf("bad argument")

// Version is a plain string variable.
var Version = "1.2.3"

// Counter is a zero-valued int var.
var Counter int

// errInternal is an unexported sentinel error.
var errInternal = errors.New("internal")
`,
	})

	errs := make(map[string]string)
	for _, e := range model.Errors {
		errs[e.Name] = e.Message
	}
	if errs["ErrNotFound"] != "not found" {
		t.Errorf("ErrNotFound: got %q, want %q", errs["ErrNotFound"], "not found")
	}
	if errs["ErrBadArg"] != "bad argument" {
		t.Errorf("ErrBadArg: got %q, want %q", errs["ErrBadArg"], "bad argument")
	}
	if _, ok := errs["errInternal"]; !ok {
		t.Errorf("errInternal missing from Errors: %v", errs)
	}

	vars := make(map[string]bool)
	for _, v := range model.Variables {
		vars[v.Name] = true
	}
	for _, want := range []string{"Version", "Counter"} {
		if !vars[want] {
			t.Errorf("expected var %q in Variables, have %v", want, keys(vars))
		}
	}
	for _, excl := range []string{"ErrNotFound", "ErrBadArg", "errInternal"} {
		if vars[excl] {
			t.Errorf("sentinel error %q must not appear in Variables", excl)
		}
	}
}

// keys returns the sorted key set of a map[string]X for diagnostic output.
func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
