package java

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// loadFixture decodes a JSON fixture from testdata/.
func loadFixture(t *testing.T, name string) *javaFacts {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	facts, err := decodeFacts(raw)
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return facts
}

func TestTranslate_SimpleClass(t *testing.T) {
	facts := loadFixture(t, "facts_simple.json")
	models := translate(facts)

	if len(models) != 1 {
		t.Fatalf("want 1 package, got %d", len(models))
	}
	pkg := models[0]
	if pkg.Path != "com.example" {
		t.Errorf("path: want com.example, got %q", pkg.Path)
	}
	if pkg.Name != "example" {
		t.Errorf("name: want example, got %q", pkg.Name)
	}
	if len(pkg.Structs) != 1 {
		t.Fatalf("want 1 struct, got %d", len(pkg.Structs))
	}
	s := pkg.Structs[0]
	if s.Name != "Greeter" || !s.IsExported {
		t.Errorf("struct: want Greeter+exported, got %+v", s)
	}
	if len(s.Fields) != 1 || s.Fields[0].Name != "prefix" {
		t.Errorf("fields: %+v", s.Fields)
	}
	// Two methods: the constructor (Greeter) and greet().
	if len(s.Methods) != 2 {
		t.Fatalf("methods: %+v", s.Methods)
	}
	var greet domain.MethodDef
	for _, m := range s.Methods {
		if m.Name == "greet" {
			greet = m
		}
	}
	if greet.Name == "" || len(greet.Returns) != 1 || greet.Returns[0].Name != "String" {
		t.Errorf("greet return: %+v", greet)
	}
}

func TestTranslate_Inheritance(t *testing.T) {
	facts := loadFixture(t, "facts_inheritance.json")
	models := translate(facts)
	if len(models) != 1 {
		t.Fatalf("want 1 package, got %d", len(models))
	}
	pkg := models[0]

	// Animal + Dog land as structs; Trainable lands as an interface.
	if len(pkg.Structs) != 2 {
		t.Errorf("structs: want 2, got %d (%v)", len(pkg.Structs), structNames(pkg.Structs))
	}
	if len(pkg.Interfaces) != 1 || pkg.Interfaces[0].Name != "Trainable" {
		t.Errorf("interfaces: %+v", pkg.Interfaces)
	}

	// Dog extends Animal must be present as a Dependency{extends}, with
	// the target resolved to the in-source Animal class (External=false).
	var sawExtends, sawImplements bool
	for _, dep := range pkg.Dependencies {
		if dep.From.Symbol == "Dog" && dep.Kind == domain.DependencyExtends {
			sawExtends = true
			if dep.To.Symbol != "Animal" || dep.To.External {
				t.Errorf("extends edge: want Dog→Animal in-source, got %+v", dep)
			}
		}
		if dep.From.Symbol == "Dog" && dep.Kind == domain.DependencyImplements {
			sawImplements = true
		}
	}
	if !sawExtends {
		t.Errorf("missing Dog→Animal extends edge: %+v", pkg.Dependencies)
	}
	if !sawImplements {
		t.Errorf("missing Dog→Trainable implements edge: %+v", pkg.Dependencies)
	}

	// Dog implements Trainable → one Implementation entry with concrete=Dog.
	var sawImpl bool
	for _, impl := range pkg.Implementations {
		if impl.Concrete.Symbol == "Dog" && impl.Interface.Symbol == "Trainable" {
			sawImpl = true
		}
	}
	if !sawImpl {
		t.Errorf("missing Dog→Trainable Implementation: %+v", pkg.Implementations)
	}

	// imports → uses dependencies (Dog uses java.util.List).
	var sawUses bool
	for _, dep := range pkg.Dependencies {
		if dep.Kind == domain.DependencyUses && dep.To.Symbol == "List" {
			sawUses = true
		}
	}
	if !sawUses {
		t.Errorf("expected uses dep on java.util.List: %+v", pkg.Dependencies)
	}
}

func TestTranslate_Calls_ResolvedOnly(t *testing.T) {
	facts := loadFixture(t, "facts_inheritance.json")
	models := translate(facts)
	pkg := models[0]

	// describe() in Animal calls sound() resolving to com.example.Animal.
	var animal domain.StructDef
	for _, s := range pkg.Structs {
		if s.Name == "Animal" {
			animal = s
		}
	}
	if animal.Name == "" {
		t.Fatalf("Animal not found in %v", structNames(pkg.Structs))
	}
	var describe domain.MethodDef
	for _, m := range animal.Methods {
		if m.Name == "describe" {
			describe = m
		}
	}
	if describe.Name == "" {
		t.Fatalf("describe not found")
	}
	if len(describe.Calls) != 1 {
		t.Fatalf("calls: want 1 resolved, got %d (%+v)", len(describe.Calls), describe.Calls)
	}
	got := describe.Calls[0]
	if got.To.Package != "com.example" || got.To.Symbol != "Animal.sound" {
		t.Errorf("call edge: want com.example/Animal.sound, got %+v", got.To)
	}

	// Dog.learn calls System.out.println — external — should be dropped.
	var dog domain.StructDef
	for _, s := range pkg.Structs {
		if s.Name == "Dog" {
			dog = s
		}
	}
	for _, m := range dog.Methods {
		if m.Name == "learn" && len(m.Calls) != 0 {
			t.Errorf("learn(): external calls must be dropped, got %+v", m.Calls)
		}
	}
}

func TestTranslate_Record_AsValueStruct(t *testing.T) {
	facts := loadFixture(t, "facts_record.json")
	models := translate(facts)
	if len(models) != 1 {
		t.Fatalf("packages: %+v", models)
	}
	pkg := models[0]
	if len(pkg.Structs) != 1 {
		t.Fatalf("structs: %+v", pkg.Structs)
	}
	s := pkg.Structs[0]
	if s.Stereotype != domain.StereotypeValue {
		t.Errorf("record should be StereotypeValue, got %q", s.Stereotype)
	}
}

func TestTranslate_SealedAndEnums(t *testing.T) {
	facts := loadFixture(t, "facts_sealed_enums.json")
	models := translate(facts)
	if len(models) != 1 {
		t.Fatalf("packages: %+v", models)
	}
	pkg := models[0]

	// At least one enum should land in TypeDefs with StereotypeEnum.
	var foundEnum bool
	for _, td := range pkg.TypeDefs {
		if td.Stereotype == domain.StereotypeEnum && len(td.Constants) > 0 {
			foundEnum = true
		}
	}
	if !foundEnum {
		t.Errorf("expected at least one enum TypeDef, got %+v", pkg.TypeDefs)
	}
}

func TestTranslate_DeterministicOrdering(t *testing.T) {
	// Same input twice → identical output.
	facts := loadFixture(t, "facts_inheritance.json")
	a := translate(facts)
	b := translate(facts)
	if !sameModelShape(a, b) {
		t.Errorf("translate not deterministic")
	}
}

func TestTranslate_StereotypeHeuristics(t *testing.T) {
	cases := []struct {
		name string
		in   javaClass
		want domain.Stereotype
	}{
		{"repository suffix", javaClass{Name: "UserRepository", Kind: "class"}, domain.StereotypeRepository},
		{"dao suffix", javaClass{Name: "UserDao", Kind: "class"}, domain.StereotypeRepository},
		{"service suffix", javaClass{Name: "UserService", Kind: "class"}, domain.StereotypeService},
		{"controller suffix", javaClass{Name: "UserController", Kind: "class"}, domain.StereotypeService},
		{
			"RestController annotation",
			javaClass{
				Name: "Users", Kind: "class",
				Annotations: []javaAnnotation{{FQN: "org.springframework.web.bind.annotation.RestController"}},
			},
			domain.StereotypeService,
		},
		{"plain class", javaClass{Name: "Plain", Kind: "class"}, domain.StereotypeNone},
		{"record", javaClass{Name: "Point", Kind: "record"}, domain.StereotypeValue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectClassStereotype(c.in)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTranslate_FactoryMethodLifts(t *testing.T) {
	c := javaClass{
		Name: "Point", FQN: "com.example.Point", Kind: "class",
		Methods: []javaMethod{
			{
				Name: "of", Kind: "method", Modifiers: []string{"public", "static"},
				Returns: "Point",
			},
		},
	}
	st := detectFactoryStereotype(c, c.Methods[0])
	if st != domain.StereotypeFactory {
		t.Errorf("static of()→Point should be factory, got %q", st)
	}
	// Non-static `of` is not a factory.
	c.Methods[0].Modifiers = []string{"public"}
	if detectFactoryStereotype(c, c.Methods[0]) == domain.StereotypeFactory {
		t.Error("non-static of() should not be factory")
	}
}

func TestParseTypeRef(t *testing.T) {
	cases := []struct {
		in          string
		wantName    string
		wantPackage string
		wantSlice   bool
	}{
		{"int", "int", "", false},
		{"String", "String", "", false},
		{"java.util.List", "List", "java.util", false},
		{"List<String>", "List<String>", "", false},
		{"java.util.List<String>", "List<String>", "java.util", false},
		{"int[]", "int", "", true},
		{"String[][]", "String", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref := parseTypeRef(c.in, "com.foo")
			if ref.Name != c.wantName || ref.Package != c.wantPackage || ref.IsSlice != c.wantSlice {
				t.Errorf("got %+v", ref)
			}
		})
	}
}

func TestSchemaVersionMismatchRejected(t *testing.T) {
	bad := []byte(`{"schema":"javafacts/v0","src_roots":[],"packages":[],"classes":[],"imports":[],"parse_warnings":[]}`)
	if _, err := decodeFacts(bad); err == nil {
		t.Error("decodeFacts should reject unknown schema version")
	}
}

func structNames(ss []domain.StructDef) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

// sameModelShape compares two slices of PackageModel for shape equality
// (counts of each member kind). A full deep-equal on PackageModel is
// brittle and unnecessary for a determinism check.
func sameModelShape(a, b []domain.PackageModel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path ||
			len(a[i].Structs) != len(b[i].Structs) ||
			len(a[i].Interfaces) != len(b[i].Interfaces) ||
			len(a[i].TypeDefs) != len(b[i].TypeDefs) ||
			len(a[i].Dependencies) != len(b[i].Dependencies) ||
			len(a[i].Implementations) != len(b[i].Implementations) {
			return false
		}
	}
	return true
}
