package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/tools/go/packages"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
)

// reader reads Go source code and converts it to domain.PackageModel structures.
type reader struct {
	// modulePath is cached from the first package load to calculate relative paths.
	modulePath string
}

// NewReader creates a new Go code reader that implements service.ModelReader.
func NewReader() service.ModelReader {
	return &reader{}
}

// Read parses Go source code at the given paths and returns package models.
// Paths can be package patterns like "./...", "./internal/...", etc.
func (r *reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	// Check context cancellation before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedModule,
		Context: ctx,
	}

	pkgs, err := packages.Load(cfg, paths...)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}

	// Check for package loading errors
	var loadErrors []string
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			loadErrors = append(loadErrors, pkgErr.Error())
		}
	}
	if len(loadErrors) > 0 {
		return nil, fmt.Errorf("package errors: %s", strings.Join(loadErrors, "; "))
	}

	// Cache module path from first package
	if len(pkgs) > 0 && pkgs[0].Module != nil {
		r.modulePath = pkgs[0].Module.Path
	}

	// Sort loaded packages by import path so downstream output (the
	// returned []PackageModel) is deterministic regardless of how the
	// underlying go/packages loader scheduled them. The convertPackage
	// fan-out below relies on this stable ordering.
	sort.SliceStable(pkgs, func(i, j int) bool {
		return pkgs[i].PkgPath < pkgs[j].PkgPath
	})

	models, convErr := r.convertPackagesParallel(ctx, pkgs)
	if convErr != nil {
		return nil, convErr
	}

	// Compute interface implementations across all loaded packages.
	// This must run after all packages are loaded because implementations
	// may cross package boundaries.
	r.computeImplementations(pkgs, models)

	// Extract static call edges. Must run after implementations so that
	// interface-dispatched calls can be fanned out to concrete targets.
	r.extractCalls(pkgs, models)

	return models, nil
}

// parallelConvertThreshold is the minimum number of packages that must
// be present before convertPackagesParallel spawns a worker pool. Below
// this, the per-goroutine overhead and the runtime/sync costs more than
// erase the conversion-side speedup measured during #58 profiling
// (archai's own per-package conversion is small relative to the work
// already parallelised inside golang.org/x/tools/go/packages.Load).
//
// The threshold is also gated on runtime.GOMAXPROCS > 1: on a single-CPU
// machine there is nothing to parallelise.
const parallelConvertThreshold = 8

// convertPackagesParallel converts the loaded packages into PackageModels.
// The pkgs slice must be sorted by PkgPath before this is called so the
// resulting []PackageModel is deterministic regardless of goroutine
// scheduling.
//
// When the workload or hardware does not justify parallelism (small
// package counts, GOMAXPROCS == 1) the function executes a serial loop.
// In the parallel path, each worker writes only into its own index of
// the results slice, so no locking is required for the conversion
// output. r.modulePath is set before this function runs and is read-only
// thereafter, which is the only field shared across workers. ctx is
// checked once per package so a cancelled context aborts promptly.
func (r *reader) convertPackagesParallel(ctx context.Context, pkgs []*packages.Package) ([]domain.PackageModel, error) {
	results := make([]domain.PackageModel, len(pkgs))
	if len(pkgs) == 0 {
		return results, nil
	}

	maxProcs := runtime.GOMAXPROCS(0)
	if maxProcs < 2 || len(pkgs) < parallelConvertThreshold {
		// Serial fallback: preserves the deterministic order of pkgs and
		// avoids goroutine overhead on small workloads.
		for i, pkg := range pkgs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			model, err := r.convertPackage(pkg)
			if err != nil {
				return nil, fmt.Errorf("converting package %s: %w", pkg.PkgPath, err)
			}
			results[i] = model
		}
		return results, nil
	}

	workers := maxProcs
	if workers > len(pkgs) {
		workers = len(pkgs)
	}

	indices := make(chan int, len(pkgs))
	for i := range pkgs {
		indices <- i
	}
	close(indices)

	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	captureErr := func(err error) {
		errOnce.Do(func() { firstErr = err })
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indices {
				if ctx.Err() != nil {
					captureErr(ctx.Err())
					return
				}
				model, err := r.convertPackage(pkgs[i])
				if err != nil {
					captureErr(fmt.Errorf("converting package %s: %w", pkgs[i].PkgPath, err))
					return
				}
				results[i] = model
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// convertPackage converts a loaded go/packages.Package to a domain.PackageModel.
func (r *reader) convertPackage(pkg *packages.Package) (domain.PackageModel, error) {
	model := domain.PackageModel{
		Path: r.relativePath(pkg.PkgPath),
		Name: pkg.Name,
	}

	// Build a map of AST files for doc comment extraction
	astFiles := make(map[string]*ast.File)
	for _, f := range pkg.Syntax {
		filename := pkg.Fset.File(f.Pos()).Name()
		astFiles[filename] = f
	}

	// Build a map of type -> methods for associating methods with structs
	methodsByReceiver := r.collectMethodsByReceiver(pkg, astFiles)

	// Build a map of type name -> constants for enum detection
	constantsByType := r.collectConstantsByType(pkg)

	// Extract interfaces
	model.Interfaces = r.extractInterfaces(pkg, astFiles)

	// Extract structs with their methods
	model.Structs = r.extractStructs(pkg, astFiles, methodsByReceiver)

	// Extract functions (package-level, no receiver)
	model.Functions = r.extractFunctions(pkg, astFiles)

	// Extract type definitions
	model.TypeDefs = r.extractTypeDefs(pkg, astFiles, constantsByType)

	// Extract standalone constants (those not captured as TypeDef enum values).
	model.Constants = r.extractConstants(pkg, astFiles)

	// Extract package-level variables and sentinel errors.
	model.Variables, model.Errors = r.extractVarsAndErrors(pkg, astFiles)

	// Collect dependencies from type signatures (params, returns, fields).
	model.Dependencies = r.collectDependencies(pkg, &model)

	// Collect construction dependencies from composite literals in function/method bodies.
	// This captures T{...} / &T{...} patterns that structural signature analysis misses,
	// particularly when the resulting value is passed through interface{}/any.
	constructDeps := r.collectConstructionDependencies(pkg, &model, astFiles)
	model.Dependencies = append(model.Dependencies, constructDeps...)

	// Apply stereotype detection
	r.applyStereotypes(&model)

	return model, nil
}

// relativePath converts an absolute package path to a path relative to the module.
func (r *reader) relativePath(pkgPath string) string {
	if r.modulePath != "" && strings.HasPrefix(pkgPath, r.modulePath) {
		relPath := strings.TrimPrefix(pkgPath, r.modulePath)
		relPath = strings.TrimPrefix(relPath, "/")
		if relPath == "" {
			return "."
		}
		return relPath
	}
	return pkgPath
}

// collectMethodsByReceiver builds a map from receiver type name to its methods.
func (r *reader) collectMethodsByReceiver(pkg *packages.Package, astFiles map[string]*ast.File) map[string][]domain.MethodDef {
	methodsByReceiver := make(map[string][]domain.MethodDef)

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}

		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}

		// Get methods for this named type
		for i := 0; i < named.NumMethods(); i++ {
			fn := named.Method(i)
			sig := fn.Type().(*types.Signature)
			method := r.convertMethod(fn, sig)
			method.Span = r.findMethodDeclSpan(pkg, astFiles, name, fn.Name())
			methodsByReceiver[name] = append(methodsByReceiver[name], method)
		}
	}

	return methodsByReceiver
}

// collectConstantsByType builds a map from type name to its constants (for enum detection).
func (r *reader) collectConstantsByType(pkg *packages.Package) map[string][]string {
	constantsByType := make(map[string][]string)

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		cnst, ok := obj.(*types.Const)
		if !ok {
			continue
		}

		// Get the type of the constant
		named, ok := cnst.Type().(*types.Named)
		if !ok {
			continue
		}

		typeName := named.Obj().Name()
		constantsByType[typeName] = append(constantsByType[typeName], cnst.Name())
	}

	return constantsByType
}

// extractInterfaces extracts all interface definitions from the package.
func (r *reader) extractInterfaces(pkg *packages.Package, astFiles map[string]*ast.File) []domain.InterfaceDef {
	var interfaces []domain.InterfaceDef

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}

		named, _ := typeName.Type().(*types.Named)
		iface, ok := typeName.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}

		// Get position info for source file
		pos := typeName.Pos()
		sourceFile := r.getSourceFile(pkg.Fset, pos)

		// Get doc comment
		doc := r.getDocComment(astFiles, sourceFile, name)

		ifaceDef := domain.InterfaceDef{
			Name:       name,
			TypeParams: r.extractTypeParams(namedTypeParams(named)),
			Methods:    r.extractInterfaceMethods(iface),
			IsExported: isExported(name),
			SourceFile: sourceFile,
			Span:       r.findTypeSpecSpan(pkg, astFiles, name),
			Doc:        doc,
		}

		interfaces = append(interfaces, ifaceDef)
	}

	return interfaces
}

// extractInterfaceMethods extracts method definitions from an interface type.
func (r *reader) extractInterfaceMethods(iface *types.Interface) []domain.MethodDef {
	var methods []domain.MethodDef

	for i := 0; i < iface.NumMethods(); i++ {
		fn := iface.Method(i)
		sig := fn.Type().(*types.Signature)
		methods = append(methods, r.convertMethod(fn, sig))
	}

	return methods
}

// extractStructs extracts all struct definitions from the package.
func (r *reader) extractStructs(
	pkg *packages.Package,
	astFiles map[string]*ast.File,
	methodsByReceiver map[string][]domain.MethodDef,
) []domain.StructDef {
	var structs []domain.StructDef

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}

		named, _ := typeName.Type().(*types.Named)
		structType, ok := typeName.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}

		// Get position info for source file
		pos := typeName.Pos()
		sourceFile := r.getSourceFile(pkg.Fset, pos)

		// Get doc comment
		doc := r.getDocComment(astFiles, sourceFile, name)

		structDef := domain.StructDef{
			Name:       name,
			TypeParams: r.extractTypeParams(namedTypeParams(named)),
			Fields:     r.extractStructFields(structType),
			Methods:    methodsByReceiver[name],
			IsExported: isExported(name),
			SourceFile: sourceFile,
			Span:       r.findTypeSpecSpan(pkg, astFiles, name),
			Doc:        doc,
		}

		structs = append(structs, structDef)
	}

	return structs
}

// extractStructFields extracts field definitions from a struct type.
func (r *reader) extractStructFields(structType *types.Struct) []domain.FieldDef {
	var fields []domain.FieldDef

	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)

		fieldDef := domain.FieldDef{
			Name:       field.Name(),
			Type:       r.convertTypeRef(field.Type()),
			IsExported: field.Exported(),
			Tag:        structType.Tag(i),
		}

		fields = append(fields, fieldDef)
	}

	return fields
}

// extractFunctions extracts all package-level functions (no receiver).
func (r *reader) extractFunctions(pkg *packages.Package, astFiles map[string]*ast.File) []domain.FunctionDef {
	var functions []domain.FunctionDef

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok {
			continue
		}

		sig := fn.Type().(*types.Signature)

		// Skip methods (have a receiver)
		if sig.Recv() != nil {
			continue
		}

		// Get position info for source file
		pos := fn.Pos()
		sourceFile := r.getSourceFile(pkg.Fset, pos)

		// Get doc comment
		doc := r.getDocComment(astFiles, sourceFile, name)

		fnDef := domain.FunctionDef{
			Name:       name,
			TypeParams: r.extractTypeParams(sig.TypeParams()),
			Params:     r.extractParams(sig.Params()),
			Returns:    r.extractReturns(sig.Results()),
			IsExported: isExported(name),
			SourceFile: sourceFile,
			Span:       r.findFuncDeclSpan(pkg, astFiles, name),
			Doc:        doc,
		}

		functions = append(functions, fnDef)
	}

	return functions
}

// extractTypeDefs extracts type definitions (type aliases with constants for enums).
func (r *reader) extractTypeDefs(
	pkg *packages.Package,
	astFiles map[string]*ast.File,
	constantsByType map[string][]string,
) []domain.TypeDef {
	var typeDefs []domain.TypeDef

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}

		// Skip interfaces and structs - we handle those separately
		named, _ := typeName.Type().(*types.Named)
		switch typeName.Type().Underlying().(type) {
		case *types.Interface, *types.Struct:
			continue
		}

		// Get position info for source file
		pos := typeName.Pos()
		sourceFile := r.getSourceFile(pkg.Fset, pos)

		// Get doc comment
		doc := r.getDocComment(astFiles, sourceFile, name)

		typeDef := domain.TypeDef{
			Name:           name,
			TypeParams:     r.extractTypeParams(namedTypeParams(named)),
			UnderlyingType: r.convertTypeRef(typeName.Type().Underlying()),
			Constants:      constantsByType[name],
			IsExported:     isExported(name),
			SourceFile:     sourceFile,
			Span:           r.findTypeSpecSpan(pkg, astFiles, name),
			Doc:            doc,
		}

		typeDefs = append(typeDefs, typeDef)
	}

	return typeDefs
}

// extractConstants extracts standalone package-level constants.
// Constants whose type is a named user-defined type are considered part of
// a TypeDef (enum pattern) and are skipped here — they remain accessible
// through TypeDef.Constants.
func (r *reader) extractConstants(pkg *packages.Package, astFiles map[string]*ast.File) []domain.ConstDef {
	var constants []domain.ConstDef

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		cnst, ok := obj.(*types.Const)
		if !ok {
			continue
		}

		// Skip enum constants — those whose type is a named user-defined type
		// and are captured via TypeDef.Constants.
		if _, isNamed := cnst.Type().(*types.Named); isNamed {
			continue
		}

		sourceFile := r.getSourceFile(pkg.Fset, cnst.Pos())
		doc := r.getDocComment(astFiles, sourceFile, name)
		value := r.getConstValueText(astFiles, sourceFile, name)
		if value == "" {
			value = cnst.Val().ExactString()
		}

		constants = append(constants, domain.ConstDef{
			Name:       name,
			Type:       r.convertTypeRef(cnst.Type()),
			Value:      value,
			IsExported: isExported(name),
			SourceFile: sourceFile,
			Span:       r.findValueSpecSpan(pkg, astFiles, name, token.CONST),
			Doc:        doc,
		})
	}

	return constants
}

// extractVarsAndErrors extracts package-level variables, separating sentinel
// errors (errors.New(...) / fmt.Errorf(...)) into a dedicated slice.
func (r *reader) extractVarsAndErrors(
	pkg *packages.Package,
	astFiles map[string]*ast.File,
) ([]domain.VarDef, []domain.ErrorDef) {
	var vars []domain.VarDef
	var errs []domain.ErrorDef

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		v, ok := obj.(*types.Var)
		if !ok {
			continue
		}

		// Skip fields and function params — only package-level vars.
		if v.IsField() {
			continue
		}

		sourceFile := r.getSourceFile(pkg.Fset, v.Pos())
		doc := r.getDocComment(astFiles, sourceFile, name)

		if msg, isErr := r.extractErrorMessage(astFiles, sourceFile, name); isErr {
			errs = append(errs, domain.ErrorDef{
				Name:       name,
				Message:    msg,
				IsExported: isExported(name),
				SourceFile: sourceFile,
				Span:       r.findValueSpecSpan(pkg, astFiles, name, token.VAR),
				Doc:        doc,
			})
			continue
		}

		vars = append(vars, domain.VarDef{
			Name:       name,
			Type:       r.convertTypeRef(v.Type()),
			IsExported: isExported(name),
			SourceFile: sourceFile,
			Span:       r.findValueSpecSpan(pkg, astFiles, name, token.VAR),
			Doc:        doc,
		})
	}

	return vars, errs
}

// getConstValueText returns the literal value source text for a constant.
// Returns an empty string if the declaration cannot be located or the
// value is not a basic literal.
func (r *reader) getConstValueText(astFiles map[string]*ast.File, filename, name string) string {
	for path, f := range astFiles {
		if filepath.Base(path) != filename {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					if n.Name != name {
						continue
					}
					if i < len(vs.Values) {
						return exprText(vs.Values[i])
					}
					// No explicit value (e.g. grouped iota) — fall back later.
					return ""
				}
			}
		}
	}
	return ""
}

// extractErrorMessage inspects the var declaration for `name` in the given
// source file and, if it is initialised with errors.New(...) or fmt.Errorf(...)
// where the first argument is a string literal, returns the unquoted message.
// The second return value reports whether the variable looked like a sentinel
// error at all (even when we couldn't extract a literal message).
func (r *reader) extractErrorMessage(astFiles map[string]*ast.File, filename, name string) (string, bool) {
	for path, f := range astFiles {
		if filepath.Base(path) != filename {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					if n.Name != name {
						continue
					}
					if i >= len(vs.Values) {
						return "", false
					}
					call, ok := vs.Values[i].(*ast.CallExpr)
					if !ok {
						return "", false
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return "", false
					}
					pkgIdent, ok := sel.X.(*ast.Ident)
					if !ok {
						return "", false
					}
					isErrorCtor := (pkgIdent.Name == "errors" && sel.Sel.Name == "New") ||
						(pkgIdent.Name == "fmt" && sel.Sel.Name == "Errorf")
					if !isErrorCtor {
						return "", false
					}
					if len(call.Args) == 0 {
						return "", true
					}
					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return "", true
					}
					// Strip quotes from the string literal.
					unquoted := lit.Value
					if len(unquoted) >= 2 {
						unquoted = unquoted[1 : len(unquoted)-1]
					}
					return unquoted, true
				}
			}
		}
	}
	return "", false
}

// exprText returns a best-effort textual representation of a simple expression.
// Only basic literals and identifiers (for `iota` and similar) are supported;
// more complex expressions yield an empty string.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Value
	case *ast.Ident:
		return v.Name
	case *ast.UnaryExpr:
		if lit, ok := v.X.(*ast.BasicLit); ok {
			return v.Op.String() + lit.Value
		}
	}
	return ""
}

// convertMethod converts a types.Func to a domain.MethodDef.
func (r *reader) convertMethod(fn *types.Func, sig *types.Signature) domain.MethodDef {
	return domain.MethodDef{
		Name:       fn.Name(),
		TypeParams: r.extractTypeParams(sig.TypeParams()),
		Params:     r.extractParams(sig.Params()),
		Returns:    r.extractReturns(sig.Results()),
		IsExported: isExported(fn.Name()),
	}
}

func (r *reader) extractTypeParams(typeParams *types.TypeParamList) []domain.ParamDef {
	if typeParams == nil || typeParams.Len() == 0 {
		return nil
	}
	params := make([]domain.ParamDef, 0, typeParams.Len())
	for i := 0; i < typeParams.Len(); i++ {
		tp := typeParams.At(i)
		params = append(params, domain.ParamDef{
			Name: tp.Obj().Name(),
			Type: r.convertTypeParamConstraint(tp.Constraint()),
		})
	}
	return params
}

func (r *reader) convertTypeParamConstraint(t types.Type) domain.TypeRef {
	if t == nil {
		return domain.TypeRef{Name: "any"}
	}
	name := r.stripModulePath(t.String())
	if name == "interface{}" {
		name = "any"
	}
	return domain.TypeRef{Name: name}
}

func namedTypeParams(named *types.Named) *types.TypeParamList {
	if named == nil {
		return nil
	}
	return named.TypeParams()
}

// extractParams extracts parameter definitions from a types.Tuple.
func (r *reader) extractParams(tuple *types.Tuple) []domain.ParamDef {
	if tuple == nil {
		return nil
	}

	var params []domain.ParamDef
	for i := 0; i < tuple.Len(); i++ {
		v := tuple.At(i)
		params = append(params, domain.ParamDef{
			Name: v.Name(),
			Type: r.convertTypeRef(v.Type()),
		})
	}

	return params
}

// extractReturns extracts return type references from a types.Tuple.
func (r *reader) extractReturns(tuple *types.Tuple) []domain.TypeRef {
	if tuple == nil {
		return nil
	}

	var returns []domain.TypeRef
	for i := 0; i < tuple.Len(); i++ {
		v := tuple.At(i)
		returns = append(returns, r.convertTypeRef(v.Type()))
	}

	return returns
}

// convertTypeRef converts a types.Type to a domain.TypeRef.
func (r *reader) convertTypeRef(t types.Type) domain.TypeRef {
	ref := domain.TypeRef{}

	// Handle pointer types
	if ptr, ok := t.(*types.Pointer); ok {
		ref.IsPointer = true
		t = ptr.Elem()
	}

	// Handle slice types
	if slice, ok := t.(*types.Slice); ok {
		ref.IsSlice = true
		elemRef := r.convertTypeRef(slice.Elem())
		ref.Name = elemRef.Name
		ref.Package = elemRef.Package
		ref.IsPointer = elemRef.IsPointer
		ref.TypeArgs = elemRef.TypeArgs
		return ref
	}

	// Handle map types
	if mapType, ok := t.(*types.Map); ok {
		ref.IsMap = true
		keyRef := r.convertTypeRef(mapType.Key())
		valueRef := r.convertTypeRef(mapType.Elem())
		ref.KeyType = &keyRef
		ref.ValueType = &valueRef
		return ref
	}

	// Handle type parameters
	if typeParam, ok := t.(*types.TypeParam); ok {
		ref.Name = typeParam.Obj().Name()
		return ref
	}

	// Handle named types
	if named, ok := t.(*types.Named); ok {
		ref.Name = named.Obj().Name()
		pkg := named.Obj().Pkg()
		if pkg != nil {
			if rel := r.relativePath(pkg.Path()); rel != "." {
				ref.Package = rel
			}
		}
		if typeArgs := named.TypeArgs(); typeArgs != nil {
			for i := 0; i < typeArgs.Len(); i++ {
				ref.TypeArgs = append(ref.TypeArgs, r.convertTypeRef(typeArgs.At(i)))
			}
		}
		return ref
	}

	// Handle basic types
	if basic, ok := t.(*types.Basic); ok {
		ref.Name = basic.Name()
		return ref
	}

	// Handle interface types (like error)
	if iface, ok := t.(*types.Interface); ok {
		if iface.Empty() {
			ref.Name = "interface{}"
		} else {
			// For non-empty interfaces, use the string representation
			ref.Name = r.stripModulePath(t.String())
		}
		return ref
	}

	// Handle signature/function types
	if sig, ok := t.(*types.Signature); ok {
		ref.Name = r.stripModulePath(sig.String())
		return ref
	}

	// Handle anonymous struct types - simplify to avoid D2 parsing issues
	// with curly braces, colons, and quotes in the full struct definition
	if _, ok := t.(*types.Struct); ok {
		ref.Name = "struct{...}"
		return ref
	}

	// Fallback: use string representation with module path stripped
	ref.Name = r.stripModulePath(t.String())
	return ref
}

// stripModulePath removes the module path prefix from type strings.
// e.g., "github.com/user/project/pkg.Type" becomes "pkg.Type"
func (r *reader) stripModulePath(s string) string {
	if r.modulePath == "" {
		return s
	}
	// Replace full module path with empty string, leaving just the relative path
	// The module path in type strings appears as "module/path/pkg.Type"
	return strings.ReplaceAll(s, r.modulePath+"/", "")
}

// getSourceFile extracts the filename from a token position.
func (r *reader) getSourceFile(fset *token.FileSet, pos token.Pos) string {
	if !pos.IsValid() {
		return ""
	}
	position := fset.Position(pos)
	return filepath.Base(position.Filename)
}

// getSpan creates a Span from AST node positions. The file path is relative
// to the module root. If moduleDir is empty or positions are invalid, returns
// a zero Span.
func (r *reader) getSpan(fset *token.FileSet, moduleDir string, start, end token.Pos) domain.Span {
	if !start.IsValid() || !end.IsValid() {
		return domain.Span{}
	}
	startPos := fset.Position(start)
	endPos := fset.Position(end)

	// Compute file path relative to module root
	file := startPos.Filename
	if moduleDir != "" {
		if rel, err := filepath.Rel(moduleDir, file); err == nil {
			file = rel
		}
	}

	return domain.Span{
		File:      file,
		StartByte: startPos.Offset,
		EndByte:   endPos.Offset,
		StartLine: startPos.Line,
		EndLine:   endPos.Line,
	}
}

// findTypeSpecSpan locates the AST TypeSpec for the given name and returns its span.
func (r *reader) findTypeSpecSpan(pkg *packages.Package, astFiles map[string]*ast.File, name string) domain.Span {
	moduleDir := ""
	if pkg.Module != nil {
		moduleDir = pkg.Module.Dir
	}

	for _, f := range astFiles {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != name {
					continue
				}
				// Use the GenDecl's start (includes 'type' keyword and doc) to spec's end
				return r.getSpan(pkg.Fset, moduleDir, gd.Pos(), ts.End())
			}
		}
	}
	return domain.Span{}
}

// findFuncDeclSpan locates the AST FuncDecl for the given function name and returns its span.
func (r *reader) findFuncDeclSpan(pkg *packages.Package, astFiles map[string]*ast.File, name string) domain.Span {
	moduleDir := ""
	if pkg.Module != nil {
		moduleDir = pkg.Module.Dir
	}

	for _, f := range astFiles {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil { // Skip methods
				continue
			}
			if fd.Name.Name != name {
				continue
			}
			return r.getSpan(pkg.Fset, moduleDir, fd.Pos(), fd.End())
		}
	}
	return domain.Span{}
}

// findMethodDeclSpan locates the AST FuncDecl for a method on the given receiver type.
func (r *reader) findMethodDeclSpan(pkg *packages.Package, astFiles map[string]*ast.File, receiverType, methodName string) domain.Span {
	moduleDir := ""
	if pkg.Module != nil {
		moduleDir = pkg.Module.Dir
	}

	for _, f := range astFiles {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil {
				continue
			}
			if fd.Name.Name != methodName {
				continue
			}
			// Check receiver type
			if len(fd.Recv.List) == 0 {
				continue
			}
			recvType := fd.Recv.List[0].Type
			// Handle *T
			if star, ok := recvType.(*ast.StarExpr); ok {
				recvType = star.X
			}
			if ident, ok := recvType.(*ast.Ident); ok && ident.Name == receiverType {
				return r.getSpan(pkg.Fset, moduleDir, fd.Pos(), fd.End())
			}
			// Handle generic receiver T[P]
			if idx, ok := recvType.(*ast.IndexExpr); ok {
				if ident, ok := idx.X.(*ast.Ident); ok && ident.Name == receiverType {
					return r.getSpan(pkg.Fset, moduleDir, fd.Pos(), fd.End())
				}
			}
			if idx, ok := recvType.(*ast.IndexListExpr); ok {
				if ident, ok := idx.X.(*ast.Ident); ok && ident.Name == receiverType {
					return r.getSpan(pkg.Fset, moduleDir, fd.Pos(), fd.End())
				}
			}
		}
	}
	return domain.Span{}
}

// findValueSpecSpan locates the AST ValueSpec for a const or var declaration.
func (r *reader) findValueSpecSpan(pkg *packages.Package, astFiles map[string]*ast.File, name string, tok token.Token) domain.Span {
	moduleDir := ""
	if pkg.Module != nil {
		moduleDir = pkg.Module.Dir
	}

	for _, f := range astFiles {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != tok {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					if n.Name == name {
						// For single-spec declarations, use GenDecl span (includes const/var keyword)
						if len(gd.Specs) == 1 {
							return r.getSpan(pkg.Fset, moduleDir, gd.Pos(), gd.End())
						}
						// For grouped declarations, use just the ValueSpec
						return r.getSpan(pkg.Fset, moduleDir, vs.Pos(), vs.End())
					}
				}
			}
		}
	}
	return domain.Span{}
}

// getDocComment retrieves the doc comment for a named declaration.
func (r *reader) getDocComment(astFiles map[string]*ast.File, filename, name string) string {
	for path, f := range astFiles {
		if filepath.Base(path) != filename {
			continue
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.Name == name {
							// Check GenDecl doc first, then TypeSpec doc
							if d.Doc != nil {
								return d.Doc.Text()
							}
							if s.Doc != nil {
								return s.Doc.Text()
							}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.Name == name {
								if d.Doc != nil {
									return d.Doc.Text()
								}
								if s.Doc != nil {
									return s.Doc.Text()
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Name.Name == name {
					if d.Doc != nil {
						return d.Doc.Text()
					}
				}
			}
		}
	}

	return ""
}

// collectDependencies collects all dependencies for the package.
// All dependencies are collected regardless of visibility - filtering is done
// by the diagram builders based on the mode (public vs internal).
// Each dependency tracks whether it's through an exported method/field.
func (r *reader) collectDependencies(pkg *packages.Package, model *domain.PackageModel) []domain.Dependency {
	var deps []domain.Dependency
	seenDeps := make(map[string]bool)

	// Helper to add a dependency if it's not a duplicate. Use a
	// hand-rolled key to avoid the fmt.Sprintf allocation; this is one
	// of the hot loops on a full extraction (see #58 profiling notes).
	var keyBuf strings.Builder
	addDep := func(dep domain.Dependency) {
		keyBuf.Reset()
		keyBuf.Grow(len(dep.From.Package) + len(dep.From.Symbol) + len(dep.To.Package) + len(dep.To.Symbol) + len(dep.Kind) + 8)
		keyBuf.WriteString(dep.From.Package)
		keyBuf.WriteByte('.')
		keyBuf.WriteString(dep.From.Symbol)
		keyBuf.WriteString("->")
		keyBuf.WriteString(dep.To.Package)
		keyBuf.WriteByte('.')
		keyBuf.WriteString(dep.To.Symbol)
		keyBuf.WriteByte(':')
		keyBuf.WriteString(string(dep.Kind))
		key := keyBuf.String()
		if !seenDeps[key] {
			seenDeps[key] = true
			deps = append(deps, dep)
		}
	}

	// Process interfaces
	for _, iface := range model.Interfaces {
		fromRef := domain.SymbolRef{
			Package: model.Path,
			File:    iface.SourceFile,
			Symbol:  iface.Name,
		}

		for _, method := range iface.Methods {
			// Interface + exported method = through exported
			throughExported := iface.IsExported && method.IsExported

			// Emit interface-level edge (existing behavior).
			r.collectMethodDependenciesWithVisibility(fromRef, method, pkg, throughExported, addDep)

			// Emit method-level edge for graph connectivity.
			methodRef := domain.SymbolRef{
				Package: model.Path,
				File:    iface.SourceFile,
				Symbol:  iface.Name + "." + method.Name,
			}
			r.collectMethodDependenciesWithVisibility(methodRef, method, pkg, throughExported, addDep)
		}
	}

	// Process structs
	for _, s := range model.Structs {
		fromRef := domain.SymbolRef{
			Package: model.Path,
			File:    s.SourceFile,
			Symbol:  s.Name,
		}

		// Field dependencies — use typeRefToSymbolRefs to capture map/slice element types.
		for _, field := range s.Fields {
			throughExported := s.IsExported && field.IsExported
			for _, toRef := range r.typeRefToSymbolRefs(field.Type, pkg) {
				addDep(domain.Dependency{
					From:            fromRef,
					To:              toRef,
					Kind:            domain.DependencyUses,
					ThroughExported: throughExported,
				})
			}
		}

		// Method dependencies — emit edges from BOTH the struct (for backward
		// compatibility / aggregate-level views) AND the method node itself
		// (for method-level connectivity in the graph).
		for _, method := range s.Methods {
			// Struct + exported method = through exported
			throughExported := s.IsExported && method.IsExported

			// Emit struct-level edge (existing behavior).
			r.collectMethodDependenciesWithVisibility(fromRef, method, pkg, throughExported, addDep)

			// Emit method-level edge for graph connectivity.
			methodRef := domain.SymbolRef{
				Package: model.Path,
				File:    s.SourceFile,
				Symbol:  s.Name + "." + method.Name,
			}
			r.collectMethodDependenciesWithVisibility(methodRef, method, pkg, throughExported, addDep)
		}
	}

	// Process functions
	for _, fn := range model.Functions {
		fromRef := domain.SymbolRef{
			Package: model.Path,
			File:    fn.SourceFile,
			Symbol:  fn.Name,
		}

		// Function is its own visibility gate
		throughExported := fn.IsExported

		// Parameter dependencies — use typeRefToSymbolRefs to capture map/slice element types.
		for _, param := range fn.Params {
			for _, toRef := range r.typeRefToSymbolRefs(param.Type, pkg) {
				addDep(domain.Dependency{
					From:            fromRef,
					To:              toRef,
					Kind:            domain.DependencyUses,
					ThroughExported: throughExported,
				})
			}
		}

		// Return type dependencies — use typeRefToSymbolRefs to capture map/slice element types.
		for _, ret := range fn.Returns {
			for _, toRef := range r.typeRefToSymbolRefs(ret, pkg) {
				addDep(domain.Dependency{
					From:            fromRef,
					To:              toRef,
					Kind:            domain.DependencyReturns,
					ThroughExported: throughExported,
				})
			}
		}
	}

	// Process constants — emit uses edge to the constant's declared type.
	for _, c := range model.Constants {
		// Skip untyped constants (empty Type).
		if c.Type.Name == "" {
			continue
		}
		fromRef := domain.SymbolRef{
			Package: model.Path,
			File:    c.SourceFile,
			Symbol:  c.Name,
		}
		for _, toRef := range r.typeRefToSymbolRefs(c.Type, pkg) {
			addDep(domain.Dependency{
				From:            fromRef,
				To:              toRef,
				Kind:            domain.DependencyUses,
				ThroughExported: c.IsExported,
			})
		}
	}

	// Process variables — emit uses edge to the variable's declared type.
	for _, v := range model.Variables {
		// Skip variables with unresolved types.
		if v.Type.Name == "" {
			continue
		}
		fromRef := domain.SymbolRef{
			Package: model.Path,
			File:    v.SourceFile,
			Symbol:  v.Name,
		}
		for _, toRef := range r.typeRefToSymbolRefs(v.Type, pkg) {
			addDep(domain.Dependency{
				From:            fromRef,
				To:              toRef,
				Kind:            domain.DependencyUses,
				ThroughExported: v.IsExported,
			})
		}
	}

	// Process sentinel errors — they don't have Type (they are error values),
	// but if we wanted to emit edges to 'error' interface, we'd do it here.
	// For now, errors are connectivity sinks (degree-0 is acceptable for sentinel errors).

	return deps
}

// collectConstructionDependencies walks function and method bodies looking for
// composite literal constructions (T{...}, &T{...}) of named types from the
// loaded package set. For each, it emits a DependencyUses edge from the
// enclosing function/method to the constructed type. This captures connectivity
// that structural signature analysis misses — particularly when the constructed
// value is passed through interface{}/any.
func (r *reader) collectConstructionDependencies(pkg *packages.Package, model *domain.PackageModel, astFiles map[string]*ast.File) []domain.Dependency {
	var deps []domain.Dependency
	seenDeps := make(map[string]bool)

	addDep := func(dep domain.Dependency) {
		key := dep.From.Package + "." + dep.From.Symbol + "->" + dep.To.Package + "." + dep.To.Symbol + ":" + string(dep.Kind)
		if !seenDeps[key] {
			seenDeps[key] = true
			deps = append(deps, dep)
		}
	}

	// Collect named types by package for quick lookup.
	loadedTypes := make(map[*types.Package]bool)
	if pkg.Types != nil {
		loadedTypes[pkg.Types] = true
	}
	for _, imp := range pkg.Imports {
		if imp.Types != nil {
			loadedTypes[imp.Types] = true
		}
	}

	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}

			// Determine the "from" symbol for this function/method.
			var fromRef domain.SymbolRef
			var throughExported bool
			if fd.Recv == nil {
				// Package-level function.
				fn := findFunctionDef(model.Functions, fd.Name.Name)
				if fn == nil {
					continue
				}
				fromRef = domain.SymbolRef{
					Package: model.Path,
					File:    fn.SourceFile,
					Symbol:  fn.Name,
				}
				throughExported = fn.IsExported
			} else {
				// Method.
				recvName := receiverTypeName(fd.Recv)
				if recvName == "" {
					continue
				}
				// Find the struct or interface owning this method.
				s := findStructDef(model.Structs, recvName)
				if s != nil {
					m := findMethodInStruct(s, fd.Name.Name)
					if m == nil {
						continue
					}
					fromRef = domain.SymbolRef{
						Package: model.Path,
						File:    s.SourceFile,
						Symbol:  s.Name,
					}
					throughExported = s.IsExported && m.IsExported
				} else {
					// Could be an interface method (rare to have body).
					continue
				}
			}

			// Walk the body looking for composite literals.
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}

				// Resolve the composite literal's type.
				tv, ok := pkg.TypesInfo.Types[cl]
				if !ok {
					return true
				}

				// Unwrap pointer type (for &T{}).
				t := tv.Type
				if ptr, ok := t.(*types.Pointer); ok {
					t = ptr.Elem()
				}

				named, ok := t.(*types.Named)
				if !ok {
					return true
				}

				// Check if the type's package is in the loaded set.
				typePkg := named.Obj().Pkg()
				if typePkg == nil || !loadedTypes[typePkg] {
					return true
				}

				// Emit a uses dependency to the constructed type.
				toRef := domain.SymbolRef{
					Package:  r.relativePath(typePkg.Path()),
					Symbol:   named.Obj().Name(),
					External: false,
				}

				addDep(domain.Dependency{
					From:            fromRef,
					To:              toRef,
					Kind:            domain.DependencyUses,
					ThroughExported: throughExported,
				})

				return true
			})
		}
	}

	return deps
}

// findFunctionDef finds a FunctionDef by name.
func findFunctionDef(fns []domain.FunctionDef, name string) *domain.FunctionDef {
	for i := range fns {
		if fns[i].Name == name {
			return &fns[i]
		}
	}
	return nil
}

// findStructDef finds a StructDef by name.
func findStructDef(structs []domain.StructDef, name string) *domain.StructDef {
	for i := range structs {
		if structs[i].Name == name {
			return &structs[i]
		}
	}
	return nil
}

// findMethodInStruct finds a method by name in a struct.
func findMethodInStruct(s *domain.StructDef, methodName string) *domain.MethodDef {
	for i := range s.Methods {
		if s.Methods[i].Name == methodName {
			return &s.Methods[i]
		}
	}
	return nil
}

// collectMethodDependenciesWithVisibility collects dependencies from a method's parameters and returns.
// throughExported indicates if this is through an exported method on an exported type.
func (r *reader) collectMethodDependenciesWithVisibility(
	fromRef domain.SymbolRef,
	method domain.MethodDef,
	pkg *packages.Package,
	throughExported bool,
	addDep func(domain.Dependency),
) {
	// Parameter dependencies — use typeRefToSymbolRefs to capture map/slice element types.
	for _, param := range method.Params {
		for _, toRef := range r.typeRefToSymbolRefs(param.Type, pkg) {
			addDep(domain.Dependency{
				From:            fromRef,
				To:              toRef,
				Kind:            domain.DependencyUses,
				ThroughExported: throughExported,
			})
		}
	}

	// Return type dependencies — use typeRefToSymbolRefs to capture map/slice element types.
	for _, ret := range method.Returns {
		for _, toRef := range r.typeRefToSymbolRefs(ret, pkg) {
			addDep(domain.Dependency{
				From:            fromRef,
				To:              toRef,
				Kind:            domain.DependencyReturns,
				ThroughExported: throughExported,
			})
		}
	}
}

// typeRefToSymbolRef converts a TypeRef to a SymbolRef for dependency tracking.
// Returns nil for basic types that don't create meaningful dependencies.
// For composite types (maps, slices, channels), use typeRefToSymbolRefs to get all component types.
func (r *reader) typeRefToSymbolRef(ref domain.TypeRef, pkg *packages.Package) *domain.SymbolRef {
	// Handle maps - delegate to typeRefToSymbolRefs for multi-type extraction.
	if ref.IsMap {
		return nil
	}

	// Skip basic types (string, int, bool, error, etc.)
	if isBasicType(ref.Name) {
		return nil
	}

	// Skip empty type names
	if ref.Name == "" {
		return nil
	}

	symRef := &domain.SymbolRef{
		Symbol: ref.Name,
	}

	currentPkgRelPath := r.relativePath(pkg.PkgPath)

	if ref.Package == "" || ref.Package == "." {
		// Local type - look up in current package to find file
		symRef.Package = currentPkgRelPath
		symRef.File = r.findSymbolFile(pkg, ref.Name)
	} else if ref.Package == currentPkgRelPath {
		// Same package (handles root-level packages like "observability")
		symRef.Package = ref.Package
		symRef.File = r.findSymbolFile(pkg, ref.Name)
	} else if r.modulePath != "" && (strings.HasPrefix(ref.Package, r.modulePath) || strings.Contains(ref.Package, "/")) {
		// Internal package: either has full module prefix or contains /
		symRef.Package = ref.Package
	} else if !strings.Contains(ref.Package, "/") && r.modulePath != "" {
		// No slash and we have a module path - check if it could be a sibling root package
		// by seeing if the module has no slashes (meaning root packages are valid)
		// For now, assume packages without / that aren't the current package are external (stdlib)
		symRef.Package = ref.Package
		symRef.External = true
	} else {
		// External package (standard library like "context", "time")
		symRef.Package = ref.Package
		symRef.External = true
	}

	return symRef
}

// typeRefToSymbolRefs extracts all symbol references from a TypeRef, including
// composite type components (map keys/values, slice elements). Returns an empty
// slice for basic types. This captures dependencies that typeRefToSymbolRef misses.
func (r *reader) typeRefToSymbolRefs(ref domain.TypeRef, pkg *packages.Package) []domain.SymbolRef {
	var refs []domain.SymbolRef

	// Handle maps — extract both key and value types.
	if ref.IsMap {
		if ref.KeyType != nil {
			refs = append(refs, r.typeRefToSymbolRefs(*ref.KeyType, pkg)...)
		}
		if ref.ValueType != nil {
			refs = append(refs, r.typeRefToSymbolRefs(*ref.ValueType, pkg)...)
		}
		return refs
	}

	// Handle slices — extract element type from the underlying Name after stripping [].
	if ref.IsSlice {
		// The element type info should be in Name without the leading [].
		// However, TypeRef for slices has IsSlice=true and Name = element type name.
		// So we recurse with a TypeRef for the element.
		elemRef := domain.TypeRef{
			Name:      ref.Name,
			Package:   ref.Package,
			IsPointer: ref.IsPointer,
			TypeArgs:  ref.TypeArgs,
		}
		return r.typeRefToSymbolRefs(elemRef, pkg)
	}

	// For non-composite types, use the existing single-ref logic.
	if symRef := r.typeRefToSymbolRef(ref, pkg); symRef != nil {
		refs = append(refs, *symRef)
	}

	return refs
}

// findSymbolFile finds the source file for a symbol in a package.
func (r *reader) findSymbolFile(pkg *packages.Package, symbolName string) string {
	scope := pkg.Types.Scope()
	obj := scope.Lookup(symbolName)
	if obj == nil {
		return ""
	}
	return r.getSourceFile(pkg.Fset, obj.Pos())
}

// applyStereotypes applies stereotype detection to all symbols in the model.
func (r *reader) applyStereotypes(model *domain.PackageModel) {
	for i := range model.Interfaces {
		model.Interfaces[i].Stereotype = detectInterfaceStereotype(model.Interfaces[i], model.Path)
	}

	for i := range model.Structs {
		model.Structs[i].Stereotype = detectStructStereotype(model.Structs[i], model.Path)
	}

	for i := range model.Functions {
		model.Functions[i].Stereotype = detectFunctionStereotype(model.Functions[i])
	}

	for i := range model.TypeDefs {
		model.TypeDefs[i].Stereotype = detectTypeDefStereotype(model.TypeDefs[i])
	}
}

// computeImplementations computes interface implementations via go/types.Implements.
// For each exported interface defined in the loaded packages, it iterates over all
// named types across loaded packages and records which concrete types (T or *T)
// implement it. Results are stored in the owning interface's PackageModel.
func (r *reader) computeImplementations(pkgs []*packages.Package, models []domain.PackageModel) {
	// Collect all interfaces (from loaded packages only) that are exported.
	type ifaceEntry struct {
		pkg        *packages.Package
		name       string
		iface      *types.Interface
		modelIdx   int // index into models slice
		sourceFile string
	}

	// Map model.Path -> model index for quick lookup.
	modelIdxByPath := make(map[string]int, len(models))
	for i, m := range models {
		modelIdxByPath[m.Path] = i
	}

	var ifaceEntries []ifaceEntry
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		relPath := r.relativePath(pkg.PkgPath)
		modelIdx, ok := modelIdxByPath[relPath]
		if !ok {
			continue
		}
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			if !typeName.Exported() {
				continue
			}
			iface, ok := typeName.Type().Underlying().(*types.Interface)
			if !ok {
				continue
			}
			// Skip empty interfaces (any/interface{}): every type satisfies them.
			if iface.Empty() {
				continue
			}
			ifaceEntries = append(ifaceEntries, ifaceEntry{
				pkg:        pkg,
				name:       name,
				iface:      iface,
				modelIdx:   modelIdx,
				sourceFile: r.getSourceFile(pkg.Fset, typeName.Pos()),
			})
		}
	}

	if len(ifaceEntries) == 0 {
		return
	}

	// Collect all named concrete types (non-interfaces) across loaded packages.
	type concreteEntry struct {
		pkg        *packages.Package
		name       string
		named      *types.Named
		sourceFile string
	}
	var concretes []concreteEntry
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok {
				continue
			}
			// Skip interfaces — we only care about concrete types as implementers.
			if _, isIface := named.Underlying().(*types.Interface); isIface {
				continue
			}
			concretes = append(concretes, concreteEntry{
				pkg:        pkg,
				name:       name,
				named:      named,
				sourceFile: r.getSourceFile(pkg.Fset, typeName.Pos()),
			})
		}
	}

	// Deduplicate implementations per owning package.
	seen := make(map[string]bool)

	for _, ie := range ifaceEntries {
		ifaceRelPath := r.relativePath(ie.pkg.PkgPath)
		ifaceRef := domain.SymbolRef{
			Package: ifaceRelPath,
			File:    ie.sourceFile,
			Symbol:  ie.name,
		}

		for _, c := range concretes {
			concreteRelPath := r.relativePath(c.pkg.PkgPath)
			// Skip if concrete type is identical to the interface type itself.
			if c.pkg == ie.pkg && c.name == ie.name {
				continue
			}

			concreteRef := domain.SymbolRef{
				Package: concreteRelPath,
				File:    c.sourceFile,
				Symbol:  c.name,
			}

			// Check value type first.
			if types.Implements(c.named, ie.iface) {
				key := fmt.Sprintf("%s.%s->%s.%s|v",
					concreteRef.Package, concreteRef.Symbol,
					ifaceRef.Package, ifaceRef.Symbol)
				if !seen[key] {
					seen[key] = true
					models[ie.modelIdx].Implementations = append(
						models[ie.modelIdx].Implementations,
						domain.Implementation{
							Concrete:  concreteRef,
							Interface: ifaceRef,
							IsPointer: false,
						},
					)
				}
				continue
			}

			// Check pointer type only if value type does not implement.
			ptr := types.NewPointer(c.named)
			if types.Implements(ptr, ie.iface) {
				key := fmt.Sprintf("%s.%s->%s.%s|p",
					concreteRef.Package, concreteRef.Symbol,
					ifaceRef.Package, ifaceRef.Symbol)
				if !seen[key] {
					seen[key] = true
					models[ie.modelIdx].Implementations = append(
						models[ie.modelIdx].Implementations,
						domain.Implementation{
							Concrete:  concreteRef,
							Interface: ifaceRef,
							IsPointer: true,
						},
					)
				}
			}
		}
	}
}

// isExported returns true if the name starts with an uppercase letter.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)
	return unicode.IsUpper(r[0])
}

// isBasicType returns true if the type name is a Go basic type.
func isBasicType(name string) bool {
	basicTypes := map[string]bool{
		"bool":        true,
		"byte":        true,
		"complex64":   true,
		"complex128":  true,
		"error":       true,
		"float32":     true,
		"float64":     true,
		"int":         true,
		"int8":        true,
		"int16":       true,
		"int32":       true,
		"int64":       true,
		"rune":        true,
		"string":      true,
		"uint":        true,
		"uint8":       true,
		"uint16":      true,
		"uint32":      true,
		"uint64":      true,
		"uintptr":     true,
		"any":         true,
		"interface{}": true,
	}
	return basicTypes[name]
}
