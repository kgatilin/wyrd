package http

import (
	"encoding/json"
	nethttp "net/http"
	"sort"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
)

// handleModel serves GET /api/model?tree=<fingerprint>&reader=<key>: the Go
// model of this worktree as it is on disk now, for wyrd-check to run its
// gates over instead of parsing the module itself.
//
// The model is synced with the disk first, so an edit the watcher has not
// delivered yet is in the answer. It is answered only to a caller looking
// at the same thing: tree is the caller's treestamp fingerprint of the
// worktree, reader the modelstore.StampKey of its Go reader, and either one
// differing from the daemon's is 409 — the caller would get a model of other
// source, or one another reader version wrote, and parses locally instead.
// Packages come in the order the Go reader returns them, by import path.
func (s *Server) handleModel(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		w.Header().Set("Allow", nethttp.MethodGet)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	tree, reader := r.URL.Query().Get("tree"), r.URL.Query().Get("reader")
	if tree == "" || reader == "" {
		nethttp.Error(w, "tree and reader are required", nethttp.StatusBadRequest)
		return
	}
	if want := modelstore.StampKey(golang.ReaderStamp); reader != want {
		nethttp.Error(w, "reader "+reader+" differs from the daemon's "+want, nethttp.StatusConflict)
		return
	}
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "state unavailable", nethttp.StatusServiceUnavailable)
		return
	}
	moved, err := state.Sync(r.Context())
	if err != nil {
		nethttp.Error(w, "sync: "+err.Error(), nethttp.StatusInternalServerError)
		return
	}
	// The sync consumed the change the watcher would have broadcast.
	if len(moved) > 0 {
		state.PublishPackageReload(moved)
	}
	snap := state.Snapshot()
	if snap.Tree != tree {
		nethttp.Error(w, "the daemon's model is of another source tree", nethttp.StatusConflict)
		return
	}
	pkgs := snap.Packages
	sort.Slice(pkgs, func(i, j int) bool { return importOrder(pkgs[i].Path) < importOrder(pkgs[j].Path) })
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Packages any `json:"packages"`
	}{pkgs})
}

// importOrder is the part of a package's import path that orders it: the
// module root (".") sorts first, the rest by their path below the module.
func importOrder(pkgPath string) string {
	if pkgPath == "." {
		return ""
	}
	return "/" + pkgPath
}
