package java

import (
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// translate converts a JavaFacts document to the equivalent slice of
// domain.PackageModel.
//
// Mapping rules (v1):
//
//	Java package "com.foo.bar"  → PackageModel{Path: "com.foo.bar", Name: "bar"}
//	Java class                  → StructDef
//	Java interface              → InterfaceDef
//	Java enum                   → TypeDef (Constants populated from enum_constants)
//	Java record                 → StructDef + Stereotype "value"
//	Java annotation             → TypeDef
//	extends                     → Dependency{Kind: "extends"} (class kinds)
//	implements                  → Dependency{Kind: "implements"} +
//	                              domain.Implementation when the interface
//	                              is in the parsed source set
//	imports                     → Dependency{Kind: "uses"}
//	calls (target_fqn != "")    → CallEdge on the source method
//	modifiers contains "public" → IsExported = true
//
// translate is a pure function: no I/O, no globals, deterministic ordering
// (packages sorted by Path; classes within a package sorted by simple
// name; field/method order preserved as emitted by the JAR).
func translate(facts *javaFacts) []domain.PackageModel {
	if facts == nil {
		return nil
	}

	// Group classes by Java package, preserving the JAR's class ordering
	// (the JAR sorts by FQN, so this is already deterministic).
	byPkg := make(map[string][]javaClass)
	for _, c := range facts.Classes {
		byPkg[c.Package] = append(byPkg[c.Package], c)
	}

	// Index of FQN → simple class for fast in-source lookup. Used to decide
	// whether an `implements` / `extends` / call target lives in the parsed
	// source set (becomes a non-external SymbolRef) or not.
	classByFQN := make(map[string]javaClass, len(facts.Classes))
	for _, c := range facts.Classes {
		classByFQN[c.FQN] = c
	}

	// Group imports by the importing class's FQN so we can attach uses
	// dependencies per package.
	importsByFrom := make(map[string][]javaImport)
	for _, imp := range facts.Imports {
		importsByFrom[imp.From] = append(importsByFrom[imp.From], imp)
	}

	pkgPaths := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgPaths = append(pkgPaths, p)
	}
	sort.Strings(pkgPaths)

	models := make([]domain.PackageModel, 0, len(pkgPaths))
	for _, pkgPath := range pkgPaths {
		models = append(models, buildPackage(pkgPath, byPkg[pkgPath], classByFQN, importsByFrom))
	}
	return models
}

// buildPackage assembles one PackageModel from a slice of classes that all
// share the same Java package name.
func buildPackage(
	pkgPath string,
	classes []javaClass,
	classByFQN map[string]javaClass,
	importsByFrom map[string][]javaImport,
) domain.PackageModel {
	model := domain.PackageModel{
		Path: pkgPath,
		Name: simpleName(pkgPath),
	}

	for _, c := range classes {
		appendClass(&model, c, classByFQN, importsByFrom[c.FQN])
	}

	// Stable ordering for deterministic golden tests. Java pkg has no
	// "natural" intra-package order, so sort by simple name.
	sort.SliceStable(model.Interfaces, func(i, j int) bool { return model.Interfaces[i].Name < model.Interfaces[j].Name })
	sort.SliceStable(model.Structs, func(i, j int) bool { return model.Structs[i].Name < model.Structs[j].Name })
	sort.SliceStable(model.TypeDefs, func(i, j int) bool { return model.TypeDefs[i].Name < model.TypeDefs[j].Name })
	sort.SliceStable(model.Implementations, func(i, j int) bool {
		if model.Implementations[i].Concrete.Symbol != model.Implementations[j].Concrete.Symbol {
			return model.Implementations[i].Concrete.Symbol < model.Implementations[j].Concrete.Symbol
		}
		return model.Implementations[i].Interface.QualifiedName() < model.Implementations[j].Interface.QualifiedName()
	})
	sort.SliceStable(model.Dependencies, func(i, j int) bool {
		if model.Dependencies[i].From.Symbol != model.Dependencies[j].From.Symbol {
			return model.Dependencies[i].From.Symbol < model.Dependencies[j].From.Symbol
		}
		if model.Dependencies[i].Kind != model.Dependencies[j].Kind {
			return string(model.Dependencies[i].Kind) < string(model.Dependencies[j].Kind)
		}
		return model.Dependencies[i].To.QualifiedName() < model.Dependencies[j].To.QualifiedName()
	})

	return model
}

// appendClass dispatches a JavaClass to the right slice on the model,
// depending on its kind, and records its inheritance / implements / uses
// dependencies.
func appendClass(
	model *domain.PackageModel,
	c javaClass,
	classByFQN map[string]javaClass,
	imports []javaImport,
) {
	switch c.Kind {
	case "interface":
		model.Interfaces = append(model.Interfaces, buildInterface(c))
	case "enum":
		model.TypeDefs = append(model.TypeDefs, buildEnum(c))
	case "annotation":
		model.TypeDefs = append(model.TypeDefs, buildAnnotation(c))
	default: // class, record
		model.Structs = append(model.Structs, buildStruct(c))
	}

	addInheritanceEdges(model, c, classByFQN)
	addImportEdges(model, c, imports)
}

func buildInterface(c javaClass) domain.InterfaceDef {
	return domain.InterfaceDef{
		Name:       c.Name,
		Methods:    convertMethods(c.Methods, c.Package),
		IsExported: containsModifier(c.Modifiers, "public"),
		SourceFile: c.SourceFile,
		Doc:        c.Doc,
		Stereotype: detectInterfaceStereotype(c),
	}
}

func buildStruct(c javaClass) domain.StructDef {
	stereotype := detectClassStereotype(c)
	methods := convertMethods(c.Methods, c.Package)
	for i := range methods {
		// If the class itself has no stereotype, look for a factory method
		// to bubble up to the type level — matches the Go adapter's
		// "factory function" heuristic, but Java factories live as static
		// methods on a class rather than as standalone funcs.
		if stereotype == domain.StereotypeNone {
			if s := detectFactoryStereotype(c, c.Methods[i]); s != domain.StereotypeNone {
				stereotype = s
				break
			}
		}
	}
	return domain.StructDef{
		Name:       c.Name,
		Fields:     convertFields(c.Fields, c.Package),
		Methods:    methods,
		IsExported: containsModifier(c.Modifiers, "public"),
		SourceFile: c.SourceFile,
		Doc:        c.Doc,
		Stereotype: stereotype,
	}
}

func buildEnum(c javaClass) domain.TypeDef {
	return domain.TypeDef{
		Name:           c.Name,
		UnderlyingType: domain.TypeRef{Name: "enum"},
		Constants:      append([]string(nil), c.EnumConstants...),
		IsExported:     containsModifier(c.Modifiers, "public"),
		SourceFile:     c.SourceFile,
		Doc:            c.Doc,
		Stereotype:     domain.StereotypeEnum,
	}
}

func buildAnnotation(c javaClass) domain.TypeDef {
	return domain.TypeDef{
		Name:           c.Name,
		UnderlyingType: domain.TypeRef{Name: "annotation"},
		IsExported:     containsModifier(c.Modifiers, "public"),
		SourceFile:     c.SourceFile,
		Doc:            c.Doc,
	}
}

// addInheritanceEdges emits Dependency{extends} for `extends` and
// Dependency{implements} (+ Implementation) for `implements`. The kind on
// `interface extends X` is treated as `implements` per SCHEMA.md (the JAR
// canonicalises super-interfaces into the implements slot).
func addInheritanceEdges(model *domain.PackageModel, c javaClass, classByFQN map[string]javaClass) {
	from := classRef(c)

	if c.Extends != "" {
		to := refForName(c.Extends, classByFQN)
		model.Dependencies = append(model.Dependencies, domain.Dependency{
			From: from, To: to, Kind: domain.DependencyExtends, ThroughExported: true,
		})
	}

	for _, impl := range c.Implements {
		to := refForName(impl, classByFQN)
		model.Dependencies = append(model.Dependencies, domain.Dependency{
			From: from, To: to, Kind: domain.DependencyImplements, ThroughExported: true,
		})
		// Only record an Implementation when the interface is in the parsed
		// source set — a domain.Implementation cross-package reference is
		// only meaningful for types we actually know about.
		if !to.External && c.Kind != "interface" {
			model.Implementations = append(model.Implementations, domain.Implementation{
				Concrete:  from,
				Interface: to,
			})
		}
	}
}

// addImportEdges turns every `import` statement on a class into a
// Dependency{uses} edge. Kept out of the per-class struct because imports
// are a unit-level concept; from the diagram's perspective they belong to
// the importing class.
func addImportEdges(model *domain.PackageModel, c javaClass, imports []javaImport) {
	from := classRef(c)
	for _, imp := range imports {
		// Wildcard imports have no concrete target symbol; skip them.
		if imp.Kind == "wildcard" || imp.Kind == "static_wildcard" {
			continue
		}
		model.Dependencies = append(model.Dependencies, domain.Dependency{
			From: from, To: fqnRef(imp.ToClass), Kind: domain.DependencyUses,
		})
	}
}

func convertFields(in []javaField, pkgPath string) []domain.FieldDef {
	out := make([]domain.FieldDef, 0, len(in))
	for _, f := range in {
		out = append(out, domain.FieldDef{
			Name:       f.Name,
			Type:       parseTypeRef(f.Type, pkgPath),
			IsExported: containsModifier(f.Modifiers, "public"),
		})
	}
	return out
}

func convertMethods(in []javaMethod, pkgPath string) []domain.MethodDef {
	out := make([]domain.MethodDef, 0, len(in))
	for _, m := range in {
		method := domain.MethodDef{
			Name:       m.Name,
			Params:     convertParams(m.Params, pkgPath),
			IsExported: containsModifier(m.Modifiers, "public"),
			Calls:      convertCalls(m.Calls),
		}
		// Constructors return their enclosing type implicitly — modelling
		// `void` here would lie. We follow the JAR's choice (`returns:
		// "void"`) for plain methods and skip Returns for constructors.
		if m.Kind != "constructor" && m.Returns != "" && m.Returns != "void" {
			method.Returns = []domain.TypeRef{parseTypeRef(m.Returns, pkgPath)}
		}
		out = append(out, method)
	}
	return out
}

func convertParams(in []javaParam, pkgPath string) []domain.ParamDef {
	out := make([]domain.ParamDef, 0, len(in))
	for _, p := range in {
		ref := parseTypeRef(p.Type, pkgPath)
		if p.Varargs {
			// Varargs are slices over the element type at the call site.
			ref.IsSlice = true
		}
		out = append(out, domain.ParamDef{Name: p.Name, Type: ref})
	}
	return out
}

// convertCalls keeps only resolved calls — those that bound to a class in
// the analyzed source set (target_fqn != ""). External / unresolved calls
// would render as edges to nothing, so we drop them per SCHEMA.md.
func convertCalls(in []javaCall) []domain.CallEdge {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.CallEdge, 0, len(in))
	for _, c := range in {
		if c.External || c.TargetFQN == "" {
			continue
		}
		out = append(out, domain.CallEdge{To: fqnMethodRef(c.TargetFQN, c.ToMethod)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseTypeRef builds a domain.TypeRef from a Java type as written:
// "int", "String", "List<String>", "java.util.List<String>", "int[]".
//
// Type-parameter substitution is intentionally shallow (matches SCHEMA.md
// "What's not in v1" section). Generic args are preserved verbatim in
// TypeRef.Name so the diagram can still print "List<String>".
func parseTypeRef(raw, pkgPath string) domain.TypeRef {
	t := strings.TrimSpace(raw)
	if t == "" {
		return domain.TypeRef{}
	}

	ref := domain.TypeRef{}

	// Java arrays: `String[]` / `int[][]`. Drop one bracket pair → slice.
	for strings.HasSuffix(t, "[]") {
		ref.IsSlice = true
		t = strings.TrimSuffix(t, "[]")
	}

	// Split fqn / simple. We don't try to follow type parameters across
	// the dot (Java doesn't allow "."-qualified generics) so simple name
	// extraction is just "last segment before the first <".
	bare := t
	if idx := strings.Index(t, "<"); idx >= 0 {
		bare = t[:idx]
	}
	if dot := strings.LastIndex(bare, "."); dot >= 0 {
		ref.Package = bare[:dot]
		// preserve generic suffix on the type name itself
		ref.Name = bare[dot+1:] + t[len(bare):]
	} else {
		// Local type or builtin → leave Package empty; D2 layer handles it.
		ref.Name = t
	}

	return ref
}

// classRef returns a SymbolRef for an in-source class, populated for the
// translator's edge construction.
func classRef(c javaClass) domain.SymbolRef {
	return domain.SymbolRef{Package: c.Package, File: c.SourceFile, Symbol: c.Name}
}

// refForName resolves a textual `extends` / `implements` target. If the
// name is known (in classByFQN) the SymbolRef points into the source set;
// otherwise it's external. Names without a "." are looked up by simple
// name as a best-effort match — Java implements clauses often appear
// unqualified when an `import` brought the type in.
func refForName(name string, classByFQN map[string]javaClass) domain.SymbolRef {
	// Strip generic args.
	bare := name
	if idx := strings.Index(name, "<"); idx >= 0 {
		bare = name[:idx]
	}
	if class, ok := classByFQN[bare]; ok {
		return classRef(class)
	}
	if !strings.Contains(bare, ".") {
		for _, class := range classByFQN {
			if class.Name == bare {
				return classRef(class)
			}
		}
	}
	return fqnRef(bare)
}

// fqnRef makes a SymbolRef for a fully-qualified type name that lives
// outside the parsed source set.
func fqnRef(fqn string) domain.SymbolRef {
	pkg, sym := splitFQN(fqn)
	return domain.SymbolRef{Package: pkg, Symbol: sym, External: true}
}

// fqnMethodRef builds a SymbolRef pointing at a specific method on an
// in-source class.
func fqnMethodRef(targetFQN, method string) domain.SymbolRef {
	pkg, cls := splitFQN(targetFQN)
	return domain.SymbolRef{Package: pkg, Symbol: cls + "." + method}
}

// splitFQN cuts a Java FQN at its last dot. "com.foo.Bar" → ("com.foo", "Bar").
// FQNs without a dot (rare — usually default-package tests) yield an empty
// package.
func splitFQN(fqn string) (pkg, simple string) {
	if dot := strings.LastIndex(fqn, "."); dot >= 0 {
		return fqn[:dot], fqn[dot+1:]
	}
	return "", fqn
}

// simpleName returns the last dot-separated segment of a Java package
// path. "com.foo.bar" → "bar"; "" → "".
func simpleName(pkgPath string) string {
	if dot := strings.LastIndex(pkgPath, "."); dot >= 0 {
		return pkgPath[dot+1:]
	}
	return pkgPath
}
