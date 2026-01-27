package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
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

	var models []domain.PackageModel
	for _, pkg := range pkgs {
		// Check context cancellation between packages
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		model, err := r.convertPackage(pkg)
		if err != nil {
			return nil, fmt.Errorf("converting package %s: %w", pkg.PkgPath, err)
		}

		models = append(models, model)
	}

	return models, nil
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
	methodsByReceiver := r.collectMethodsByReceiver(pkg)

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

	// Collect dependencies
	model.Dependencies = r.collectDependencies(pkg, &model)

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
func (r *reader) collectMethodsByReceiver(pkg *packages.Package) map[string][]domain.MethodDef {
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
			Methods:    r.extractInterfaceMethods(iface),
			IsExported: isExported(name),
			SourceFile: sourceFile,
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
			Fields:     r.extractStructFields(structType),
			Methods:    methodsByReceiver[name],
			IsExported: isExported(name),
			SourceFile: sourceFile,
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
			Params:     r.extractParams(sig.Params()),
			Returns:    r.extractReturns(sig.Results()),
			IsExported: isExported(name),
			SourceFile: sourceFile,
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
			UnderlyingType: r.convertTypeRef(typeName.Type().Underlying()),
			Constants:      constantsByType[name],
			IsExported:     isExported(name),
			SourceFile:     sourceFile,
			Doc:            doc,
		}

		typeDefs = append(typeDefs, typeDef)
	}

	return typeDefs
}

// convertMethod converts a types.Func to a domain.MethodDef.
func (r *reader) convertMethod(fn *types.Func, sig *types.Signature) domain.MethodDef {
	return domain.MethodDef{
		Name:       fn.Name(),
		Params:     r.extractParams(sig.Params()),
		Returns:    r.extractReturns(sig.Results()),
		IsExported: isExported(fn.Name()),
	}
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

	// Handle named types
	if named, ok := t.(*types.Named); ok {
		ref.Name = named.Obj().Name()
		pkg := named.Obj().Pkg()
		if pkg != nil {
			ref.Package = r.relativePath(pkg.Path())
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
			ref.Name = t.String()
		}
		return ref
	}

	// Fallback: use string representation
	ref.Name = t.String()
	return ref
}

// getSourceFile extracts the filename from a token position.
func (r *reader) getSourceFile(fset *token.FileSet, pos token.Pos) string {
	if !pos.IsValid() {
		return ""
	}
	position := fset.Position(pos)
	return filepath.Base(position.Filename)
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

	// Helper to add a dependency if it's not a duplicate
	addDep := func(dep domain.Dependency) {
		key := fmt.Sprintf("%s->%s:%s", dep.From.String(), dep.To.String(), dep.Kind)
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
			r.collectMethodDependenciesWithVisibility(fromRef, method, pkg, throughExported, addDep)
		}
	}

	// Process structs
	for _, s := range model.Structs {
		fromRef := domain.SymbolRef{
			Package: model.Path,
			File:    s.SourceFile,
			Symbol:  s.Name,
		}

		// Field dependencies
		for _, field := range s.Fields {
			if toRef := r.typeRefToSymbolRef(field.Type, pkg); toRef != nil {
				// Struct + exported field = through exported
				throughExported := s.IsExported && field.IsExported
				addDep(domain.Dependency{
					From:            fromRef,
					To:              *toRef,
					Kind:            domain.DependencyUses,
					ThroughExported: throughExported,
				})
			}
		}

		// Method dependencies
		for _, method := range s.Methods {
			// Struct + exported method = through exported
			throughExported := s.IsExported && method.IsExported
			r.collectMethodDependenciesWithVisibility(fromRef, method, pkg, throughExported, addDep)
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

		// Parameter dependencies
		for _, param := range fn.Params {
			if toRef := r.typeRefToSymbolRef(param.Type, pkg); toRef != nil {
				addDep(domain.Dependency{
					From:            fromRef,
					To:              *toRef,
					Kind:            domain.DependencyUses,
					ThroughExported: throughExported,
				})
			}
		}

		// Return type dependencies
		for _, ret := range fn.Returns {
			if toRef := r.typeRefToSymbolRef(ret, pkg); toRef != nil {
				addDep(domain.Dependency{
					From:            fromRef,
					To:              *toRef,
					Kind:            domain.DependencyReturns,
					ThroughExported: throughExported,
				})
			}
		}
	}

	return deps
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
	// Parameter dependencies
	for _, param := range method.Params {
		if toRef := r.typeRefToSymbolRef(param.Type, pkg); toRef != nil {
			addDep(domain.Dependency{
				From:            fromRef,
				To:              *toRef,
				Kind:            domain.DependencyUses,
				ThroughExported: throughExported,
			})
		}
	}

	// Return type dependencies
	for _, ret := range method.Returns {
		if toRef := r.typeRefToSymbolRef(ret, pkg); toRef != nil {
			addDep(domain.Dependency{
				From:            fromRef,
				ThroughExported: throughExported,
				To:   *toRef,
				Kind: domain.DependencyReturns,
			})
		}
	}
}

// typeRefToSymbolRef converts a TypeRef to a SymbolRef for dependency tracking.
// Returns nil for basic types that don't create meaningful dependencies.
func (r *reader) typeRefToSymbolRef(ref domain.TypeRef, pkg *packages.Package) *domain.SymbolRef {
	// Handle maps - collect dependencies for key and value types
	if ref.IsMap {
		// We don't create a single dependency for maps, the caller should handle
		// key and value types separately if needed
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

	if ref.Package == "" || ref.Package == "." {
		// Local type - look up in current package to find file
		symRef.Package = r.relativePath(pkg.PkgPath)
		symRef.File = r.findSymbolFile(pkg, ref.Name)
	} else if strings.Contains(ref.Package, "/") {
		// Internal package path
		symRef.Package = ref.Package
	} else {
		// External package (likely standard library like "context")
		symRef.Package = ref.Package
		symRef.External = true
	}

	return symRef
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
		"bool":       true,
		"byte":       true,
		"complex64":  true,
		"complex128": true,
		"error":      true,
		"float32":    true,
		"float64":    true,
		"int":        true,
		"int8":       true,
		"int16":      true,
		"int32":      true,
		"int64":      true,
		"rune":       true,
		"string":     true,
		"uint":       true,
		"uint8":      true,
		"uint16":     true,
		"uint32":     true,
		"uint64":     true,
		"uintptr":    true,
		"any":        true,
		"interface{}": true,
	}
	return basicTypes[name]
}
