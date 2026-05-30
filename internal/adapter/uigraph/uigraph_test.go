package uigraph

import (
	"testing"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
)

func TestParseChangePath(t *testing.T) {
	cases := []struct {
		in         string
		wantPkg    string
		wantType   string
		wantMember string
		wantLevel  string // "package" | "type" | "member"
	}{
		{"internal/service", "internal/service", "", "", "package"},
		{"internal/service.Service", "internal/service", "Service", "", "type"},
		{"internal/service.Service.Handle", "internal/service", "Service", "Handle", "member"},
		{"github.com/x/y/pkg.T.M", "github.com/x/y/pkg", "T", "M", "member"},
	}
	for _, c := range cases {
		got := parseChangePath(c.in)
		if got.Pkg != c.wantPkg || got.Type != c.wantType || got.Member != c.wantMember || got.Level != c.wantLevel {
			t.Errorf("parseChangePath(%q) = %+v, want pkg=%q type=%q member=%q level=%q",
				c.in, got, c.wantPkg, c.wantType, c.wantMember, c.wantLevel)
		}
	}
}

func TestProjectMarksAddedInterface(t *testing.T) {
	// "current" has an interface Svc.
	// "target" has no interfaces (Svc is new in current).
	// After diff.Compute(current, target), Svc should show as OpAdd.
	current := []domain.PackageModel{
		{
			Path: "internal/svc",
			Name: "svc",
			Interfaces: []domain.InterfaceDef{
				{
					Name:       "Svc",
					IsExported: true,
					Methods: []domain.MethodDef{
						{Name: "Handle", IsExported: true},
					},
				},
			},
		},
	}
	target := []domain.PackageModel{
		{
			Path: "internal/svc",
			Name: "svc",
			// No interfaces - Svc is new
		},
	}

	d := diff.Compute(current, target)

	g, err := Project(current, nil, d)
	if err != nil {
		t.Fatal(err)
	}

	if g.Schema != Schema {
		t.Errorf("Schema = %q, want %q", g.Schema, Schema)
	}
	if len(g.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(g.Components))
	}

	comp := g.Components[0]
	if comp.ID != "internal/svc" {
		t.Errorf("Component.ID = %q, want %q", comp.ID, "internal/svc")
	}

	var svcInternal *Internal
	for i := range comp.Internals {
		if comp.Internals[i].Name == "Svc" {
			svcInternal = &comp.Internals[i]
			break
		}
	}
	if svcInternal == nil {
		t.Fatal("Svc internal not found")
	}
	if svcInternal.Kind != "iface" {
		t.Errorf("Svc.Kind = %q, want %q", svcInternal.Kind, "iface")
	}
	// diff.Compute sees Svc in current but not in target => OpAdd
	if svcInternal.Diff != "added" {
		t.Errorf("Svc.Diff = %q, want %q", svcInternal.Diff, "added")
	}

	// Check that the member is present
	if len(svcInternal.Members) != 1 {
		t.Fatalf("len(Members) = %d, want 1", len(svcInternal.Members))
	}
	if svcInternal.Members[0].Name != "Handle()" {
		t.Errorf("Member.Name = %q, want %q", svcInternal.Members[0].Name, "Handle()")
	}
}

func TestProjectWithNoDiff(t *testing.T) {
	// Test that Project works without a diff
	models := []domain.PackageModel{
		{
			Path: "internal/domain",
			Name: "domain",
			Structs: []domain.StructDef{
				{
					Name:       "Order",
					IsExported: true,
					Fields: []domain.FieldDef{
						{Name: "ID", IsExported: true, Type: domain.TypeRef{Name: "string"}},
					},
				},
			},
		},
	}

	g, err := Project(models, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if g.Schema != Schema {
		t.Errorf("Schema = %q, want %q", g.Schema, Schema)
	}
	if g.PR != nil {
		t.Error("PR should be nil when no diff")
	}
	if len(g.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(g.Components))
	}

	comp := g.Components[0]
	if comp.Name != "domain" {
		t.Errorf("Component.Name = %q, want %q", comp.Name, "domain")
	}
	if comp.Tech != "Go" {
		t.Errorf("Component.Tech = %q, want %q", comp.Tech, "Go")
	}

	// Check struct is an internal
	if len(comp.Internals) != 1 {
		t.Fatalf("len(Internals) = %d, want 1", len(comp.Internals))
	}
	internal := comp.Internals[0]
	if internal.Kind != "class" {
		t.Errorf("Internal.Kind = %q, want %q", internal.Kind, "class")
	}
	if internal.Name != "Order" {
		t.Errorf("Internal.Name = %q, want %q", internal.Name, "Order")
	}

	// Check field is a member
	if len(internal.Members) != 1 {
		t.Fatalf("len(Members) = %d, want 1", len(internal.Members))
	}
	member := internal.Members[0]
	if member.Kind != "prop" {
		t.Errorf("Member.Kind = %q, want %q", member.Kind, "prop")
	}
}
