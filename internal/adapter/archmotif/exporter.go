// Package archmotif adapts archai's design-time domain model
// (a slice of domain.PackageModel built from archai.yaml plus
// per-package .arch/*.yaml specs) into an archmotif typed graph
// constructed through the public pkg/archmotifimport shim.
//
// archai depends on archmotif; archmotif never depends on archai
// (see kgatilin/archmotif#53 — the import shim is intentionally
// archai-unaware). With this adapter, archai's target architecture
// becomes a first-class analyzable artifact for any downstream
// archmotif analysis (motifs, anomalies, patterns, modularity).
//
// # Stable IDs
//
// IDs are derived purely from path + symbol name so the same input
// always produces the same graph (ordering of nodes is also
// deterministic — packages, then their symbols, processed in input
// order, all sorted by stable id within a kind so additions to the
// caller's slice don't reorder existing ids).
//
//	package          pkg:<package-path>
//	interface/struct/typedef type:<package-path>.<Name>
//	function         fn:<package-path>.<Name>
//	method           method:<package-path>.<RecvName>.<MethodName>
//	field            field:<package-path>.<StructName>.<FieldName>
//
// External symbols (Dependency.To.External == true) are skipped:
// archmotif's typed graph models the loaded package set, not
// arbitrary third-party packages.
//
// # Stereotype → archmotif role
//
//	StereotypeService    -> "service"
//	StereotypeRepository -> "repository"
//	StereotypePort       -> "port"
//	StereotypeFactory    -> "factory"
//	StereotypeAggregate  -> "aggregate"
//	StereotypeEntity     -> "entity"
//	StereotypeValue      -> "value"
//	StereotypeEnum       -> "enum"
//	StereotypeInterface  -> "interface"
//	StereotypeNone       -> "" (role attr omitted)
//
// # Dependency kind → archmotif edge kind
//
//	DependencyUses       -> archmotifimport.DependencyUsesType
//	DependencyReturns    -> archmotifimport.DependencyReturns
//	DependencyImplements -> AddImplements (dedicated method)
//	DependencyExtends    -> archmotifimport.DependencyEmbeds
//	                        (Java inheritance: closest typed analog
//	                        in archmotif's edge vocabulary)
//	DependencyNestedIn   -> AddContains (structural)
//
// A coarse package→package archmotifimport.DependencyDependsOn
// edge is emitted whenever any internal symbol-level dependency
// crosses a package boundary, so package-level analyses
// (modularity, layering rules) see the design's import graph.
package archmotif

import (
	"fmt"
	"sort"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
)

// ToArchmotifGraph turns archai's design-time package models into an
// archmotif typed graph. The overlay (loaded from archai.yaml) is
// optional; when non-nil the package-level layer/aggregate metadata
// it carries supplements per-package fields on the input models.
//
// The returned *archmotifimport.Graph is a type alias for archmotif's
// internal/graph.Graph, so callers can pass it straight to any
// archmotif analysis that accepts *graph.Graph.
func ToArchmotifGraph(models []domain.PackageModel, _ *overlay.Config) (*archmotifimport.Graph, error) {
	b := archmotifimport.NewBuilder()

	// Index packages by path so we can resolve cross-package symbol
	// references to package nodes when emitting coarse depends-on
	// edges. We do not consult the overlay for layer/aggregate today
	// because PackageModel already carries those fields populated by
	// archai's reader pipeline; the overlay parameter is kept for
	// forward-compatibility with derived metadata not yet on
	// PackageModel.
	pkgByPath := make(map[string]*domain.PackageModel, len(models))
	for i := range models {
		pkgByPath[models[i].Path] = &models[i]
	}

	// Sort package iteration order by path for fully deterministic
	// output regardless of the caller's input ordering.
	paths := make([]string, 0, len(models))
	for _, p := range models {
		paths = append(paths, p.Path)
	}
	sort.Strings(paths)

	// Pass 1: create package nodes.
	for _, path := range paths {
		p := pkgByPath[path]
		if err := b.AddPackage(packageID(p.Path), p.Layer, p.Aggregate); err != nil {
			return nil, fmt.Errorf("archmotif: package %q: %w", p.Path, err)
		}
	}

	// Pass 2: create type/function/method/field nodes inside each
	// package and their natural contains-edges. Sorted by id so
	// runs are byte-identical given the same input.
	for _, path := range paths {
		p := pkgByPath[path]
		if err := addPackageContents(b, p); err != nil {
			return nil, err
		}
	}

	// Pass 3: implements edges declared on Implementations.
	// PackageModel.Implementations holds interface-side declarations:
	// the interface lives in this package, the concrete may be
	// elsewhere. We only emit the edge if both endpoints exist in
	// the graph (i.e. both sides are loaded packages).
	for _, path := range paths {
		p := pkgByPath[path]
		if err := addImplementations(b, p, pkgByPath); err != nil {
			return nil, err
		}
	}

	// Pass 4: dependency edges between symbols (uses, returns,
	// extends, nested-in). Implements dependencies are handled in
	// Pass 3 via the structural Implementations list, which is
	// archai's canonical source — emitting both would double-count.
	// External / unloaded targets are skipped.
	if err := addSymbolDependencies(b, models, pkgByPath); err != nil {
		return nil, err
	}

	// Pass 5: coarse package→package depends-on edges derived from
	// the cross-package dependency set. Provides input to
	// modularity/layer-rule analyses without forcing them to walk
	// every symbol edge.
	if err := addPackageDependsOn(b, models, pkgByPath); err != nil {
		return nil, err
	}

	// Pass 6: call edges from function/method bodies. These are
	// behavioral edges captured by the Go reader's call-extraction pass.
	// They provide the dynamic connectivity that structural
	// uses/returns edges miss (e.g., handler → DTO construction).
	if err := addCallEdges(b, models, pkgByPath); err != nil {
		return nil, err
	}

	return b.Build()
}

// addPackageContents creates type/function/method/field nodes for a
// single package along with their structural contains edges.
func addPackageContents(b *archmotifimport.Builder, p *domain.PackageModel) error {
	pkgID := packageID(p.Path)

	// Interfaces — sorted by name for determinism.
	ifaceOrder := sortedInterfaceNames(p.Interfaces)
	for _, name := range ifaceOrder {
		iface := findInterface(p.Interfaces, name)
		tid := typeID(p.Path, iface.Name)
		role := stereotypeRole(iface.Stereotype)
		if role == "" {
			role = "interface"
		}
		if err := b.AddType(tid, pkgID, true, role); err != nil {
			return fmt.Errorf("archmotif: interface %s: %w", tid, err)
		}
		methodOrder := sortedMethodNames(iface.Methods)
		for _, m := range methodOrder {
			mid := methodID(p.Path, iface.Name, m)
			if err := b.AddMethod(mid, tid); err != nil {
				return fmt.Errorf("archmotif: method %s: %w", mid, err)
			}
		}
	}

	// Structs — sorted by name; emit fields and methods.
	structOrder := sortedStructNames(p.Structs)
	for _, name := range structOrder {
		s := findStruct(p.Structs, name)
		tid := typeID(p.Path, s.Name)
		role := stereotypeRole(s.Stereotype)
		if err := b.AddType(tid, pkgID, false, role); err != nil {
			return fmt.Errorf("archmotif: struct %s: %w", tid, err)
		}
		fieldOrder := sortedFieldNames(s.Fields)
		for _, fname := range fieldOrder {
			f := findField(s.Fields, fname)
			fid := fieldID(p.Path, s.Name, f.Name)
			if err := b.AddField(fid, tid, f.Type.String()); err != nil {
				return fmt.Errorf("archmotif: field %s: %w", fid, err)
			}
		}
		methodOrder := sortedMethodNames(s.Methods)
		for _, mname := range methodOrder {
			mid := methodID(p.Path, s.Name, mname)
			if err := b.AddMethod(mid, tid); err != nil {
				return fmt.Errorf("archmotif: method %s: %w", mid, err)
			}
		}
	}

	// TypeDefs — modelled as types (struct-like nodes). Stereotype
	// drives role; enum TypeDefs get role="enum".
	typedefOrder := sortedTypeDefNames(p.TypeDefs)
	for _, name := range typedefOrder {
		td := findTypeDef(p.TypeDefs, name)
		tid := typeID(p.Path, td.Name)
		role := stereotypeRole(td.Stereotype)
		if role == "" && td.IsEnum() {
			role = "enum"
		}
		if err := b.AddType(tid, pkgID, false, role); err != nil {
			return fmt.Errorf("archmotif: typedef %s: %w", tid, err)
		}
	}

	// Package-level functions.
	fnOrder := sortedFunctionNames(p.Functions)
	for _, name := range fnOrder {
		fid := functionID(p.Path, name)
		if err := b.AddFunction(fid, pkgID); err != nil {
			return fmt.Errorf("archmotif: function %s: %w", fid, err)
		}
	}

	return nil
}

// addImplementations emits EdgeImplements between concrete types
// (possibly in another package) and the interfaces they implement.
// If the concrete side is not in any loaded package, the edge is
// skipped — archmotif's graph only models nodes for loaded code.
func addImplementations(b *archmotifimport.Builder, p *domain.PackageModel, pkgByPath map[string]*domain.PackageModel) error {
	for _, impl := range p.Implementations {
		// Interface side: always in this package (archai's reader
		// guarantees that), but defend against the alternative.
		ifacePkg, ok := pkgByPath[impl.Interface.Package]
		if !ok {
			continue
		}
		// Concrete side may be in another loaded package or external.
		if impl.Concrete.External {
			continue
		}
		concretePkg, ok := pkgByPath[impl.Concrete.Package]
		if !ok {
			continue
		}
		if !packageHasStruct(concretePkg, impl.Concrete.Symbol) {
			continue
		}
		if !packageHasInterface(ifacePkg, impl.Interface.Symbol) {
			continue
		}
		structTID := typeID(impl.Concrete.Package, impl.Concrete.Symbol)
		ifaceTID := typeID(impl.Interface.Package, impl.Interface.Symbol)
		if err := b.AddImplements(structTID, ifaceTID); err != nil {
			return fmt.Errorf("archmotif: implements %s->%s: %w", structTID, ifaceTID, err)
		}
	}
	return nil
}

// addSymbolDependencies emits typed edges for each PackageModel
// dependency, skipping anything that points outside the loaded set.
// DependencyImplements is intentionally skipped here — the canonical
// implements edge is the structural Implementations slice handled
// in Pass 3.
func addSymbolDependencies(b *archmotifimport.Builder, models []domain.PackageModel, pkgByPath map[string]*domain.PackageModel) error {
	// Collect dependencies into a slice and sort for deterministic
	// edge insertion order. (Duplicate edges between the same node
	// pair with the same kind would be rejected by the underlying
	// graph; we dedupe up front.)
	type depEdge struct {
		fromID string
		toID   string
		kind   archmotifimport.DependencyKind
	}
	seen := map[depEdge]bool{}
	var edges []depEdge

	for _, p := range models {
		for _, d := range p.Dependencies {
			if d.Kind == domain.DependencyImplements {
				continue
			}
			fromID, ok := resolveSymbolID(d.From, pkgByPath)
			if !ok {
				continue
			}
			toID, ok := resolveSymbolID(d.To, pkgByPath)
			if !ok {
				continue
			}
			if fromID == toID {
				// Self-edges are uninteresting and archmotif rejects them.
				continue
			}
			kind, ok := mapDependencyKind(d.Kind)
			if !ok {
				continue
			}
			e := depEdge{fromID, toID, kind}
			if seen[e] {
				continue
			}
			seen[e] = true
			edges = append(edges, e)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].fromID != edges[j].fromID {
			return edges[i].fromID < edges[j].fromID
		}
		if edges[i].toID != edges[j].toID {
			return edges[i].toID < edges[j].toID
		}
		return string(edges[i].kind) < string(edges[j].kind)
	})

	for _, e := range edges {
		// nested-in becomes a contains-edge, not a typed dependency.
		if e.kind == nestedInSentinel {
			if err := b.AddContains(e.toID, e.fromID); err != nil {
				return fmt.Errorf("archmotif: nested-in %s->%s: %w", e.fromID, e.toID, err)
			}
			continue
		}
		if err := b.AddDependency(e.fromID, e.toID, e.kind); err != nil {
			return fmt.Errorf("archmotif: dep %s->%s (%s): %w", e.fromID, e.toID, e.kind, err)
		}
	}
	return nil
}

// addPackageDependsOn aggregates symbol-level cross-package
// dependencies into one depends-on edge per (fromPkg, toPkg) pair.
func addPackageDependsOn(b *archmotifimport.Builder, models []domain.PackageModel, pkgByPath map[string]*domain.PackageModel) error {
	type pkgPair struct{ from, to string }
	seen := map[pkgPair]bool{}
	var pairs []pkgPair
	for _, p := range models {
		for _, d := range p.Dependencies {
			if d.From.External || d.To.External {
				continue
			}
			if _, ok := pkgByPath[d.From.Package]; !ok {
				continue
			}
			if _, ok := pkgByPath[d.To.Package]; !ok {
				continue
			}
			if d.From.Package == d.To.Package {
				continue
			}
			pair := pkgPair{d.From.Package, d.To.Package}
			if seen[pair] {
				continue
			}
			seen[pair] = true
			pairs = append(pairs, pair)
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].from != pairs[j].from {
			return pairs[i].from < pairs[j].from
		}
		return pairs[i].to < pairs[j].to
	})
	for _, p := range pairs {
		if err := b.AddDependency(packageID(p.from), packageID(p.to), archmotifimport.DependencyDependsOn); err != nil {
			return fmt.Errorf("archmotif: pkg-depends-on %s->%s: %w", p.from, p.to, err)
		}
	}
	return nil
}

// addCallEdges emits call edges from function/method bodies to their
// call targets. These behavioral edges complement the structural
// uses/returns edges and are essential for graph connectivity (e.g.,
// a handler that constructs a DTO has no structural edge to it, but
// the call graph captures that the handler's body references the DTO).
func addCallEdges(b *archmotifimport.Builder, models []domain.PackageModel, pkgByPath map[string]*domain.PackageModel) error {
	type callEdge struct {
		fromID string
		toID   string
	}
	seen := map[callEdge]bool{}
	var edges []callEdge

	// emitCalls processes call edges from a single callable.
	emitCalls := func(fromID string, calls []domain.CallEdge) {
		for _, c := range calls {
			toID, ok := resolveCallTarget(c.To, pkgByPath)
			if !ok {
				continue
			}
			if fromID == toID {
				continue // skip self-calls
			}
			e := callEdge{fromID, toID}
			if seen[e] {
				continue
			}
			seen[e] = true
			edges = append(edges, e)
		}
	}

	for _, p := range models {
		// Package-level functions.
		for _, fn := range p.Functions {
			emitCalls(functionID(p.Path, fn.Name), fn.Calls)
		}
		// Struct methods.
		for _, s := range p.Structs {
			for _, m := range s.Methods {
				emitCalls(methodID(p.Path, s.Name, m.Name), m.Calls)
			}
		}
		// Interface methods (rarely have bodies, but included for completeness).
		for _, iface := range p.Interfaces {
			for _, m := range iface.Methods {
				emitCalls(methodID(p.Path, iface.Name, m.Name), m.Calls)
			}
		}
	}

	// Sort for deterministic output.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].fromID != edges[j].fromID {
			return edges[i].fromID < edges[j].fromID
		}
		return edges[i].toID < edges[j].toID
	})

	for _, e := range edges {
		if err := b.AddDependency(e.fromID, e.toID, archmotifimport.DependencyCalls); err != nil {
			return fmt.Errorf("archmotif: call %s->%s: %w", e.fromID, e.toID, err)
		}
	}
	return nil
}

// resolveCallTarget maps a CallEdge's To SymbolRef to an archmotif node ID.
// Call targets can be functions or methods (Struct.Method format in Symbol).
func resolveCallTarget(ref domain.SymbolRef, pkgByPath map[string]*domain.PackageModel) (string, bool) {
	if ref.External {
		return "", false
	}
	p, ok := pkgByPath[ref.Package]
	if !ok {
		return "", false
	}

	// CallEdge.To.Symbol may be:
	//   - "FunctionName" for package-level functions
	//   - "TypeName.MethodName" for methods
	if idx := indexDot(ref.Symbol); idx >= 0 {
		// Method call: "Struct.Method"
		typeName := ref.Symbol[:idx]
		// Verify the type exists in the package.
		if packageHasStruct(p, typeName) || packageHasInterface(p, typeName) {
			return methodID(ref.Package, typeName, ref.Symbol[idx+1:]), true
		}
		return "", false
	}

	// Package-level function.
	if packageHasFunction(p, ref.Symbol) {
		return functionID(ref.Package, ref.Symbol), true
	}
	return "", false
}

// indexDot returns the index of the first '.' in s, or -1 if not found.
func indexDot(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// nestedInSentinel marks a dependency that should be exported as a
// structural contains edge rather than a typed dependency. It is
// distinct from any real DependencyKind value.
const nestedInSentinel archmotifimport.DependencyKind = "__nested_in__"

// mapDependencyKind translates an archai dependency kind to an
// archmotifimport DependencyKind. Returns (_, false) for kinds that
// are intentionally not emitted as typed dependency edges (currently
// only DependencyImplements, which is handled structurally).
func mapDependencyKind(k domain.DependencyKind) (archmotifimport.DependencyKind, bool) {
	switch k {
	case domain.DependencyUses:
		return archmotifimport.DependencyUsesType, true
	case domain.DependencyReturns:
		return archmotifimport.DependencyReturns, true
	case domain.DependencyExtends:
		return archmotifimport.DependencyEmbeds, true
	case domain.DependencyNestedIn:
		return nestedInSentinel, true
	case domain.DependencyImplements:
		return "", false
	}
	return "", false
}

// stereotypeRole maps an archai stereotype to the role attribute
// archmotif expects on a type node. Empty for StereotypeNone or
// stereotypes archai does not surface in archmotif's vocabulary.
func stereotypeRole(s domain.Stereotype) string {
	switch s {
	case domain.StereotypeService:
		return "service"
	case domain.StereotypeRepository:
		return "repository"
	case domain.StereotypePort:
		return "port"
	case domain.StereotypeFactory:
		return "factory"
	case domain.StereotypeAggregate:
		return "aggregate"
	case domain.StereotypeEntity:
		return "entity"
	case domain.StereotypeValue:
		return "value"
	case domain.StereotypeEnum:
		return "enum"
	case domain.StereotypeInterface:
		return "interface"
	}
	return ""
}

// resolveSymbolID maps an archai SymbolRef to the archmotif graph id
// for that symbol, returning false when the symbol is external or
// not present in the loaded package set.
func resolveSymbolID(ref domain.SymbolRef, pkgByPath map[string]*domain.PackageModel) (string, bool) {
	if ref.External {
		return "", false
	}
	p, ok := pkgByPath[ref.Package]
	if !ok {
		return "", false
	}

	// Check for method symbol format: "Type.Method".
	if idx := indexDot(ref.Symbol); idx >= 0 {
		typeName := ref.Symbol[:idx]
		methodName := ref.Symbol[idx+1:]
		// Verify the type exists in the package.
		if packageHasStruct(p, typeName) && packageHasMethod(p, typeName, methodName) {
			return methodID(ref.Package, typeName, methodName), true
		}
		if packageHasInterface(p, typeName) && packageHasInterfaceMethod(p, typeName, methodName) {
			return methodID(ref.Package, typeName, methodName), true
		}
		return "", false
	}

	// Resolve in symbol-priority order: interface, struct, typedef,
	// function. Methods/fields are not first-class targets of
	// dependency edges in archai today.
	if packageHasInterface(p, ref.Symbol) {
		return typeID(ref.Package, ref.Symbol), true
	}
	if packageHasStruct(p, ref.Symbol) {
		return typeID(ref.Package, ref.Symbol), true
	}
	if packageHasTypeDef(p, ref.Symbol) {
		return typeID(ref.Package, ref.Symbol), true
	}
	if packageHasFunction(p, ref.Symbol) {
		return functionID(ref.Package, ref.Symbol), true
	}
	return "", false
}

// --- ID helpers ----------------------------------------------------------

func packageID(path string) string   { return "pkg:" + path }
func typeID(pkg, name string) string { return "type:" + pkg + "." + name }
func functionID(pkg, name string) string {
	return "fn:" + pkg + "." + name
}
func methodID(pkg, recv, method string) string {
	return "method:" + pkg + "." + recv + "." + method
}
func fieldID(pkg, structName, field string) string {
	return "field:" + pkg + "." + structName + "." + field
}

// --- lookup helpers ------------------------------------------------------

func packageHasInterface(p *domain.PackageModel, name string) bool {
	for _, i := range p.Interfaces {
		if i.Name == name {
			return true
		}
	}
	return false
}

func packageHasStruct(p *domain.PackageModel, name string) bool {
	for _, s := range p.Structs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func packageHasTypeDef(p *domain.PackageModel, name string) bool {
	for _, t := range p.TypeDefs {
		if t.Name == name {
			return true
		}
	}
	return false
}

func packageHasFunction(p *domain.PackageModel, name string) bool {
	for _, f := range p.Functions {
		if f.Name == name {
			return true
		}
	}
	return false
}

func packageHasMethod(p *domain.PackageModel, structName, methodName string) bool {
	for _, s := range p.Structs {
		if s.Name == structName {
			for _, m := range s.Methods {
				if m.Name == methodName {
					return true
				}
			}
			return false
		}
	}
	return false
}

func packageHasInterfaceMethod(p *domain.PackageModel, ifaceName, methodName string) bool {
	for _, i := range p.Interfaces {
		if i.Name == ifaceName {
			for _, m := range i.Methods {
				if m.Name == methodName {
					return true
				}
			}
			return false
		}
	}
	return false
}

// --- sorting/finder helpers ---------------------------------------------

func sortedInterfaceNames(xs []domain.InterfaceDef) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func sortedStructNames(xs []domain.StructDef) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func sortedTypeDefNames(xs []domain.TypeDef) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func sortedFunctionNames(xs []domain.FunctionDef) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func sortedMethodNames(xs []domain.MethodDef) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func sortedFieldNames(xs []domain.FieldDef) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func findInterface(xs []domain.InterfaceDef, name string) domain.InterfaceDef {
	for _, x := range xs {
		if x.Name == name {
			return x
		}
	}
	return domain.InterfaceDef{}
}

func findStruct(xs []domain.StructDef, name string) domain.StructDef {
	for _, x := range xs {
		if x.Name == name {
			return x
		}
	}
	return domain.StructDef{}
}

func findTypeDef(xs []domain.TypeDef, name string) domain.TypeDef {
	for _, x := range xs {
		if x.Name == name {
			return x
		}
	}
	return domain.TypeDef{}
}

func findField(xs []domain.FieldDef, name string) domain.FieldDef {
	for _, x := range xs {
		if x.Name == name {
			return x
		}
	}
	return domain.FieldDef{}
}
