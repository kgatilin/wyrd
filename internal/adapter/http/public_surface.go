package http

import (
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/publicapi"
	"github.com/kgatilin/archai/internal/serve"
)

const publicSurfaceResponseSchema = "archai.public-surface-review/v0"

type publicSurfaceResponse struct {
	Schema  string            `json:"schema"`
	Repo    publicSurfaceRepo `json:"repo"`
	Surface publicapi.Surface `json:"surface"`
	Diff    *publicapi.Diff   `json:"diff,omitempty"`
}

type publicSurfaceRepo struct {
	Root           string `json:"root,omitempty"`
	ActiveWorktree string `json:"activeWorktree,omitempty"`
	BaseRef        string `json:"baseRef,omitempty"`
	BaseWorktree   string `json:"baseWorktree,omitempty"`
	BaseRev        string `json:"baseRev,omitempty"`
	Compare        string `json:"compare,omitempty"`
}

func (s *Server) handlePublicSurfaceJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}

	snap := state.Snapshot()
	active := s.currentWorktree(r)
	baseRef := reviewBaseRefOf(r)

	surface := publicapi.Project(snap.Packages)

	var base serve.ReviewBase
	var publicDiff *publicapi.Diff
	if s.multiMode() && active != "" {
		var err error
		base, err = s.reviewBase(r.Context(), active, baseRef)
		if err != nil {
			nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
			return
		}
		if len(base.Models) > 0 {
			baseSurface := publicapi.Project(base.Models)
			d := publicapi.Compare(surface, baseSurface)
			publicDiff = &d
		}
	}

	writeJSON(w, publicSurfaceResponse{
		Schema:  publicSurfaceResponseSchema,
		Surface: surface,
		Diff:    publicDiff,
		Repo: publicSurfaceRepo{
			Root:           snap.Root,
			ActiveWorktree: active,
			BaseRef:        baseRef,
			BaseWorktree:   base.Worktree,
			BaseRev:        base.Rev,
			Compare:        compareLabel(active, base.Worktree, baseRef, base.Rev),
		},
	})
}
