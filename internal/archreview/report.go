// Package archreview builds the architecture review report: what a branch did
// to the structure and where, when the worktree has a base to compare against,
// and what to refactor next when it does not.
//
// It is transport-free — no HTTP, no MCP — and owns both the computation and
// the report's wording. A section exists only for a state a reviewer acts on,
// so every row is a state, an action and a click target. A section whose state
// has not occurred renders as one "ok" line: a clean branch yields a handful of
// "none" lines rather than a grid of figures nobody reads.
//
// The analysis is not written here. The package model becomes an archmotif
// graph through internal/adapter/archmotif and is measured by archmotif's
// trophic, components and filestats lenses — the same ones the MCP tools
// expose — while policy tags come from internal/policy. Only the
// strongly-connected-component search over the group graph is local: Go forbids
// package import cycles, so cycles exist one level up, on a graph archmotif is
// never handed.
//
// Click targets are expressed in uigraph's id conventions (component id =
// package path, internal id = "{package}.{Symbol}", member id =
// "{package}.{Type}.{Member}", file = module-relative path) so the panel maps a
// row to a canvas action without translating anything.
package archreview

import (
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// Schema versions the payload the review UI consumes.
const Schema = "archai.archreview/v1"

// Report modes. Review mode answers "did this branch make it worse, where?";
// repo mode answers "what to refactor next?".
const (
	ModeReview = "review"
	ModeRepo   = "repo"
)

// Section states. A section is "ok" exactly when its actionable state has not
// occurred, and the panel collapses it to a single line.
const (
	StateOK   = "ok"
	StateFlag = "flag"
)

// Edge tags on new cross-package edges, in descending order of urgency.
const (
	TagPolicy   = "policy"
	TagBackward = "backward"
	TagOK       = "ok"
	// TagDead marks an unused export with no incoming edge at all.
	TagDead = "dead"
	// TagGrew marks a group cycle that existed at base and gained a member.
	TagGrew = "grew"
	// TagNew marks a group cycle absent at base.
	TagNew = "new"
)

// LineRange is an inclusive, 1-based span of changed lines in the post-change
// file, as a git hunk reports it.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// IndexStatus mirrors the retrieval service's indexing progress. It reaches the
// report only when the index is in the reviewer's way — still building, or
// missing an embedder — and is omitted once it is ready.
type IndexStatus struct {
	Ready          bool `json:"ready"`
	Indexing       bool `json:"indexing"`
	Embedded       int  `json:"embedded"`
	Embeddable     int  `json:"embeddable"`
	DenseAvailable bool `json:"denseAvailable"`
}

// Input is everything Build needs. It is deliberately plain data: the caller
// resolves the base models, the git hunks and the index status, so the report
// itself never touches a repository or a daemon.
type Input struct {
	// Head is the model of the worktree under review.
	Head []domain.PackageModel
	// Base is the model of merge-base(baseRef, HEAD). Nil, or a model that
	// turns out identical to Head, selects repo mode.
	Base []domain.PackageModel
	// Overlay carries the groups, ports, policy and layers the report reads.
	Overlay *overlay.Config
	// BaseRef and BaseRev name the comparison in review mode.
	BaseRef string
	BaseRev string
	// Changed maps a module-relative file path to the line ranges the git
	// diff touched. Nil in repo mode.
	Changed map[string][]LineRange
	// Index is the retrieval index's state; reported only when not ready.
	Index IndexStatus
}

// Report is the whole answer, in severity order.
type Report struct {
	Schema   string       `json:"schema"`
	Mode     string       `json:"mode"`
	Base     *Base        `json:"base,omitempty"`
	Sections []Section    `json:"sections"`
	Totals   Totals       `json:"totals"`
	Index    *IndexStatus `json:"index,omitempty"`
	// Warnings records analysis that could not run. It is empty in the
	// normal case; a non-empty entry means a section is missing, not that a
	// section is clean, and the panel must say so rather than read silence
	// as a pass.
	Warnings []string `json:"warnings,omitempty"`
}

// Base names what the branch was compared against.
type Base struct {
	Ref string `json:"ref"`
	Rev string `json:"rev"`
}

// Totals is the muted footer: the size of the graph the sections were read
// off, and nothing a reviewer is expected to act on.
type Totals struct {
	Packages   int `json:"packages"`
	Edges      int `json:"edges"`
	Components int `json:"components"`
}

// Section is one actionable state. Summary carries the section's own wording:
// the "none" line when State is "ok", and the heading when it is not — the
// place a figure like the trophic F0 belongs, attached to the list it explains
// rather than standing alone.
type Section struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Severity int    `json:"severity"`
	State    string `json:"state"`
	Count    int    `json:"count"`
	Summary  string `json:"summary"`
	Items    []Item `json:"items"`
	// More is how many further occurrences the list omits, so a capped
	// section says "and N more" instead of pretending it is complete.
	More int `json:"more,omitempty"`
}

// Item is one row: what happened (Text), what to do about it (Detail) and
// where to click (Target).
type Item struct {
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Target Target `json:"target"`
}

// Target is the row's click target in uigraph id conventions. Edge names the
// single edge a row is about; Edges is the set to highlight together, which is
// how a cycle is shown.
type Target struct {
	ComponentID string `json:"componentId,omitempty"`
	InternalID  string `json:"internalId,omitempty"`
	MemberID    string `json:"memberId,omitempty"`
	File        string `json:"file,omitempty"`
	Edge        *Edge  `json:"edge,omitempty"`
	Edges       []Edge `json:"edges,omitempty"`
}

// Edge is a package-to-package edge, both ends component ids.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}
