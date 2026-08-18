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

	var base serve.ReviewBase
	var d *diff.Diff
	var publicDiff *publicapi.Diff
	if s.multiMode() && active != "" {
		var err error
		base, err = s.reviewBase(r, active, baseRef)
		if err != nil {
			nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
			return
		}
		if len(base.Models) > 0 {
			d = diff.Compute(snap.Packages, base.Models)
			currentSurface := publicapi.Project(snap.Packages)
			baseSurface := publicapi.Project(base.Models)
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
		BaseWorktree:   base.Worktree,
		BaseRev:        base.Rev,
		Compare:        compareLabel(active, base.Worktree, baseRef, base.Rev),
	}
	g.Worktrees = s.reviewWorktrees(active, base.Worktree)
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

// reviewBase resolves what the active worktree is reviewed against: the
// models of merge-base(baseRef, HEAD). Both review surfaces go through it so
// the architecture diff and the file diff answer the same question — the one
// the reviewer asked, "what did this branch change", rather than "how does
// this branch differ from wherever the base has got to".
func (s *Server) reviewBase(r *nethttp.Request, active, baseRef string) (serve.ReviewBase, error) {
	if s.multi == nil || baseRef == "" || active == "" {
		return serve.ReviewBase{}, nil
	}
	base, err := s.multi.ReviewBase(r.Context(), active, baseRef)
	if err != nil {
		return base, fmt.Errorf("resolve review base %q: %w", baseRef, err)
	}
	return base, nil
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

// compareLabel names the comparison, including the commit it was taken
// from: without the revision, "feature vs main" hides which main.
func compareLabel(active, baseWorktree, baseRef, baseRev string) string {
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
	if baseRev != "" {
		base += "@" + shortRev(baseRev)
	}
	return active + " vs " + base
}

func shortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
