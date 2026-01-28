package d2

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// combinedBuilder builds D2 diagram content from multiple packages.
// Unlike d2TextBuilder (which groups by file within a single package),
// combinedBuilder creates package-level containers and shows cross-package dependencies.
type combinedBuilder struct {
	buf    strings.Builder
	indent int
}

// newCombinedBuilder creates a new combined builder.
func newCombinedBuilder() *combinedBuilder {
	return &combinedBuilder{}
}

// Build generates D2 diagram content from multiple packages.
// It creates package-level containers with exported symbols only,
// and renders both intra-package and cross-package dependencies.
func (b *combinedBuilder) Build(packages []domain.PackageModel) string {
	b.buf.Reset()
	b.indent = 0

	// 1. Write header comment
	b.writeComment("Combined Architecture Diagram")
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

	// 4. Sort packages for deterministic output
	sortedPackages := make([]domain.PackageModel, len(packages))
	copy(sortedPackages, packages)
	sort.Slice(sortedPackages, func(i, j int) bool {
		return sortedPackages[i].Path < sortedPackages[j].Path
	})

	// 5. Build symbol index for dependency filtering
	symbolIndex := b.buildSymbolIndex(sortedPackages)

	// 6. Write package containers (including intra-package dependencies)
	b.writeComment("Packages")
	for _, pkg := range sortedPackages {
		b.writePackageContainer(pkg, symbolIndex)
		b.writeLine("")
	}

	// 7. Write cross-package dependencies
	crossDeps := b.collectCrossPackageDeps(sortedPackages, symbolIndex)
	if len(crossDeps) > 0 {
		b.writeComment("Cross-package dependencies")
		for _, dep := range crossDeps {
			b.writeCrossPackageDep(dep)
		}
	}

	return b.buf.String()
}

// writePackageContainer writes a D2 container for a package with all its exported symbols.
func (b *combinedBuilder) writePackageContainer(pkg domain.PackageModel, symbolIndex map[string]map[string]bool) {
	pkgID := sanitizePackageID(pkg.Path)

	// Calculate container class from dominant stereotype
	symbols := b.collectSymbolInfo(pkg)
	class := fileContainerClass(symbols)

	b.writeLine(fmt.Sprintf("%s: {", pkgID))
	b.indent++

	// Use package path as label for clarity
	label := pkg.Path
	if label == "" || label == "." {
		label = pkg.Name
	}
	b.writeLine(fmt.Sprintf(`label: "%s"`, label))
	b.writeLine(fmt.Sprintf(`class: %s`, class))
	b.writeLine("")

	// Write exported interfaces
	for _, iface := range pkg.ExportedInterfaces() {
		b.writeInterface(iface)
		b.writeLine("")
	}

	// Write exported structs
	for _, s := range pkg.ExportedStructs() {
		b.writeStruct(s)
		b.writeLine("")
	}

	// Write exported functions
	for _, fn := range pkg.ExportedFunctions() {
		b.writeFunction(fn)
		b.writeLine("")
	}

	// Write exported type definitions
	for _, td := range pkg.ExportedTypeDefs() {
		b.writeTypeDef(td)
		b.writeLine("")
	}

	// Write intra-package dependencies
	intraDeps := b.collectIntraPackageDeps(pkg, symbolIndex)
	if len(intraDeps) > 0 {
		b.writeComment("Dependencies")
		for _, dep := range intraDeps {
			b.writeIntraPackageDep(dep)
		}
		b.writeLine("")
	}

	b.indent--
	b.writeLine("}")
}

// collectSymbolInfo collects stereotype information from all exported symbols in a package.
func (b *combinedBuilder) collectSymbolInfo(pkg domain.PackageModel) []symbolInfo {
	var symbols []symbolInfo

	for _, iface := range pkg.ExportedInterfaces() {
		symbols = append(symbols, symbolInfo{stereotype: iface.Stereotype})
	}
	for _, s := range pkg.ExportedStructs() {
		symbols = append(symbols, symbolInfo{stereotype: s.Stereotype})
	}
	for _, fn := range pkg.ExportedFunctions() {
		symbols = append(symbols, symbolInfo{stereotype: fn.Stereotype})
	}
	for _, td := range pkg.ExportedTypeDefs() {
		symbols = append(symbols, symbolInfo{stereotype: td.Stereotype})
	}

	return symbols
}

// writeInterface writes a D2 class shape for an exported interface.
func (b *combinedBuilder) writeInterface(iface domain.InterfaceDef) {
	b.writeLine(fmt.Sprintf("%s: {", iface.Name))
	b.indent++

	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<interface>>"`)

	// Write exported methods only
	exportedMethods := b.filterExportedMethods(iface.Methods)
	if len(exportedMethods) > 0 {
		b.writeLine("")
		for _, m := range exportedMethods {
			b.writeMethod(m)
		}
	}

	b.indent--
	b.writeLine("}")
}

// writeStruct writes a D2 class shape for an exported struct.
func (b *combinedBuilder) writeStruct(s domain.StructDef) {
	b.writeLine(fmt.Sprintf("%s: {", s.Name))
	b.indent++

	b.writeLine("shape: class")
	b.writeLine(`stereotype: "<<struct>>"`)

	// Write exported fields
	exportedFields := b.filterExportedFields(s.Fields)
	if len(exportedFields) > 0 {
		b.writeLine("")
		for _, f := range exportedFields {
			b.writeField(f)
		}
	}

	// Write exported methods
	exportedMethods := b.filterExportedMethods(s.Methods)
	if len(exportedMethods) > 0 {
		b.writeLine("")
		for _, m := range exportedMethods {
			b.writeMethod(m)
		}
	}

	b.indent--
	b.writeLine("}")
}

// writeFunction writes a D2 class shape for an exported function.
func (b *combinedBuilder) writeFunction(fn domain.FunctionDef) {
	b.writeLine(fmt.Sprintf("%s: {", fn.Name))
	b.indent++

	b.writeLine("shape: class")

	// Write stereotype
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

// writeTypeDef writes a D2 class shape for an exported type definition.
func (b *combinedBuilder) writeTypeDef(td domain.TypeDef) {
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
func (b *combinedBuilder) writeMethod(m domain.MethodDef) {
	sig := b.formatMethodSignature(m)
	ret := b.formatReturns(m.Returns)
	b.writeLine(fmt.Sprintf(`"%s": "%s"`, sig, ret))
}

// writeField writes a D2 class field entry.
func (b *combinedBuilder) writeField(f domain.FieldDef) {
	fieldName := f.StringWithVisibility()
	fieldName = strings.ReplaceAll(fieldName, `"`, `\"`)
	b.writeLine(fmt.Sprintf(`"%s": ""`, fieldName))
}

// formatMethodSignature formats a method signature for D2.
func (b *combinedBuilder) formatMethodSignature(m domain.MethodDef) string {
	prefix := "-"
	if m.IsExported {
		prefix = "+"
	}

	params := b.formatParams(m.Params)
	return fmt.Sprintf("%s%s(%s)", prefix, m.Name, params)
}

// formatParams formats parameter list for display.
func (b *combinedBuilder) formatParams(params []domain.ParamDef) string {
	var parts []string
	for _, p := range params {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ", ")
}

// formatReturns formats return types for D2 display.
func (b *combinedBuilder) formatReturns(returns []domain.TypeRef) string {
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

// filterExportedMethods returns only exported methods.
func (b *combinedBuilder) filterExportedMethods(methods []domain.MethodDef) []domain.MethodDef {
	var result []domain.MethodDef
	for _, m := range methods {
		if m.IsExported {
			result = append(result, m)
		}
	}
	return result
}

// filterExportedFields returns only exported fields.
func (b *combinedBuilder) filterExportedFields(fields []domain.FieldDef) []domain.FieldDef {
	var result []domain.FieldDef
	for _, f := range fields {
		if f.IsExported {
			result = append(result, f)
		}
	}
	return result
}

// depInfo represents a dependency between symbols.
type depInfo struct {
	fromPkg    string
	fromSymbol string
	toPkg      string
	toSymbol   string
	kind       domain.DependencyKind
}

// buildSymbolIndex builds an index of visible (exported) symbols per package.
func (b *combinedBuilder) buildSymbolIndex(packages []domain.PackageModel) map[string]map[string]bool {
	symbolIndex := make(map[string]map[string]bool)
	for _, pkg := range packages {
		symbols := make(map[string]bool)
		for _, iface := range pkg.ExportedInterfaces() {
			symbols[iface.Name] = true
		}
		for _, s := range pkg.ExportedStructs() {
			symbols[s.Name] = true
		}
		for _, fn := range pkg.ExportedFunctions() {
			symbols[fn.Name] = true
		}
		for _, td := range pkg.ExportedTypeDefs() {
			symbols[td.Name] = true
		}
		symbolIndex[pkg.Path] = symbols
	}
	return symbolIndex
}

// collectIntraPackageDeps collects dependencies within a single package.
// Combined mode is always public-only, so only dependencies through exported methods/fields are included.
func (b *combinedBuilder) collectIntraPackageDeps(pkg domain.PackageModel, symbolIndex map[string]map[string]bool) []depInfo {
	var deps []depInfo
	seen := make(map[string]bool)

	for _, dep := range pkg.Dependencies {
		// Only include intra-package dependencies
		if dep.From.Package != dep.To.Package {
			continue
		}

		// Must be in this package
		if dep.From.Package != pkg.Path {
			continue
		}

		// Combined mode: only show dependencies through exported methods/fields
		if !dep.ThroughExported {
			continue
		}

		// Skip if source symbol not visible (not exported)
		if !symbolIndex[dep.From.Package][dep.From.Symbol] {
			continue
		}

		// Skip if target symbol not visible (not exported)
		if !symbolIndex[dep.To.Package][dep.To.Symbol] {
			continue
		}

		// Skip self-references
		if dep.From.Symbol == dep.To.Symbol {
			continue
		}

		// Deduplicate
		key := fmt.Sprintf("%s->%s:%s", dep.From.Symbol, dep.To.Symbol, dep.Kind)
		if seen[key] {
			continue
		}
		seen[key] = true

		deps = append(deps, depInfo{
			fromPkg:    dep.From.Package,
			fromSymbol: dep.From.Symbol,
			toPkg:      dep.To.Package,
			toSymbol:   dep.To.Symbol,
			kind:       dep.Kind,
		})
	}

	// Sort for deterministic output
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].fromSymbol != deps[j].fromSymbol {
			return deps[i].fromSymbol < deps[j].fromSymbol
		}
		return deps[i].toSymbol < deps[j].toSymbol
	})

	return deps
}

// collectCrossPackageDeps collects dependencies between packages.
// Combined mode is always public-only, so only dependencies through exported methods/fields are included.
func (b *combinedBuilder) collectCrossPackageDeps(packages []domain.PackageModel, symbolIndex map[string]map[string]bool) []depInfo {
	var deps []depInfo
	seen := make(map[string]bool)

	for _, pkg := range packages {
		for _, dep := range pkg.Dependencies {
			// Skip intra-package dependencies (handled separately)
			if dep.From.Package == dep.To.Package {
				continue
			}

			// Combined mode: only show dependencies through exported methods/fields
			if !dep.ThroughExported {
				continue
			}

			// Skip if target package not in diagram
			if _, ok := symbolIndex[dep.To.Package]; !ok {
				continue
			}

			// Skip if source symbol not visible (not exported)
			if !symbolIndex[dep.From.Package][dep.From.Symbol] {
				continue
			}

			// Skip if target symbol not visible (not exported)
			if !symbolIndex[dep.To.Package][dep.To.Symbol] {
				continue
			}

			// Deduplicate
			key := fmt.Sprintf("%s.%s->%s.%s:%s",
				dep.From.Package, dep.From.Symbol,
				dep.To.Package, dep.To.Symbol,
				dep.Kind)
			if seen[key] {
				continue
			}
			seen[key] = true

			deps = append(deps, depInfo{
				fromPkg:    dep.From.Package,
				fromSymbol: dep.From.Symbol,
				toPkg:      dep.To.Package,
				toSymbol:   dep.To.Symbol,
				kind:       dep.Kind,
			})
		}
	}

	// Sort for deterministic output
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].fromPkg != deps[j].fromPkg {
			return deps[i].fromPkg < deps[j].fromPkg
		}
		if deps[i].fromSymbol != deps[j].fromSymbol {
			return deps[i].fromSymbol < deps[j].fromSymbol
		}
		if deps[i].toPkg != deps[j].toPkg {
			return deps[i].toPkg < deps[j].toPkg
		}
		return deps[i].toSymbol < deps[j].toSymbol
	})

	return deps
}

// writeIntraPackageDep writes a D2 dependency arrow within a package (no package prefix).
func (b *combinedBuilder) writeIntraPackageDep(dep depInfo) {
	b.writeLine(fmt.Sprintf(`%s -> %s: "%s"`, dep.fromSymbol, dep.toSymbol, dep.kind))
}

// writeCrossPackageDep writes a D2 dependency arrow for a cross-package dependency.
func (b *combinedBuilder) writeCrossPackageDep(dep depInfo) {
	fromID := fmt.Sprintf("%s.%s", sanitizePackageID(dep.fromPkg), dep.fromSymbol)
	toID := fmt.Sprintf("%s.%s", sanitizePackageID(dep.toPkg), dep.toSymbol)
	b.writeLine(fmt.Sprintf(`%s -> %s: "%s"`, fromID, toID, dep.kind))
}

// sanitizePackageID converts a package path to a valid D2 identifier.
// It replaces "/" with "." so "internal/service" becomes "internal.service".
func sanitizePackageID(path string) string {
	if path == "" || path == "." {
		return "root"
	}
	return strings.ReplaceAll(path, "/", ".")
}

// writeComment writes a D2 comment line.
func (b *combinedBuilder) writeComment(text string) {
	b.writeLine("# " + text)
}

// writeLine writes an indented line to the buffer.
func (b *combinedBuilder) writeLine(line string) {
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
func (b *combinedBuilder) writeRaw(text string) {
	b.buf.WriteString(text)
}
