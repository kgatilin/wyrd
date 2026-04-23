package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// writeTestModule creates a temporary Go module with given files and returns the module root.
func writeTestModule(t *testing.T, modPath string, files map[string]string) string {
	t.Helper()
	tmpDir := t.TempDir()
	goMod := "module " + modPath + "\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	for rel, content := range files {
		full := filepath.Join(tmpDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return tmpDir
}

// findImpl locates an implementation by concrete symbol name within a model.
func findImpl(impls []domain.Implementation, concrete, iface string) *domain.Implementation {
	for i := range impls {
		if impls[i].Concrete.Symbol == concrete && impls[i].Interface.Symbol == iface {
			return &impls[i]
		}
	}
	return nil
}

func TestImplementations_ValueReceiver(t *testing.T) {
	code := `package impls

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hi" }
`
	dir := writeTestModule(t, "test.example/impls", map[string]string{"impls.go": code})

	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	impls := models[0].Implementations
	got := findImpl(impls, "Hello", "Greeter")
	if got == nil {
		t.Fatalf("expected Hello -> Greeter implementation, got %+v", impls)
	}
	if got.IsPointer {
		t.Errorf("expected IsPointer=false for value-receiver impl, got true")
	}
}

func TestImplementations_PointerReceiverOnly(t *testing.T) {
	code := `package impls

type Writer interface {
	Write(p []byte) (int, error)
}

type Buffer struct{ data []byte }

func (b *Buffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
`
	dir := writeTestModule(t, "test.example/ptr", map[string]string{"ptr.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := findImpl(models[0].Implementations, "Buffer", "Writer")
	if got == nil {
		t.Fatalf("expected Buffer -> Writer implementation, got %+v", models[0].Implementations)
	}
	if !got.IsPointer {
		t.Errorf("expected IsPointer=true for pointer-only receiver, got false")
	}
}

func TestImplementations_CrossPackage(t *testing.T) {
	// Interface in package "api", concrete in package "impl".
	files := map[string]string{
		"api/api.go": `package api

type Service interface {
	Do() error
}
`,
		"impl/impl.go": `package impl

type Worker struct{}

func (w Worker) Do() error { return nil }
`,
	}
	dir := writeTestModule(t, "test.example/cross", files)
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// The implementation should be stored in the interface's owning package ("api").
	var apiModel *domain.PackageModel
	for i := range models {
		if models[i].Name == "api" {
			apiModel = &models[i]
			break
		}
	}
	if apiModel == nil {
		t.Fatalf("api model not found; got %d models", len(models))
	}

	got := findImpl(apiModel.Implementations, "Worker", "Service")
	if got == nil {
		t.Fatalf("expected Worker -> Service implementation in api model, got %+v", apiModel.Implementations)
	}
	if got.Concrete.Package != "impl" {
		t.Errorf("expected concrete package 'impl', got %q", got.Concrete.Package)
	}
	if got.Interface.Package != "api" {
		t.Errorf("expected interface package 'api', got %q", got.Interface.Package)
	}
}

func TestImplementations_SkipEmptyAndUnexported(t *testing.T) {
	code := `package skip

// Empty interface - should be skipped.
type Any interface{}

// unexported interface - should be skipped.
type helper interface {
	help()
}

type T struct{}

func (t T) help() {}
`
	dir := writeTestModule(t, "test.example/skip", map[string]string{"skip.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Should have no implementations recorded (empty iface skipped, unexported iface skipped).
	if len(models[0].Implementations) != 0 {
		t.Errorf("expected no implementations, got %+v", models[0].Implementations)
	}
}
