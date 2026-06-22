package golang

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpanCorrectness verifies that the spans populated by the reader
// contain valid byte offsets that, when sliced from the source file,
// produce text containing the symbol's name and expected syntax.
func TestSpanCorrectness(t *testing.T) {
	// Create a temporary Go module with test fixtures
	tmpDir := t.TempDir()
	modPath := "example.com/testmod"

	// Write go.mod
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module "+modPath+"\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	// Write test source file with various symbol types
	srcContent := `package testpkg

// MyInterface is a test interface.
type MyInterface interface {
	DoSomething() error
}

// MyStruct is a test struct.
type MyStruct struct {
	Field1 string
	Field2 int
}

// NewMyStruct is a factory function.
func NewMyStruct(field1 string) *MyStruct {
	return &MyStruct{Field1: field1}
}

// DoThing is a method on MyStruct.
func (m *MyStruct) DoThing() {
	// implementation
}

// StatusCode is an enum type.
type StatusCode int

const MaxRetries = 5

var DefaultTimeout = 30
`
	pkgDir := filepath.Join(tmpDir, "testpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("creating package dir: %v", err)
	}
	srcPath := filepath.Join(pkgDir, "source.go")
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	// Initialize go.sum to make the module valid
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("go mod tidy: %v", err)
	}

	// Parse using the reader with the module pattern
	reader := NewReader()
	ctx := context.Background()
	// Change to tmpDir so the package load works
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	models, err := reader.Read(ctx, []string{"./testpkg"})
	if err != nil {
		t.Fatalf("reading package: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	model := models[0]

	// Helper to read span from file
	readSpan := func(t *testing.T, span interface{ IsValid() bool }, startByte, endByte int, file string) string {
		t.Helper()
		fullPath := filepath.Join(tmpDir, file)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("reading %s: %v", fullPath, err)
		}
		if startByte < 0 || endByte > len(data) || startByte >= endByte {
			t.Fatalf("invalid span [%d:%d] for file of length %d", startByte, endByte, len(data))
		}
		return string(data[startByte:endByte])
	}

	// Test interface span
	t.Run("interface span", func(t *testing.T) {
		if len(model.Interfaces) == 0 {
			t.Skip("no interfaces found")
		}
		iface := model.Interfaces[0]
		if !iface.Span.IsValid() {
			t.Fatal("interface span is invalid")
		}
		text := readSpan(t, iface.Span, iface.Span.StartByte, iface.Span.EndByte, iface.Span.File)
		if !strings.Contains(text, "MyInterface") {
			t.Errorf("interface span should contain 'MyInterface', got: %s", text)
		}
		if !strings.Contains(text, "interface") {
			t.Errorf("interface span should contain 'interface', got: %s", text)
		}
	})

	// Test struct span
	t.Run("struct span", func(t *testing.T) {
		if len(model.Structs) == 0 {
			t.Skip("no structs found")
		}
		s := model.Structs[0]
		if !s.Span.IsValid() {
			t.Fatal("struct span is invalid")
		}
		text := readSpan(t, s.Span, s.Span.StartByte, s.Span.EndByte, s.Span.File)
		if !strings.Contains(text, "MyStruct") {
			t.Errorf("struct span should contain 'MyStruct', got: %s", text)
		}
		if !strings.Contains(text, "struct") {
			t.Errorf("struct span should contain 'struct', got: %s", text)
		}
	})

	// Test function span
	t.Run("function span", func(t *testing.T) {
		if len(model.Functions) == 0 {
			t.Skip("no functions found")
		}
		fn := model.Functions[0]
		if !fn.Span.IsValid() {
			t.Fatal("function span is invalid")
		}
		text := readSpan(t, fn.Span, fn.Span.StartByte, fn.Span.EndByte, fn.Span.File)
		if !strings.Contains(text, "NewMyStruct") {
			t.Errorf("function span should contain 'NewMyStruct', got: %s", text)
		}
		if !strings.Contains(text, "func") {
			t.Errorf("function span should contain 'func', got: %s", text)
		}
		if !strings.Contains(text, "{") {
			t.Errorf("function span should contain opening brace, got: %s", text)
		}
	})

	// Test method span
	t.Run("method span", func(t *testing.T) {
		if len(model.Structs) == 0 || len(model.Structs[0].Methods) == 0 {
			t.Skip("no methods found")
		}
		method := model.Structs[0].Methods[0]
		if !method.Span.IsValid() {
			t.Fatal("method span is invalid")
		}
		text := readSpan(t, method.Span, method.Span.StartByte, method.Span.EndByte, method.Span.File)
		if !strings.Contains(text, "DoThing") {
			t.Errorf("method span should contain 'DoThing', got: %s", text)
		}
		if !strings.Contains(text, "func") {
			t.Errorf("method span should contain 'func', got: %s", text)
		}
	})

	// Test typedef span
	t.Run("typedef span", func(t *testing.T) {
		if len(model.TypeDefs) == 0 {
			t.Skip("no typedefs found")
		}
		td := model.TypeDefs[0]
		if !td.Span.IsValid() {
			t.Fatal("typedef span is invalid")
		}
		text := readSpan(t, td.Span, td.Span.StartByte, td.Span.EndByte, td.Span.File)
		if !strings.Contains(text, "StatusCode") {
			t.Errorf("typedef span should contain 'StatusCode', got: %s", text)
		}
		if !strings.Contains(text, "type") {
			t.Errorf("typedef span should contain 'type', got: %s", text)
		}
	})

	// Test constant span
	t.Run("constant span", func(t *testing.T) {
		if len(model.Constants) == 0 {
			t.Skip("no constants found")
		}
		c := model.Constants[0]
		if !c.Span.IsValid() {
			t.Fatal("constant span is invalid")
		}
		text := readSpan(t, c.Span, c.Span.StartByte, c.Span.EndByte, c.Span.File)
		if !strings.Contains(text, "MaxRetries") {
			t.Errorf("constant span should contain 'MaxRetries', got: %s", text)
		}
		if !strings.Contains(text, "const") {
			t.Errorf("constant span should contain 'const', got: %s", text)
		}
	})

	// Test variable span
	t.Run("variable span", func(t *testing.T) {
		if len(model.Variables) == 0 {
			t.Skip("no variables found")
		}
		v := model.Variables[0]
		if !v.Span.IsValid() {
			t.Fatal("variable span is invalid")
		}
		text := readSpan(t, v.Span, v.Span.StartByte, v.Span.EndByte, v.Span.File)
		if !strings.Contains(text, "DefaultTimeout") {
			t.Errorf("variable span should contain 'DefaultTimeout', got: %s", text)
		}
		if !strings.Contains(text, "var") {
			t.Errorf("variable span should contain 'var', got: %s", text)
		}
	})
}

// TestSpanRelativePath verifies that span.File is relative to module root.
func TestSpanRelativePath(t *testing.T) {
	tmpDir := t.TempDir()
	modPath := "example.com/testmod"

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module "+modPath+"\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	// Create nested package
	pkgDir := filepath.Join(tmpDir, "internal", "nested")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("creating package dir: %v", err)
	}

	srcContent := `package nested

func Foo() {}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "foo.go"), []byte(srcContent), 0644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	// Initialize go.sum to make the module valid
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("go mod tidy: %v", err)
	}

	// Change to tmpDir so the package load works
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	reader := NewReader()
	models, err := reader.Read(context.Background(), []string{"./internal/nested"})
	if err != nil {
		t.Fatalf("reading package: %v", err)
	}

	if len(models) != 1 || len(models[0].Functions) == 0 {
		t.Fatal("expected 1 model with 1 function")
	}

	fn := models[0].Functions[0]
	expectedPath := filepath.Join("internal", "nested", "foo.go")
	if fn.Span.File != expectedPath {
		t.Errorf("expected span.File = %q, got %q", expectedPath, fn.Span.File)
	}
}
