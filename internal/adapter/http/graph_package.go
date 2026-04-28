package http

import (
	"fmt"
	"sort"
	"strings"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/domain"
)

// buildPackageOverviewGraph builds the package-only client-side graph
// for the Package Overview tab. The subject package contains its
// visible types/functions as children, with same-package symbol
// dependency edges between them. Cross-package inbound/outbound peers
// intentionally live in buildPackageDepsGraph so the two concerns do
// not make one unreadable mixed diagram.
//
// In OverviewModePublic (the default) only exported symbols are
// rendered, and entry-point functions (factories / `New<Type>`
// constructors) are tagged with kind="entry-point" so the browser can
// style them distinctly. OverviewModeFull additionally renders
// unexported symbols.
func buildPackageOverviewGraph(pkg domain.PackageModel, allPkgs []domain.PackageModel, mode d2adapter.OverviewMode) graphPayload {
	mode = mode.Normalize()
	out := graphPayload{
		Meta: graphMeta{
			View:   "package-overview",
			Layout: "dagre",
			Title:  pkg.Path,
			Mode:   string(mode),
		},
	}

	rootID := "pkg:" + pkg.Path
	typeIndex := buildOverviewTypeIndex(allPkgs)
	symbolNodes := make(map[string]string)
	out.Nodes = append(out.Nodes, graphNode{
		ID:    rootID,
		Label: pkg.Name,
		Kind:  "package",
		Root:  true,
	})

	// Decide which symbols to render based on mode.
	var ifaces []domain.InterfaceDef
	var structs []domain.StructDef
	var fns []domain.FunctionDef
	if mode == d2adapter.OverviewModeFull {
		ifaces = append([]domain.InterfaceDef(nil), pkg.Interfaces...)
		structs = append([]domain.StructDef(nil), pkg.Structs...)
		fns = append([]domain.FunctionDef(nil), pkg.Functions...)
	} else {
		ifaces = append([]domain.InterfaceDef(nil), pkg.ExportedInterfaces()...)
		structs = append([]domain.StructDef(nil), pkg.ExportedStructs()...)
		fns = append([]domain.FunctionDef(nil), pkg.ExportedFunctions()...)
	}

	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })
	for _, i := range ifaces {
		id := "type:" + pkg.Path + "." + i.Name
		symbolNodes[i.Name] = id
		out.Nodes = append(out.Nodes, graphNode{
			ID:     id,
			Label:  interfaceOverviewLabel(i, mode, pkg.Path, typeIndex),
			Kind:   "interface",
			Parent: rootID,
		})
	}
	sort.Slice(structs, func(i, j int) bool { return structs[i].Name < structs[j].Name })
	for _, s := range structs {
		id := "type:" + pkg.Path + "." + s.Name
		symbolNodes[s.Name] = id
		out.Nodes = append(out.Nodes, graphNode{
			ID:     id,
			Label:  structOverviewLabel(s, mode, pkg.Path, typeIndex),
			Kind:   "struct",
			Parent: rootID,
		})
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name < fns[j].Name })
	for _, f := range fns {
		id := "fn:" + pkg.Path + "." + f.Name
		symbolNodes[f.Name] = id
		kind := "function"
		// Entry-point detection: factories + `New<Type>` constructors.
		// We only mark exported entry points; unexported helpers stay
		// as plain "function" nodes even in full mode.
		if d2adapter.IsEntryPoint(f) {
			kind = "entry-point"
		}
		out.Nodes = append(out.Nodes, graphNode{
			ID:     id,
			Label:  functionOverviewLabel(f, pkg.Path, typeIndex),
			Kind:   kind,
			Parent: rootID,
		})
	}

	addPackageOverviewSymbolEdges(&out, pkg, mode, symbolNodes)

	return out
}

func addPackageOverviewSymbolEdges(out *graphPayload, pkg domain.PackageModel, mode d2adapter.OverviewMode, symbolNodes map[string]string) {
	seen := make(map[string]struct{})
	for _, dep := range pkg.Dependencies {
		if dep.From.Package != pkg.Path || dep.To.Package != pkg.Path || dep.To.External {
			continue
		}
		if mode.Normalize() != d2adapter.OverviewModeFull && !dep.ThroughExported {
			continue
		}
		source, ok := overviewSymbolNodeID(symbolNodes, dep.From.Symbol)
		if !ok {
			continue
		}
		target, ok := overviewSymbolNodeID(symbolNodes, dep.To.Symbol)
		if !ok || source == target {
			continue
		}
		kind := strings.TrimSpace(dep.Kind.String())
		if kind == "" {
			kind = "uses"
		}
		key := source + "\x00" + target + "\x00" + kind
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out.Edges = append(out.Edges, graphEdge{
			Source: source,
			Target: target,
			Label:  kind,
			Kind:   kind,
		})
	}
}

func overviewSymbolNodeID(symbolNodes map[string]string, symbol string) (string, bool) {
	if id, ok := symbolNodes[symbol]; ok {
		return id, true
	}
	if dot := strings.Index(symbol, "."); dot > 0 {
		id, ok := symbolNodes[symbol[:dot]]
		return id, ok
	}
	return "", false
}

func interfaceOverviewLabel(iface domain.InterfaceDef, mode d2adapter.OverviewMode, currentPkg string, typeIndex map[string]map[string]string) string {
	lines := []string{iface.Name, "interface"}
	methods := visibleOverviewMethods(iface.Methods, mode)
	if len(methods) > 0 {
		lines = append(lines, "methods:")
	}
	for _, m := range methods {
		lines = append(lines, "  "+methodOverviewLine(m, currentPkg, typeIndex))
	}
	return strings.Join(lines, "\n")
}

func structOverviewLabel(st domain.StructDef, mode d2adapter.OverviewMode, currentPkg string, typeIndex map[string]map[string]string) string {
	lines := []string{st.Name, "struct"}
	fields := visibleOverviewFields(st.Fields, mode)
	methods := visibleOverviewMethods(st.Methods, mode)
	if len(fields) > 0 {
		lines = append(lines, "fields:")
	}
	for _, f := range fields {
		lines = append(lines, "  "+fieldOverviewLine(f, currentPkg))
	}
	if len(methods) > 0 {
		lines = append(lines, "methods:")
	}
	for _, m := range methods {
		lines = append(lines, "  "+methodOverviewLine(m, currentPkg, typeIndex))
	}
	return strings.Join(lines, "\n")
}

func functionOverviewLabel(fn domain.FunctionDef, currentPkg string, typeIndex map[string]map[string]string) string {
	kind := "function"
	switch {
	case d2adapter.IsConstructor(fn):
		kind = "constructor"
	case fn.Stereotype == domain.StereotypeFactory:
		kind = "factory"
	}
	lines := []string{fn.Name, kind}
	if len(fn.Params) > 0 {
		lines = append(lines, "args:")
		for _, p := range fn.Params {
			lines = append(lines, "  "+paramOverviewLine(p, currentPkg))
		}
	}
	if len(fn.Returns) > 0 {
		lines = append(lines, "returns:")
		for _, r := range fn.Returns {
			lines = append(lines, "  "+returnOverviewLine(r, currentPkg, typeIndex))
		}
	}
	return strings.Join(lines, "\n")
}

func buildOverviewTypeIndex(packages []domain.PackageModel) map[string]map[string]string {
	out := make(map[string]map[string]string, len(packages))
	ensure := func(pkg string) map[string]string {
		if out[pkg] == nil {
			out[pkg] = make(map[string]string)
		}
		return out[pkg]
	}
	for _, pkg := range packages {
		types := ensure(pkg.Path)
		for _, iface := range pkg.Interfaces {
			types[iface.Name] = "interface"
		}
		for _, st := range pkg.Structs {
			types[st.Name] = "struct"
		}
		for _, td := range pkg.TypeDefs {
			types[td.Name] = "type"
		}
	}
	return out
}

func visibleOverviewMethods(methods []domain.MethodDef, mode d2adapter.OverviewMode) []domain.MethodDef {
	out := make([]domain.MethodDef, 0, len(methods))
	for _, m := range methods {
		if mode.Normalize() == d2adapter.OverviewModeFull || m.IsExported {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Signature() < out[j].Signature()
	})
	return out
}

func visibleOverviewFields(fields []domain.FieldDef, mode d2adapter.OverviewMode) []domain.FieldDef {
	out := make([]domain.FieldDef, 0, len(fields))
	for _, f := range fields {
		if mode.Normalize() == d2adapter.OverviewModeFull || f.IsExported {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func methodOverviewLine(m domain.MethodDef, currentPkg string, typeIndex map[string]map[string]string) string {
	prefix := "-"
	if m.IsExported {
		prefix = "+"
	}
	params := make([]string, 0, len(m.Params))
	for _, p := range m.Params {
		params = append(params, paramOverviewLine(p, currentPkg))
	}
	line := fmt.Sprintf("%s %s(%s)", prefix, m.Name, strings.Join(params, ", "))
	if len(m.Returns) == 0 {
		return line
	}
	return line + ": " + formatOverviewReturns(m.Returns, currentPkg, typeIndex)
}

func fieldOverviewLine(f domain.FieldDef, currentPkg string) string {
	prefix := "-"
	if f.IsExported {
		prefix = "+"
	}
	return fmt.Sprintf("%s %s: %s", prefix, f.Name, shortOverviewType(f.Type, currentPkg))
}

func paramOverviewLine(p domain.ParamDef, currentPkg string) string {
	if p.Name == "" {
		return shortOverviewType(p.Type, currentPkg)
	}
	return fmt.Sprintf("%s: %s", p.Name, shortOverviewType(p.Type, currentPkg))
}

func returnOverviewLine(r domain.TypeRef, currentPkg string, typeIndex map[string]map[string]string) string {
	typ := shortOverviewType(r, currentPkg)
	kind := overviewTypeKind(r, currentPkg, typeIndex)
	if kind == "" || kind == "value" {
		return typ
	}
	return kind + " " + typ
}

func formatOverviewReturns(returns []domain.TypeRef, currentPkg string, typeIndex map[string]map[string]string) string {
	if len(returns) == 0 {
		return ""
	}
	parts := make([]string, 0, len(returns))
	for _, r := range returns {
		parts = append(parts, returnOverviewLine(r, currentPkg, typeIndex))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func shortOverviewType(t domain.TypeRef, currentPkg string) string {
	prefix := ""
	if t.IsSlice {
		prefix += "[]"
	}
	if t.IsMap {
		key := ""
		if t.KeyType != nil {
			key = shortOverviewType(*t.KeyType, currentPkg)
		}
		value := ""
		if t.ValueType != nil {
			value = shortOverviewType(*t.ValueType, currentPkg)
		}
		return prefix + "map[" + key + "]" + value
	}
	if t.IsPointer {
		prefix += "*"
	}
	name := t.Name
	if t.Package != "" {
		name = shortName(t.Package) + "." + name
	}
	return prefix + name
}

func overviewTypeKind(t domain.TypeRef, currentPkg string, typeIndex map[string]map[string]string) string {
	if t.IsMap {
		return "value"
	}
	pkg := t.Package
	if pkg == "" {
		pkg = currentPkg
	}
	if types := typeIndex[pkg]; types != nil {
		if kind := types[t.Name]; kind != "" {
			return kind
		}
	}
	return "value"
}
