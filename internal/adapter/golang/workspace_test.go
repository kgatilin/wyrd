package golang

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/domain"
)

// writeWorkspace lays out a repository with no module at its root: two
// modules the overlay lists (store, app, where app imports store) and one it
// does not (tools).
func writeWorkspace(t *testing.T, modules string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("wyrd.yaml", "module: test.example\ngo:\n  modules: "+modules+"\nlayers:\n  all: [\"...\"]\n")
	write("store/go.mod", "module test.example/store\n\ngo 1.22\n")
	write("store/inventory/inventory.go", `package inventory

// Item is a stocked item.
type Item struct{ Name string }

// Counter counts items.
type Counter interface{ Count() int }
`)
	write("app/go.mod", "module test.example/app\n\ngo 1.22\n")
	write("app/cmd/run/run.go", `package run

import "test.example/store/inventory"

// Shelf holds items.
type Shelf struct{ Items []inventory.Item }

// Count implements inventory.Counter.
func (s Shelf) Count() int { return len(s.Items) }
`)
	write("tools/go.mod", "module test.example/tools\n\ngo 1.22\n")
	write("tools/gen/gen.go", "package gen\n\n// Gen generates.\nfunc Gen() {}\n")
	return root
}

func readWorkspace(t *testing.T, root string) map[string]domain.PackageModel {
	t.Helper()
	t.Chdir(root)
	models, err := NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	byPath := make(map[string]domain.PackageModel, len(models))
	for _, m := range models {
		byPath[m.Path] = m
	}
	return byPath
}

// TestReader_ListedModulesReadAsOneModel: with go.modules, "./..." is every
// listed module, a package path is its directory under the root, and an
// import from one listed module into another is an in-model edge.
func TestReader_ListedModulesReadAsOneModel(t *testing.T) {
	byPath := readWorkspace(t, writeWorkspace(t, "[store, app]"))

	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if want := []string{"app/cmd/run", "store/inventory"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("packages = %v, want %v (the unlisted tools module stays out)", paths, want)
	}

	run := byPath["app/cmd/run"]
	if want := []string{"store/inventory"}; !reflect.DeepEqual(run.Imports, want) {
		t.Errorf("Imports = %v, want %v", run.Imports, want)
	}

	var crossModule bool
	for _, d := range run.Dependencies {
		if d.To.Package == "store/inventory" && d.To.Symbol == "Item" {
			crossModule = true
			if d.To.External {
				t.Errorf("dependency on store/inventory.Item is marked external")
			}
		}
	}
	if !crossModule {
		t.Errorf("no dependency on store/inventory.Item in %+v", run.Dependencies)
	}

	var implements bool
	for _, impl := range run.Implementations {
		if impl.Concrete.Symbol == "Shelf" && impl.Interface.Package == "store/inventory" && impl.Interface.Symbol == "Counter" {
			implements = true
		}
	}
	if !implements {
		t.Errorf("Shelf does not implement store/inventory.Counter: %+v", run.Implementations)
	}

	var spanFile string
	for _, s := range run.Structs {
		if s.Name == "Shelf" {
			spanFile = s.Span.File
		}
	}
	if spanFile != "app/cmd/run/run.go" {
		t.Errorf("Shelf span file = %q, want it relative to the root", spanFile)
	}
}

// TestReader_ListedModuleWithoutGoMod: a listed directory that is not a
// module fails the read by name rather than reading the others alone.
func TestReader_ListedModuleWithoutGoMod(t *testing.T) {
	root := writeWorkspace(t, "[store, app, missing]")
	t.Chdir(root)
	_, err := NewReader().Read(context.Background(), []string{"./..."})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Read error = %v, want one naming the module without a go.mod", err)
	}
}

// TestReader_WorkspaceLeavesProjectUntouched: the go.work the read needs is
// written outside the project.
func TestReader_WorkspaceLeavesProjectUntouched(t *testing.T) {
	root := writeWorkspace(t, "[store, app]")
	readWorkspace(t, root)
	for _, name := range []string{"go.work", "go.work.sum"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s written into the project", name)
		}
	}
}
