package archreview

import (
	"unicode"

	"github.com/kgatilin/wyrd/internal/domain"
)

// Fixtures record dependency and call targets with **module-relative**
// package paths ("internal/adapter", never "example.com/m/internal/adapter"),
// because that is the form the Go reader produces. A fully-qualified fixture
// would pass against code that only understands the qualified shape, so it
// would prove nothing about the daemon's models.

type pkgBuilder struct {
	model domain.PackageModel
}

func newPkg(path string) *pkgBuilder {
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			name = path[i+1:]
			break
		}
	}
	return &pkgBuilder{model: domain.PackageModel{Path: path, Name: name}}
}

// layer tags the package so @layer policy selectors resolve, exactly as
// overlay.Merge would have tagged it before the report ran.
func (b *pkgBuilder) layer(name string) *pkgBuilder {
	b.model.Layer = name
	return b
}

// fn adds a package-level function occupying lines [line, line+3] of file.
func (b *pkgBuilder) fn(name, file string, line int, calls ...domain.CallEdge) *pkgBuilder {
	b.model.Functions = append(b.model.Functions, domain.FunctionDef{
		Name:       name,
		IsExported: isExportedName(name),
		SourceFile: file,
		Span:       b.span(file, line),
		Calls:      calls,
	})
	return b
}

// strct adds a struct, with any methods given as name/call pairs.
func (b *pkgBuilder) strct(name, file string, line int, methods ...domain.MethodDef) *pkgBuilder {
	b.model.Structs = append(b.model.Structs, domain.StructDef{
		Name:       name,
		IsExported: isExportedName(name),
		SourceFile: file,
		Span:       b.span(file, line),
		Methods:    methods,
	})
	return b
}

// iface adds an interface declaring the named methods.
func (b *pkgBuilder) iface(name, file string, line int, methods ...string) *pkgBuilder {
	def := domain.InterfaceDef{
		Name:       name,
		IsExported: isExportedName(name),
		SourceFile: file,
		Span:       b.span(file, line),
	}
	for _, m := range methods {
		def.Methods = append(def.Methods, domain.MethodDef{Name: m, IsExported: isExportedName(m)})
	}
	b.model.Interfaces = append(b.model.Interfaces, def)
	return b
}

// dep records a symbol-level dependency from a symbol in this package to one
// elsewhere, which is what makes a package edge.
func (b *pkgBuilder) dep(fromSym, toPkg, toSym string) *pkgBuilder {
	b.model.Dependencies = append(b.model.Dependencies, domain.Dependency{
		From: domain.SymbolRef{Package: b.model.Path, Symbol: fromSym},
		To:   domain.SymbolRef{Package: toPkg, Symbol: toSym},
		Kind: domain.DependencyUses,
	})
	return b
}

// implements records that a concrete type satisfies an interface declared in
// this package — the graph evidence that a method is used through the
// interface rather than by a direct caller.
func (b *pkgBuilder) implements(concretePkg, concrete, iface string) *pkgBuilder {
	b.model.Implementations = append(b.model.Implementations, domain.Implementation{
		Concrete:  domain.SymbolRef{Package: concretePkg, Symbol: concrete},
		Interface: domain.SymbolRef{Package: b.model.Path, Symbol: iface},
	})
	return b
}

func (b *pkgBuilder) build() domain.PackageModel { return b.model }

func (b *pkgBuilder) span(file string, line int) domain.Span {
	path := file
	if b.model.Path != "" && b.model.Path != "." {
		path = b.model.Path + "/" + file
	}
	return domain.Span{File: path, StartByte: line * 10, EndByte: line*10 + 40, StartLine: line, EndLine: line + 3}
}

// method builds a method definition with its call edges.
func method(name string, calls ...domain.CallEdge) domain.MethodDef {
	return domain.MethodDef{Name: name, IsExported: isExportedName(name), Calls: calls}
}

// callTo is a static call edge to a package-level function ("Name") or a
// method ("Type.Name") in the named package.
func callTo(pkg, symbol string) domain.CallEdge {
	return domain.CallEdge{To: domain.SymbolRef{Package: pkg, Symbol: symbol}, Count: 1}
}

func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper([]rune(name)[0])
}

// models assembles a model set from builders.
func models(builders ...*pkgBuilder) []domain.PackageModel {
	out := make([]domain.PackageModel, 0, len(builders))
	for _, b := range builders {
		out = append(out, b.build())
	}
	return out
}

// sectionByID finds a section in a report, so a test names what it asserts on
// instead of indexing into an order it also has to keep in sync.
func sectionByID(report Report, id string) (Section, bool) {
	for _, section := range report.Sections {
		if section.ID == id {
			return section, true
		}
	}
	return Section{}, false
}

// itemTexts flattens a section's rows for assertions.
func itemTexts(section Section) []string {
	out := make([]string, 0, len(section.Items))
	for _, item := range section.Items {
		out = append(out, item.Text)
	}
	return out
}
