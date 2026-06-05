package http

import (
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/publicapi"
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
	baseRef := r.URL.Query().Get("base")
	if baseRef == "" {
		baseRef = defaultReviewBaseRef
	}

	surface := publicapi.Project(snap.Packages)

	var baseWorktree string
	var publicDiff *publicapi.Diff
	if s.multiMode() && active != "" {
		baseState, name, err := s.baseStateForReview(r, baseRef)
		if err != nil {
			nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
			return
		}
		baseWorktree = name
		if baseState != nil && baseWorktree != "" && baseWorktree != active {
			baseSurface := publicapi.Project(baseState.Snapshot().Packages)
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
			BaseWorktree:   baseWorktree,
			Compare:        compareLabel(active, baseWorktree, baseRef),
		},
	})
}
