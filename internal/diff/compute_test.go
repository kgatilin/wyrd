package diff

import (
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func pkg(path, name string) domain.PackageModel {
	return domain.PackageModel{Path: path, Name: name}
}

func findChange(changes []Change, op Op, kind Kind, path string) *Change {
	for i := range changes {
		if changes[i].Op == op && changes[i].Kind == kind && changes[i].Path == path {
			return &changes[i]
		}
	}
	return nil
}

func TestCompute_NoDiff(t *testing.T) {
	cur := []domain.PackageModel{pkg("internal/svc", "svc")}
	tgt := []domain.PackageModel{pkg("internal/svc", "svc")}
	d := Compute(cur, tgt)
	if !d.IsEmpty() {
		t.Fatalf("expected empty diff, got %d changes: %+v", len(d.Changes), d.Changes)
	}
}

func TestCompute_AddedPackage(t *testing.T) {
	cur := []domain.PackageModel{pkg("internal/svc", "svc")}
	tgt := []domain.PackageModel{
		pkg("internal/svc", "svc"),
		pkg("internal/new", "new"),
	}
	d := Compute(cur, tgt)
	if got := len(d.Changes); got != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", got, d.Changes)
	}
	c := d.Changes[0]
	if c.Op != OpAdd || c.Kind != KindPackage || c.Path != "internal/new" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.After == nil {
		t.Errorf("expected After populated for add, got nil")
	}
	if c.Before != nil {
		t.Errorf("expected Before nil for add, got %+v", c.Before)
	}
}

func TestCompute_RemovedPackage(t *testing.T) {
	cur := []domain.PackageModel{
		pkg("internal/svc", "svc"),
		pkg("internal/gone", "gone"),
	}
	tgt := []domain.PackageModel{pkg("internal/svc", "svc")}
	d := Compute(cur, tgt)
	if got := len(d.Changes); got != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", got, d.Changes)
	}
	c := d.Changes[0]
	if c.Op != OpRemove || c.Kind != KindPackage || c.Path != "internal/gone" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.Before == nil {
		t.Errorf("expected Before populated for remove, got nil")
	}
}

func TestCompute_AddedStruct(t *testing.T) {
	cur := []domain.PackageModel{pkg("internal/svc", "svc")}
	tgtPkg := pkg("internal/svc", "svc")
	tgtPkg.Structs = []domain.StructDef{{Name: "Service", IsExported: true}}
	tgt := []domain.PackageModel{tgtPkg}

	d := Compute(cur, tgt)
	c := findChange(d.Changes, OpAdd, KindStruct, "internal/svc.Service")
	if c == nil {
		t.Fatalf("expected added struct, got %+v", d.Changes)
	}
}

func TestCompute_RemovedInterface(t *testing.T) {
	curPkg := pkg("internal/svc", "svc")
	curPkg.Interfaces = []domain.InterfaceDef{{Name: "Reader", IsExported: true}}
	cur := []domain.PackageModel{curPkg}
	tgt := []domain.PackageModel{pkg("internal/svc", "svc")}

	d := Compute(cur, tgt)
	c := findChange(d.Changes, OpRemove, KindInterface, "internal/svc.Reader")
	if c == nil {
		t.Fatalf("expected removed interface, got %+v", d.Changes)
	}
}

func TestCompute_ChangedMethodSignature(t *testing.T) {
	mk := func(params ...domain.ParamDef) domain.InterfaceDef {
		return domain.InterfaceDef{
			Name:       "Reader",
			IsExported: true,
			Methods: []domain.MethodDef{{
				Name:       "Read",
				IsExported: true,
				Params:     params,
			}},
		}
	}

	curPkg := pkg("internal/svc", "svc")
	curPkg.Interfaces = []domain.InterfaceDef{mk(
		domain.ParamDef{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
	)}
	tgtPkg := pkg("internal/svc", "svc")
	tgtPkg.Interfaces = []domain.InterfaceDef{mk(
		domain.ParamDef{Name: "ctx", Type: domain.TypeRef{Name: "Context", Package: "context"}},
		domain.ParamDef{Name: "id", Type: domain.TypeRef{Name: "string"}},
	)}

	d := Compute([]domain.PackageModel{curPkg}, []domain.PackageModel{tgtPkg})
	c := findChange(d.Changes, OpChange, KindInterface, "internal/svc.Reader")
	if c == nil {
		t.Fatalf("expected changed interface, got %+v", d.Changes)
	}
	if c.Before == nil || c.After == nil {
		t.Errorf("expected Before and After populated for change, got before=%v after=%v", c.Before, c.After)
	}
}

func TestCompute_ChangedStructFields(t *testing.T) {
	curPkg := pkg("internal/svc", "svc")
	curPkg.Structs = []domain.StructDef{{
		Name: "Config", IsExported: true,
		Fields: []domain.FieldDef{{Name: "Host", IsExported: true, Type: domain.TypeRef{Name: "string"}}},
	}}
	tgtPkg := pkg("internal/svc", "svc")
	tgtPkg.Structs = []domain.StructDef{{
		Name: "Config", IsExported: true,
		Fields: []domain.FieldDef{
			{Name: "Host", IsExported: true, Type: domain.TypeRef{Name: "string"}},
			{Name: "Port", IsExported: true, Type: domain.TypeRef{Name: "int"}},
		},
	}}

	d := Compute([]domain.PackageModel{curPkg}, []domain.PackageModel{tgtPkg})
	c := findChange(d.Changes, OpChange, KindStruct, "internal/svc.Config")
	if c == nil {
		t.Fatalf("expected changed struct, got %+v", d.Changes)
	}
}

func TestCompute_ConstsVarsErrors(t *testing.T) {
	curPkg := pkg("internal/svc", "svc")
	curPkg.Constants = []domain.ConstDef{{Name: "Max", IsExported: true, Value: "10"}}
	curPkg.Errors = []domain.ErrorDef{{Name: "ErrGone", IsExported: true, Message: "gone"}}
	tgtPkg := pkg("internal/svc", "svc")
	tgtPkg.Constants = []domain.ConstDef{{Name: "Max", IsExported: true, Value: "20"}}
	tgtPkg.Variables = []domain.VarDef{{Name: "Default", IsExported: true, Type: domain.TypeRef{Name: "int"}}}

	d := Compute([]domain.PackageModel{curPkg}, []domain.PackageModel{tgtPkg})
	if findChange(d.Changes, OpChange, KindConst, "internal/svc.Max") == nil {
		t.Errorf("expected const change, got %+v", d.Changes)
	}
	if findChange(d.Changes, OpRemove, KindError, "internal/svc.ErrGone") == nil {
		t.Errorf("expected error removal, got %+v", d.Changes)
	}
	if findChange(d.Changes, OpAdd, KindVar, "internal/svc.Default") == nil {
		t.Errorf("expected var add, got %+v", d.Changes)
	}
}

func TestCompute_Dependencies(t *testing.T) {
	dep := domain.Dependency{
		From: domain.SymbolRef{Package: "internal/a", Symbol: "A"},
		To:   domain.SymbolRef{Package: "internal/b", Symbol: "B"},
		Kind: domain.DependencyUses,
	}
	curPkg := pkg("internal/a", "a")
	tgtPkg := pkg("internal/a", "a")
	tgtPkg.Dependencies = []domain.Dependency{dep}

	d := Compute([]domain.PackageModel{curPkg}, []domain.PackageModel{tgtPkg})
	var found bool
	for _, c := range d.Changes {
		if c.Op == OpAdd && c.Kind == KindDep {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected added dep, got %+v", d.Changes)
	}
}

func TestCompute_StableOrderingByPath(t *testing.T) {
	cur := []domain.PackageModel{
		pkg("zeta", "zeta"),
		pkg("alpha", "alpha"),
	}
	tgt := []domain.PackageModel{
		pkg("beta", "beta"),
	}
	d := Compute(cur, tgt)
	// Expect ordering: alpha (remove), beta (add), zeta (remove)
	if len(d.Changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(d.Changes), d.Changes)
	}
	wantPaths := []string{"alpha", "beta", "zeta"}
	for i, p := range wantPaths {
		if d.Changes[i].Path != p {
			t.Errorf("change[%d].Path = %q, want %q", i, d.Changes[i].Path, p)
		}
	}
}
