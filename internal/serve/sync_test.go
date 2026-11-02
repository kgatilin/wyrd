package serve

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kgatilin/wyrd/internal/domain"
)

func loadFixture(t *testing.T, opts ...StateOption) (string, *State) {
	t.Helper()
	root := newFixture(t)
	st := NewState(root, opts...)
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return root, st
}

func TestStateSyncReadsAnEditNoEventReported(t *testing.T) {
	root, st := loadFixture(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), `package foo

type Thing struct{ Name string }
type Second struct{ X int }

func New() *Thing { return &Thing{} }
`)

	moved, err := st.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !equalStrings(moved, []string{"internal/foo"}) {
		t.Fatalf("moved = %v, want [internal/foo]", moved)
	}
	if got := countStructs(t, st, "internal/foo"); got != 2 {
		t.Fatalf("internal/foo structs = %d, want 2", got)
	}

	// Nothing moved since: no package is reported, so nothing is broadcast.
	moved, err = st.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync (second): %v", err)
	}
	if moved != nil {
		t.Fatalf("second Sync moved %v on an unchanged tree", moved)
	}
}

// TestStateSyncReportsNoMoveForAnIdenticalRewrite: a file rewritten with the
// same declarations moves its stamp but not its model.
func TestStateSyncReportsNoMoveForAnIdenticalRewrite(t *testing.T) {
	root, st := loadFixture(t)
	touchFile(t, filepath.Join(root, "internal", "foo", "foo.go"))

	moved, err := st.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if moved != nil {
		t.Fatalf("moved = %v for a file whose content did not change", moved)
	}
}

// TestStateSyncFollowsAddedAndDeletedPackages covers a checkout that removes
// a whole package tree: nothing is left on disk to name the package, and the
// model must still lose it.
func TestStateSyncFollowsAddedAndDeletedPackages(t *testing.T) {
	root, st := loadFixture(t)

	if err := os.RemoveAll(filepath.Join(root, "internal", "foo")); err != nil {
		t.Fatalf("remove package dir: %v", err)
	}
	writeFile(t, filepath.Join(root, "internal", "added", "added.go"), "package added\n\ntype Added struct{}\n")

	moved, err := st.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !equalStrings(moved, []string{"internal/added", "internal/foo"}) {
		t.Fatalf("moved = %v, want [internal/added internal/foo]", moved)
	}
	if hasPackagePath(st, "internal/foo") || !hasPackagePath(st, "internal/added") || !hasPackagePath(st, "internal/bar") {
		t.Fatalf("packages after Sync = %v, want bar and added, not foo", packagePaths(st.Snapshot().Packages))
	}
}

// TestStateSyncLeavesTestFilesAlone: the reader ignores _test.go files, so
// editing one is not a reason to read anything.
func TestStateSyncLeavesTestFilesAlone(t *testing.T) {
	root := newFixture(t)
	reader := &recordingReader{responses: map[string][]domain.PackageModel{
		"./...": {{Path: "internal/foo", Name: "foo"}, {Path: "internal/bar", Name: "bar"}},
	}}
	st := NewState(root, WithReader(reader))
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeFile(t, filepath.Join(root, "internal", "foo", "foo_test.go"), "package foo\n")
	if _, err := st.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := flattenCalls(reader.calls); got != "./..." {
		t.Fatalf("reads = %q, want only the load", got)
	}
}

// TestStateSyncReadsOverTheRestOfTheModel: the changed packages are read with
// the others as known, so interface dispatch into them keeps its edges.
func TestStateSyncReadsOverTheRestOfTheModel(t *testing.T) {
	root := newFixture(t)
	reader := &recordingReader{responses: map[string][]domain.PackageModel{
		"./...":          {{Path: "internal/foo", Name: "foo"}, {Path: "internal/bar", Name: "bar"}},
		"./internal/foo": {{Path: "internal/foo", Name: "foo", Structs: []domain.StructDef{{Name: "Thing"}}}},
	}}
	st := NewState(root, WithReader(reader))
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), "package foo\n\ntype Thing struct{}\n")
	if _, err := st.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := flattenCalls(reader.calls); got != "./...;./internal/foo" {
		t.Fatalf("reads = %q, want the load and then ./internal/foo", got)
	}
	if got := reader.knownPaths(1); !equalStrings(got, []string{"internal/bar"}) {
		t.Fatalf("known models handed to the read = %v, want [internal/bar]", got)
	}
}

// TestStateSyncReadsInFullWhenTheModuleChanges: a go.mod or go.sum edit can
// change how every package resolves.
func TestStateSyncReadsInFullWhenTheModuleChanges(t *testing.T) {
	root := newFixture(t)
	reader := &recordingReader{responses: map[string][]domain.PackageModel{
		"./...": {{Path: "internal/foo", Name: "foo"}, {Path: "internal/bar", Name: "bar"}},
	}}
	st := NewState(root, WithReader(reader))
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeFile(t, filepath.Join(root, "go.sum"), "")
	if _, err := st.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := flattenCalls(reader.calls); got != "./...;./..." {
		t.Fatalf("reads = %q, want a second full read", got)
	}
}

// TestStateSyncRetriesWhatItCouldNotRead pins that a failed read is neither a
// deletion nor forgotten: the model stays as it was, ReadError says it is
// stale, and the next Sync reads the package again because its stamp was
// never recorded.
func TestStateSyncRetriesWhatItCouldNotRead(t *testing.T) {
	root := newFixture(t)
	reader := &recordingReader{responses: map[string][]domain.PackageModel{
		"./...":          {{Path: "internal/foo", Name: "foo", Structs: []domain.StructDef{{Name: "Thing"}}}, {Path: "internal/bar", Name: "bar"}},
		"./internal/foo": {{Path: "internal/foo", Name: "foo", Structs: []domain.StructDef{{Name: "Thing"}, {Name: "Second"}}}},
	}}
	st := NewState(root, WithReader(reader))
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), "package foo\n\ntype Thing struct{}\ntype Second struct{}\n")
	reader.fail = true
	if _, err := st.Sync(ctx); err == nil {
		t.Fatal("Sync swallowed a read failure")
	}
	if got := countStructs(t, st, "internal/foo"); got != 1 {
		t.Fatalf("internal/foo structs = %d, want the model it had (1)", got)
	}
	if st.ReadError() == nil {
		t.Fatal("ReadError is nil while the model predates a failed read")
	}

	reader.fail = false
	moved, err := st.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync (retry): %v", err)
	}
	if err := st.ReadError(); err != nil {
		t.Fatalf("ReadError = %v after a read that succeeded", err)
	}
	if !equalStrings(moved, []string{"internal/foo"}) || countStructs(t, st, "internal/foo") != 2 {
		t.Fatalf("retry moved %v, structs %d; want [internal/foo] and 2", moved, countStructs(t, st, "internal/foo"))
	}
}

// TestStateReloadRecordsTheStampsItRead: after a Reload has read an edit, a
// Sync has nothing left to do.
func TestStateReloadRecordsTheStampsItRead(t *testing.T) {
	root, st := loadFixture(t)
	ctx := context.Background()
	writeFile(t, filepath.Join(root, "internal", "bar", "baz.go"), "package bar\n\ntype Baz struct{}\n")

	if _, err := st.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	moved, err := st.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if moved != nil {
		t.Fatalf("Sync after Reload moved %v", moved)
	}
}

// TestStateSyncReadsImportersOnlyWhenTheAPIMoved: an importer's model follows
// the API of what it imports, so a changed package whose APIHash moved takes
// its importers — direct and transitive — with it, and one whose body alone
// changed does not.
func TestStateSyncReadsImportersOnlyWhenTheAPIMoved(t *testing.T) {
	for _, tc := range []struct {
		name    string
		apiHash string
		reads   string
	}{
		{name: "body edit", apiHash: "v1", reads: "./...;./internal/foo"},
		{name: "API edit", apiHash: "v2", reads: "./...;./internal/foo;./internal/bar,./internal/baz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			writeFile(t, filepath.Join(root, "internal", "baz", "baz.go"), "package baz\n")
			writeFile(t, filepath.Join(root, "internal", "qux", "qux.go"), "package qux\n")
			reader := &recordingReader{responses: map[string][]domain.PackageModel{
				"./...": {
					{Path: "internal/foo", Name: "foo", APIHash: "v1"},
					{Path: "internal/bar", Name: "bar", Imports: []string{"internal/foo"}},
					{Path: "internal/baz", Name: "baz", Imports: []string{"internal/bar"}},
					{Path: "internal/qux", Name: "qux"},
				},
				"./internal/foo":                {{Path: "internal/foo", Name: "foo", APIHash: tc.apiHash}},
				"./internal/bar,./internal/baz": {{Path: "internal/bar", Name: "bar"}, {Path: "internal/baz", Name: "baz"}},
			}}
			st := NewState(root, WithReader(reader))
			ctx := context.Background()
			if err := st.Load(ctx); err != nil {
				t.Fatalf("Load: %v", err)
			}

			writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), "package foo\n\ntype Thing struct{}\n")
			if _, err := st.Sync(ctx); err != nil {
				t.Fatalf("Sync: %v", err)
			}
			if got := flattenCalls(reader.calls); got != tc.reads {
				t.Fatalf("reads = %q, want %q", got, tc.reads)
			}
		})
	}
}

// TestStateSyncMatchesAFullReadAfterAnAPIChange: an untouched package whose
// dependency comes from an inferred generic argument gets the dependency a
// full read gives it once the API it is inferred from changes.
func TestStateSyncMatchesAFullReadAfterAnAPIChange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "internal", "gen", "gen.go"), `package gen

func First[T any](xs []T) T { return xs[0] }
`)
	writeFile(t, filepath.Join(root, "internal", "store", "store.go"), `package store

type Item struct{}
type Other struct{}

func Get() []Item { return nil }
`)
	writeFile(t, filepath.Join(root, "internal", "app", "app.go"), `package app

import (
	"example.com/fixture/internal/gen"
	"example.com/fixture/internal/store"
)

func Use() { _ = gen.First(store.Get()) }
`)
	st := NewState(root)
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := dependencyTargets(st.Snapshot().Packages, "internal/app"); !equalStrings(got, []string{"internal/store.Item"}) {
		t.Fatalf("app dependencies before = %v, want [internal/store.Item]", got)
	}

	writeFile(t, filepath.Join(root, "internal", "store", "store.go"), `package store

type Item struct{}
type Other struct{}

func Get() []Other { return nil }
`)
	if _, err := st.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	full := NewState(root)
	if err := full.Load(ctx); err != nil {
		t.Fatalf("full Load: %v", err)
	}
	want := dependencyTargets(full.Snapshot().Packages, "internal/app")
	if !equalStrings(want, []string{"internal/store.Other"}) {
		t.Fatalf("full read: app dependencies = %v, want [internal/store.Other]", want)
	}
	if got := dependencyTargets(st.Snapshot().Packages, "internal/app"); !equalStrings(got, want) {
		t.Fatalf("synced app dependencies = %v, want %v as a full read gives", got, want)
	}
}

func dependencyTargets(models []domain.PackageModel, pkgPath string) []string {
	var out []string
	for _, m := range models {
		if m.Path != pkgPath {
			continue
		}
		for _, d := range m.Dependencies {
			if !d.To.External {
				out = append(out, d.To.Package+"."+d.To.Symbol)
			}
		}
	}
	sort.Strings(out)
	return out
}
