package golang

import (
	"context"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/kgatilin/wyrd/internal/domain"
)

// referencesFrom returns "pkg.Symbol" of every references dependency whose
// From symbol is from, sorted.
func referencesFrom(model domain.PackageModel, from string) []string {
	var out []string
	for _, d := range model.Dependencies {
		if d.Kind == domain.DependencyReferences && d.From.Symbol == from {
			out = append(out, d.To.Package+"."+d.To.Symbol)
		}
	}
	sort.Strings(out)
	return out
}

func TestReferences_FunctionValuesBecomeEdges(t *testing.T) {
	files := map[string]string{
		"store/store.go": `package store

type Backend struct{}

func NewBackend() *Backend { return &Backend{} }

func (b *Backend) Close() {}

func Generic[T any]() {}
`,
		"app/app.go": `package app

import (
	"strings"

	"test.example/refs/store"
)

type Closer interface{ Close() }

type Server struct{}

func (s *Server) handle() {}

func (s *Server) Routes(register func(func())) {
	register(s.handle)
	s.handle()
}

func run(factory func() *store.Backend, cleanup func(), f func(string) string) {}

func Wire(c Closer, b *store.Backend) {
	run(store.NewBackend, b.Close, strings.ToUpper)
	store.NewBackend()
	store.Generic[int]()
	hooks := []any{c.Close, (*store.Backend).Close}
	_ = hooks
	_ = Wire
}
`,
	}
	dir := writeTestModule(t, "test.example/refs", files)
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	os.Chdir(dir)

	models, err := NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	app := findModel(models, "app")
	if app == nil {
		t.Fatalf("app model not found")
	}

	// NewBackend handed to run, and the method value and method expression
	// of Backend.Close collapsed to one edge. Called functions (run,
	// store.Generic, the second NewBackend), the stdlib function, the
	// interface method value and Wire naming itself leave no reference.
	want := []string{"store.Backend.Close", "store.NewBackend"}
	if got := referencesFrom(*app, "Wire"); !slices.Equal(got, want) {
		t.Errorf("references from Wire = %v, want %v", got, want)
	}
	// A method value is recorded from the method, not from its receiver.
	if got, want := referencesFrom(*app, "Server.Routes"), []string{"app.Server.handle"}; !slices.Equal(got, want) {
		t.Errorf("references from Server.Routes = %v, want %v", got, want)
	}

	// Naming a function as a value adds no call edge. (store.Generic[int]()
	// has none either: the call walk does not resolve an explicit
	// instantiation, which is a separate gap.)
	calls := findFuncCalls(*app, "Wire")
	var called []string
	for _, c := range calls {
		called = append(called, c.To.Symbol)
	}
	sort.Strings(called)
	if want := []string{"NewBackend", "run"}; !slices.Equal(called, want) {
		t.Errorf("calls from Wire = %v, want %v", called, want)
	}
}
