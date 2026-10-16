package serve

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/service"
)

// storeRepo is a committed module with two packages, api and other, and the
// models a reader answers a full read of it with.
func storeRepo(t *testing.T) (string, []domain.PackageModel) {
	t.Helper()
	root := newGitRepo(t)
	writeGo(t, root, "api/api.go", "package api\n\ntype Existing struct{}\n")
	writeGo(t, root, "other/other.go", "package other\n\ntype Other struct{}\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "api and other")
	return root, []domain.PackageModel{
		{Path: "api", Name: "api", Structs: []domain.StructDef{{Name: "Existing"}}},
		{Path: "other", Name: "other", Structs: []domain.StructDef{{Name: "Other"}}},
	}
}

func packageOf(t *testing.T, st *State, path string) domain.PackageModel {
	t.Helper()
	for _, p := range st.Snapshot().Packages {
		if p.Path == path {
			return p
		}
	}
	t.Fatalf("package %q not in snapshot: %v", path, packagePaths(st.Snapshot().Packages))
	return domain.PackageModel{}
}

// A full parse of a clean worktree is the snapshot of its HEAD, so the next
// start of the same checkout parses nothing.
func TestStateLoad_CleanWorktreeRestartsFromItsSnapshot(t *testing.T) {
	root, models := storeRepo(t)
	store := testStore(t)

	first := &recordingReader{responses: map[string][]domain.PackageModel{"./...": models}}
	st := NewState(root, WithReader(first), WithModelStore(store, "main"))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok, _ := store.Snapshot(git.HeadRev(root)); !ok {
		t.Fatal("a full parse of a clean worktree was not recorded")
	}

	again := &recordingReader{fail: true}
	st = NewState(root, WithReader(again), WithModelStore(store, "main"))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load (restart): %v", err)
	}
	if len(again.calls) != 0 {
		t.Fatalf("restart read %v, want nothing", again.calls)
	}
	if !hasPackagePath(st, "api") || !hasPackagePath(st, "other") {
		t.Fatalf("packages = %v", packagePaths(st.Snapshot().Packages))
	}
	if v := st.Snapshot().Versions; v["api"] == "" || v["other"] == "" {
		t.Fatalf("Snapshot.Versions = %v, want a hash per package", v)
	}
}

// Uncommitted work over a recorded HEAD reads the packages it touches, with
// the rest of the snapshot as known, and nothing else. A dirty tree is not
// recorded.
func TestStateLoad_DirtyWorktreeReadsOnlyItsEdits(t *testing.T) {
	root, models := storeRepo(t)
	store := testStore(t)
	if _, err := store.Record(git.HeadRev(root), models); err != nil {
		t.Fatalf("Record: %v", err)
	}
	writeGo(t, root, "api/api.go", "package api\n\ntype Existing struct{}\ntype Edited struct{}\n")

	reader := &recordingReader{responses: map[string][]domain.PackageModel{
		"./api": {{Path: "api", Name: "api", Structs: []domain.StructDef{{Name: "Existing"}, {Name: "Edited"}}}},
	}}
	st := NewState(root, WithReader(reader), WithModelStore(store, "main"))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := flattenCalls(reader.calls), "./api"; got != want {
		t.Fatalf("reads = %q, want %q", got, want)
	}
	if got, want := reader.knownPaths(0), []string{"other"}; !equalStrings(got, want) {
		t.Fatalf("known = %v, want %v", got, want)
	}
	if got := countStructs(t, st, "api"); got != 2 {
		t.Fatalf("api structs = %d, want the edit's 2", got)
	}
}

// A directory the reader cannot model is skipped by a full read; the same
// answer to a read of just the diff must not fail the load.
func TestStateLoad_DiffPackageWithNothingToModelIsSkipped(t *testing.T) {
	root, models := storeRepo(t)
	store := testStore(t)
	if _, err := store.Record(git.HeadRev(root), models); err != nil {
		t.Fatalf("Record: %v", err)
	}
	writeGo(t, root, "tools/tools.go", "//go:build tools\n\npackage tools\n")

	reader := &recordingReader{errs: map[string]error{
		"./tools": fmt.Errorf("build constraints exclude all Go files: %w", service.ErrNothingToModel),
	}}
	st := NewState(root, WithReader(reader), WithModelStore(store, "main"))
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if hasPackagePath(st, "tools") || !hasPackagePath(st, "api") {
		t.Fatalf("packages = %v, want the snapshot's alone", packagePaths(st.Snapshot().Packages))
	}
}

// The point of the store: a branch whose merge base was parsed by main reads
// the packages its diff touches and nothing else, and holds main's instance
// of every package it did not touch.
func TestMultiState_BranchColdStartParsesOnlyItsDiff(t *testing.T) {
	t.Setenv("WYRD_HOME", t.TempDir())
	root, models := storeRepo(t)
	feature := filepath.Join(t.TempDir(), "feature")
	runGit(t, root, "worktree", "add", "-q", "-b", "feature", feature)
	writeGo(t, feature, "api/api.go", "package api\n\ntype Existing struct{}\ntype Edited struct{}\n")
	runGit(t, feature, "commit", "-qam", "edit api")
	writeGo(t, feature, "client/client.go", "package client\n\ntype Client struct{}\n")

	readers := map[string]*recordingReader{
		realPath(root): {responses: map[string][]domain.PackageModel{"./...": models}},
		realPath(feature): {responses: map[string][]domain.PackageModel{
			"./api,./client": {
				{Path: "api", Name: "api", Structs: []domain.StructDef{{Name: "Existing"}, {Name: "Edited"}}},
				{Path: "client", Name: "client", Structs: []domain.StructDef{{Name: "Client"}}},
			},
		}},
	}
	store := testStore(t)
	loader := func(ctx context.Context, _, path string) (*State, error) {
		st := NewState(path, WithReader(readers[realPath(path)]), WithModelStore(store, "main"))
		return st, st.Load(ctx)
	}
	multi := NewMultiState(root, loader)
	if err := multi.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	multi.SetModelStore(store)
	multi.SetReviewBaseRef("main")
	ctx := context.Background()

	mainState, err := multi.Get(ctx, worktreeNameFor(t, multi, root))
	if err != nil {
		t.Fatalf("Get(main): %v", err)
	}
	branch, err := multi.Get(ctx, worktreeNameFor(t, multi, feature))
	if err != nil {
		t.Fatalf("Get(branch): %v", err)
	}
	if got, want := flattenCalls(readers[realPath(feature)].calls), "./api,./client"; got != want {
		t.Fatalf("branch reads = %q, want only its diff %q", got, want)
	}
	names := declaredTypeNames(branch.Snapshot().Packages)
	for _, want := range []string{"Existing", "Edited", "Client", "Other"} {
		if !names[want] {
			t.Errorf("branch model missing %s: %v", want, names)
		}
	}
	if &packageOf(t, branch, "other").Structs[0] != &packageOf(t, mainState, "other").Structs[0] {
		t.Error("the branch holds its own copy of a package it did not touch")
	}

	// The review base is the same snapshot: no materialized tree, same instances.
	base, err := multi.ReviewBase(ctx, worktreeNameFor(t, multi, feature), "")
	if err != nil {
		t.Fatalf("ReviewBase: %v", err)
	}
	for _, m := range base.Models {
		if m.Path == "other" && &m.Structs[0] != &packageOf(t, mainState, "other").Structs[0] {
			t.Error("the review base holds a copy of a package main already holds")
		}
	}
}

// Shared models are read-only, but a holder's own copy is its own: the
// overlay tagging a package and an append to one of its slices — the two
// writes a consumer makes — must not reach another State's view.
func TestStateViewsOfOneVersionAreIndependent(t *testing.T) {
	root, models := storeRepo(t)
	store := testStore(t)
	a := NewState(root, WithReader(&recordingReader{responses: map[string][]domain.PackageModel{"./...": models}}), WithModelStore(store, ""))
	if err := a.Load(context.Background()); err != nil {
		t.Fatalf("Load a: %v", err)
	}
	b := NewState(root, WithReader(&recordingReader{fail: true}), WithModelStore(store, ""))
	if err := b.Load(context.Background()); err != nil {
		t.Fatalf("Load b: %v", err)
	}

	view := a.Snapshot().Packages
	for i := range view {
		view[i].Layer = "domain"
		view[i].Structs = append(view[i].Structs, domain.StructDef{Name: "Injected"})
	}

	other := packageOf(t, b, "other")
	if other.Layer != "" {
		t.Errorf("Layer leaked into another State: %q", other.Layer)
	}
	if got := other.Structs[:cap(other.Structs)]; len(got) != 1 || got[0].Name != "Other" {
		t.Errorf("an append reached another State's view: %+v", got)
	}
	if again := packageOf(t, a, "other"); len(again.Structs) != 1 || again.Layer != "" {
		t.Errorf("a State's own model changed through its snapshot: %+v", again)
	}
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
