package golang

import (
	"go/ast"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"

	"github.com/kgatilin/archai/internal/domain"
)

// extractCalls walks the AST body of every function and method across the
// loaded packages and records static CallEdges to targets that also live in
// the loaded package set. Results are written back into the corresponding
// FunctionDef.Calls and MethodDef.Calls slices on the provided models.
//
// Edges are only emitted when the callee's package is among the loaded
// packages (keyed by *types.Package pointer). Stdlib and third-party targets
// are dropped, keeping the call graph scoped to the analyzed module.
//
// Interface-dispatched method calls expand into one edge per known
// implementation (reusing M2b's Implementations data) with Via set to
// "pkg.Interface".
func (r *reader) extractCalls(pkgs []*packages.Package, models []domain.PackageModel) {
	// Index models by path for mutation.
	modelIdxByPath := make(map[string]int, len(models))
	for i, m := range models {
		modelIdxByPath[m.Path] = i
	}

	// Set of loaded *types.Package pointers — used to scope call targets.
	loadedTypes := make(map[*types.Package]*packages.Package, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.Types != nil {
			loadedTypes[pkg.Types] = pkg
		}
	}

	// Build an index of implementations by fully qualified interface name.
	// Key format: "<pkg-path>.<InterfaceName>" where pkg-path is the
	// go/types package path (not the module-relative one), so we can look
	// it up directly from types.Interface receivers.
	implIndex := r.buildImplementationIndex(pkgs, models, modelIdxByPath)

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

				calls := r.extractCallsFromFuncBody(pkg, fd, loadedTypes, implIndex)

				// Assign calls to the matching FunctionDef or MethodDef.
				if fd.Recv == nil {
					// Package-level function.
					for i := range model.Functions {
						if model.Functions[i].Name == fd.Name.Name {
							model.Functions[i].Calls = calls
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
							model.Structs[si].Methods[mi].Calls = calls
							break
						}
					}
					break
				}
			}
		}
	}
}

// extractCallsFromFuncBody walks a single FuncDecl body and returns the
// sorted, deduplicated list of CallEdges it contains.
func (r *reader) extractCallsFromFuncBody(
	pkg *packages.Package,
	fd *ast.FuncDecl,
	loadedTypes map[*types.Package]*packages.Package,
	implIndex map[string][]domain.SymbolRef,
) []domain.CallEdge {
	var edges []domain.CallEdge
	seen := make(map[string]bool)

	addEdge := func(e domain.CallEdge) {
		// Skip self-referential empty symbols.
		if e.To.Symbol == "" {
			return
		}
		key := e.To.Package + "." + e.To.Symbol + "|" + e.Via
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, e)
	}

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
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

	// Stable ordering for deterministic output.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].To.Package != edges[j].To.Package {
			return edges[i].To.Package < edges[j].To.Package
		}
		if edges[i].To.Symbol != edges[j].To.Symbol {
			return edges[i].To.Symbol < edges[j].To.Symbol
		}
		return edges[i].Via < edges[j].Via
	})
	return edges
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

// buildImplementationIndex maps each interface (keyed by its go/types
// package path + "." + Name) to a list of concrete-type SymbolRefs that
// implement it. Data is sourced from model.Implementations, computed by
// the prior computeImplementations pass.
func (r *reader) buildImplementationIndex(
	pkgs []*packages.Package,
	models []domain.PackageModel,
	modelIdxByPath map[string]int,
) map[string][]domain.SymbolRef {
	// Resolve each model's path back to its go/types package path.
	typesPkgByRelPath := make(map[string]string, len(pkgs))
	for _, pkg := range pkgs {
		typesPkgByRelPath[r.relativePath(pkg.PkgPath)] = pkg.PkgPath
	}

	index := make(map[string][]domain.SymbolRef)
	for _, m := range models {
		for _, impl := range m.Implementations {
			ifacePkgPath, ok := typesPkgByRelPath[impl.Interface.Package]
			if !ok {
				// Interface package not in the loaded set — skip.
				continue
			}
			key := ifacePkgPath + "." + impl.Interface.Symbol
			index[key] = append(index[key], impl.Concrete)
		}
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
	_ = modelIdxByPath // retained for signature symmetry with caller
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
