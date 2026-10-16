package serve

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildHandlerDropsAPackageDeletedFromTheTree covers the two shapes a
// deleted package arrives in. Both must leave the model holding only what the
// tree still holds: the review base is re-read from a materialized commit, so
// a package that outlives its source on the worktree side is reported as one
// the branch added.
func TestBuildHandlerDropsAPackageDeletedFromTheTree(t *testing.T) {
	cases := []struct {
		name string
		// event returns the path the watcher reports, given the root.
		event func(root string) string
	}{
		{
			name:  "the deleted .go file",
			event: func(root string) string { return filepath.Join(root, "internal", "foo", "foo.go") },
		},
		{
			// A directory removed wholesale can arrive as the bare directory,
			// with no event for the files that went with it.
			name:  "the bare directory",
			event: func(root string) string { return filepath.Join(root, "internal", "foo") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			st := NewState(root)
			ctx := context.Background()
			if err := st.Load(ctx); err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !hasPackagePath(st, "internal/foo") {
				t.Fatalf("internal/foo missing from the loaded model: %v", packagePaths(st.Snapshot().Packages))
			}

			path := tc.event(root)
			if err := os.RemoveAll(filepath.Join(root, "internal", "foo")); err != nil {
				t.Fatalf("remove package dir: %v", err)
			}

			var logOut bytes.Buffer
			buildHandler(ctx, st, &logOut, false)([]string{path})

			if hasPackagePath(st, "internal/foo") {
				t.Fatalf("internal/foo survived its own source: %v", packagePaths(st.Snapshot().Packages))
			}
			if !hasPackagePath(st, "internal/bar") {
				t.Fatalf("internal/bar was dropped too: %v", packagePaths(st.Snapshot().Packages))
			}
		})
	}
}

// TestBuildHandlerLeavesTheModelAloneOnAnEdit guards the reconcile against
// running on every save: an edit deletes nothing, so nothing may be dropped.
func TestBuildHandlerLeavesTheModelAloneOnAnEdit(t *testing.T) {
	root := newFixture(t)
	st := NewState(root)
	ctx := context.Background()
	if err := st.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	foo := filepath.Join(root, "internal", "foo", "foo.go")
	writeFile(t, foo, `package foo

type Thing struct{ Name string }
type Second struct{ X int }

func New() *Thing { return &Thing{} }
`)

	var logOut bytes.Buffer
	buildHandler(ctx, st, &logOut, false)([]string{foo})

	if !hasPackagePath(st, "internal/foo") || !hasPackagePath(st, "internal/bar") {
		t.Fatalf("an edit dropped packages: %v", packagePaths(st.Snapshot().Packages))
	}
	if countStructs(t, st, "internal/foo") != 2 {
		t.Fatalf("the edit was not picked up: %d struct(s)", countStructs(t, st, "internal/foo"))
	}
}
