package http

import (
	nethttp "net/http"

	"github.com/kgatilin/wyrd/internal/plugin"
	"github.com/kgatilin/wyrd/internal/serve"
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

	// M13: plugin asset bundles. Mounted before the review UI root so
	// /plugins/<name>/assets/... is matched by its prefix. The API routes
	// come with the content routes below, because what a plugin answers is
	// a property of the worktree being served.
	s.registerPluginAssetRoutes(mux)

	// Content routes at their historical top-level paths.
	s.routesContent(mux)
	mux.HandleFunc("/", s.handleReviewUIRoot)
}

// registerPluginAPIRoutes mounts every plugin-contributed HTTP handler
// under /api/plugins/<plugin-name><Path>, wrapped so the handler sees the
// Host of the worktree it is answering for. No-op when the server was
// constructed without a BootstrapResult.
//
// These go on the content mux, not the top-level one: a plugin's answer is
// read out of the worktree being served, so /w/<name>/api/plugins/... has to
// reach a differently-scoped plugin for each <name>.
func (s *Server) registerPluginAPIRoutes(mux *nethttp.ServeMux) {
	if !s.pluginsWired {
		return
	}
	plugin.MountPluginAPIHandlers(mux, s.plugins.HTTPHandlers, s.withPluginHost)
}

// registerPluginAssetRoutes mounts every plugin's UI Assets fs.FS under
// /plugins/<plugin-name>/assets/. Bundles are the same bytes for every
// worktree, so they stay at the top level and one browser cache serves all
// of them. No-op when the server was constructed without a BootstrapResult.
func (s *Server) registerPluginAssetRoutes(mux *nethttp.ServeMux) {
	if !s.pluginsWired {
		return
	}
	plugin.MountPluginAssetHandlers(mux, s.plugins.UIComponents)
}

// withPluginHost wraps next so the request carries the plugin.Host of the
// worktree it resolved to. Without it a plugin can only answer from the Host
// it was handed at bootstrap, which in a repo-level daemon names no worktree
// in particular.
func (s *Server) withPluginHost(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if host := s.pluginHost(r); host != nil {
			r = r.WithContext(plugin.ContextWithHost(r.Context(), host))
		}
		next.ServeHTTP(w, r)
	})
}

// pluginHost builds the Host that answers for r's worktree, or nil when no
// state resolves (the plugin then falls back to its bootstrap Host).
func (s *Server) pluginHost(r *nethttp.Request) plugin.Host {
	state := s.stateFor(r)
	if state == nil {
		return nil
	}
	return serve.NewHost(state, nil)
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
	// Plugin API routes are per-worktree for the same reason the routes
	// above are: they answer out of one worktree's tree.
	s.registerPluginAPIRoutes(mux)
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
