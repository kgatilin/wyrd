package http

import (
	"fmt"
	nethttp "net/http"
	"strings"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/diff"
)

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
