package golang

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// findFuncCalls returns the Calls slice for a package-level function by name.
func findFuncCalls(model domain.PackageModel, name string) []domain.CallEdge {
	for _, fn := range model.Functions {
		if fn.Name == name {
			return fn.Calls
		}
	}
	return nil
}

// findMethodCalls returns the Calls slice for a method on a struct.
func findMethodCalls(model domain.PackageModel, structName, methodName string) []domain.CallEdge {
	for _, s := range model.Structs {
		if s.Name != structName {
			continue
		}
		for _, m := range s.Methods {
			if m.Name == methodName {
				return m.Calls
			}
		}
	}
	return nil
}

// findModel returns the model for a package by name.
func findModel(models []domain.PackageModel, name string) *domain.PackageModel {
	for i := range models {
		if models[i].Name == name {
			return &models[i]
		}
	}
	return nil
}

func TestCalls_DirectFunctionCall(t *testing.T) {
	code := `package calls

func A() {
	B()
}

func B() {}
`
	dir := writeTestModule(t, "test.example/calls/direct", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	calls := findFuncCalls(models[0], "A")
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from A, got %d: %+v", len(calls), calls)
	}
	if calls[0].To.Symbol != "B" {
		t.Errorf("expected call to B, got %q", calls[0].To.Symbol)
	}
	if calls[0].Via != "" {
		t.Errorf("expected empty Via for direct call, got %q", calls[0].Via)
	}
}

func TestCalls_PackageQualifiedCall(t *testing.T) {
	// Call across packages within the same module.
	files := map[string]string{
		"util/util.go": `package util

func Helper() int { return 42 }
`,
		"app/app.go": `package app

import "test.example/calls/pkg/util"

func Run() int {
	return util.Helper()
}
`,
	}
	dir := writeTestModule(t, "test.example/calls/pkg", files)
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	appModel := findModel(models, "app")
	if appModel == nil {
		t.Fatalf("app model not found")
	}
	calls := findFuncCalls(*appModel, "Run")
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(calls), calls)
	}
	if calls[0].To.Symbol != "Helper" || calls[0].To.Package != "util" {
		t.Errorf("expected util.Helper, got %+v", calls[0].To)
	}
}

func TestCalls_SkipStdlibAndBuiltins(t *testing.T) {
	code := `package calls

import "fmt"

func A() {
	_ = len("hi")
	fmt.Println("x")
	B()
}

func B() {}
`
	dir := writeTestModule(t, "test.example/calls/skip", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	calls := findFuncCalls(models[0], "A")
	if len(calls) != 1 {
		t.Fatalf("expected only B to survive, got %d: %+v", len(calls), calls)
	}
	if calls[0].To.Symbol != "B" {
		t.Errorf("expected B, got %+v", calls[0])
	}
}

func TestCalls_MethodCallOnConcreteReceiver(t *testing.T) {
	code := `package calls

type Repo struct{}

func (r *Repo) Get() string { return "" }

type Handler struct {
	repo *Repo
}

func (h *Handler) X() {
	h.repo.Get()
}
`
	dir := writeTestModule(t, "test.example/calls/method", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	calls := findMethodCalls(models[0], "Handler", "X")
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(calls), calls)
	}
	want := "Repo.Get"
	if calls[0].To.Symbol != want {
		t.Errorf("expected %q, got %q", want, calls[0].To.Symbol)
	}
	if calls[0].Via != "" {
		t.Errorf("expected empty Via, got %q", calls[0].Via)
	}
}

func TestCalls_InterfaceDispatchFansOut(t *testing.T) {
	// Interface with two implementations — method call via interface
	// should emit edges to each implementation with Via set.
	code := `package calls

type Repository interface {
	Get(id string) error
}

type MemRepo struct{}

func (m *MemRepo) Get(id string) error { return nil }

type SQLRepo struct{}

func (s *SQLRepo) Get(id string) error { return nil }

type Handler struct {
	repo Repository
}

func (h *Handler) Serve(id string) {
	_ = h.repo.Get(id)
}
`
	dir := writeTestModule(t, "test.example/calls/iface", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	calls := findMethodCalls(models[0], "Handler", "Serve")
	if len(calls) != 2 {
		t.Fatalf("expected 2 interface-dispatched edges, got %d: %+v", len(calls), calls)
	}

	// Sort so we can assert deterministically.
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].To.Symbol < calls[j].To.Symbol
	})

	wantSymbols := []string{"MemRepo.Get", "SQLRepo.Get"}
	for i, e := range calls {
		if e.To.Symbol != wantSymbols[i] {
			t.Errorf("edge %d: expected %q, got %q", i, wantSymbols[i], e.To.Symbol)
		}
		if e.Via != "Repository" && e.Via != ".Repository" {
			// Single-package module: the relative path is ".", and the
			// implementation stores that path. Accept either form.
			// The reader may emit either "Repository" (when relPath is
			// ".") or "<pkg>.Repository" for multi-package setups.
			if e.Via == "" {
				t.Errorf("edge %d: expected Via set, got empty", i)
			}
		}
	}
}

func TestCalls_NoImplsForInterfaceSkipped(t *testing.T) {
	// Interface receiver with no loaded impls — no edges should be emitted.
	code := `package calls

type Thing interface {
	Do()
}

type Caller struct {
	t Thing
}

func (c *Caller) Run() {
	c.t.Do()
}
`
	dir := writeTestModule(t, "test.example/calls/noimpl", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	calls := findMethodCalls(models[0], "Caller", "Run")
	if len(calls) != 0 {
		t.Errorf("expected no edges when interface has no impls, got %+v", calls)
	}
}

func TestCalls_EmptyBody(t *testing.T) {
	// Functions with empty bodies should have empty Calls without errors.
	code := `package calls

func Empty() {}

type T struct{}

func (t T) Noop() {}
`
	dir := writeTestModule(t, "test.example/calls/empty", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if calls := findFuncCalls(models[0], "Empty"); len(calls) != 0 {
		t.Errorf("expected no calls from Empty, got %+v", calls)
	}
	if calls := findMethodCalls(models[0], "T", "Noop"); len(calls) != 0 {
		t.Errorf("expected no calls from T.Noop, got %+v", calls)
	}
}

func TestCalls_Deduplication(t *testing.T) {
	// Calling the same function twice should result in a single edge.
	code := `package calls

func A() {
	B()
	B()
	B()
}

func B() {}
`
	dir := writeTestModule(t, "test.example/calls/dedup", map[string]string{"calls.go": code})
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	calls := findFuncCalls(models[0], "A")
	if len(calls) != 1 {
		t.Fatalf("expected 1 deduplicated edge, got %d: %+v", len(calls), calls)
	}
}
