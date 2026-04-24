// Package http implements the HTTP transport for the archai serve
// daemon. It exposes a browser UI (dashboard, layers, packages,
// configs, targets, diff, search) built on plain html/template + HTMX,
// plus a small /render endpoint used by the other M7 sub-milestones to
// turn D2 source into SVG.
//
// All templates and static assets are embedded at compile time via
// //go:embed so the compiled binary is fully self-contained.
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
	"time"

	"github.com/kgatilin/archai/internal/serve"
)

//go:embed templates/*.html assets
var embedded embed.FS

// Server is the HTTP transport. It wraps a net/http.Server and holds
// a reference to the shared serve.State so handlers can render
// snapshots without reloading the model.
type Server struct {
	state     *serve.State
	templates *template.Template
	assets    fs.FS
}

// NewServer constructs a Server backed by the given state. Templates
// are parsed eagerly so malformed templates fail at construction time
// rather than on the first request.
func NewServer(state *serve.State) (*Server, error) {
	if state == nil {
		return nil, errors.New("http: nil state")
	}

	tmpls, err := template.New("").Funcs(templateFuncs()).ParseFS(embedded, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("http: parse templates: %w", err)
	}

	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		return nil, fmt.Errorf("http: assets sub-fs: %w", err)
	}

	return &Server{
		state:     state,
		templates: tmpls,
		assets:    assets,
	}, nil
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
