package apply

import (
	"testing"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
)

func TestApply_NilDiff_ReturnsClone(t *testing.T) {
	target := []domain.PackageModel{{Path: "p", Name: "p"}}
	out, err := Apply(nil, nil, target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0].Path != "p" {
		t.Fatalf("got %+v, want 1 package 'p'", out)
	}
	// Mutate original; result must be independent.
	target[0].Path = "mutated"
	if out[0].Path != "p" {
		t.Fatalf("result shares memory with input target")
	}
}

func TestApply_AddFunction(t *testing.T) {
	// Current has NewFunc, target doesn't; diff.Compute emits OpRemove
	// because current has something target lacks, and Apply reverses it
	// by adding NewFunc into target.
	current := []domain.PackageModel{{
		Path: "pkg",
		Name: "pkg",
		Functions: []domain.FunctionDef{
			{Name: "Existing", IsExported: true},
			{Name: "NewFunc", IsExported: true},
		},
	}}
	target := []domain.PackageModel{{
		Path: "pkg",
		Name: "pkg",
		Functions: []domain.FunctionDef{
			{Name: "Existing", IsExported: true},
		},
	}}
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpRemove, Kind: diff.KindFunction, Path: "pkg.NewFunc"},
	}}

	out, err := Apply(d, current, target)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d packages, want 1", len(out))
	}
	if len(out[0].Functions) != 2 {
		t.Fatalf("got %d functions, want 2: %+v", len(out[0].Functions), out[0].Functions)
	}
	found := false
	for _, f := range out[0].Functions {
		if f.Name == "NewFunc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("NewFunc not added")
	}
}

func TestApply_RemoveFunction(t *testing.T) {
	// Target has Old, current doesn't; diff.Compute emits OpAdd,
	// Apply reverses by dropping Old from target.
	current := []domain.PackageModel{{
		Path: "pkg", Name: "pkg",
		Functions: []domain.FunctionDef{{Name: "Keep", IsExported: true}},
	}}
	target := []domain.PackageModel{{
		Path: "pkg",
		Name: "pkg",
		Functions: []domain.FunctionDef{
			{Name: "Old", IsExported: true},
			{Name: "Keep", IsExported: true},
		},
	}}
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindFunction, Path: "pkg.Old"},
	}}
	out, err := Apply(d, current, target)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out[0].Functions) != 1 || out[0].Functions[0].Name != "Keep" {
		t.Fatalf("unexpected functions after remove: %+v", out[0].Functions)
	}
}

func TestApply_ChangeStruct(t *testing.T) {
	current := []domain.PackageModel{{
		Path: "pkg", Name: "pkg",
		Structs: []domain.StructDef{
			{Name: "S", IsExported: true, Fields: []domain.FieldDef{{Name: "X", Type: domain.TypeRef{Name: "int"}, IsExported: true}}},
		},
	}}
	target := []domain.PackageModel{{
		Path: "pkg", Name: "pkg",
		Structs: []domain.StructDef{
			{Name: "S", IsExported: true},
		},
	}}
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpChange, Kind: diff.KindStruct, Path: "pkg.S"},
	}}
	out, err := Apply(d, current, target)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out[0].Structs[0].Fields) != 1 || out[0].Structs[0].Fields[0].Name != "X" {
		t.Fatalf("struct not updated from current: %+v", out[0].Structs[0])
	}
}

func TestApply_AddPackage(t *testing.T) {
	// current has the package, target lacks it -> OpRemove, apply copies in.
	current := []domain.PackageModel{{Path: "new/pkg", Name: "pkg"}}
	target := []domain.PackageModel{}
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpRemove, Kind: diff.KindPackage, Path: "new/pkg"},
	}}
	out, err := Apply(d, current, target)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out) != 1 || out[0].Path != "new/pkg" {
		t.Fatalf("package not added: %+v", out)
	}
}

func TestApply_RemovePackage(t *testing.T) {
	// target has pkg, current doesn't -> OpAdd, apply drops from target.
	current := []domain.PackageModel{}
	target := []domain.PackageModel{{Path: "old/pkg", Name: "pkg"}}
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpAdd, Kind: diff.KindPackage, Path: "old/pkg"},
	}}
	out, err := Apply(d, current, target)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("package not removed: %+v", out)
	}
}

func TestApply_UnknownOp_Errors(t *testing.T) {
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.Op("bogus"), Kind: diff.KindFunction, Path: "pkg.F"},
	}}
	if _, err := Apply(d, nil, nil); err == nil {
		t.Fatalf("expected error for unknown op")
	}
}

func TestApply_UnknownKind_Errors(t *testing.T) {
	current := []domain.PackageModel{{Path: "pkg", Name: "pkg"}}
	target := []domain.PackageModel{{Path: "pkg", Name: "pkg"}}
	d := &diff.Diff{Changes: []diff.Change{
		{Op: diff.OpChange, Kind: diff.Kind("bogus"), Path: "pkg.F"},
	}}
	if _, err := Apply(d, current, target); err == nil {
		t.Fatalf("expected error for unknown kind")
	}
}

func TestApply_ComputeThenApply_IsIdentity(t *testing.T) {
	// The apply of a computed diff should make target match current.
	current := []domain.PackageModel{{
		Path: "pkg", Name: "pkg",
		Functions: []domain.FunctionDef{
			{Name: "A", IsExported: true},
			{Name: "B", IsExported: true},
		},
	}}
	target := []domain.PackageModel{{
		Path: "pkg", Name: "pkg",
		Functions: []domain.FunctionDef{
			{Name: "A", IsExported: true},
		},
	}}

	d := diff.Compute(current, target)
	out, err := Apply(d, current, target)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// After applying, computing a new diff between current and out must be empty.
	d2 := diff.Compute(current, out)
	if !d2.IsEmpty() {
		t.Fatalf("expected empty diff after applying, got %d changes: %+v", len(d2.Changes), d2.Changes)
	}
}
