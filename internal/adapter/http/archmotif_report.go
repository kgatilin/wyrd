package http

import (
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/adapter/git"
	"github.com/kgatilin/archai/internal/archreview"
	"github.com/kgatilin/archai/internal/serve"
)

// handleArchMotifReport serves the architecture review report for the active
// worktree. It resolves the base models through the same reviewBase the
// uigraph handler uses and the changed hunks through the same git.Diff the
// file-diff handler uses, so the report, the canvas and the patch list always
// describe one working tree compared against one base.
func (s *Server) handleArchMotifReport(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "state unavailable", nethttp.StatusServiceUnavailable)
		return
	}

	snap := state.Snapshot()
	baseRef := r.URL.Query().Get("base")
	if baseRef == "" {
		baseRef = defaultReviewBaseRef
	}

	var base serve.ReviewBase
	if active := s.currentWorktree(r); s.multiMode() && active != "" {
		var err error
		base, err = s.reviewBase(r, active, baseRef)
		if err != nil {
			nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
			return
		}
	}

	in := archreview.Input{
		Head:    snap.Packages,
		Base:    base.Models,
		Overlay: snap.Overlay,
		BaseRef: baseRef,
		BaseRev: base.Rev,
	}

	// The hunks are only meaningful against a base. Failing loudly when git
	// cannot produce them is deliberate: a report that quietly drops the
	// line-level half would read as "nothing else changed".
	if len(base.Models) > 0 {
		if repoPath, _ := s.repoPathFor(r); repoPath != "" {
			res, err := git.Diff(repoPath, baseRef)
			if err != nil {
				nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
				return
			}
			in.Changed = changedLines(res)
		}
	}

	if svc := state.Retrieval(); svc != nil {
		status := svc.IndexStatus()
		in.Index = archreview.IndexStatus{
			Ready:          status.DenseAvailable && !status.InProgress,
			Indexing:       status.InProgress,
			Embedded:       status.Embedded,
			Embeddable:     status.Embeddable,
			DenseAvailable: status.DenseAvailable,
		}
	}

	writeJSON(w, archreview.Build(in))
}

// changedLines maps each changed file to the line ranges its patch touched,
// keyed by module-relative path — the same key a symbol's span carries.
func changedLines(res git.Result) map[string][]archreview.LineRange {
	out := make(map[string][]archreview.LineRange, len(res.Files))
	for _, f := range res.Files {
		if f.Binary {
			continue
		}
		hunks := git.Hunks(f.Patch)
		ranges := make([]archreview.LineRange, 0, len(hunks))
		for _, h := range hunks {
			ranges = append(ranges, archreview.LineRange{Start: h.Start, End: h.End})
		}
		out[f.Path] = ranges
	}
	return out
}
