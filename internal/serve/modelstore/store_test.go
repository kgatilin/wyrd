package modelstore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/domain"
)

func writeTree(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/shapes\n\ngo 1.21\n",
		"shape/shape.go": `package shape

// Shape is anything with an area.
type Shape interface{ Area() float64 }

// Square is a Shape.
type Square struct{ Side float64 }

// Area implements Shape.
func (s Square) Area() float64 { return s.Side * s.Side }
`,
		"render/render.go": `package render

import "example.com/shapes/shape"

// Total sums the areas.
func Total(shapes []shape.Shape) float64 {
	var sum float64
	for _, s := range shapes {
		sum += s.Area()
	}
	return sum
}
`,
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func parse(t *testing.T, root string) []domain.PackageModel {
	t.Helper()
	t.Chdir(root)
	models, err := golang.NewReader().Read(context.Background(), []string{"./..."})
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	return models
}

func hashesByPath(versions []Version) map[string]string {
	out := make(map[string]string, len(versions))
	for _, v := range versions {
		out[v.Model.Path] = v.Hash
	}
	return out
}

// The same source checked out at two places is the same Version: nothing in
// the model may carry the checkout's location.
func TestHashIsTheSameForTheSameSourceInTwoRoots(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeTree(t, a)
	writeTree(t, b)

	var s *Store
	ha := hashesByPath(s.Versions(parse(t, a)))
	hb := hashesByPath(s.Versions(parse(t, b)))
	if len(ha) != 2 {
		t.Fatalf("parsed %d packages, want 2: %v", len(ha), ha)
	}
	if !reflect.DeepEqual(ha, hb) {
		t.Fatalf("hashes differ between two roots of one source:\n%v\n%v", ha, hb)
	}
}

func TestOverlayFactsAreNotPartOfAVersion(t *testing.T) {
	m := domain.PackageModel{Path: "a", Name: "a"}
	tagged := m
	tagged.Layer, tagged.Aggregate = "domain", "root"

	var s *Store
	v := s.Versions([]domain.PackageModel{m, tagged})
	if v[0].Hash != v[1].Hash {
		t.Fatal("Layer/Aggregate changed the hash")
	}
	if v[1].Model.Layer != "" || v[1].Model.Aggregate != "" {
		t.Fatalf("a Version carries overlay facts: %+v", v[1].Model)
	}
}

func openStore(t *testing.T, dir, stamp string) *Store {
	t.Helper()
	s, err := Open(dir, stamp)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func sample() []domain.PackageModel {
	return []domain.PackageModel{
		{Path: "b", Name: "b", Structs: []domain.StructDef{{Name: "B"}}},
		{Path: "a", Name: "a", Structs: []domain.StructDef{{Name: "A"}}, Imports: []string{"b"}},
	}
}

// Recording a model identical to a held one returns the held instance, and so
// does looking one up: an unchanged package is one allocation.
func TestInternReturnsTheSameInstance(t *testing.T) {
	s := openStore(t, t.TempDir(), "bin-1")
	first, err := s.Record("rev1", sample())
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	second, err := s.Record("rev2", sample()) // a fresh parse, equal content
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	looked := s.Versions(sample())
	for i := range first {
		if &first[i].Model.Structs[0] != &second[i].Model.Structs[0] {
			t.Errorf("%s: two snapshots hold two instances of one Version", first[i].Model.Path)
		}
	}
	for _, v := range looked {
		var held Version
		for _, f := range first {
			if f.Hash == v.Hash {
				held = f
			}
		}
		if &held.Model.Structs[0] != &v.Model.Structs[0] {
			t.Errorf("%s: Versions did not swap in the held instance", v.Model.Path)
		}
	}
}

func TestSnapshotRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	recorded, err := openStore(t, dir, "bin-1").Record("rev1", sample())
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	s := openStore(t, dir, "bin-1") // a restart: nothing in memory
	got, ok, err := s.Snapshot("rev1")
	if err != nil || !ok {
		t.Fatalf("Snapshot = ok %v, err %v", ok, err)
	}
	if !reflect.DeepEqual(got, recorded) {
		t.Fatalf("round trip changed the snapshot:\n got %+v\nwant %+v", got, recorded)
	}
	if got[0].Model.Path != "a" {
		t.Errorf("snapshot not sorted by path: %s first", got[0].Model.Path)
	}
	// A second read is served from memory, as the same instances.
	again, _, _ := s.Snapshot("rev1")
	if &again[0].Model.Structs[0] != &got[0].Model.Structs[0] {
		t.Error("a second read of one snapshot decoded a second copy")
	}
	if _, ok, err := s.Snapshot("other"); ok || err != nil {
		t.Fatalf("unknown rev: ok %v err %v", ok, err)
	}
}

// A model written by another binary is not served: the parser that wrote it
// may not produce it from the same source any more.
func TestAnotherBinaryStartsEmptyAndRemovesTheOldModels(t *testing.T) {
	dir := t.TempDir()
	if _, err := openStore(t, dir, "bin-1").Record("rev1", sample()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	s := openStore(t, dir, "bin-2")
	if _, ok, _ := s.Snapshot("rev1"); ok {
		t.Fatal("a snapshot recorded by another binary was served")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != stampKey("bin-2") {
		t.Fatalf("store dir holds %v, want only this binary's", entries)
	}
}

func TestRetentionDropsOldSnapshotsAndTheirVersions(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir, "bin-1")
	for i := 0; i <= snapshotLimit; i++ {
		models := []domain.PackageModel{{Path: "a", Name: "a", Imports: []string{strconv.Itoa(i)}}}
		if _, err := s.Record("rev"+strconv.Itoa(i), models); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	if _, ok, _ := s.Snapshot("rev0"); ok {
		t.Error("the snapshot beyond the limit is still served from memory")
	}
	root := filepath.Join(dir, stampKey("bin-1"))
	snaps, _ := os.ReadDir(filepath.Join(root, snapshotsDir))
	versions, _ := os.ReadDir(filepath.Join(root, versionsDir))
	if len(snaps) != snapshotLimit || len(versions) != snapshotLimit {
		t.Fatalf("disk holds %d snapshots and %d versions, want %d of each", len(snaps), len(versions), snapshotLimit)
	}
	if _, ok, _ := openStore(t, dir, "bin-1").Snapshot("rev0"); ok {
		t.Error("the evicted snapshot came back from disk")
	}
}

// The canonical instance's slices are capped at their length, so an append by
// one holder cannot write into memory another holder can see.
func TestAppendToAHeldVersionDoesNotReachOtherHolders(t *testing.T) {
	s := openStore(t, t.TempDir(), "bin-1")
	models := sample()
	models[1].Imports = append(make([]string, 0, 8), "b") // spare capacity
	v, err := s.Record("rev1", models)
	if err != nil {
		t.Fatal(err)
	}
	mine := v[0].Model
	mine.Imports = append(mine.Imports, "mine")
	theirs, _, _ := s.Snapshot("rev1")
	if got := theirs[0].Model.Imports[:cap(theirs[0].Model.Imports)]; len(got) != 1 {
		t.Fatalf("held Imports grew to %v", got)
	}
}

func TestComputeSchemaFingerprint_StableAndShapeSensitive(t *testing.T) {
	type inner struct{ A string }
	type v1 struct {
		X int
		Y inner
	}
	type v2 struct {
		X int
		Y inner
		Z string // added field — must change the fingerprint
	}
	f1 := computeSchemaFingerprint(reflect.TypeOf(v1{}))
	if f1 == "" || f1 != computeSchemaFingerprint(reflect.TypeOf(v1{})) {
		t.Fatalf("fingerprint empty or not deterministic: %q", f1)
	}
	if f1 == computeSchemaFingerprint(reflect.TypeOf(v2{})) {
		t.Error("fingerprint unchanged after adding a field")
	}
}

func TestComputeSchemaFingerprint_RecursiveTypeTerminates(t *testing.T) {
	type node struct {
		Name     string
		Children []node
	}
	if computeSchemaFingerprint(reflect.TypeOf(node{})) == "" {
		t.Fatal("recursive type produced empty fingerprint")
	}
}
