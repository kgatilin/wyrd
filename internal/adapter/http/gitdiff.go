package http

import (
	nethttp "net/http"

	"github.com/kgatilin/archai/internal/adapter/git"
)

// gitDiffSchema versions the payload consumed by the review UI's file diff.
const gitDiffSchema = "archai.gitdiff/1"

type gitDiffFileJSON struct {
	Path       string `json:"path"`
	OldPath    string `json:"oldPath,omitempty"`
	Status     string `json:"status"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Binary     bool   `json:"binary,omitempty"`
	Untracked  bool   `json:"untracked,omitempty"`
	Patch      string `json:"patch,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type gitDiffStatsJSON struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

type gitDiffJSON struct {
	Schema   string            `json:"schema"`
	Worktree string            `json:"worktree,omitempty"`
	Branch   string            `json:"branch"`
	BaseRef  string            `json:"baseRef"`
	BaseRev  string            `json:"baseRev"`
	Files    []gitDiffFileJSON `json:"files"`
	Stats    gitDiffStatsJSON  `json:"stats"`
}

// handleGitDiffJSON serves the textual working-tree diff of the active
// worktree against the review base. It is deliberately independent of the
// model snapshot: /api/uigraph answers "what changed architecturally",
// this answers "what changed in the files", and a reviewer wants both
// without waiting for a parse.
func (s *Server) handleGitDiffJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	repoPath, worktree := s.repoPathFor(r)
	if repoPath == "" {
		nethttp.Error(w, "no worktree available", nethttp.StatusServiceUnavailable)
		return
	}

	baseRef := r.URL.Query().Get("base")
	if baseRef == "" {
		baseRef = defaultReviewBaseRef
	}

	res, err := git.Diff(repoPath, baseRef)
	if err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
		return
	}

	out := gitDiffJSON{
		Schema:   gitDiffSchema,
		Worktree: worktree,
		Branch:   res.Branch,
		BaseRef:  res.BaseRef,
		BaseRev:  res.BaseRev,
		Files:    make([]gitDiffFileJSON, 0, len(res.Files)),
	}
	for _, f := range res.Files {
		out.Files = append(out.Files, gitDiffFileJSON{
			Path:       f.Path,
			OldPath:    f.OldPath,
			Status:     f.Status,
			Insertions: f.Insertions,
			Deletions:  f.Deletions,
			Binary:     f.Binary,
			Untracked:  f.Untracked,
			Patch:      f.Patch,
			Truncated:  f.Truncated,
		})
		out.Stats.Insertions += f.Insertions
		out.Stats.Deletions += f.Deletions
	}
	out.Stats.Files = len(out.Files)

	writeJSON(w, out)
}

// repoPathFor resolves the on-disk directory whose git state answers r:
// the dispatched worktree in multi mode, the served root otherwise.
func (s *Server) repoPathFor(r *nethttp.Request) (string, string) {
	if name := s.currentWorktree(r); name != "" && s.multi != nil {
		if entry, ok := s.multi.Entry(name); ok && entry.Path != "" {
			return entry.Path, name
		}
	}
	state := s.stateFor(r)
	if state == nil {
		return "", ""
	}
	return state.Snapshot().Root, s.currentWorktree(r)
}
