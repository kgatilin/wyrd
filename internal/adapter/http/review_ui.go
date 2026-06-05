package http

import (
	"io/fs"
	nethttp "net/http"
	"strings"
)

const reviewUIPrefix = "/review/"

func (s *Server) registerReviewUIRoutes(mux *nethttp.ServeMux) {
	if !s.reviewUIEnabled() {
		return
	}
	mux.HandleFunc("/review", s.handleReviewUIBare)
	mux.Handle("/review/assets/", nethttp.StripPrefix("/review/", nethttp.FileServer(nethttp.FS(s.reviewUI))))
	mux.HandleFunc(reviewUIPrefix, s.handleReviewUIIndex)
}

func (s *Server) handleReviewUIBare(w nethttp.ResponseWriter, r *nethttp.Request) {
	nethttp.Redirect(w, r, s.reviewUIPathFor(r), nethttp.StatusMovedPermanently)
}

func (s *Server) handleReviewUIRoot(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.URL.Path != "/" {
		nethttp.NotFound(w, r)
		return
	}
	nethttp.Redirect(w, r, s.reviewUIPathFor(r), nethttp.StatusFound)
}

func (s *Server) handleWorktreeReviewUIRoot(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.URL.Path != "/" {
		nethttp.NotFound(w, r)
		return
	}
	nethttp.Redirect(w, r, s.reviewUIPathFor(r), nethttp.StatusFound)
}

func (s *Server) handleReviewUIIndex(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.reviewUIEnabled() {
		nethttp.NotFound(w, r)
		return
	}
	if r.URL.Path != reviewUIPrefix && r.URL.Path != "/review/index.html" {
		nethttp.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.reviewUI, "index.html")
	if err != nil {
		nethttp.Error(w, "review UI index not found", nethttp.StatusInternalServerError)
		return
	}
	html := string(data)
	assetPrefix := strings.TrimRight(s.reviewUIPathFor(r), "/")
	html = strings.ReplaceAll(html, `="/assets/`, `="`+assetPrefix+`/assets/`)
	html = strings.ReplaceAll(html, `='/assets/`, `='`+assetPrefix+`/assets/`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (s *Server) reviewUIPathFor(r *nethttp.Request) string {
	if !s.multiMode() {
		return reviewUIPrefix
	}
	prefix := s.navPrefix(r)
	if prefix == "" {
		name := s.selectedWorktree(r)
		if name != "" {
			prefix = "/w/" + name
		}
	}
	if prefix == "" {
		return reviewUIPrefix
	}
	return prefix + reviewUIPrefix
}
