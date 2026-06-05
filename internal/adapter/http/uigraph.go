package http

import (
	"fmt"
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/adapter/uigraph"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/publicapi"
	"github.com/kgatilin/archai/internal/serve"
)

const defaultReviewBaseRef = "main"

// handleUIGraphJSON serves the React review UI's graph document directly from
// the daemon state. In multi-worktree mode /w/{name}/api/uigraph compares the
// selected worktree against the worktree on branch "main" by default.
func (s *Server) handleUIGraphJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
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

	var baseWorktree string
	var d *diff.Diff
	var publicDiff *publicapi.Diff
	if s.multiMode() && active != "" {
		baseState, name, err := s.baseStateForReview(r, baseRef)
		if err != nil {
			nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
			return
		}
		baseWorktree = name
		if baseState != nil && baseWorktree != "" && baseWorktree != active {
			baseSnap := baseState.Snapshot()
			d = diff.Compute(snap.Packages, baseSnap.Packages)
			currentSurface := publicapi.Project(snap.Packages)
			baseSurface := publicapi.Project(baseSnap.Packages)
			pd := publicapi.Compare(currentSurface, baseSurface)
			publicDiff = &pd
		}
	}

	g, err := uigraph.ProjectWithPublicDiff(snap.Packages, snap.Overlay, d, publicDiff)
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("project uigraph: %v", err), nethttp.StatusInternalServerError)
		return
	}

	g.Repo = &uigraph.Repo{
		Root:           snap.Root,
		ActiveWorktree: active,
		BaseRef:        baseRef,
		BaseWorktree:   baseWorktree,
		Compare:        compareLabel(active, baseWorktree, baseRef),
	}
	g.Worktrees = s.reviewWorktrees(active, baseWorktree)
	if g.PR != nil {
		g.PR.Title = "Architecture Review"
		g.PR.Branch = active
		g.PR.Agent = "archai"
		if g.Repo.Compare != "" {
			g.PR.Summary = "Compared " + g.Repo.Compare
		}
	}

	writeJSON(w, g)
}

func (s *Server) baseStateForReview(r *nethttp.Request, baseRef string) (*serve.State, string, error) {
	if s.multi == nil || baseRef == "" {
		return nil, "", nil
	}
	state, name, err := s.multi.GetByRef(r.Context(), baseRef)
	if err != nil {
		return nil, name, fmt.Errorf("load base worktree %q: %w", name, err)
	}
	return state, name, nil
}

func (s *Server) reviewWorktrees(active, base string) []uigraph.Worktree {
	if s.multi == nil {
		return nil
	}
	entries := s.multi.Worktrees()
	out := make([]uigraph.Worktree, 0, len(entries))
	for _, e := range entries {
		out = append(out, uigraph.Worktree{
			Name:    e.Name,
			Branch:  e.Branch,
			Head:    e.Head,
			Current: e.Name == active,
			Base:    e.Name == base,
		})
	}
	return out
}

func compareLabel(active, baseWorktree, baseRef string) string {
	if active == "" {
		return ""
	}
	base := baseWorktree
	if base == "" {
		base = baseRef
	}
	if base == "" {
		return active
	}
	return active + " vs " + base
}
