package http

import (
	"fmt"
	"sort"
	"strings"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/domain"
)

// d2SourceForPackage renders a minimal, self-contained D2 source for a
// single package overview. The default (OverviewModePublic) emits only
// exported symbols and tags exported `New<Type>` constructors / factory
// functions with the "<<entry-point>>" stereotype so they are visually
// distinct in the rendered SVG. OverviewModeFull additionally renders
// unexported symbols (constructors / functions / types) for debugging.
//
// Output is deterministic — callers can rely on byte-identical results
// for identical inputs (golden tests, caching, drift detection).
func d2SourceForPackage(pkg domain.PackageModel, mode d2adapter.OverviewMode) string {
	mode = mode.Normalize()

	// Decide which symbol slices to render based on mode.
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
	sort.Slice(structs, func(i, j int) bool { return structs[i].Name < structs[j].Name })
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name < fns[j].Name })

	// Track which symbol IDs we've actually rendered so dependency
	// edges don't dangle when their endpoints aren't visible in this
	// mode.
	visible := make(map[string]struct{}, len(ifaces)+len(structs)+len(fns))
	for _, iface := range ifaces {
		visible[iface.Name] = struct{}{}
	}
	for _, st := range structs {
		visible[st.Name] = struct{}{}
	}
	for _, fn := range fns {
		visible[fn.Name] = struct{}{}
	}

	var sb strings.Builder
	// Header comments tag the active mode for trivial diffability.
	fmt.Fprintf(&sb, "# mode: %s\n", mode)
	// Title as a node header so the diagram is never empty even when
	// the package has no exported symbols.
	fmt.Fprintf(&sb, "title: {\n  label: %s\n  near: top-center\n  shape: text\n}\n", quoteD2(pkg.Path))
	for _, iface := range ifaces {
		fmt.Fprintf(&sb, "%s: {\n  shape: class\n  label: \"%s\\n<<interface>>\"\n}\n",
			quoteD2(iface.Name), iface.Name)
	}
	for _, st := range structs {
		fmt.Fprintf(&sb, "%s: {\n  shape: class\n  label: \"%s\\n<<struct>>\"\n}\n",
			quoteD2(st.Name), st.Name)
	}
	for _, fn := range fns {
		stereo := "<<function>>"
		if d2adapter.IsEntryPoint(fn) {
			// Entry-point styling: bold stroke + dedicated stereotype
			// label. The label itself doubles as the visual marker so
			// renderers without CSS hooks still differentiate them.
			stereo = "<<entry-point>>"
			fmt.Fprintf(&sb, "%s: {\n  shape: class\n  label: \"%s\\n%s\"\n  style.bold: true\n  style.stroke-width: 2\n}\n",
				quoteD2(fn.Name), fn.Name, stereo)
			continue
		}
		fmt.Fprintf(&sb, "%s: {\n  shape: class\n  label: \"%s\\n%s\"\n}\n",
			quoteD2(fn.Name), fn.Name, stereo)
	}
	// Edges for same-package dependencies to give the diagram some
	// structure. We don't draw cross-package edges here — they live in
	// the Dependencies tab.
	seenEdge := make(map[string]struct{})
	for _, d := range pkg.Dependencies {
		if d.To.Package != pkg.Path {
			continue
		}
		// Skip edges that point at symbols that aren't rendered in
		// this mode (avoids D2 referring to undeclared identifiers).
		if _, ok := visible[d.From.Symbol]; !ok {
			continue
		}
		if _, ok := visible[d.To.Symbol]; !ok {
			continue
		}
		key := d.From.Symbol + "->" + d.To.Symbol
		if _, ok := seenEdge[key]; ok {
			continue
		}
		seenEdge[key] = struct{}{}
		fmt.Fprintf(&sb, "%s -> %s\n", quoteD2(d.From.Symbol), quoteD2(d.To.Symbol))
	}
	return sb.String()
}
