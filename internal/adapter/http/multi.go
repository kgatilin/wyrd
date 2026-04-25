package http

import (
	"context"
	"fmt"
	nethttp "net/http"
	"sort"
	"strings"
)

// registerMultiRoutes installs the multi-worktree routing shell on
// mux. All content routes are re-scoped under /w/{name}/*; legacy top
// paths redirect to the current worktree; a small /worktree API lets
// the switcher dropdown set the cookie without JS heavy-lifting.
//
// Static assets (/assets/), /render, and the switcher endpoint stay
// at the top level because they do not depend on the active worktree.
func (s *Server) registerMultiRoutes(mux *nethttp.ServeMux) {
	mux.Handle("/assets/", nethttp.StripPrefix("/assets/", nethttp.FileServer(nethttp.FS(s.assets))))
	mux.HandleFunc("/render", s.handleRender)
	mux.HandleFunc("/worktree/select", s.handleWorktreeSelect)

	// M13: plugin routes live at the top level (not per worktree) so a
	// single asset bundle / API surface backs every worktree's UI.
	s.registerPluginRoutes(mux)

	// All /w/... URLs are dispatched through dispatchWorktree, which
	// strips the /w/<name> prefix, resolves the State, and hands off
	// to the content mux built by routesMux.
	contentMux := nethttp.NewServeMux()
	s.routesContent(contentMux)
	mux.Handle("/w/", s.dispatchWorktree(contentMux))

	// Legacy roots — redirect to the current worktree. We enumerate
	// them explicitly so unknown paths still 404 (rather than getting
	// silently rewritten).
	legacyRoots := []string{
		"/",
		"/layers",
		"/packages",
		"/packages/",
		"/configs",
		"/diff",
		"/diff/",
		"/targets",
		"/targets/",
		"/search",
		"/search/results",
		"/types/",
		"/api/types/",
	}
	for _, p := range legacyRoots {
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
		if !s.multi.Has(name) {
			nethttp.Error(w, "unknown worktree: "+name, nethttp.StatusNotFound)
			return
		}

		state, err := s.multi.Get(r.Context(), name)
		if err != nil {
			nethttp.Error(w, "load worktree: "+err.Error(), nethttp.StatusInternalServerError)
			return
		}

		// Rewrite the URL so the content mux sees /layers instead of
		// /w/foo/layers. We clone the URL so the original request is
		// unchanged (useful for logging/debugging middleware).
		r2 := r.Clone(r.Context())
		u := *r.URL
		u.Path = rest
		r2.URL = &u

		ctx := context.WithValue(r.Context(), ctxWorktreeName, name)
		ctx = context.WithValue(ctx, ctxWorktreeState, state)
		r2 = r2.WithContext(ctx)

		next.ServeHTTP(w, r2)
	})
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

// buildWorktreeList returns the switcher metadata (names + current
// selection) for the top-bar dropdown. The caller passes r so the
// current selection comes from context (inside dispatchWorktree) or
// cookie/default (top-level legacy handlers, which redirect anyway).
func (s *Server) buildWorktreeList(r *nethttp.Request) []worktreeOption {
	if s.multi == nil {
		return nil
	}
	cur := s.currentWorktree(r)
	entries := s.multi.Worktrees()
	out := make([]worktreeOption, 0, len(entries))
	for _, e := range entries {
		out = append(out, worktreeOption{
			Name:    e.Name,
			Branch:  e.Branch,
			Current: e.Name == cur,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// worktreeOption is one entry in the switcher dropdown.
type worktreeOption struct {
	Name    string
	Branch  string
	Current bool
}

// worktreeSwitcherLabel renders a compact label for the dropdown:
// "name (branch)" when Branch is set, else just the name.
func (o worktreeOption) Label() string {
	if o.Branch == "" {
		return o.Name
	}
	return fmt.Sprintf("%s (%s)", o.Name, o.Branch)
}
