package serve

import (
	"context"
	"os"
	"path/filepath"
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
// deletion nor forgotten: the model stays as it was, and the next Sync reads
// the package again because its stamp was never recorded.
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

	reader.fail = false
	moved, err := st.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync (retry): %v", err)
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
