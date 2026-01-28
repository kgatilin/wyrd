package d2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"oss.terrastruct.com/d2/d2ast"
	"oss.terrastruct.com/d2/d2parser"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/service"
)

// Error definitions for the D2 reader.
var (
	// ErrNoPackages indicates no package containers were found in the D2 file.
	ErrNoPackages = errors.New("no package containers found in D2 file")

	// ErrInvalidLabel indicates a package container has an invalid or missing label.
	ErrInvalidLabel = errors.New("package container has invalid or missing label")

	// ErrEmptyDiagram indicates the D2 file is empty or contains no meaningful content.
	ErrEmptyDiagram = errors.New("D2 file is empty or contains no meaningful content")
)

// reader reads D2 diagram files and converts them to domain.PackageModel structures.
type reader struct{}

// NewReader creates a new D2 diagram reader that implements service.ModelReader.
func NewReader() service.ModelReader {
	return &reader{}
}

// Read parses D2 diagram files and returns package models.
// Each file is expected to be a combined diagram with package containers.
func (r *reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	if len(paths) == 0 {
		return nil, ErrEmptyDiagram
	}

	var allPackages []domain.PackageModel

	for _, path := range paths {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		packages, err := r.readFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		allPackages = append(allPackages, packages...)
	}

	if len(allPackages) == 0 {
		return nil, ErrNoPackages
	}

	return allPackages, nil
}

// readFile parses a single D2 file and extracts package models.
func (r *reader) readFile(path string) ([]domain.PackageModel, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	if len(bytes.TrimSpace(content)) == 0 {
		return nil, ErrEmptyDiagram
	}

	ast, err := d2parser.Parse(path, bytes.NewReader(content), nil)
	if err != nil {
		return nil, fmt.Errorf("parsing D2: %w", err)
	}

	// Detect if this is a split-mode file (per-file grouping) vs combined-mode (per-package)
	if r.isSplitMode(ast) {
		return r.parseSplitModeAST(ast, path)
	}

	return r.parseAST(ast)
}

// isSplitMode detects if a D2 file uses split-mode format (per-file grouping).
// Split-mode files have top-level containers with labels like "reader.go" (file names),
// while combined-mode files have containers with labels like "internal/adapter/d2" (package paths).
func (r *reader) isSplitMode(ast *d2ast.Map) bool {
	if ast == nil || len(ast.Nodes) == 0 {
		return false
	}

	for _, node := range ast.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		containerID := getKeyPathString(node.MapKey.Key)

		// Skip legend and other special containers
		if containerID == "legend" || containerID == "classes" {
			continue
		}

		// Skip edges
		if len(node.MapKey.Edges) > 0 {
			continue
		}

		// Must have a map value to be a container
		if node.MapKey.Value.Map == nil {
			continue
		}

		// Check the label of this container
		for _, child := range node.MapKey.Value.Map.Nodes {
			if child.MapKey == nil || child.MapKey.Key == nil {
				continue
			}
			keyName := getKeyPathString(child.MapKey.Key)
			if keyName == "label" {
				label := getScalarValue(child.MapKey.Value)
				// Split-mode: labels are file names (end with .go)
				// Combined-mode: labels are package paths (contain /)
				if strings.HasSuffix(label, ".go") {
					return true
				}
				if strings.Contains(label, "/") {
					return false
				}
			}
		}
	}

	return false
}

// parseSplitModeAST parses a split-mode D2 file where symbols are grouped by file.
// It extracts the package path from the file's directory and collects all symbols
// into a single PackageModel.
func (r *reader) parseSplitModeAST(ast *d2ast.Map, filePath string) ([]domain.PackageModel, error) {
	if ast == nil || len(ast.Nodes) == 0 {
		return nil, ErrEmptyDiagram
	}

	// Derive package path from file location: /path/to/pkg/.arch/pub.d2 -> /path/to/pkg
	pkgPath := derivePackagePath(filePath)
	pkgName := extractPackageName(pkgPath)

	pkg := domain.PackageModel{
		Name: pkgName,
		Path: pkgPath,
	}

	// Iterate through file groups and collect all symbols
	for _, node := range ast.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		containerID := getKeyPathString(node.MapKey.Key)

		// Skip legend and other special containers
		if containerID == "legend" || containerID == "classes" {
			continue
		}

		// Skip edges at this level (handled separately)
		if len(node.MapKey.Edges) > 0 {
			continue
		}

		// Must have a map value to be a file group
		if node.MapKey.Value.Map == nil {
			continue
		}

		// Parse symbols from this file group
		r.extractSymbolsFromFileGroup(node.MapKey.Value.Map, &pkg)
	}

	// Extract dependencies from root level (intra-package dependencies in split-mode)
	// In split-mode, dependencies use file-group prefixes like "dependency.Dependency"
	// We need to strip the prefix and use just the symbol name
	rawDeps := r.extractDependencies(ast)
	for _, dep := range rawDeps {
		// For split-mode, strip file-group prefixes from symbol references
		// e.g., "dependency.Dependency" -> symbol="Dependency", package=pkgPath
		dep.From = stripFileGroupPrefix(dep.From, pkgPath)
		dep.To = stripFileGroupPrefix(dep.To, pkgPath)
		pkg.Dependencies = append(pkg.Dependencies, dep)
	}

	if len(pkg.Interfaces) == 0 && len(pkg.Structs) == 0 &&
		len(pkg.Functions) == 0 && len(pkg.TypeDefs) == 0 {
		return nil, ErrNoPackages
	}

	return []domain.PackageModel{pkg}, nil
}

// extractSymbolsFromFileGroup extracts symbols from a file group container in split-mode.
func (r *reader) extractSymbolsFromFileGroup(m *d2ast.Map, pkg *domain.PackageModel) {
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		// Skip label and style properties
		if keyName == "label" || strings.HasPrefix(keyName, "style.") {
			continue
		}

		// Skip edges
		if len(node.MapKey.Edges) > 0 {
			continue
		}

		// Skip if not a map (symbols are maps with shape: class)
		if node.MapKey.Value.Map == nil {
			continue
		}

		// Parse the symbol
		symbol, kind := r.parseSymbol(keyName, node.MapKey.Value.Map)
		switch kind {
		case symbolKindInterface:
			if iface, ok := symbol.(domain.InterfaceDef); ok {
				pkg.Interfaces = append(pkg.Interfaces, iface)
			}
		case symbolKindStruct:
			if s, ok := symbol.(domain.StructDef); ok {
				pkg.Structs = append(pkg.Structs, s)
			}
		case symbolKindFunction:
			if fn, ok := symbol.(domain.FunctionDef); ok {
				pkg.Functions = append(pkg.Functions, fn)
			}
		case symbolKindTypeDef:
			if td, ok := symbol.(domain.TypeDef); ok {
				pkg.TypeDefs = append(pkg.TypeDefs, td)
			}
		}
	}
}

// derivePackagePath extracts the package path from a .arch/ file path.
// Example: "/path/to/pkg/.arch/pub.d2" -> "/path/to/pkg"
func derivePackagePath(filePath string) string {
	// Get directory containing the file
	dir := filepath.Dir(filePath)

	// If we're in .arch, go up one level
	if filepath.Base(dir) == ".arch" {
		return filepath.Dir(dir)
	}

	return dir
}

// extractPackageName extracts the package name from a path.
func extractPackageName(pkgPath string) string {
	return filepath.Base(pkgPath)
}

// parseEdge parses a D2 edge node into a domain.Dependency.
// D2 edges look like: "From -> To: label" where label is "uses", "returns", etc.
func (r *reader) parseEdge(key *d2ast.Key) *domain.Dependency {
	if len(key.Edges) == 0 {
		return nil
	}

	edge := key.Edges[0]
	if edge.Src == nil || edge.Dst == nil {
		return nil
	}

	fromStr := getKeyPathString(edge.Src)
	toStr := getKeyPathString(edge.Dst)

	// Get the dependency kind from the edge label
	kindStr := getScalarValue(key.Value)
	kind := domain.DependencyUses
	switch kindStr {
	case "returns":
		kind = domain.DependencyReturns
	case "implements":
		kind = domain.DependencyImplements
	default:
		kind = domain.DependencyUses
	}

	return &domain.Dependency{
		From:            parseSymbolRef(fromStr),
		To:              parseSymbolRef(toStr),
		Kind:            kind,
		ThroughExported: true, // pub.d2 files only contain exported dependencies
	}
}

// stripFileGroupPrefix converts a split-mode symbol ref like "dependency.Dependency"
// to a proper SymbolRef with the actual package path.
// In split-mode, the prefix (e.g., "dependency") is a file group, not a package.
func stripFileGroupPrefix(ref domain.SymbolRef, pkgPath string) domain.SymbolRef {
	// If the symbol has a package that looks like a file group (no slashes),
	// it's actually just a file prefix from split-mode, not a real package
	if ref.Package != "" && !strings.Contains(ref.Package, "/") {
		// The "package" is actually a file group prefix, use the real package path
		return domain.SymbolRef{
			Package: pkgPath,
			Symbol:  ref.Symbol,
		}
	}
	// Already has a proper package path or no package at all
	if ref.Package == "" {
		ref.Package = pkgPath
	}
	return ref
}

// parseSymbolRef parses a symbol reference string like "internal.service.Service" or "Service".
func parseSymbolRef(s string) domain.SymbolRef {
	// Handle qualified names like "internal.service.Service"
	lastDot := strings.LastIndex(s, ".")
	if lastDot == -1 {
		return domain.SymbolRef{Symbol: s}
	}

	pkg := s[:lastDot]
	symbol := s[lastDot+1:]

	// Convert D2 path format (dots) to Go package path (slashes)
	// e.g., "internal.service" -> "internal/service"
	pkg = strings.ReplaceAll(pkg, ".", "/")

	return domain.SymbolRef{
		Package: pkg,
		Symbol:  symbol,
	}
}

// extractDependencies extracts all dependency edges from a D2 map.
func (r *reader) extractDependencies(m *d2ast.Map) []domain.Dependency {
	var deps []domain.Dependency

	for _, node := range m.Nodes {
		if node.MapKey == nil {
			continue
		}

		// Only process edges
		if len(node.MapKey.Edges) == 0 {
			continue
		}

		if dep := r.parseEdge(node.MapKey); dep != nil {
			deps = append(deps, *dep)
		}
	}

	return deps
}

// parseAST extracts package models from the D2 AST.
func (r *reader) parseAST(ast *d2ast.Map) ([]domain.PackageModel, error) {
	if ast == nil || len(ast.Nodes) == 0 {
		return nil, ErrEmptyDiagram
	}

	var packages []domain.PackageModel

	for _, node := range ast.Nodes {
		if node.MapKey == nil {
			continue
		}

		key := node.MapKey

		// Skip edges for now (handled below for cross-package deps)
		if len(key.Edges) > 0 {
			continue
		}

		// Skip nodes without a key path
		if key.Key == nil || len(key.Key.Path) == 0 {
			continue
		}

		containerID := getKeyPathString(key.Key)

		// Skip the "legend" and "classes" containers
		if containerID == "legend" || containerID == "classes" {
			continue
		}

		// Only process nodes that have a map value (package containers)
		if key.Value.Map == nil {
			continue
		}

		pkg, err := r.parsePackageContainer(key.Value.Map)
		if err != nil {
			// Skip containers that aren't valid packages (might be other D2 content)
			if errors.Is(err, ErrInvalidLabel) {
				continue
			}
			return nil, fmt.Errorf("parsing package %s: %w", containerID, err)
		}

		packages = append(packages, pkg)
	}

	// Extract cross-package dependencies from root level and distribute to packages
	crossPkgDeps := r.extractDependencies(ast)
	if len(crossPkgDeps) > 0 {
		// Build a map of package paths to indices for quick lookup
		pkgIndex := make(map[string]int)
		for i, pkg := range packages {
			pkgIndex[pkg.Path] = i
		}

		// Assign each cross-package dependency to its "From" package
		for _, dep := range crossPkgDeps {
			fromPkg := dep.From.Package
			if idx, ok := pkgIndex[fromPkg]; ok {
				packages[idx].Dependencies = append(packages[idx].Dependencies, dep)
			}
		}
	}

	return packages, nil
}

// parsePackageContainer extracts a PackageModel from a D2 map container.
func (r *reader) parsePackageContainer(m *d2ast.Map) (domain.PackageModel, error) {
	pkg := domain.PackageModel{}

	// First pass: find the label to get the package path
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		if keyName == "label" {
			label := getScalarValue(node.MapKey.Value)
			if label == "" {
				return pkg, ErrInvalidLabel
			}
			pkg.Path = label
			// Extract package name from path (last segment)
			parts := strings.Split(label, "/")
			pkg.Name = parts[len(parts)-1]
			break
		}
	}

	if pkg.Path == "" {
		return pkg, ErrInvalidLabel
	}

	// Second pass: extract symbols
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		// Skip style and label properties
		if keyName == "label" || strings.HasPrefix(keyName, "style.") {
			continue
		}

		// Skip edges (dependencies) inside the package
		if len(node.MapKey.Edges) > 0 {
			continue
		}

		// Skip if not a map (symbol containers are maps)
		if node.MapKey.Value.Map == nil {
			continue
		}

		// Parse the symbol
		symbol, kind := r.parseSymbol(keyName, node.MapKey.Value.Map)
		switch kind {
		case symbolKindInterface:
			if iface, ok := symbol.(domain.InterfaceDef); ok {
				pkg.Interfaces = append(pkg.Interfaces, iface)
			}
		case symbolKindStruct:
			if s, ok := symbol.(domain.StructDef); ok {
				pkg.Structs = append(pkg.Structs, s)
			}
		case symbolKindFunction:
			if fn, ok := symbol.(domain.FunctionDef); ok {
				pkg.Functions = append(pkg.Functions, fn)
			}
		case symbolKindTypeDef:
			if td, ok := symbol.(domain.TypeDef); ok {
				pkg.TypeDefs = append(pkg.TypeDefs, td)
			}
		}
	}

	// Extract intra-package dependencies
	pkg.Dependencies = r.extractDependencies(m)

	return pkg, nil
}

// symbolKind identifies the type of symbol parsed from D2.
type symbolKind int

const (
	symbolKindUnknown symbolKind = iota
	symbolKindInterface
	symbolKindStruct
	symbolKindFunction
	symbolKindTypeDef
)

// parseSymbol extracts a domain symbol from a D2 class shape.
func (r *reader) parseSymbol(name string, m *d2ast.Map) (any, symbolKind) {
	// Extract shape and stereotype
	shape := ""
	stereotype := ""

	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		switch keyName {
		case "shape":
			shape = getScalarValue(node.MapKey.Value)
		case "stereotype":
			stereotype = getScalarValue(node.MapKey.Value)
		}
	}

	// Only process class shapes
	if shape != "class" {
		return nil, symbolKindUnknown
	}

	// Determine the symbol type from stereotype
	switch stereotype {
	case "<<interface>>":
		return r.parseInterface(name, m), symbolKindInterface
	case "<<struct>>":
		return r.parseStruct(name, m), symbolKindStruct
	case "<<factory>>", "<<function>>":
		return r.parseFunction(name, m, stereotype), symbolKindFunction
	case "<<enum>>":
		return r.parseTypeDef(name, m, domain.StereotypeEnum), symbolKindTypeDef
	default:
		// If stereotype contains known DDD stereotypes, treat accordingly
		if strings.Contains(stereotype, "<<") {
			// Could be a custom stereotype like <<aggregate>>, <<entity>>, etc.
			// Default to struct for unknown stereotypes
			return r.parseStruct(name, m), symbolKindStruct
		}
		return nil, symbolKindUnknown
	}
}

// parseInterface extracts an InterfaceDef from a D2 class shape.
func (r *reader) parseInterface(name string, m *d2ast.Map) domain.InterfaceDef {
	iface := domain.InterfaceDef{
		Name:       name,
		IsExported: isExported(name),
	}

	// Parse methods
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		// Skip shape, stereotype, and style properties
		if keyName == "shape" || keyName == "stereotype" || strings.HasPrefix(keyName, "style.") {
			continue
		}

		// Methods are like "+MethodName(params)": "returns"
		if strings.HasPrefix(keyName, "+") || strings.HasPrefix(keyName, "-") {
			method := r.parseMethod(keyName, getScalarValue(node.MapKey.Value))
			iface.Methods = append(iface.Methods, method)
		}
	}

	return iface
}

// parseStruct extracts a StructDef from a D2 class shape.
func (r *reader) parseStruct(name string, m *d2ast.Map) domain.StructDef {
	s := domain.StructDef{
		Name:       name,
		IsExported: isExported(name),
	}

	// Parse fields and methods
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		// Skip shape, stereotype, and style properties
		if keyName == "shape" || keyName == "stereotype" || strings.HasPrefix(keyName, "style.") {
			continue
		}

		// Determine if this is a field or method based on format
		// Methods have parentheses: "+Method(params)"
		// Fields look like: "+FieldName Type"
		if strings.Contains(keyName, "(") {
			// This is a method
			method := r.parseMethod(keyName, getScalarValue(node.MapKey.Value))
			s.Methods = append(s.Methods, method)
		} else if strings.HasPrefix(keyName, "+") || strings.HasPrefix(keyName, "-") {
			// This is a field
			field := r.parseField(keyName)
			s.Fields = append(s.Fields, field)
		}
	}

	return s
}

// parseFunction extracts a FunctionDef from a D2 class shape.
func (r *reader) parseFunction(name string, m *d2ast.Map, stereotype string) domain.FunctionDef {
	fn := domain.FunctionDef{
		Name:       name,
		IsExported: isExported(name),
	}

	// Set stereotype based on D2 stereotype
	if stereotype == "<<factory>>" {
		fn.Stereotype = domain.StereotypeFactory
	}

	// Parse parameters and return type
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		// Skip shape, stereotype, and style properties
		if keyName == "shape" || keyName == "stereotype" || strings.HasPrefix(keyName, "style.") {
			continue
		}

		value := getScalarValue(node.MapKey.Value)

		if keyName == "return" {
			// Parse return types
			fn.Returns = parseReturnTypes(value)
		} else {
			// This is a parameter: "paramName": "Type"
			param := domain.ParamDef{
				Name: keyName,
				Type: parseTypeRef(value),
			}
			fn.Params = append(fn.Params, param)
		}
	}

	return fn
}

// parseTypeDef extracts a TypeDef from a D2 class shape.
func (r *reader) parseTypeDef(name string, m *d2ast.Map, stereotype domain.Stereotype) domain.TypeDef {
	td := domain.TypeDef{
		Name:       name,
		IsExported: isExported(name),
		Stereotype: stereotype,
	}

	// Parse underlying type and constants
	for _, node := range m.Nodes {
		if node.MapKey == nil || node.MapKey.Key == nil {
			continue
		}

		keyName := getKeyPathString(node.MapKey.Key)

		// Skip shape, stereotype, and style properties
		if keyName == "shape" || keyName == "stereotype" || strings.HasPrefix(keyName, "style.") {
			continue
		}

		value := getScalarValue(node.MapKey.Value)

		if keyName == "type" {
			// This is the underlying type
			td.UnderlyingType = parseTypeRef(value)
		} else if value == "const" {
			// This is a constant
			td.Constants = append(td.Constants, keyName)
		}
	}

	return td
}

// parseMethod parses a method signature from D2 format.
// Input: "+MethodName(param1 Type1, param2 Type2)" and "ReturnType"
func (r *reader) parseMethod(signature, returnStr string) domain.MethodDef {
	m := domain.MethodDef{}

	// Extract visibility
	if strings.HasPrefix(signature, "+") {
		m.IsExported = true
		signature = signature[1:]
	} else if strings.HasPrefix(signature, "-") {
		m.IsExported = false
		signature = signature[1:]
	}

	// Parse method name and parameters
	parenIdx := strings.Index(signature, "(")
	if parenIdx == -1 {
		m.Name = signature
		return m
	}

	m.Name = signature[:parenIdx]

	// Extract parameter string
	closeIdx := strings.LastIndex(signature, ")")
	if closeIdx == -1 || closeIdx <= parenIdx {
		return m
	}

	paramStr := signature[parenIdx+1 : closeIdx]
	if paramStr != "" {
		m.Params = parseParams(paramStr)
	}

	// Parse return types
	if returnStr != "" {
		m.Returns = parseReturnTypes(returnStr)
	}

	return m
}

// parseField parses a field from D2 format.
// Input: "+FieldName Type" or "-fieldName Type"
func (r *reader) parseField(fieldStr string) domain.FieldDef {
	f := domain.FieldDef{}

	// Extract visibility
	if strings.HasPrefix(fieldStr, "+") {
		f.IsExported = true
		fieldStr = fieldStr[1:]
	} else if strings.HasPrefix(fieldStr, "-") {
		f.IsExported = false
		fieldStr = fieldStr[1:]
	}

	// Split name and type by first space
	parts := strings.SplitN(fieldStr, " ", 2)
	f.Name = parts[0]
	if len(parts) > 1 {
		f.Type = parseTypeRef(parts[1])
	}

	return f
}

// parseParams parses a comma-separated parameter list.
// Input: "param1 Type1, param2 Type2"
func parseParams(paramStr string) []domain.ParamDef {
	var params []domain.ParamDef

	// Split by comma, handling nested types
	parts := splitParams(paramStr)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split by last space to separate name from type
		spaceIdx := strings.LastIndex(part, " ")
		if spaceIdx == -1 {
			// Type only, no name
			params = append(params, domain.ParamDef{
				Type: parseTypeRef(part),
			})
		} else {
			name := part[:spaceIdx]
			typeStr := part[spaceIdx+1:]
			params = append(params, domain.ParamDef{
				Name: name,
				Type: parseTypeRef(typeStr),
			})
		}
	}

	return params
}

// splitParams splits a parameter string by comma, respecting nested types.
func splitParams(s string) []string {
	var result []string
	var current strings.Builder
	depth := 0

	for _, r := range s {
		switch r {
		case '(':
			depth++
			current.WriteRune(r)
		case ')':
			depth--
			current.WriteRune(r)
		case '[':
			depth++
			current.WriteRune(r)
		case ']':
			depth--
			current.WriteRune(r)
		case ',':
			if depth == 0 {
				result = append(result, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// parseReturnTypes parses return type(s) from D2 format.
// Input: "Type" or "(Type1, Type2)"
func parseReturnTypes(returnStr string) []domain.TypeRef {
	returnStr = strings.TrimSpace(returnStr)
	if returnStr == "" {
		return nil
	}

	// Check for multiple returns (wrapped in parentheses)
	if strings.HasPrefix(returnStr, "(") && strings.HasSuffix(returnStr, ")") {
		inner := returnStr[1 : len(returnStr)-1]
		parts := splitParams(inner)
		var returns []domain.TypeRef
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				returns = append(returns, parseTypeRef(part))
			}
		}
		return returns
	}

	// Single return type
	return []domain.TypeRef{parseTypeRef(returnStr)}
}

// parseTypeRef parses a type reference from string format.
// Handles: Type, *Type, []Type, map[K]V, package.Type
func parseTypeRef(typeStr string) domain.TypeRef {
	typeStr = strings.TrimSpace(typeStr)
	ref := domain.TypeRef{}

	// Handle slice
	if strings.HasPrefix(typeStr, "[]") {
		ref.IsSlice = true
		typeStr = typeStr[2:]
	}

	// Handle map
	if strings.HasPrefix(typeStr, "map[") {
		ref.IsMap = true
		// Find the closing bracket
		depth := 1
		closeIdx := -1
		for i := 4; i < len(typeStr); i++ {
			if typeStr[i] == '[' {
				depth++
			} else if typeStr[i] == ']' {
				depth--
				if depth == 0 {
					closeIdx = i
					break
				}
			}
		}
		if closeIdx > 4 {
			keyType := parseTypeRef(typeStr[4:closeIdx])
			ref.KeyType = &keyType
			if closeIdx+1 < len(typeStr) {
				valueType := parseTypeRef(typeStr[closeIdx+1:])
				ref.ValueType = &valueType
			}
		}
		ref.Name = "map"
		return ref
	}

	// Handle pointer
	if strings.HasPrefix(typeStr, "*") {
		ref.IsPointer = true
		typeStr = typeStr[1:]
	}

	// Handle package.Type
	lastDot := strings.LastIndex(typeStr, ".")
	if lastDot != -1 {
		ref.Package = typeStr[:lastDot]
		ref.Name = typeStr[lastDot+1:]
	} else {
		ref.Name = typeStr
	}

	return ref
}

// getKeyPathString extracts the string representation of a KeyPath.
func getKeyPathString(kp *d2ast.KeyPath) string {
	if kp == nil || len(kp.Path) == 0 {
		return ""
	}

	// Use the full path joined by dots
	var parts []string
	for _, sb := range kp.Path {
		parts = append(parts, sb.ScalarString())
	}
	return strings.Join(parts, ".")
}

// getScalarValue extracts a string value from a ValueBox.
func getScalarValue(vb d2ast.ValueBox) string {
	if sb := vb.StringBox(); sb != nil {
		return sb.ScalarString()
	}
	return ""
}

// isExported returns true if the name starts with an uppercase letter.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)[0]
	return unicode.IsUpper(r)
}
