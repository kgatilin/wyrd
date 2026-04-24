// Package http implements the HTTP transport for the archai serve
// daemon. It exposes a browser UI (dashboard, layers, packages,
// configs, targets, diff, search) built on plain html/template + HTMX,
// plus a small /render endpoint used by the other M7 sub-milestones to
// turn D2 source into SVG.
//
// All templates and static assets are embedded at compile time via
// //go:embed so the compiled binary is fully self-contained.
//
// The transport supports two serving modes (M10):
//
//   - Single-worktree (default): NewServer(state) serves the familiar
//     routes (/, /layers, /packages, …) backed by one *serve.State.
//   - Multi-worktree: NewMultiServer(multi) serves the same routes
//     re-scoped under /w/{name}/* and adds redirects + a switcher so
//     one HTTP port can expose every discovered worktree at once.
package http

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/serve"
)

//go:embed templates/*.html assets
var embedded embed.FS

// Server is the HTTP transport. It wraps a net/http.Server and holds
// a reference to the shared serve.State (single mode) or a
// serve.MultiState (multi mode) so handlers can render snapshots
// without reloading the model.
type Server struct {
	state     *serve.State
	multi     *serve.MultiState
	templates *template.Template
	assets    fs.FS
}

// NewServer constructs a single-worktree Server backed by the given
// state. Templates are parsed eagerly so malformed templates fail at
// construction time rather than on the first request. This is the
// Mode A constructor: routes stay at their historical paths (/,
// /layers, …).
func NewServer(state *serve.State) (*Server, error) {
	if state == nil {
		return nil, errors.New("http: nil state")
	}
	tmpls, assets, err := parseEmbedded()
	if err != nil {
		return nil, err
	}
	return &Server{
		state:     state,
		templates: tmpls,
		assets:    assets,
	}, nil
}

// NewMultiServer constructs a multi-worktree Server backed by the
// given MultiState. All content routes are served under /w/{name}/*;
// legacy roots redirect to the cookie-selected (or first alphabetical)
// worktree so existing bookmarks still resolve.
func NewMultiServer(multi *serve.MultiState) (*Server, error) {
	if multi == nil {
		return nil, errors.New("http: nil multi-state")
	}
	tmpls, assets, err := parseEmbedded()
	if err != nil {
		return nil, err
	}
	return &Server{
		multi:     multi,
		templates: tmpls,
		assets:    assets,
	}, nil
}

// parseEmbedded reads the embedded templates and assets FS. Shared
// between the single- and multi-mode constructors.
func parseEmbedded() (*template.Template, fs.FS, error) {
	tmpls, err := template.New("").Funcs(templateFuncs()).ParseFS(embedded, "templates/*.html")
	if err != nil {
		return nil, nil, fmt.Errorf("http: parse templates: %w", err)
	}
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		return nil, nil, fmt.Errorf("http: assets sub-fs: %w", err)
	}
	return tmpls, assets, nil
}

// Serve listens on addr and serves HTTP requests until ctx is
// cancelled. It returns nil on a graceful shutdown and the underlying
// error otherwise. ready — when non-nil — is invoked exactly once
// after the listener binds successfully, with the actual bound
// address (useful when addr uses port 0). Callers are expected to
// bridge SIGINT/SIGTERM into ctx.
func (s *Server) Serve(ctx context.Context, addr string, ready func(boundAddr string)) error {
	mux := nethttp.NewServeMux()
	s.routes(mux)

	srv := &nethttp.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Listen synchronously so port binding errors are returned before
	// the goroutine fires.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("http: listen %s: %w", addr, err)
	}

	if ready != nil {
		ready(ln.Addr().String())
	}

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-serveErr:
		return err
	}
}

// multiMode reports whether this Server is serving multiple worktrees.
func (s *Server) multiMode() bool { return s.multi != nil }

// cookieName is the HTTP cookie storing the selected worktree in
// multi mode. Cleared on an invalid value.
const cookieName = "archai_worktree"

// ctxKey is the unexported type for request-context keys so other
// packages can't collide with ours.
type ctxKey int

const (
	ctxWorktreeName ctxKey = iota
	ctxWorktreeState
)

// stateFor returns the *serve.State that should answer r. In single
// mode it returns the fixed state; in multi mode it reads the state
// cached on the request context (populated by the /w/{name} dispatch
// middleware). When no context state is present it falls back to the
// default worktree's state so out-of-band handlers (e.g. /render)
// still work.
func (s *Server) stateFor(r *nethttp.Request) *serve.State {
	if s.state != nil {
		return s.state
	}
	if v := r.Context().Value(ctxWorktreeState); v != nil {
		if st, ok := v.(*serve.State); ok {
			return st
		}
	}
	// Fall back to the default worktree. This keeps top-level routes
	// like /render usable without forcing them through a worktree
	// dispatch.
	name := s.multi.Default()
	if name == "" {
		return nil
	}
	st, err := s.multi.Get(r.Context(), name)
	if err != nil {
		return nil
	}
	return st
}

// currentWorktree returns the worktree name in effect for r (empty in
// single mode).
func (s *Server) currentWorktree(r *nethttp.Request) string {
	if s.state != nil {
		return ""
	}
	if v := r.Context().Value(ctxWorktreeName); v != nil {
		if n, ok := v.(string); ok {
			return n
		}
	}
	return s.selectedWorktree(r)
}

// selectedWorktree returns the worktree the client has currently
// chosen: cookie value when valid, otherwise the default
// (first-alphabetical) worktree. Returns "" only when the MultiState
// has no worktrees at all.
func (s *Server) selectedWorktree(r *nethttp.Request) string {
	if s.multi == nil {
		return ""
	}
	if c, err := r.Cookie(cookieName); err == nil && c != nil {
		if s.multi.Has(c.Value) {
			return c.Value
		}
	}
	return s.multi.Default()
}

// navPrefix returns the URL prefix to prepend to internal links so
// they stay inside the active worktree. In single mode it returns "".
// In multi mode it returns "/w/<name>" (no trailing slash).
func (s *Server) navPrefix(r *nethttp.Request) string {
	name := s.currentWorktree(r)
	if name == "" {
		return ""
	}
	return "/w/" + name
}

// stripWorktreePrefix removes the "/w/<name>" prefix from path when
// present, returning (name, remainder). When path does not start with
// /w/, returns ("", path) unchanged.
func stripWorktreePrefix(path string) (name, rest string) {
	if !strings.HasPrefix(path, "/w/") {
		return "", path
	}
	trimmed := strings.TrimPrefix(path, "/w/")
	// Name runs up to the next "/" (or end of string).
	slash := strings.IndexByte(trimmed, '/')
	if slash < 0 {
		return trimmed, "/"
	}
	return trimmed[:slash], trimmed[slash:]
}
