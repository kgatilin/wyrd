package http

import (
	"context"
	"fmt"
	nethttp "net/http"
	"sort"
	"strings"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// graphPayload is the structured graph document returned by every
// /api/.../graph endpoint. The browser-side graph.js reads payload.meta
// to pick a layout + style, then maps nodes/edges into Cytoscape
// elements. Each node's Parent points at a compound parent (e.g. layer
// group); root-level nodes leave it empty.
type graphPayload struct {
	Meta  graphMeta   `json:"meta"`
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

// graphMeta carries view-specific rendering hints for the client. View
// ("layer-map" / "layer-map-mini" / "package-overview" / "diff-overlay")
// triggers the right Cytoscape style preset in graph.js. Layout names
// match cytoscape layout ids ("elk", "dagre").
type graphMeta struct {
	View   string `json:"view"`
	Layout string `json:"layout"`
	Title  string `json:"title,omitempty"`
	// Mode is set on overview-style payloads ("public" / "full") so the
	// browser can render the correct toggle state without a round-trip.
	Mode string `json:"mode,omitempty"`
}

// graphEndpointDoc describes where graph.js should refresh the data
// from, used by .cy-graph divs that carry a data-api attribute rather
// than an inline data-graph JSON payload.
//
// The payload itself is self-contained — graph.js consumes the same
// shape whether it comes from an inline attribute or an API fetch.

// --- Layers view ----------------------------------------------------

// buildLayerGraph produces the cytoscape payload for the Layers page.
// It places every layer as a compound parent node and each package as a
// child. Cross-layer edges come straight from buildLayerEdges with the
// colour/kind mapping the Layers CSS uses.
//
// The mini=true variant drops package children and violations so the
// dashboard preview stays readable.
func buildLayerGraph(cfg *overlay.Config, packages []domain.PackageModel, mini bool) graphPayload {
	out := graphPayload{
		Meta: graphMeta{
			View:   "layer-map",
			Layout: "elk",
			Title:  "Layer map",
		},
	}
	if mini {
		out.Meta.View = "layer-map-mini"
	}
	if cfg == nil || len(cfg.Layers) == 0 {
		return out
	}

	pkgLayer := computePackageLayers(cfg, packages)
	pkgCount := make(map[string]int)
	for _, p := range packages {
		if l := pkgLayer[p.Path]; l != "" {
			pkgCount[l]++
		}
	}
	layerNames := sortedLayerNames(cfg)

	for _, name := range layerNames {
		label := name
		if !mini {
			label = fmt.Sprintf("%s (%d)", name, pkgCount[name])
		}
		out.Nodes = append(out.Nodes, graphNode{
			ID:    "layer:" + name,
			Label: label,
			Kind:  "layer",
		})
	}

	if !mini {
		// Package children, parented by their layer.
		for _, p := range packages {
			l := pkgLayer[p.Path]
			if l == "" {
				continue
			}
			out.Nodes = append(out.Nodes, graphNode{
				ID:     "pkg:" + p.Path,
				Label:  p.Path,
				Kind:   "package",
				Parent: "layer:" + l,
			})
		}
		// Deterministic child ordering.
		sort.Slice(out.Nodes, func(i, j int) bool {
			if out.Nodes[i].Kind != out.Nodes[j].Kind {
				// layers first, then packages — stable and intuitive.
				return out.Nodes[i].Kind == "layer"
			}
			return out.Nodes[i].ID < out.Nodes[j].ID
		})
	}

	violations, allowed, declared := buildLayerEdges(cfg, packages)
	addEdge := func(from, to, kind string, details []string) {
		label := ""
		if !mini {
			label = kind
		}
		out.Edges = append(out.Edges, graphEdge{
			Source:  "layer:" + from,
			Target:  "layer:" + to,
			Label:   label,
			Kind:    kind,
			Details: details,
		})
	}
	// Declared-but-unused: drawn first (gray dashed).
	if !mini {
		for _, e := range declared {
			addEdge(e.From, e.To, "declared", e.Details)
		}
	}
	for _, e := range allowed {
		addEdge(e.From, e.To, "allowed", e.Details)
	}
	if !mini {
		for _, e := range violations {
			addEdge(e.From, e.To, "violation", e.Details)
		}
	}
	return out
}

func sortedLayerNames(cfg *overlay.Config) []string {
	names := make([]string, 0, len(cfg.Layers))
	for n := range cfg.Layers {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, knownI := layerDisplayRank(names[i])
		rj, knownJ := layerDisplayRank(names[j])
		if knownI != knownJ {
			return knownI
		}
		if knownI && ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

func layerDisplayRank(name string) (int, bool) {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "_")) {
	case "domain":
		return 0, true
	case "application", "app", "service", "services", "model_ops":
		return 1, true
	case "infrastructure", "infra", "runtime", "storage":
		return 2, true
	case "adapter", "adapters", "transport", "source_adapter", "cli":
		return 3, true
	default:
		return 0, false
	}
}

// --- Package overview -----------------------------------------------

// buildPackageOverviewGraph builds the client-side graph for the
// Package Overview tab. The subject package sits at the centre with its
// types/functions as children; immediate internal dependency targets
// (inbound + outbound packages) appear around it.
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
		out.Nodes = append(out.Nodes, graphNode{
			ID:     "type:" + pkg.Path + "." + i.Name,
			Label:  interfaceOverviewLabel(i, mode, pkg.Path, typeIndex),
			Kind:   "interface",
			Parent: rootID,
		})
	}
	sort.Slice(structs, func(i, j int) bool { return structs[i].Name < structs[j].Name })
	for _, s := range structs {
		out.Nodes = append(out.Nodes, graphNode{
			ID:     "type:" + pkg.Path + "." + s.Name,
			Label:  structOverviewLabel(s, mode, pkg.Path, typeIndex),
			Kind:   "struct",
			Parent: rootID,
		})
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name < fns[j].Name })
	for _, f := range fns {
		kind := "function"
		// Entry-point detection: factories + `New<Type>` constructors.
		// We only mark exported entry points; unexported helpers stay
		// as plain "function" nodes even in full mode.
		if d2adapter.IsEntryPoint(f) {
			kind = "entry-point"
		}
		out.Nodes = append(out.Nodes, graphNode{
			ID:     "fn:" + pkg.Path + "." + f.Name,
			Label:  functionOverviewLabel(f, pkg.Path, typeIndex),
			Kind:   kind,
			Parent: rootID,
		})
	}

	// Outbound dep packages (internal only) — each becomes a top-level
	// package node with an edge from the subject package.
	known := knownPackagePaths(allPkgs)
	seenOut := make(map[string]struct{})
	for _, d := range pkg.Dependencies {
		if d.To.External || d.To.Package == "" {
			continue
		}
		if d.To.Package == pkg.Path {
			continue
		}
		// Normalize when dep includes module prefix — the existing
		// Overview relies on same-path matching so we skip unknowns.
		if _, ok := known[d.To.Package]; !ok {
			continue
		}
		if _, dup := seenOut[d.To.Package]; dup {
			continue
		}
		seenOut[d.To.Package] = struct{}{}
		out.Nodes = append(out.Nodes, graphNode{
			ID:    "pkg:" + d.To.Package,
			Label: shortName(d.To.Package),
			Kind:  "package-out",
		})
		out.Edges = append(out.Edges, graphEdge{
			Source: rootID,
			Target: "pkg:" + d.To.Package,
			Kind:   "outbound",
		})
	}

	// Inbound dep packages — every other pkg that targets ours.
	seenIn := make(map[string]struct{})
	for _, src := range allPkgs {
		if src.Path == pkg.Path {
			continue
		}
		for _, d := range src.Dependencies {
			if d.To.Package != pkg.Path {
				continue
			}
			if _, dup := seenIn[src.Path]; dup {
				continue
			}
			seenIn[src.Path] = struct{}{}
			if _, dup := seenOut[src.Path]; dup {
				// Symmetric: already present as outbound; the edge below
				// still makes sense (A uses B and B uses A).
			} else {
				out.Nodes = append(out.Nodes, graphNode{
					ID:    "pkg:" + src.Path,
					Label: shortName(src.Path),
					Kind:  "package-in",
				})
			}
			out.Edges = append(out.Edges, graphEdge{
				Source: "pkg:" + src.Path,
				Target: rootID,
				Kind:   "inbound",
			})
			break
		}
	}

	return out
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

// --- Diff overlay ---------------------------------------------------

// buildDiffGraph produces a cytoscape overlay of current vs. target.
// Nodes are identified by their diff.Change.Path, colored by operation
// (add=green / remove=red / change=yellow). Edges connect parent
// packages to their changed children so the graph reveals the impact
// surface per package.
func buildDiffGraph(d *diff.Diff, filter string) graphPayload {
	out := graphPayload{
		Meta: graphMeta{
			View:   "diff-overlay",
			Layout: "dagre",
			Title:  "Diff",
		},
	}
	if d == nil || len(d.Changes) == 0 {
		return out
	}

	seen := make(map[string]struct{})
	addNode := func(n graphNode) {
		if _, ok := seen[n.ID]; ok {
			return
		}
		seen[n.ID] = struct{}{}
		out.Nodes = append(out.Nodes, n)
	}

	for _, c := range d.Changes {
		if filter != "" && string(c.Kind) != filter {
			continue
		}
		parent := diffParentID(c.Path)
		if parent != "" {
			addNode(graphNode{
				ID:    parent,
				Label: parent,
				Kind:  "package",
			})
		}
		// The change itself is a child of the parent, if any.
		nodeID := string(c.Kind) + ":" + c.Path
		addNode(graphNode{
			ID:     nodeID,
			Label:  shortNameForDiff(c.Path),
			Kind:   string(c.Kind),
			Parent: parent,
			Op:     string(c.Op),
		})
		if parent != "" {
			out.Edges = append(out.Edges, graphEdge{
				Source: parent,
				Target: nodeID,
				Kind:   string(c.Op),
			})
		}
	}
	return out
}

// diffParentID returns the "package:" parent id for a diff change path.
// Paths look like "pkg.Symbol" or "pkg.Symbol.Field"; the substring up
// to the first dot is taken as the package.
func diffParentID(path string) string {
	if path == "" {
		return ""
	}
	// diff paths use '.' but packages may contain '/' — so the first dot
	// after the last slash is the package/type boundary.
	cut := strings.LastIndex(path, "/")
	tail := path
	prefix := ""
	if cut >= 0 {
		prefix = path[:cut+1]
		tail = path[cut+1:]
	}
	dot := strings.Index(tail, ".")
	if dot < 0 {
		return ""
	}
	return "pkg:" + prefix + tail[:dot]
}

// shortNameForDiff returns the trailing component of a diff path
// (everything after the final dot), suitable as a short node label.
func shortNameForDiff(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 && i < len(path)-1 {
		return path[i+1:]
	}
	return path
}

// --- HTTP handlers --------------------------------------------------

// registerGraphRoutes installs the cytoscape JSON API + D2/SVG export
// endpoints added by M8. Kept in a single place so the routes table
// stays easy to scan.
func (s *Server) registerGraphRoutes(mux *nethttp.ServeMux) {
	mux.HandleFunc("/api/layers", s.handleLayersGraphJSON)
	mux.HandleFunc("/api/layers/mini", s.handleLayersGraphMiniJSON)
	mux.HandleFunc("/api/packages/", s.handlePackageGraphJSON) // expects /api/packages/<path>/graph
	mux.HandleFunc("/api/diff", s.handleDiffGraphJSON)

	// Export endpoints. "/view/..." carries the display view name so
	// it's easy to discover in dev tools; the d2/svg suffix chooses the
	// encoding. svg is rendered server-side from the d2 source on each
	// request — no caching, renders are already fast.
	mux.HandleFunc("/view/layers/d2", s.handleLayersExportD2)
	mux.HandleFunc("/view/layers/svg", s.handleLayersExportSVG)
	mux.HandleFunc("/view/packages/", s.handlePackageExport) // /view/packages/<path>/{d2,svg}
	mux.HandleFunc("/view/diff/d2", s.handleDiffExportD2)
	mux.HandleFunc("/view/diff/svg", s.handleDiffExportSVG)
}

// handleLayersGraphJSON returns the full layers cytoscape payload.
func (s *Server) handleLayersGraphJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	writeJSON(w, buildLayerGraph(snap.Overlay, snap.Packages, false))
}

// handleLayersGraphMiniJSON returns the dashboard preview variant —
// layers + allowed edges only, no violations, no package children.
func (s *Server) handleLayersGraphMiniJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	writeJSON(w, buildLayerGraph(snap.Overlay, snap.Packages, true))
}

// handlePackageGraphJSON serves /api/packages/<path>/graph. The path
// may contain slashes; the trailing "/graph" segment is mandatory so
// this handler never collides with a list/detail endpoint later.
func (s *Server) handlePackageGraphJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	const prefix = "/api/packages/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	if !strings.HasSuffix(rest, "/graph") {
		nethttp.NotFound(w, r)
		return
	}
	pkgPath := strings.TrimSuffix(rest, "/graph")
	pkgPath = strings.Trim(pkgPath, "/")
	if pkgPath == "" {
		nethttp.NotFound(w, r)
		return
	}
	snap := s.state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)
	pkg, ok := findPackage(pkgs, pkgPath)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}
	mode := d2adapter.ParseOverviewMode(r.URL.Query().Get("mode"))
	writeJSON(w, buildPackageOverviewGraph(pkg, pkgs, mode))
}

// handleDiffGraphJSON serves /api/diff?against=<targetID>&kind=<kind>.
// When ?against is missing the active target is used (matching the
// existing /diff page semantics).
func (s *Server) handleDiffGraphJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	against := q.Get("against")
	filter := q.Get("kind")

	snap := s.state.Snapshot()
	if against == "" {
		against = snap.CurrentTarget
	}
	if against == "" {
		// No target — return an empty payload rather than 404 so the UI
		// stays stable.
		writeJSON(w, graphPayload{Meta: graphMeta{View: "diff-overlay", Layout: "dagre"}})
		return
	}

	current, tgt, err := loadDiffSides(r.Context(), snap.Root, against)
	if err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
		return
	}
	d := diff.Compute(current, tgt)
	writeJSON(w, buildDiffGraph(d, filter))
}

// --- export handlers -------------------------------------------------

func (s *Server) handleLayersExportD2(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	if snap.Overlay == nil {
		nethttp.Error(w, "no overlay loaded", nethttp.StatusNotFound)
		return
	}
	src := buildLayerMapD2(snap.Overlay, snap.Packages, true)
	writeText(w, "layers.d2", src)
}

func (s *Server) handleLayersExportSVG(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	if snap.Overlay == nil {
		nethttp.Error(w, "no overlay loaded", nethttp.StatusNotFound)
		return
	}
	src := buildLayerMapD2(snap.Overlay, snap.Packages, true)
	writeSVG(w, r.Context(), "layers.svg", src)
}

func (s *Server) handlePackageExport(w nethttp.ResponseWriter, r *nethttp.Request) {
	const prefix = "/view/packages/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	var fmtSuffix string
	switch {
	case strings.HasSuffix(rest, "/d2"):
		fmtSuffix = "d2"
		rest = strings.TrimSuffix(rest, "/d2")
	case strings.HasSuffix(rest, "/svg"):
		fmtSuffix = "svg"
		rest = strings.TrimSuffix(rest, "/svg")
	default:
		nethttp.NotFound(w, r)
		return
	}
	pkgPath := strings.Trim(rest, "/")
	if pkgPath == "" {
		nethttp.NotFound(w, r)
		return
	}
	snap := s.state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)
	pkg, ok := findPackage(pkgs, pkgPath)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}
	mode := d2adapter.ParseOverviewMode(r.URL.Query().Get("mode"))
	src := d2SourceForPackage(pkg, mode)
	name := safeFilename(pkg.Path)
	if fmtSuffix == "d2" {
		writeText(w, name+".d2", src)
		return
	}
	writeSVG(w, r.Context(), name+".svg", src)
}

func (s *Server) handleDiffExportD2(w nethttp.ResponseWriter, r *nethttp.Request) {
	src, err := s.diffD2Source(r)
	if err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusBadRequest)
		return
	}
	writeText(w, "diff.d2", src)
}

func (s *Server) handleDiffExportSVG(w nethttp.ResponseWriter, r *nethttp.Request) {
	src, err := s.diffD2Source(r)
	if err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusBadRequest)
		return
	}
	writeSVG(w, r.Context(), "diff.svg", src)
}

// diffD2Source builds a D2 source string for the current diff. The
// ?against=<target-id> query param overrides the active target; ?kind=
// filters to a single diff.Kind.
func (s *Server) diffD2Source(r *nethttp.Request) (string, error) {
	q := r.URL.Query()
	against := q.Get("against")
	filter := q.Get("kind")
	snap := s.state.Snapshot()
	if against == "" {
		against = snap.CurrentTarget
	}
	if against == "" {
		return "", fmt.Errorf("no target selected")
	}
	current, tgt, err := loadDiffSides(r.Context(), snap.Root, against)
	if err != nil {
		return "", err
	}
	d := diff.Compute(current, tgt)
	return renderDiffD2(d, filter), nil
}

// renderDiffD2 returns a deterministic D2 source for the given diff,
// one node per changed symbol plus edges from parent packages to the
// changes they contain.
func renderDiffD2(d *diff.Diff, filter string) string {
	var b strings.Builder
	if d == nil {
		return "title: \"(no diff)\"\n"
	}
	b.WriteString("title: {\n  label: \"Diff overlay\"\n  near: top-center\n  shape: text\n}\n")

	parents := make(map[string]struct{})
	for _, c := range d.Changes {
		if filter != "" && string(c.Kind) != filter {
			continue
		}
		parent := diffParentID(c.Path)
		if parent != "" {
			parents[parent] = struct{}{}
		}
		color := diffOpColor(string(c.Op))
		fmt.Fprintf(&b, "%s: {\n  label: %s\n  style.stroke: %q\n}\n",
			d2ID(string(c.Kind)+":"+c.Path),
			quoteD2(c.Path),
			color)
		if parent != "" {
			fmt.Fprintf(&b, "%s -> %s\n",
				d2ID(parent), d2ID(string(c.Kind)+":"+c.Path))
		}
	}
	parentIDs := make([]string, 0, len(parents))
	for p := range parents {
		parentIDs = append(parentIDs, p)
	}
	sort.Strings(parentIDs)
	for _, p := range parentIDs {
		fmt.Fprintf(&b, "%s: {\n  label: %s\n  shape: package\n}\n", d2ID(p), quoteD2(p))
	}
	return b.String()
}

func diffOpColor(op string) string {
	switch op {
	case "add":
		return "#16a34a"
	case "remove":
		return "#dc2626"
	case "change":
		return "#d97706"
	}
	return "#64748b"
}

// --- IO helpers ------------------------------------------------------

// writeText sends text/plain with a Content-Disposition attachment so
// browsers prompt the user to save. Used by the D2 export endpoints.
func writeText(w nethttp.ResponseWriter, filename, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write([]byte(body))
}

// writeSVG renders D2 source to SVG and sends it as an attachment.
func writeSVG(w nethttp.ResponseWriter, ctx context.Context, filename, source string) {
	svg, err := renderD2(ctx, source)
	if err != nil {
		nethttp.Error(w, "render: "+err.Error(), nethttp.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(svg)
}

// safeFilename replaces path separators so a package path is usable as
// a download filename.
func safeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	if s == "" {
		s = "package"
	}
	return s
}
