package http

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/adapter/mcp"
	"github.com/kgatilin/archai/internal/serve"
)

// registerMultiRoutes installs the multi-worktree routing shell on
// mux. All content routes are re-scoped under /w/{name}/*; top-level
// API paths redirect to the current worktree; POST /worktree/select
// sets the cookie that decides which worktree those redirects land in.
//
// The review UI bundle, /api/version, the plugin surfaces and the
// switcher endpoint stay at the top level because they do not depend
// on the active worktree.
func (s *Server) registerMultiRoutes(mux *nethttp.ServeMux) {
	s.registerReviewUIRoutes(mux)
	mux.HandleFunc("/worktree/select", s.handleWorktreeSelect)
	// /api/version stays at the top level so its URL is the same in
	// single- and multi-worktree mode.
	mux.HandleFunc("/api/version", s.handleAPIVersion)

	// M13: plugin routes live at the top level (not per worktree) so a
	// single asset bundle / API surface backs every worktree's UI.
	s.registerPluginRoutes(mux)

	// All /w/... URLs are dispatched through dispatchWorktree, which
	// strips the /w/<name> prefix, resolves the State, and hands off
	// to the content mux built by routesMux.
	contentMux := nethttp.NewServeMux()
	s.routesContent(contentMux)
	mux.Handle("/w/", s.dispatchWorktree(contentMux))

	// Worktree-less API roots — redirect to the current worktree so a
	// caller that does not know the worktree name still resolves. We
	// enumerate them explicitly so unknown paths still 404 (rather than
	// getting silently rewritten).
	apiRoots := []string{
		"/api/uigraph",
		"/api/sequence",
		"/api/source",
		"/api/events",
		"/api/public-surface",
		"/api/gitdiff",
		"/api/archmotif/metrics",
		"/api/archmotif/embed",
	}
	if s.reviewUIEnabled() {
		mux.HandleFunc("/", s.handleReviewUIRoot)
	} else {
		apiRoots = append([]string{"/"}, apiRoots...)
	}
	for _, p := range apiRoots {
		path := p
		mux.HandleFunc(path, func(w nethttp.ResponseWriter, r *nethttp.Request) {
			s.redirectToWorktree(w, r)
		})
	}
}

// redirectToWorktree rewrites the request URL to its /w/{name}
// variant and issues a 302. When there are no discovered worktrees
// (should not happen post-Refresh), it returns a 503.
func (s *Server) redirectToWorktree(w nethttp.ResponseWriter, r *nethttp.Request) {
	name := s.selectedWorktree(r)
	if name == "" {
		nethttp.Error(w, "no worktrees discovered", nethttp.StatusServiceUnavailable)
		return
	}
	target := "/w/" + name + r.URL.Path
	// "/" becomes "/w/name/" — trim trailing slash for aesthetics.
	if r.URL.Path == "/" {
		target = "/w/" + name + "/"
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	nethttp.Redirect(w, r, target, nethttp.StatusFound)
}

// dispatchWorktree wraps the given content mux so that requests to
// /w/{name}/... are routed to the state for {name}. Unknown worktrees
// return 404. The resolved State and worktree name are stashed on the
// request context so downstream handlers can use stateFor/currentWorktree.
func (s *Server) dispatchWorktree(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		name, rest := stripWorktreePrefix(r.URL.Path)
		if name == "" {
			nethttp.NotFound(w, r)
			return
		}
		// EnsureKnown (not Has) so a worktree created after the daemon
		// started is picked up on its first request instead of 404ing
		// until a restart. The re-discovery is debounced, so a genuinely
		// unknown name still 404s cheaply.
		if !s.multi.EnsureKnown(name) {
			nethttp.Error(w, "unknown worktree: "+name, nethttp.StatusNotFound)
			return
		}

		if s.canServeWorktreeRouteWithoutState(rest) {
			r2 := s.rewriteWorktreeRequest(r, name, rest, nil)
			next.ServeHTTP(w, r2)
			return
		}

		// Warm hook: kick off this worktree's background parse and answer
		// immediately. An MCP client that attaches inside a linked worktree
		// pings this at startup so the cold go/packages parse overlaps with
		// the agent's first turn, instead of starting on the first tool call.
		// Deliberately does not wait for the load, so it never blocks.
		if rest == "/api/warm" {
			_, loaded := s.multi.Loaded(name)
			body := map[string]any{"worktree": name, "loaded": loaded}
			// A worktree whose load failed would otherwise look identical to
			// one that is merely cold; say so at the first touch.
			if _, loadErr := s.multi.LoadError(name); loadErr != nil {
				body["error"] = loadErr.Error()
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(nethttp.StatusAccepted)
			_ = json.NewEncoder(w).Encode(body)
			return
		}

		// The MCP tools/call transport must never block on the cold
		// go/packages parse: if the worktree's State isn't loaded yet,
		// kick off the background load and answer "loading" immediately
		// so the client (CLI `daemon status`, MCP thin-client) sees
		// progress instead of a transport timeout. Once parsed, the
		// per-tool indexingGate covers the slower dense-embedding phase.
		if rest == "/api/mcp/tools/call" {
			state, ok := s.multi.Loaded(name)
			if !ok {
				if retryAt, loadErr := s.multi.LoadError(name); loadErr != nil {
					writeFailedToolResult(w, name, loadErr, retryAt)
					return
				}
				writeLoadingToolResult(w)
				return
			}
			next.ServeHTTP(w, s.rewriteWorktreeRequest(r, name, rest, state))
			return
		}

		state, err := s.multi.Get(r.Context(), name)
		if err != nil {
			nethttp.Error(w, "load worktree: "+err.Error(), nethttp.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, s.rewriteWorktreeRequest(r, name, rest, state))
	})
}

// writeLoadingToolResult answers a tools/call with a non-error ToolResult whose
// text payload signals that the worktree model is still being parsed. Its shape
// mirrors the `status` tool / indexingGate so the CLI and MCP client render it
// uniformly: a JSON object with ready=false and a status/phase. This is the
// parse-phase counterpart to indexingGate's dense-embedding "indexing" gate.
func writeLoadingToolResult(w nethttp.ResponseWriter) {
	payload, _ := json.Marshal(map[string]any{
		"ready":  false,
		"status": "loading",
		"phase":  "parsing",
		"message": "Daemon is parsing the project model (cold start); analysis tools aren't ready yet. " +
			"Call `status` to watch progress and retry shortly.",
	})
	res := mcp.ToolResult{Content: []mcp.ToolResultContent{{Type: "text", Text: string(payload)}}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(res)
}

// writeFailedToolResult answers a tools/call for a worktree whose background
// load failed. It mirrors writeLoadingToolResult's shape so the CLI and MCP
// client render it uniformly, but reports phase "failed" plus the loader error
// — the caller must know the daemon is not going to become ready on its own,
// instead of polling an eternal "parsing".
func writeFailedToolResult(w nethttp.ResponseWriter, name string, loadErr error, retryAt time.Time) {
	retry := "shortly"
	if !retryAt.IsZero() {
		if d := time.Until(retryAt); d > 0 {
			retry = "in " + d.Round(time.Second).String()
		} else {
			retry = "on the next request"
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"ready":  false,
		"status": "failed",
		"phase":  "failed",
		"error":  loadErr.Error(),
		"message": fmt.Sprintf(
			"Daemon failed to load worktree %q; analysis tools are unavailable for it. "+
				"The load is retried %s — fix the reported error (see the daemon log) and retry.",
			name, retry),
	})
	res := mcp.ToolResult{Content: []mcp.ToolResultContent{{Type: "text", Text: string(payload)}}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) canServeWorktreeRouteWithoutState(rest string) bool {
	if !s.reviewUIEnabled() {
		return false
	}
	return rest == "/" ||
		rest == "/review" ||
		rest == "/review/" ||
		rest == "/review/index.html" ||
		strings.HasPrefix(rest, "/review/assets/")
}

func (s *Server) rewriteWorktreeRequest(r *nethttp.Request, name, rest string, state *serve.State) *nethttp.Request {
	// Rewrite the URL so the content mux sees /api/uigraph instead of
	// /w/foo/api/uigraph. We clone the URL so the original request is
	// unchanged (useful for logging/debugging middleware).
	r2 := r.Clone(r.Context())
	u := *r.URL
	u.Path = rest
	r2.URL = &u

	ctx := context.WithValue(r.Context(), ctxWorktreeName, name)
	if state != nil {
		ctx = context.WithValue(ctx, ctxWorktreeState, state)
	}
	return r2.WithContext(ctx)
}

// handleWorktreeSelect accepts POST /worktree/select with a `name`
// field and sets the archai_worktree cookie. The response redirects
// (303) to /w/{name}/<path> when the `redirect` field is a valid
// legacy-style path; otherwise it redirects to /w/{name}/.
//
// HTMX clients can set HX-Request and the handler swaps the whole
// content body by redirecting with HX-Redirect.
func (s *Server) handleWorktreeSelect(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		nethttp.Error(w, "parse form: "+err.Error(), nethttp.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name == "" {
		nethttp.Error(w, "missing name", nethttp.StatusBadRequest)
		return
	}
	if s.multi == nil || !s.multi.Has(name) {
		nethttp.Error(w, "unknown worktree: "+name, nethttp.StatusNotFound)
		return
	}

	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     cookieName,
		Value:    name,
		Path:     "/",
		HttpOnly: true,
		SameSite: nethttp.SameSiteLaxMode,
	})

	// Determine a redirect destination. Callers may pass "redirect"
	// to preserve the current page within the new worktree.
	redirect := r.FormValue("redirect")
	target := "/w/" + name + "/"
	if redirect != "" {
		target = rewriteForWorktree(redirect, name)
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(nethttp.StatusOK)
		return
	}
	nethttp.Redirect(w, r, target, nethttp.StatusSeeOther)
}

// rewriteForWorktree rewrites a canonical (legacy) path into its
// /w/{name}/<path> form. Paths already under /w/<other>/ are
// re-anchored to /w/{name}/. Absolute URLs (http://…) are ignored to
// prevent open redirects.
func rewriteForWorktree(redirect, name string) string {
	// Defensive: don't honor off-site redirects.
	if strings.Contains(redirect, "://") || strings.HasPrefix(redirect, "//") {
		return "/w/" + name + "/"
	}
	if redirect == "" || redirect == "/" {
		return "/w/" + name + "/"
	}
	if strings.HasPrefix(redirect, "/w/") {
		// Re-anchor to the new worktree.
		_, rest := stripWorktreePrefix(redirect)
		if rest == "" || rest == "/" {
			return "/w/" + name + "/"
		}
		return "/w/" + name + rest
	}
	if strings.HasPrefix(redirect, "/") {
		return "/w/" + name + redirect
	}
	return "/w/" + name + "/" + redirect
}
