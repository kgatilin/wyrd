package http

import (
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/plugin"
)

// routes registers every handler on mux. In single-worktree mode it
// installs the familiar top-level routes; in multi-worktree mode it
// delegates to registerMultiRoutes which re-scopes content under
// /w/{name}/* and adds legacy redirects.
func (s *Server) routes(mux *nethttp.ServeMux) {
	if s.multiMode() {
		s.registerMultiRoutes(mux)
		return
	}
	s.registerReviewUIRoutes(mux)

	// /api/version is worktree-independent: register it at the top
	// level so single-mode and multi-mode share the same path.
	mux.HandleFunc("/api/version", s.handleAPIVersion)

	// M13: plugin transports. Routes are mounted before the review UI
	// root so /api/plugins/<name>/... and /plugins/<name>/assets/... are
	// matched by their prefixes.
	s.registerPluginRoutes(mux)

	// Content routes at their historical top-level paths.
	s.routesContent(mux)
	mux.HandleFunc("/", s.handleReviewUIRoot)
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

// routesContent registers the per-worktree API surface onto mux.
// Shared between single-mode (where it lives at the root) and
// multi-mode (where it lives under /w/{name}/* via dispatchWorktree).
func (s *Server) routesContent(mux *nethttp.ServeMux) {
	if s.multiMode() {
		s.registerReviewUIRoutes(mux)
	}
	// Review UI data. These routes are backed by the live daemon
	// snapshot, so the UI does not need a manually-exported archgraph.json.
	mux.HandleFunc("/api/uigraph", s.handleUIGraphJSON)
	mux.HandleFunc("/api/sequence", s.handleSequenceJSON)
	mux.HandleFunc("/api/source", s.handleSourceFileJSON)
	mux.HandleFunc("/api/events", s.handleModelEvents)
	mux.HandleFunc("/api/public-surface", s.handlePublicSurfaceJSON)
	mux.HandleFunc("/api/gitdiff", s.handleGitDiffJSON)
	mux.HandleFunc("/api/archmotif/report", s.handleArchMotifReport)
	mux.HandleFunc("/api/archmotif/domains", s.handleArchMotifDomains)
	// M11: JSON API used by the MCP thin-client wrapper. Registered
	// under /api/ so the browser UI and the machine API live side by
	// side on one listener.
	s.registerAPIRoutes(mux)
	// Retrieval API: search, expand, node detail, refresh.
	s.registerRetrievalRoutes(mux)
	// In multi mode, the root of a worktree ("/w/{name}/") is served
	// by the content mux at "/" after dispatchWorktree rewrites the
	// URL. In single mode the root is registered directly by routes()
	// so the handler precedence is identical (and duplicate
	// registration would panic).
	if s.multiMode() {
		mux.HandleFunc("/", s.handleWorktreeReviewUIRoot)
	}
}
