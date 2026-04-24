package http

import (
	"bytes"
	"context"
	"fmt"
	nethttp "net/http"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// handlePackagesList serves the /packages list view: a directory tree
// of all packages with filter controls for layer, stereotype, and
// free-text search.
//
// When the request carries HX-Request: true, the handler renders only
// the tree fragment so HTMX can swap it into the existing page without
// reloading the filter bar or triggering scroll reset.
func (s *Server) handlePackagesList(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)

	filter := packageFilter{
		Layer:      r.URL.Query().Get("layer"),
		Stereotype: r.URL.Query().Get("stereotype"),
		Search:     strings.TrimSpace(r.URL.Query().Get("q")),
	}

	summaries := buildPackageSummaries(pkgs, filter)
	tree := buildPackageTree(summaries)

	data := packageListData{
		pageData: pageData{
			Title:      "Packages",
			ActivePath: "/packages",
			NavItems:   buildNav("/packages"),
		},
		Filter:         filter,
		Packages:       summaries,
		Tree:           tree,
		LayerOptions:   collectLayerOptions(pkgs),
		StereotypeOpts: collectStereotypeOptions(pkgs),
		TotalCount:     len(summaries),
		Partial:        isHTMX(r),
	}

	if data.Partial {
		s.renderPartial(w, "packages.html", "packages_tree", data)
		return
	}
	s.renderPageWith(w, "packages.html", data)
}

// handlePackageDetail serves /packages/<path> (and /packages/<path>?tab=X).
// Path segments after /packages/ are joined back into a module-relative
// package path; the root package is /packages/. (dot).
func (s *Server) handlePackageDetail(w nethttp.ResponseWriter, r *nethttp.Request) {
	const prefix = "/packages/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		nethttp.NotFound(w, r)
		return
	}
	pkgPath := strings.TrimPrefix(r.URL.Path, prefix)
	pkgPath = strings.Trim(pkgPath, "/")
	if pkgPath == "" {
		// /packages/ with trailing slash → list view.
		s.handlePackagesList(w, r)
		return
	}

	snap := s.state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)

	pkg, ok := findPackage(pkgs, pkgPath)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}

	active := parseTab(r.URL.Query().Get("tab"))
	modulePath := ""
	if snap.Overlay != nil {
		modulePath = snap.Overlay.Module
	}

	data := buildPackageDetail(active, pkg, pkgs, snap.Overlay, modulePath)
	data.pageData = pageData{
		Title:      "Package " + pkg.Path,
		ActivePath: "/packages",
		NavItems:   buildNav("/packages"),
	}
	data.Partial = isHTMX(r)

	// M8 (#46): Overview no longer emits server-rendered D2→SVG.
	// The tab-overview template includes a .cy-graph div that fetches
	// /api/packages/<path>/graph; graph.js hydrates it client-side.
	// The data.SVG / data.SVGError fields remain on the struct (unused
	// here) so buildPackageDetail's type contract stays stable for
	// other callers that may set them.

	if data.Partial {
		s.renderPartial(w, "package_detail.html", "package_detail_tab", data)
		return
	}
	s.renderPageWith(w, "package_detail.html", data)
}

// applyOverlay merges the overlay config into a fresh copy of the
// package list so Layer/Aggregate are populated for rendering. A nil
// overlay is fine — the input slice is returned unchanged.
func applyOverlay(pkgs []domain.PackageModel, cfg *overlay.Config) []domain.PackageModel {
	if cfg == nil || len(pkgs) == 0 {
		return pkgs
	}
	cp := make([]domain.PackageModel, len(pkgs))
	copy(cp, pkgs)
	merged, _, err := overlay.Merge(cp, cfg)
	if err != nil {
		// overlay.Merge only fails for nil cfg, which we guard above.
		// Defensive fallback: keep the untouched copies.
		return cp
	}
	return merged
}

// findPackage looks up a package by module-relative path. Returns a
// zero-value and false when not found.
func findPackage(pkgs []domain.PackageModel, path string) (domain.PackageModel, bool) {
	for _, p := range pkgs {
		if p.Path == path {
			return p, true
		}
	}
	return domain.PackageModel{}, false
}

// renderPageWith is the generic form of renderPage used by handlers
// that pass a domain-specific data struct (packageListData /
// packageDetailData). Mirrors renderPage's buffer-then-write flow so
// template errors produce a 500, not a half-written response.
func (s *Server) renderPageWith(w nethttp.ResponseWriter, tmpl string, data interface{}) {
	t, err := s.templates.Clone()
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("template clone: %v", err), nethttp.StatusInternalServerError)
		return
	}
	if _, err := t.ParseFS(embedded, "templates/"+tmpl); err != nil {
		nethttp.Error(w, fmt.Sprintf("template parse %s: %v", tmpl, err), nethttp.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", data); err != nil {
		nethttp.Error(w, fmt.Sprintf("template execute: %v", err), nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// renderPartial renders a named template (not wrapped in base) for
// HTMX fragment swaps.
func (s *Server) renderPartial(w nethttp.ResponseWriter, tmpl, defineName string, data interface{}) {
	t, err := s.templates.Clone()
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("template clone: %v", err), nethttp.StatusInternalServerError)
		return
	}
	if _, err := t.ParseFS(embedded, "templates/"+tmpl); err != nil {
		nethttp.Error(w, fmt.Sprintf("template parse %s: %v", tmpl, err), nethttp.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, defineName, data); err != nil {
		nethttp.Error(w, fmt.Sprintf("template execute: %v", err), nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// renderPackageD2 produces the D2 SVG for the package's Overview tab.
// Public-only content keeps the Overview diagram readable; Internal
// details live in their own tab.
func renderPackageD2(ctx context.Context, pkg domain.PackageModel) ([]byte, error) {
	source := d2SourceForPackage(pkg)
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("render: empty package")
	}
	return renderD2(ctx, source)
}

// d2SourceForPackage renders a minimal, self-contained D2 source for a
// single package. We use the existing d2 adapter's combined builder
// with just this package — the existing per-package Build() writes
// file-grouped containers which are noisy for the Overview tab.
// Instead we emit a flat list of public symbols with directed edges
// for inter-package dependencies visible from this package.
func d2SourceForPackage(pkg domain.PackageModel) string {
	// Sort symbols deterministically.
	ifaces := append([]domain.InterfaceDef(nil), pkg.ExportedInterfaces()...)
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })
	structs := append([]domain.StructDef(nil), pkg.ExportedStructs()...)
	sort.Slice(structs, func(i, j int) bool { return structs[i].Name < structs[j].Name })
	fns := append([]domain.FunctionDef(nil), pkg.ExportedFunctions()...)
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name < fns[j].Name })

	var sb strings.Builder
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
		fmt.Fprintf(&sb, "%s: {\n  shape: class\n  label: \"%s\\n<<function>>\"\n}\n",
			quoteD2(fn.Name), fn.Name)
	}
	// Edges for same-package dependencies to give the diagram some
	// structure. We don't draw cross-package edges here — they live in
	// the Dependencies tab.
	seenEdge := make(map[string]struct{})
	for _, d := range pkg.Dependencies {
		if d.To.Package != pkg.Path {
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

// quoteD2 quotes an identifier for D2 if it contains characters that
// aren't safe bare. D2 allows dots in IDs but we keep the quoting
// conservative; the value is already restricted to Go identifiers in
// practice so quoting rarely triggers.
func quoteD2(s string) string {
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
}
