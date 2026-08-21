package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/archreview"
	"github.com/kgatilin/wyrd/internal/plugin"
	"github.com/kgatilin/wyrd/internal/serve"
)

// The daemon calls WarmWorktree through this port the moment a worktree's
// model is parsed; the assertion keeps the two in step at compile time.
var _ serve.WorktreeWarmer = (*Server)(nil)

// handleArchMotifReport serves the architecture review report for the active
// worktree. The report itself comes from the cache, which builds it through the
// same reviewBase the uigraph handler uses and the same git.Diff the file-diff
// handler uses, so the report, the canvas and the patch list always describe
// one working tree compared against one base.
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

	key := reportKey{worktree: s.currentWorktree(r), baseRef: reviewBaseRefOf(r)}
	if noCache(r) {
		s.reports.dropKey(key)
	}

	report, err := s.reports.get(r.Context(), key)
	if err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
		return
	}

	// The index status is daemon state rather than analysis: it moves as the
	// dense pass runs, on no model event at all. Stamped on the way out so a
	// report cached mid-indexing does not keep announcing it afterwards.
	writeJSON(w, report.WithIndex(indexStatusOf(state)))
}

// buildReport computes one report from scratch. It takes a worktree name rather
// than a request because warming has no request to read one off — and because
// the endpoint and the warm must compute the same thing, or the cache would be
// answering a question nobody asked.
func (s *Server) buildReport(ctx context.Context, key reportKey) (archreview.Report, error) {
	state, err := s.reportState(ctx, key.worktree)
	if err != nil {
		return archreview.Report{}, err
	}

	// Subscribed before the snapshot is taken, never after: a model event
	// landing in that gap would otherwise be missed by a cache entry built
	// from the older model, and nothing would ever correct it.
	s.watchModel(key.worktree, state)
	snap := state.Snapshot()

	var base serve.ReviewBase
	if s.multiMode() && key.worktree != "" {
		base, err = s.reviewBase(ctx, key.worktree, key.baseRef)
		if err != nil {
			return archreview.Report{}, err
		}
	}

	in := archreview.Input{
		Head:    snap.Packages,
		Base:    base.Models,
		Overlay: snap.Overlay,
		BaseRef: key.baseRef,
		BaseRev: base.Rev,
		Index:   indexStatusOf(state),
	}

	// The hunks are only meaningful against a base. Failing loudly when git
	// cannot produce them is deliberate: a report that quietly drops the
	// line-level half would read as "nothing else changed".
	if len(base.Models) > 0 {
		repoPath := s.worktreeRepoPath(key.worktree)
		if repoPath == "" {
			repoPath = snap.Root
		}
		if repoPath != "" {
			res, err := git.Diff(repoPath, key.baseRef)
			if err != nil {
				return archreview.Report{}, err
			}
			in.Changed = changedLines(res)
		}
	}

	return archreview.Build(in), nil
}

// WarmWorktree precomputes the worktree's report for the default base ref, in
// the background. It implements serve.WorktreeWarmer, so the daemon calls it
// the moment a worktree's model finishes parsing; the /api/warm ping an MCP
// client sends on attach calls it too.
//
// Only the default base is warmed. Any other ref is a miss that builds on
// demand — warming every ref anyone might name is work nobody asked for.
func (s *Server) WarmWorktree(name string, state *serve.State) {
	if state == nil {
		return
	}
	s.watchModel(name, state)
	s.reports.warm(reportKey{worktree: name, baseRef: defaultReviewBaseRef})
}

// watchModel subscribes the report cache to a worktree's model events, once per
// State. Every path that warms or serves a report calls it, because a cache
// that is not wired to the model does not fail loudly — it goes on serving a
// description of a working tree that has since changed.
//
// Keyed by the State rather than by the name so a worktree that is dropped and
// re-created subscribes to its new State's bus instead of listening to a bus
// nothing publishes on any more. The subscription itself is never cancelled: it
// lives exactly as long as the State it listens to.
func (s *Server) watchModel(worktree string, state *serve.State) {
	if state == nil {
		return
	}
	s.watchedMu.Lock()
	if s.watched[worktree] == state {
		s.watchedMu.Unlock()
		return
	}
	s.watched[worktree] = state
	s.watchedMu.Unlock()

	state.Bus().Subscribe(func(plugin.ModelEvent) {
		s.reports.drop(worktree)
		s.reports.warm(reportKey{worktree: worktree, baseRef: defaultReviewBaseRef})
	})
}

// reportState resolves the State a report is built from. It goes by worktree
// name because warming has no request whose context carries a dispatched state;
// in single-worktree mode the served state answers for the empty name.
func (s *Server) reportState(ctx context.Context, worktree string) (*serve.State, error) {
	if s.state != nil {
		return s.state, nil
	}
	if s.multi == nil {
		return nil, errors.New("no state available")
	}
	name := worktree
	if name == "" {
		name = s.multi.Default()
	}
	if name == "" {
		return nil, errors.New("no worktrees discovered")
	}
	return s.multi.Get(ctx, name)
}

// indexStatusOf reads the retrieval index's progress off a State. A State with
// no retrieval service reports the zero value, which reads as "not ready" and
// is exactly what such a daemon is.
func indexStatusOf(state *serve.State) archreview.IndexStatus {
	if state == nil {
		return archreview.IndexStatus{}
	}
	svc := state.Retrieval()
	if svc == nil {
		return archreview.IndexStatus{}
	}
	status := svc.IndexStatus()
	return archreview.IndexStatus{
		Ready:          status.DenseAvailable && !status.InProgress,
		Indexing:       status.InProgress,
		Embedded:       status.Embedded,
		Embeddable:     status.Embeddable,
		DenseAvailable: status.DenseAvailable,
	}
}

// reviewBaseRefOf is the base a review surface is asked about: the ?base= query
// the client sent, or the daemon's default. Shared by every review surface, so
// the canvas, the file diff and the report cannot disagree about which base the
// reviewer named.
func reviewBaseRefOf(r *nethttp.Request) string {
	if ref := r.URL.Query().Get("base"); ref != "" {
		return ref
	}
	return defaultReviewBaseRef
}

// noCache reports whether the client asked for a rebuilt answer rather than a
// stored one — the review panel's refresh button, which has to reach past a
// cache the daemon has no event to invalidate.
func noCache(r *nethttp.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Cache-Control")), "no-cache")
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
