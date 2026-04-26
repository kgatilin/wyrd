package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/buildinfo"
	"github.com/kgatilin/archai/internal/plugin"
)

// templateFuncs returns the funcmap shared by every page template.
// safeHTML lets a handler inline trusted HTML (e.g. a server-rendered
// SVG diagram) without html/template escaping it.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		// toJSON marshals v as a JSON string that is safe to embed in an
		// HTML attribute value (so client-side JS can read it via
		// dataset.*). Falls back to "{}" on marshal errors so the page
		// still renders.
		"toJSON": func(v any) string {
			buf, err := json.Marshal(v)
			if err != nil {
				return "{}"
			}
			return string(buf)
		},
	}
}

// navItem is one link in the top navigation bar. Active is set on the
// link matching the current page so the base template can highlight
// it.
type navItem struct {
	Label  string
	Href   string
	Active bool
}

// pageData is the model passed to every page template (which all wrap
// templates/base.html). Handlers populate Title, ActivePath, and
// (optionally) content-specific fields.
//
// NavPrefix is "/w/<name>" in multi-worktree mode and "" in
// single-worktree mode; templates prepend it to internal links so the
// current worktree stays selected when the user clicks around.
// Worktrees is populated only in multi mode and drives the switcher
// dropdown in base.html.
type pageData struct {
	Title       string
	ActivePath  string
	NavItems    []navItem
	NavPrefix   string
	Worktree    string
	MultiMode   bool
	Worktrees   []worktreeOption
	CurrentPath string // original request path (used by the switcher form)
	// Version is the build identity rendered by the footer in
	// templates/base.html. It is the same struct returned by
	// /api/version so the dashboard footer and the JSON endpoint can
	// never disagree.
	Version buildinfo.Info
}

// searchPageData is the model for the full Search page template. It
// embeds pageData so base.html sees the same Title/ActivePath/NavItems
// and also carries the initial form state + (optional) pre-rendered
// results so a non-HTMX request to /search?q=… returns a useful page
// for bookmarking/permalinking.
type searchPageData struct {
	pageData
	Query   string
	Kind    string
	Kinds   []string
	Results []searchResult
	Total   int
}

// navTemplate is the canonical, ordered nav used for every page.
var navTemplate = []navItem{
	{Label: "Dashboard", Href: "/"},
	{Label: "Layers", Href: "/layers"},
	{Label: "Packages", Href: "/packages"},
	{Label: "Configs", Href: "/configs"},
	{Label: "Domain", Href: "/bc"},
	{Label: "Targets", Href: "/targets"},
	{Label: "Diff", Href: "/diff"},
	{Label: "Search", Href: "/search"},
}

// routes registers every handler on mux. In single-worktree mode it
// installs the familiar top-level routes; in multi-worktree mode it
// delegates to registerMultiRoutes which re-scopes content under
// /w/{name}/* and adds legacy redirects.
func (s *Server) routes(mux *nethttp.ServeMux) {
	if s.multiMode() {
		s.registerMultiRoutes(mux)
		return
	}
	// Static assets (CSS, htmx.min.js). Served from the embedded FS.
	mux.Handle("/assets/", nethttp.StripPrefix("/assets/", nethttp.FileServer(nethttp.FS(s.assets))))

	// D2 → SVG smoke endpoint. Used by M7b-f to render server-side
	// diagrams; accepts POST with a `d2` form field or raw text body.
	mux.HandleFunc("/render", s.handleRender)

	// /api/version is worktree-independent: register it at the top
	// level so single-mode and multi-mode share the same path.
	mux.HandleFunc("/api/version", s.handleAPIVersion)

	// M13: plugin transports. Routes are mounted before the catch-all
	// "/" so /api/plugins/<name>/... and /plugins/<name>/assets/... are
	// matched by their prefixes rather than falling through to the
	// dashboard handler.
	s.registerPluginRoutes(mux)

	// Content routes at their historical top-level paths.
	s.routesContent(mux)
	mux.HandleFunc("/", s.handleDashboard)
}

// registerPluginRoutes mounts every plugin-contributed HTTP handler
// under /api/plugins/<plugin-name><Path> and every plugin's UI
// Assets fs.FS under /plugins/<plugin-name>/assets/. No-op when the
// server was constructed without a BootstrapResult.
func (s *Server) registerPluginRoutes(mux *nethttp.ServeMux) {
	if !s.pluginsWired {
		return
	}
	plugin.MountPluginAPIHandlers(mux, s.plugins.HTTPHandlers)
	plugin.MountPluginAssetHandlers(mux, s.plugins.UIComponents)
}

// routesContent registers just the content pages onto mux without the
// static-asset or /render handlers. Shared between single-mode (where
// they live at the root) and multi-mode (where they live under
// /w/{name}/* via dispatchWorktree).
func (s *Server) routesContent(mux *nethttp.ServeMux) {
	mux.HandleFunc("/layers", s.handleLayers)
	mux.HandleFunc("/packages", s.handlePackagesList)
	mux.HandleFunc("/packages/", s.handlePackageDetail)
	// M7d: type detail pages + JSON graph endpoint for the client-side
	// Cytoscape renderer. The prefix registration lets /types/{id} carry
	// a slash-bearing package path ("internal/service.Service").
	mux.HandleFunc("/types/", s.handleType)
	mux.HandleFunc("/api/types/", s.handleTypeGraph)
	// M7d: configs catalog (overlay-driven config type detail pages).
	mux.HandleFunc("/configs", s.handleConfigs)
	// /search/results must be registered before /search so the prefix
	// mux treats it as a distinct route rather than falling back to the
	// full page handler.
	mux.HandleFunc("/search/results", s.handleSearchResults)
	mux.HandleFunc("/search", s.handleSearch)
	// M7e: diff + targets handlers replace the placeholder pageHandlers
	// for /diff and /targets and add sub-routes for target switching +
	// cross-target comparison.
	s.registerDiffTargetsRoutes(mux)
	// M8: cytoscape JSON APIs + D2/SVG export endpoints for Layers,
	// Packages, and Diff views (#46). Registered before the catch-all
	// "/" dashboard route so prefix matches resolve correctly.
	s.registerGraphRoutes(mux)
	// M11: JSON API used by the MCP thin-client wrapper. Registered
	// under /api/ so the browser UI and the machine API live side by
	// side on one listener.
	s.registerAPIRoutes(mux)
	// M14: bounded context list + detail + graph routes.
	s.registerBCRoutes(mux)
	// In multi mode, the root of a worktree ("/w/{name}/") is served
	// by the content mux at "/" after dispatchWorktree rewrites the
	// URL. In single mode the root is registered directly by routes()
	// so the handler precedence is identical (and duplicate
	// registration would panic).
	if s.multiMode() {
		mux.HandleFunc("/", s.handleDashboard)
	}
}

// pageHandler returns a handler that renders the named template inside
// the base layout with the given title and active-path marker.
func (s *Server) pageHandler(tmpl, title, activePath string) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		s.renderPage(w, tmpl, s.basePageData(r, title, activePath))
	}
}

// basePageData fills the shared pageData fields (title, active nav,
// worktree context) for a request. Handlers that build domain-specific
// page models should embed its return value.
func (s *Server) basePageData(r *nethttp.Request, title, activePath string) pageData {
	prefix := s.navPrefix(r)
	pd := pageData{
		Title:       title,
		ActivePath:  activePath,
		NavItems:    buildNavWithPrefix(activePath, prefix),
		NavPrefix:   prefix,
		MultiMode:   s.multiMode(),
		Worktree:    s.currentWorktree(r),
		CurrentPath: r.URL.Path,
		Version:     s.versionInfo(),
	}
	if s.multiMode() {
		pd.Worktrees = s.buildWorktreeList(r)
	}
	return pd
}

// renderPage renders the given page template followed by the base
// layout. We render into a buffer first so template errors produce a
// 500 instead of a half-written response.
//
// data is declared as `any` because M7e introduces page-specific model
// structs (diffPageData, targetsPageData) that embed pageData but are
// not themselves pageData values. The base template only reads the
// promoted fields (Title, ActivePath, NavItems), so passing the
// embedding struct works as long as those fields remain exported.
//
// The search page embeds the HTMX fragment template so it can render
// initial results inline; we parse both files together when the page
// pulls in the fragment.
func (s *Server) renderPage(w nethttp.ResponseWriter, tmpl string, data any) {
	// Clone so the page template and base template live in their own
	// namespace — each page file defines its own "content" block and
	// we don't want successive requests to see a stale definition.
	t, err := s.templates.Clone()
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("template clone: %v", err), nethttp.StatusInternalServerError)
		return
	}
	files := []string{"templates/" + tmpl}
	if tmpl == "search.html" {
		files = append(files, "templates/search_results.html")
	}
	if _, err := t.ParseFS(embedded, files...); err != nil {
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

// renderFragment renders a single named template from the given
// template file without the surrounding base layout. It is used by
// HTMX-driven handlers to return partials that replace an element
// in-place.
func (s *Server) renderFragment(w nethttp.ResponseWriter, tmpl, fragment string, data any) {
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
	if err := t.ExecuteTemplate(&buf, fragment, data); err != nil {
		nethttp.Error(w, fmt.Sprintf("template execute %s#%s: %v", tmpl, fragment, err), nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// handleRender converts arbitrary D2 source to SVG. Accepts:
//   - POST form field `d2`
//   - POST raw body (any Content-Type other than application/x-www-form-urlencoded)
//   - GET query param `d2` (convenient for smoke tests; not intended for large payloads)
func (s *Server) handleRender(w nethttp.ResponseWriter, r *nethttp.Request) {
	var source string
	switch r.Method {
	case nethttp.MethodGet:
		source = r.URL.Query().Get("d2")
	case nethttp.MethodPost:
		ct := r.Header.Get("Content-Type")
		if ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data" {
			_ = r.ParseForm()
			source = r.FormValue("d2")
		} else {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				nethttp.Error(w, "read body: "+err.Error(), nethttp.StatusBadRequest)
				return
			}
			source = string(body)
		}
	default:
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	if source == "" {
		nethttp.Error(w, "missing d2 source", nethttp.StatusBadRequest)
		return
	}

	svg, err := renderD2(r.Context(), source)
	if err != nil {
		nethttp.Error(w, "render: "+err.Error(), nethttp.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	_, _ = w.Write(svg)
}

// handleSearch renders the full search page. If a `q` query parameter
// is present we also run the search and inline the results so
// ?q=… links are shareable and work without JavaScript. HTMX takes
// over on subsequent keystrokes by hitting /search/results directly.
func (s *Server) handleSearch(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query().Get("q")
	kind := r.URL.Query().Get("kind")
	if !isKnownKind(kind) {
		kind = ""
	}

	var results []searchResult
	if q != "" {
		state := s.stateFor(r)
		if state != nil {
			snap := state.Snapshot()
			results = runSearch(snap.Packages, q, kind)
		}
	}

	s.renderPage(w, "search.html", searchPageData{
		pageData: s.basePageData(r, "Search", "/search"),
		Query:    q,
		Kind:     kind,
		Kinds:    searchKinds,
		Results:  results,
		Total:    len(results),
	})
}

// handleSearchResults serves the HTMX fragment used for
// search-as-you-type. It returns just the results list without the
// surrounding page chrome so HTMX can swap it into the results
// container on every keystroke.
func (s *Server) handleSearchResults(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query().Get("q")
	kind := r.URL.Query().Get("kind")
	if !isKnownKind(kind) {
		kind = ""
	}

	state := s.stateFor(r)
	var results []searchResult
	if state != nil {
		snap := state.Snapshot()
		results = runSearch(snap.Packages, q, kind)
	}

	// Fragment templates don't extend base.html, so we parse them on
	// their own with Clone() to avoid polluting the shared parsed set.
	t, err := s.templates.Clone()
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("template clone: %v", err), nethttp.StatusInternalServerError)
		return
	}
	if _, err := t.ParseFS(embedded, "templates/search_results.html"); err != nil {
		nethttp.Error(w, fmt.Sprintf("template parse: %v", err), nethttp.StatusInternalServerError)
		return
	}

	data := searchPageData{
		Query:   q,
		Kind:    kind,
		Kinds:   searchKinds,
		Results: results,
		Total:   len(results),
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "search_results", data); err != nil {
		nethttp.Error(w, fmt.Sprintf("template execute: %v", err), nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// buildNav returns a fresh nav slice with Active set on the item
// whose Href matches activePath. The source navTemplate is never
// mutated. Single-mode callers use this wrapper; multi-mode callers
// go through buildNavWithPrefix.
func buildNav(activePath string) []navItem {
	return buildNavWithPrefix(activePath, "")
}

// buildNavWithPrefix is like buildNav but rewrites each Href to
// <prefix><original-href> so multi-mode nav links stay inside the
// current worktree. "/" becomes "<prefix>/" rather than "<prefix>".
func buildNavWithPrefix(activePath, prefix string) []navItem {
	out := make([]navItem, len(navTemplate))
	for i, n := range navTemplate {
		n.Active = n.Href == activePath
		if prefix != "" {
			if n.Href == "/" {
				n.Href = prefix + "/"
			} else {
				n.Href = prefix + n.Href
			}
		}
		out[i] = n
	}
	return out
}
