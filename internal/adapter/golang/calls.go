package golang

import (
	"go/ast"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"

	"github.com/kgatilin/wyrd/internal/domain"
)

// extractCalls walks the AST body of every function and method across the
// loaded packages and records static CallEdges to targets that also live in
// the loaded package set. Results are written back into the corresponding
// FunctionDef.Calls and MethodDef.Calls slices on the provided models.
//
// Edges are only emitted when the callee's package belongs to the analyzed
// module (keyed by *types.Package pointer). Stdlib and third-party targets
// are dropped, keeping the call graph scoped to the module.
//
// The callee side is resolved against the in-module universe — the loaded
// packages plus the in-module packages they import — not against the loaded
// packages alone. A partial read (the daemon re-reads only the packages whose
// files changed) returns just those as roots while their callees are still
// type-checked from export data; scoping to the roots would drop every
// cross-package edge out of a re-read package, erasing its callees' callers
// from the graph until the next full read.
//
// Interface-dispatched method calls expand into one edge per known
// implementation (reusing M2b's Implementations data) with Via set to
// "pkg.Interface". The implementations come from the models being read plus
// `known` — models the caller already holds for packages outside this read.
// A partial read cannot recompute them: export data carries only exported
// types, and most implementations in a real tree are unexported.
func (r *reader) extractCalls(pkgs []*packages.Package, models []domain.PackageModel, known []domain.PackageModel) {
	// Index models by path for mutation.
	modelIdxByPath := make(map[string]int, len(models))
	for i, m := range models {
		modelIdxByPath[m.Path] = i
	}

	// Set of in-module *types.Package pointers — used to scope call targets.
	universe := r.callTargetUniverse(pkgs)
	loadedTypes := make(map[*types.Package]*packages.Package, len(universe))
	for _, pkg := range universe {
		if pkg.Types != nil {
			loadedTypes[pkg.Types] = pkg
		}
	}

	// Build an index of implementations by fully qualified interface name.
	// Key format: "<pkg-path>.<InterfaceName>" where pkg-path is the
	// go/types package path (not the module-relative one), so we can look
	// it up directly from types.Interface receivers.
	implIndex := r.buildImplementationIndex(universe, models, known)

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		relPath := r.relativePath(pkg.PkgPath)
		modelIdx, ok := modelIdxByPath[relPath]
		if !ok {
			continue
		}
		model := &models[modelIdx]

		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}

				calls, refs := r.extractCallsFromFuncBody(pkg, fd, loadedTypes, implIndex)

				// Assign calls to the matching FunctionDef or MethodDef.
				if fd.Recv == nil {
					// Package-level function.
					for i := range model.Functions {
						if model.Functions[i].Name == fd.Name.Name {
							fn := model.Functions[i]
							model.Functions[i].Calls = calls
							addReferences(model, domain.SymbolRef{
								Package: model.Path,
								File:    fn.SourceFile,
								Symbol:  fn.Name,
							}, fn.IsExported, refs)
							break
						}
					}
					continue
				}

				// Method — resolve receiver type name.
				recvName := receiverTypeName(fd.Recv)
				if recvName == "" {
					continue
				}
				for si := range model.Structs {
					if model.Structs[si].Name != recvName {
						continue
					}
					for mi := range model.Structs[si].Methods {
						if model.Structs[si].Methods[mi].Name == fd.Name.Name {
							s, m := model.Structs[si], model.Structs[si].Methods[mi]
							model.Structs[si].Methods[mi].Calls = calls
							addReferences(model, domain.SymbolRef{
								Package: model.Path,
								File:    s.SourceFile,
								Symbol:  s.Name + "." + m.Name,
							}, s.IsExported && m.IsExported, refs)
							break
						}
					}
					break
				}
			}
		}
	}
}

// addReferences appends one DependencyReferences edge from `from` to each of
// refs onto the model. A function naming itself (recursion through a value)
// is not a dependency and is skipped.
func addReferences(model *domain.PackageModel, from domain.SymbolRef, throughExported bool, refs []domain.SymbolRef) {
	for _, to := range refs {
		if to.Package == from.Package && to.Symbol == from.Symbol {
			continue
		}
		model.Dependencies = append(model.Dependencies, domain.Dependency{
			From:            from,
			To:              to,
			Kind:            domain.DependencyReferences,
			ThroughExported: throughExported,
		})
	}
}

// extractCallsFromFuncBody walks a single FuncDecl body and returns the
// sorted, deduplicated list of CallEdges it contains, plus the in-module
// functions and methods it names as values without calling them — passed as
// an argument, assigned, stored in a literal — deduplicated in order of first
// appearance.
func (r *reader) extractCallsFromFuncBody(
	pkg *packages.Package,
	fd *ast.FuncDecl,
	loadedTypes map[*types.Package]*packages.Package,
	implIndex map[string][]domain.SymbolRef,
) ([]domain.CallEdge, []domain.SymbolRef) {
	var edges []domain.CallEdge
	idxByKey := make(map[string]int)

	var refs []domain.SymbolRef
	seenRef := make(map[string]bool)
	// callees holds the name in the callee position of every call seen so
	// far. The walk is pre-order, so a call is visited before its callee
	// name and that name is never read as a reference.
	callees := make(map[*ast.Ident]bool)

	addEdge := func(e domain.CallEdge) {
		// Skip self-referential empty symbols.
		if e.To.Symbol == "" {
			return
		}
		key := e.To.Package + "." + e.To.Symbol + "|" + e.Via
		// Repeated call to the same target: bump the multiplicity on the
		// first occurrence's edge and keep its source rank.
		if i, ok := idxByKey[key]; ok {
			edges[i].Count++
			return
		}
		e.Order = len(edges)
		e.Count = 1
		idxByKey[key] = len(edges)
		edges = append(edges, e)
	}

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			if callees[ident] {
				return true
			}
			if ref, ok := r.referencedFunc(pkg, ident, loadedTypes); ok {
				key := ref.Package + "." + ref.Symbol
				if !seenRef[key] {
					seenRef[key] = true
					refs = append(refs, ref)
				}
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name := calleeName(call.Fun); name != nil {
			callees[name] = true
		}

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			// Possibly a package-local function call or a built-in.
			r.handleIdentCall(pkg, fun, loadedTypes, addEdge)

		case *ast.SelectorExpr:
			r.handleSelectorCall(pkg, fun, loadedTypes, implIndex, addEdge)
		}
		return true
	})

	// Preserve source order: edges are appended by first occurrence as the
	// AST is walked in pre-order, so Order already reflects the body's call
	// flow. Sort by Order to make that contract explicit (and robust to any
	// future non-appending insertion). This is what lets the sequence
	// renderers follow the actual flow rather than an alphabetical set.
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].Order < edges[j].Order
	})
	return edges, refs
}

// calleeName returns the identifier that names the function a call invokes —
// `F` in `F()`, `pkg.F()`, `x.M()`, `F[T]()` — or nil when the callee is an
// expression with no name of its own (a call result, a func literal).
func calleeName(fun ast.Expr) *ast.Ident {
	for {
		switch e := fun.(type) {
		case *ast.ParenExpr:
			fun = e.X
		case *ast.IndexExpr:
			fun = e.X
		case *ast.IndexListExpr:
			fun = e.X
		case *ast.SelectorExpr:
			return e.Sel
		case *ast.Ident:
			return e
		default:
			return nil
		}
	}
}

// referencedFunc resolves an identifier that names an in-module function or
// concrete method to the symbol a references edge points at: `F` for a
// function, `Recv.M` for a method value or method expression. A method of an
// interface is left out — its implementations are what runs, and a value
// gives no call site to fan out from.
func (r *reader) referencedFunc(
	pkg *packages.Package,
	ident *ast.Ident,
	loadedTypes map[*types.Package]*packages.Package,
) (domain.SymbolRef, bool) {
	fn, ok := pkg.TypesInfo.Uses[ident].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return domain.SymbolRef{}, false
	}
	targetPkg, loaded := loadedTypes[fn.Pkg()]
	if !loaded {
		return domain.SymbolRef{}, false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return domain.SymbolRef{}, false
	}
	owner := fn.Name()
	symbol := fn.Name()
	if recv := sig.Recv(); recv != nil {
		t := recv.Type()
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		if _, isIface := t.Underlying().(*types.Interface); isIface {
			return domain.SymbolRef{}, false
		}
		owner = recvTypeName(t)
		if owner == "" {
			return domain.SymbolRef{}, false
		}
		symbol = owner + "." + fn.Name()
	}
	return domain.SymbolRef{
		Package: r.relativePath(fn.Pkg().Path()),
		File:    r.findSymbolFile(targetPkg, owner),
		Symbol:  symbol,
	}, true
}

// handleIdentCall resolves a bare identifier call like `Foo()` and emits an
// edge when the identifier resolves to a function in a loaded package.
func (r *reader) handleIdentCall(
	pkg *packages.Package,
	ident *ast.Ident,
	loadedTypes map[*types.Package]*packages.Package,
	addEdge func(domain.CallEdge),
) {
	obj := pkg.TypesInfo.Uses[ident]
	if obj == nil {
		obj = pkg.TypesInfo.Defs[ident]
	}
	if obj == nil {
		return
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return
	}
	// Skip built-ins and functions whose package is not loaded.
	if fn.Pkg() == nil {
		return
	}
	targetPkg, loaded := loadedTypes[fn.Pkg()]
	if !loaded {
		return
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	// Skip methods — a bare identifier should never resolve to a method,
	// but guard anyway for safety.
	if sig.Recv() != nil {
		return
	}

	addEdge(domain.CallEdge{
		To: domain.SymbolRef{
			Package: r.relativePath(fn.Pkg().Path()),
			File:    r.findSymbolFile(targetPkg, fn.Name()),
			Symbol:  fn.Name(),
		},
	})
}

// handleSelectorCall resolves `X.Y()` — either a package-qualified call
// (pkg.Func) or a method call on a value or interface receiver. Emits one
// edge per resolved target, expanding interface dispatch via implIndex.
func (r *reader) handleSelectorCall(
	pkg *packages.Package,
	sel *ast.SelectorExpr,
	loadedTypes map[*types.Package]*packages.Package,
	implIndex map[string][]domain.SymbolRef,
	addEdge func(domain.CallEdge),
) {
	// Check for package-qualified call: X is an identifier that resolves
	// to a *types.PkgName.
	if xIdent, ok := sel.X.(*ast.Ident); ok {
		if obj := pkg.TypesInfo.Uses[xIdent]; obj != nil {
			if _, isPkgName := obj.(*types.PkgName); isPkgName {
				// pkg.Func() — resolve via Uses on the selector.
				selObj := pkg.TypesInfo.Uses[sel.Sel]
				if fn, ok := selObj.(*types.Func); ok {
					if fn.Pkg() == nil {
						return
					}
					targetPkg, loaded := loadedTypes[fn.Pkg()]
					if !loaded {
						return
					}
					addEdge(domain.CallEdge{
						To: domain.SymbolRef{
							Package: r.relativePath(fn.Pkg().Path()),
							File:    r.findSymbolFile(targetPkg, fn.Name()),
							Symbol:  fn.Name(),
						},
					})
				}
				return
			}
		}
	}

	// Method call — use Selections to get the receiver type.
	selection, ok := pkg.TypesInfo.Selections[sel]
	if !ok {
		return
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return
	}
	recv := selection.Recv()
	if recv == nil {
		return
	}

	// Unwrap pointer to inspect the underlying type.
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}

	// Interface receiver — fan out to all known implementations.
	if named, ok := recv.(*types.Named); ok {
		if _, isIface := named.Underlying().(*types.Interface); isIface {
			ifacePkg := named.Obj().Pkg()
			if ifacePkg == nil {
				return
			}
			key := ifacePkg.Path() + "." + named.Obj().Name()
			impls := implIndex[key]
			if len(impls) == 0 {
				// Interface without known impls in the loaded set —
				// drop the edge rather than guessing.
				return
			}
			viaPkg := r.relativePath(ifacePkg.Path())
			via := named.Obj().Name()
			if viaPkg != "" && viaPkg != "." {
				via = viaPkg + "." + via
			}
			for _, concrete := range impls {
				// Skip impls whose package is not loaded (defensive).
				addEdge(domain.CallEdge{
					To: domain.SymbolRef{
						Package: concrete.Package,
						File:    concrete.File,
						Symbol:  concrete.Symbol + "." + fn.Name(),
					},
					Via: via,
				})
			}
			return
		}
	}

	// Concrete receiver — emit a single direct edge if the method's
	// package is loaded.
	if fn.Pkg() == nil {
		return
	}
	targetPkg, loaded := loadedTypes[fn.Pkg()]
	if !loaded {
		return
	}
	recvName := recvTypeName(recv)
	if recvName == "" {
		return
	}
	addEdge(domain.CallEdge{
		To: domain.SymbolRef{
			Package: r.relativePath(fn.Pkg().Path()),
			File:    r.findSymbolFile(targetPkg, recvName),
			Symbol:  recvName + "." + fn.Name(),
		},
	})
}

// callTargetUniverse returns the packages a call edge is allowed to point at:
// the loaded packages plus the in-module packages they import. Without module
// metadata inModuleUniverse cannot tell in-module imports from stdlib ones, so
// there we stay with the loaded set rather than admit stdlib targets.
func (r *reader) callTargetUniverse(pkgs []*packages.Package) []*packages.Package {
	if r.modulePath == "" {
		return pkgs
	}
	return r.inModuleUniverse(pkgs)
}

// buildImplementationIndex maps each interface (keyed by its go/types
// package path + "." + Name) to a list of concrete-type SymbolRefs that
// implement it. Data is sourced from model.Implementations, computed by
// the prior computeImplementations pass for the packages being read, and
// carried over from `known` for the packages outside the read. A package
// present in both takes its implementations from this read.
func (r *reader) buildImplementationIndex(
	pkgs []*packages.Package,
	models []domain.PackageModel,
	known []domain.PackageModel,
) map[string][]domain.SymbolRef {
	// Resolve each model's path back to its go/types package path.
	typesPkgByRelPath := make(map[string]string, len(pkgs))
	for _, pkg := range pkgs {
		typesPkgByRelPath[r.relativePath(pkg.PkgPath)] = pkg.PkgPath
	}

	read := make(map[string]bool, len(models))
	for _, m := range models {
		read[m.Path] = true
	}

	index := make(map[string][]domain.SymbolRef)
	seen := make(map[string]bool)
	collect := func(m domain.PackageModel) {
		for _, impl := range m.Implementations {
			ifacePkgPath, ok := typesPkgByRelPath[impl.Interface.Package]
			if !ok {
				// Interface package not in the loaded set — skip.
				continue
			}
			key := ifacePkgPath + "." + impl.Interface.Symbol
			// One edge per concrete type: the same pair reaching us from
			// both sources must not double the call's multiplicity.
			pair := key + "->" + impl.Concrete.Package + "." + impl.Concrete.Symbol
			if seen[pair] {
				continue
			}
			seen[pair] = true
			index[key] = append(index[key], impl.Concrete)
		}
	}
	for _, m := range models {
		collect(m)
	}
	for _, m := range known {
		if read[m.Path] {
			continue
		}
		collect(m)
	}

	// Keep implementation lists stably sorted for deterministic edges.
	for k := range index {
		list := index[k]
		sort.Slice(list, func(i, j int) bool {
			if list[i].Package != list[j].Package {
				return list[i].Package < list[j].Package
			}
			return list[i].Symbol < list[j].Symbol
		})
		index[k] = list
	}
	return index
}

// receiverTypeName extracts the named type from a FuncDecl receiver list,
// stripping pointer prefix. Returns empty string when the receiver type
// cannot be determined (e.g., generic type parameter lists).
func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	// Strip generic instantiation: Handler[T] → Handler.
	if idx, ok := t.(*ast.IndexExpr); ok {
		t = idx.X
	}
	if idxList, ok := t.(*ast.IndexListExpr); ok {
		t = idxList.X
	}
	if ident, ok := t.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// recvTypeName returns the underlying named-type name for a concrete
// receiver (after pointer unwrapping). Empty if not a named type.
func recvTypeName(t types.Type) string {
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}
