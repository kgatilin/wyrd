package http

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	nethttp "net/http"
	"strings"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/plugin"
)

// escapeAttr returns s safe to embed inside an HTML attribute value
// (after a leading "). Delegates to html/template's HTMLEscapeString.
func escapeAttr(s string) string { return template.HTMLEscapeString(s) }

// handlePackagesList serves the /packages list view: a directory tree
// of all packages with filter controls for layer, stereotype, and
// free-text search.
//
// When the request carries HX-Request: true, the handler renders only
// the tree fragment so HTMX can swap it into the existing page without
// reloading the filter bar or triggering scroll reset.
func (s *Server) handlePackagesList(w nethttp.ResponseWriter, r *nethttp.Request) {
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)

	filter := packageFilter{
		Layer:      r.URL.Query().Get("layer"),
		Stereotype: r.URL.Query().Get("stereotype"),
		Search:     strings.TrimSpace(r.URL.Query().Get("q")),
	}

	summaries := buildPackageSummaries(pkgs, filter)
	tree := buildPackageTree(summaries)

	data := packageListData{
		pageData:       s.basePageData(r, "Packages", "/packages"),
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

	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)

	pkg, ok := findPackage(pkgs, pkgPath)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}

	rawTab := r.URL.Query().Get("tab")
	active := parseTab(rawTab)
	modulePath := ""
	if snap.Overlay != nil {
		modulePath = snap.Overlay.Module
	}

	// Overview render mode (#61): default Public, opt-in Full via ?mode=full.
	mode := string(d2adapter.ParseOverviewMode(r.URL.Query().Get("mode")).Normalize())
	overviewScope := parseOverviewScope(r.URL.Query().Get("scope"))
	data := buildPackageDetail(active, pkg, pkgs, snap.Overlay, modulePath, mode, overviewScope)
	data.pageData = s.basePageData(r, "Package "+pkg.Path, "/packages")
	data.Partial = isHTMX(r)
	// Dependencies graph direction (#89). Distinct query param from the
	// overview Mode so toggling one tab doesn't perturb the other.
	data.DepsMode = parseDepsMode(r.URL.Query().Get("deps"))
	if active == tabOverview {
		renderSequenceSVGs(r.Context(), data.Sequences)
	}

	// M13: surface plugin extra tabs + injected scripts.
	if reg := s.UIRegistry(); reg != nil {
		entries := reg.Lookup(plugin.ViewPackageDetail, plugin.SlotExtraTab)
		if len(entries) > 0 {
			panels := make([]pluginPanel, 0, len(entries))
			activePluginTab := strings.TrimPrefix(rawTab, "plugin:")
			extraAttrs := `data-package="` + escapeAttr(pkg.Path) + `"`
			for _, e := range entries {
				tabID := "plugin:" + e.Plugin
				active := strings.HasPrefix(rawTab, "plugin:") && activePluginTab == e.Plugin
				p, ok := buildPluginPanel(tabID, e.Label, e.Element, e.ModelURL, active, extraAttrs)
				if !ok {
					continue
				}
				panels = append(panels, p)
			}
			data.PluginExtraTabs = panels
			scripts := reg.ScriptsFor(plugin.ViewPackageDetail)
			out := make([]pluginScript, 0, len(scripts))
			for _, s := range scripts {
				out = append(out, pluginScript{URL: s.URL})
			}
			data.PluginScripts = out
			if strings.HasPrefix(rawTab, "plugin:") {
				data.PluginActive = rawTab
			}
		}
	}

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
// Public-only mode keeps the Overview diagram readable; full-mode
// callers can pass d2adapter.OverviewModeFull to include internal
// detail.
func renderPackageD2(ctx context.Context, pkg domain.PackageModel, mode d2adapter.OverviewMode) ([]byte, error) {
	source := d2SourceForPackage(pkg, mode)
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("render: empty package")
	}
	return d2adapter.RenderSVG(ctx, source)
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
