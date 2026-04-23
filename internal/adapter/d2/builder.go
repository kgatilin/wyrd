package d2

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// d2TextBuilder builds D2 diagram text content from domain models.
type d2TextBuilder struct {
	buf    strings.Builder
	indent int
}

// newD2TextBuilder creates a new D2 text builder.
func newD2TextBuilder() *d2TextBuilder {
	return &d2TextBuilder{}
}

// Build generates D2 diagram content from a package model.
func (b *d2TextBuilder) Build(pkg domain.PackageModel, publicOnly bool) string {
	b.buf.Reset()
	b.indent = 0

	// 1. Write header comment
	b.writeComment(fmt.Sprintf("%s package", pkg.Name))
	b.writeLine("")

	// 2. Write reusable style classes
	b.writeComment("Style classes")
	b.writeRaw(classesTemplate())
	b.writeLine("")

	// 3. Write legend
	b.writeComment("Legend")
	b.writeRaw(legendTemplate())
	b.writeLine("")
	b.writeLine("")

	// 4. Group symbols by file and write containers
	b.writeComment("Files")
	fileGroups := b.groupByFile(pkg, publicOnly)
	for _, fg := range fileGroups {
		b.writeFileContainer(fg, publicOnly)
		b.writeLine("")
	}

	// 5. Write dependencies
	b.writeComment("Dependencies")
	deps := b.filterDependencies(pkg.Dependencies, publicOnly, pkg)
	for _, dep := range deps {
		b.writeDependency(dep)
	}

	return b.buf.String()
}

// fileGroup represents symbols grouped by source file.
type fileGroup struct {
	filename   string
	interfaces []domain.InterfaceDef
	structs    []domain.StructDef
	functions  []domain.FunctionDef
	typeDefs   []domain.TypeDef
	constants  []domain.ConstDef
	variables  []domain.VarDef
	errors     []domain.ErrorDef
}

// groupByFile groups package symbols by their source file.
func (b *d2TextBuilder) groupByFile(pkg domain.PackageModel, publicOnly bool) []fileGroup {
	groups := make(map[string]*fileGroup)

	// Helper to get or create a file group
	getGroup := func(filename string) *fileGroup {
		if fg, ok := groups[filename]; ok {
			return fg
		}
		fg := &fileGroup{filename: filename}
		groups[filename] = fg
		return fg
	}

	// Group interfaces
	for _, iface := range pkg.Interfaces {
		if publicOnly && !iface.IsExported {
			continue
		}
		fg := getGroup(iface.SourceFile)
		fg.interfaces = append(fg.interfaces, iface)
	}

	// Group structs
	for _, s := range pkg.Structs {
		if publicOnly && !s.IsExported {
			continue
		}
		fg := getGroup(s.SourceFile)
		fg.structs = append(fg.structs, s)
	}

	// Group functions
	for _, fn := range pkg.Functions {
		if publicOnly && !fn.IsExported {
			continue
		}
		fg := getGroup(fn.SourceFile)
		fg.functions = append(fg.functions, fn)
	}

	// Group type definitions
	for _, td := range pkg.TypeDefs {
		if publicOnly && !td.IsExported {
			continue
		}
		fg := getGroup(td.SourceFile)
		fg.typeDefs = append(fg.typeDefs, td)
	}

	// Group constants
	for _, c := range pkg.Constants {
		if publicOnly && !c.IsExported {
			continue
		}
		fg := getGroup(c.SourceFile)
		fg.constants = append(fg.constants, c)
	}

	// Group variables
	for _, v := range pkg.Variables {
		if publicOnly && !v.IsExported {
			continue
		}
		fg := getGroup(v.SourceFile)
		fg.variables = append(fg.variables, v)
	}

	// Group errors
	for _, e := range pkg.Errors {
		if publicOnly && !e.IsExported {
			continue
		}
		fg := getGroup(e.SourceFile)
		fg.errors = append(fg.errors, e)
	}

	// Convert map to sorted slice
	var result []fileGroup
	for _, fg := range groups {
		// Skip empty groups
		if len(fg.interfaces) == 0 && len(fg.structs) == 0 &&
			len(fg.functions) == 0 && len(fg.typeDefs) == 0 &&
			len(fg.constants) == 0 && len(fg.variables) == 0 && len(fg.errors) == 0 {
			continue
		}
		result = append(result, *fg)
	}

	// Sort by filename for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].filename < result[j].filename
	})

	return result
}

// writeFileContainer writes a D2 container for a source file.
func (b *d2TextBuilder) writeFileContainer(fg fileGroup, publicOnly bool) {
	// Calculate file container class from dominant stereotype
	symbols := b.collectSymbolInfo(fg)
	class := fileContainerClass(symbols)

	// Container ID is filename without extension
	containerID := b.fileContainerID(fg.filename)

	b.writeLine(fmt.Sprintf("%s: {", containerID))
	b.indent++

	b.writeLine(fmt.Sprintf(`label: "%s"`, fg.filename))
	b.writeLine(fmt.Sprintf(`class: %s`, class))
	b.writeLine("")

	// Write interfaces
	for _, iface := range fg.interfaces {
		b.writeInterface(iface, publicOnly)
		b.writeLine("")
	}

	// Write structs
	for _, s := range fg.structs {
		b.writeStruct(s, publicOnly)
		b.writeLine("")
	}

	// Write functions (as a special "Functions" class if any)
	if len(fg.functions) > 0 {
		b.writeFunctions(fg.functions, publicOnly)
		b.writeLine("")
	}

	// Write type definitions
	for _, td := range fg.typeDefs {
		b.writeTypeDef(td)
		b.writeLine("")
	}

	// Write constants container (if any)
	if len(fg.constants) > 0 {
		b.writeConstantsBlock(fg.constants)
		b.writeLine("")
	}

	// Write variables container (if any)
	if len(fg.variables) > 0 {
		b.writeVariablesBlock(fg.variables)
		b.writeLine("")
	}

	// Write errors container (if any)
	if len(fg.errors) > 0 {
		b.writeErrorsBlock(fg.errors)
		b.writeLine("")
	}

	b.indent--
	b.writeLine("}")
}

// writeConstantsBlock renders a D2 class grouping all standalone constants
// defined in the same source file.
func (b *d2TextBuilder) writeConstantsBlock(consts []domain.ConstDef) {
	b.writeLine("Constants: {")
	b.indent++
	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<const>>"`)
	b.writeLine("")
	for _, c := range consts {
		label := c.Type.String()
		if label == "" {
			label = "const"
		}
		if c.Value != "" {
			label = fmt.Sprintf("%s = %s", label, c.Value)
		}
		b.writeLine(fmt.Sprintf(`"%s": "%s"`, c.Name, escapeD2(label)))
	}
	b.indent--
	b.writeLine("}")
}

// writeVariablesBlock renders a D2 class grouping all package-level variables
// defined in the same source file.
func (b *d2TextBuilder) writeVariablesBlock(vars []domain.VarDef) {
	b.writeLine("Variables: {")
	b.indent++
	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<var>>"`)
	b.writeLine("")
	for _, v := range vars {
		label := v.Type.String()
		if label == "" {
			label = "var"
		}
		b.writeLine(fmt.Sprintf(`"%s": "%s"`, v.Name, escapeD2(label)))
	}
	b.indent--
	b.writeLine("}")
}

// writeErrorsBlock renders a D2 class grouping sentinel error variables
// defined in the same source file.
func (b *d2TextBuilder) writeErrorsBlock(errs []domain.ErrorDef) {
	b.writeLine("Errors: {")
	b.indent++
	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<error>>"`)
	b.writeLine("")
	for _, e := range errs {
		label := e.Message
		if label == "" {
			label = "error"
		}
		b.writeLine(fmt.Sprintf(`"%s": "%s"`, e.Name, escapeD2(label)))
	}
	b.indent--
	b.writeLine("}")
}

// escapeD2 escapes characters in a string so it can safely appear inside
// a double-quoted D2 label.
func escapeD2(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// collectSymbolInfo collects stereotype information from all symbols in a file group.
func (b *d2TextBuilder) collectSymbolInfo(fg fileGroup) []symbolInfo {
	var symbols []symbolInfo

	for _, iface := range fg.interfaces {
		symbols = append(symbols, symbolInfo{stereotype: iface.Stereotype})
	}
	for _, s := range fg.structs {
		symbols = append(symbols, symbolInfo{stereotype: s.Stereotype})
	}
	for _, fn := range fg.functions {
		symbols = append(symbols, symbolInfo{stereotype: fn.Stereotype})
	}
	for _, td := range fg.typeDefs {
		symbols = append(symbols, symbolInfo{stereotype: td.Stereotype})
	}

	return symbols
}

// fileContainerID returns the D2 container ID for a filename.
// It strips the .go extension. If filename is empty (e.g., from combined diagrams),
// returns "symbols" as a fallback container name.
func (b *d2TextBuilder) fileContainerID(filename string) string {
	if filename == "" {
		return "symbols"
	}
	return strings.TrimSuffix(filename, ".go")
}

// writeInterface writes a D2 class shape for an interface.
func (b *d2TextBuilder) writeInterface(iface domain.InterfaceDef, publicOnly bool) {
	b.writeLine(fmt.Sprintf("%s: {", iface.Name))
	b.indent++

	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<interface>>"`)


	// Write methods
	if len(iface.Methods) > 0 {
		b.writeLine("")
		for _, m := range iface.Methods {
			if publicOnly && !m.IsExported {
				continue
			}
			b.writeMethod(m)
		}
	}

	b.indent--
	b.writeLine("}")
}

// writeStruct writes a D2 class shape for a struct.
func (b *d2TextBuilder) writeStruct(s domain.StructDef, publicOnly bool) {
	b.writeLine(fmt.Sprintf("%s: {", s.Name))
	b.indent++

	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<struct>>"`)


	// Write fields
	hasVisibleFields := false
	for _, f := range s.Fields {
		if publicOnly && !f.IsExported {
			continue
		}
		hasVisibleFields = true
	}

	if hasVisibleFields {
		b.writeLine("")
		for _, f := range s.Fields {
			if publicOnly && !f.IsExported {
				continue
			}
			b.writeField(f)
		}
	}

	// Write methods
	hasVisibleMethods := false
	for _, m := range s.Methods {
		if publicOnly && !m.IsExported {
			continue
		}
		hasVisibleMethods = true
	}

	if hasVisibleMethods {
		b.writeLine("")
		for _, m := range s.Methods {
			if publicOnly && !m.IsExported {
				continue
			}
			b.writeMethod(m)
		}
	}

	b.indent--
	b.writeLine("}")
}

// writeFunctions writes each function as its own D2 class shape.
// Each function gets its own container with parameters as fields and return as a special field.
func (b *d2TextBuilder) writeFunctions(functions []domain.FunctionDef, publicOnly bool) {
	for _, fn := range functions {
		if publicOnly && !fn.IsExported {
			continue
		}
		b.writeFunctionAsClass(fn)
		b.writeLine("")
	}
}

// writeFunctionAsClass writes a function as its own D2 class shape.
// Parameters are rendered as fields, and return types as a special "return" field.
func (b *d2TextBuilder) writeFunctionAsClass(fn domain.FunctionDef) {
	b.writeLine(fmt.Sprintf("%s: {", fn.Name))
	b.indent++

	b.writeLine("shape: class")

	// Write stereotype - use <<factory>> if detected, otherwise <<function>>
	if fn.Stereotype == domain.StereotypeFactory {
		b.writeLine(`stereotype: "<<factory>>"`)
	} else {
		b.writeLine(`stereotype: "<<function>>"`)
	}

	// Write parameters as fields
	if len(fn.Params) > 0 || len(fn.Returns) > 0 {
		b.writeLine("")
	}

	for _, p := range fn.Params {
		// Format: "paramName": "Type"
		b.writeLine(fmt.Sprintf(`"%s": "%s"`, p.Name, p.Type.String()))
	}

	// Write return type as special "return" field
	if len(fn.Returns) > 0 {
		ret := b.formatReturns(fn.Returns)
		b.writeLine(fmt.Sprintf(`"return": "%s"`, ret))
	}

	b.indent--
	b.writeLine("}")
}

// writeTypeDef writes a D2 class shape for a type definition.
func (b *d2TextBuilder) writeTypeDef(td domain.TypeDef) {
	b.writeLine(fmt.Sprintf("%s: {", td.Name))
	b.indent++

	b.writeLine("shape: class")

	label := stereotypeLabel(td.Stereotype)
	if label != "" {
		b.writeLine(fmt.Sprintf(`stereotype: "%s"`, label))
	}

	// Write underlying type as a field
	b.writeLine("")
	b.writeLine(fmt.Sprintf(`"type": "%s"`, td.UnderlyingType.String()))

	// Write constants if it's an enum
	if len(td.Constants) > 0 {
		b.writeLine("")
		for _, c := range td.Constants {
			b.writeLine(fmt.Sprintf(`"%s": "const"`, c))
		}
	}

	b.indent--
	b.writeLine("}")
}

// writeMethod writes a D2 class method entry.
func (b *d2TextBuilder) writeMethod(m domain.MethodDef) {
	sig := b.formatMethodSignature(m)
	ret := b.formatReturns(m.Returns)
	b.writeLine(fmt.Sprintf(`"%s": "%s"`, sig, ret))
}

// writeField writes a D2 class field entry.
func (b *d2TextBuilder) writeField(f domain.FieldDef) {
	fieldName := f.StringWithVisibility()
	// Escape the field name for D2
	fieldName = strings.ReplaceAll(fieldName, `"`, `\"`)
	b.writeLine(fmt.Sprintf(`"%s": ""`, fieldName))
}

// formatMethodSignature formats a method signature for D2.
// Format: "+MethodName(param1 Type1, param2 Type2)"
func (b *d2TextBuilder) formatMethodSignature(m domain.MethodDef) string {
	prefix := "-"
	if m.IsExported {
		prefix = "+"
	}

	params := b.formatParams(m.Params)
	return fmt.Sprintf("%s%s(%s)", prefix, m.Name, params)
}

// formatParams formats parameter list for display.
func (b *d2TextBuilder) formatParams(params []domain.ParamDef) string {
	var parts []string
	for _, p := range params {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ", ")
}

// formatReturns formats return types for D2 display.
func (b *d2TextBuilder) formatReturns(returns []domain.TypeRef) string {
	if len(returns) == 0 {
		return ""
	}

	var parts []string
	for _, r := range returns {
		parts = append(parts, r.String())
	}

	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// filterDependencies filters dependencies based on visibility and returns
// only intra-package dependencies (within the same package).
func (b *d2TextBuilder) filterDependencies(
	deps []domain.Dependency,
	publicOnly bool,
	pkg domain.PackageModel,
) []domain.Dependency {
	// Build a map of symbol name -> source file for lookup
	symbolFiles := make(map[string]string)

	// Build a set of visible symbols (all types including functions)
	// Functions are now rendered as their own class shapes, so they can be arrow endpoints.
	visibleSymbols := make(map[string]bool)

	for _, iface := range pkg.Interfaces {
		symbolFiles[iface.Name] = iface.SourceFile
		if !publicOnly || iface.IsExported {
			visibleSymbols[iface.Name] = true
		}
	}
	for _, s := range pkg.Structs {
		symbolFiles[s.Name] = s.SourceFile
		if !publicOnly || s.IsExported {
			visibleSymbols[s.Name] = true
		}
	}
	for _, fn := range pkg.Functions {
		symbolFiles[fn.Name] = fn.SourceFile
		if !publicOnly || fn.IsExported {
			visibleSymbols[fn.Name] = true
		}
	}
	for _, td := range pkg.TypeDefs {
		symbolFiles[td.Name] = td.SourceFile
		if !publicOnly || td.IsExported {
			visibleSymbols[td.Name] = true
		}
	}

	var filtered []domain.Dependency
	for _, dep := range deps {
		// Skip external dependencies
		if dep.To.External {
			continue
		}

		// Only include dependencies where both From and To are in this package
		if dep.From.Package != pkg.Path || dep.To.Package != pkg.Path {
			continue
		}

		// Skip dependencies to/from non-visible symbols
		if !visibleSymbols[dep.From.Symbol] || !visibleSymbols[dep.To.Symbol] {
			continue
		}

		// Fix up empty file references using our symbol->file map
		if dep.From.File == "" {
			if file, ok := symbolFiles[dep.From.Symbol]; ok {
				dep.From.File = file
			}
		}
		if dep.To.File == "" {
			if file, ok := symbolFiles[dep.To.Symbol]; ok {
				dep.To.File = file
			}
		}

		// Skip if we still can't determine the file (shouldn't happen normally)
		if dep.From.File == "" || dep.To.File == "" {
			continue
		}

		// Skip self-references
		if dep.From.Symbol == dep.To.Symbol && dep.From.File == dep.To.File {
			continue
		}

		filtered = append(filtered, dep)
	}

	// Deduplicate dependencies
	seen := make(map[string]bool)
	var unique []domain.Dependency
	for _, dep := range filtered {
		key := fmt.Sprintf("%s.%s->%s.%s",
			b.fileContainerID(dep.From.File), dep.From.Symbol,
			b.fileContainerID(dep.To.File), dep.To.Symbol)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, dep)
		}
	}

	return unique
}

// writeDependency writes a D2 dependency arrow.
func (b *d2TextBuilder) writeDependency(dep domain.Dependency) {
	fromPath := b.toD2Path(dep.From)
	toPath := b.toD2Path(dep.To)
	b.writeLine(fmt.Sprintf(`%s -> %s: "%s"`, fromPath, toPath, dep.Kind))
}

// toD2Path converts a SymbolRef to a D2 container path.
func (b *d2TextBuilder) toD2Path(ref domain.SymbolRef) string {
	if ref.External {
		return ref.Package + "." + ref.Symbol
	}
	fileID := b.fileContainerID(ref.File)
	return fileID + "." + ref.Symbol
}

// writeComment writes a D2 comment line.
func (b *d2TextBuilder) writeComment(text string) {
	b.writeLine("# " + text)
}

// writeLine writes an indented line to the buffer.
func (b *d2TextBuilder) writeLine(line string) {
	if line == "" {
		b.buf.WriteString("\n")
		return
	}

	// Write indent
	for i := 0; i < b.indent; i++ {
		b.buf.WriteString("  ")
	}

	b.buf.WriteString(line)
	b.buf.WriteString("\n")
}

// writeRaw writes raw text without indentation.
func (b *d2TextBuilder) writeRaw(text string) {
	b.buf.WriteString(text)
}
