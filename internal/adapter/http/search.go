package http

import (
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// searchResult is one entry in the search results list. It identifies a
// single symbol (package, file, interface, struct, function, type, const,
// variable, error) and carries the metadata needed to render a link to
// its detail page.
type searchResult struct {
	// Kind is the symbol category used for filtering and display. One of
	// searchKind* constants below.
	Kind string

	// Name is the short display name (e.g. "Service" for a struct,
	// "internal/service" for a package, "reader.go" for a file).
	Name string

	// Package is the owning package path (empty for package/file kinds
	// where it would be redundant with Name).
	Package string

	// File is the source file path (empty for package/file kinds and for
	// aggregated locations that don't map to a single file).
	File string

	// Href is the detail-page URL the template should link to. The M7
	// detail routes are owned by sibling milestones; this handler links
	// to conventional paths so results navigate once those pages land.
	Href string

	// score ranks the match quality. Lower scores sort first. Unexported
	// because it is an implementation detail of ranking.
	score int
}

// Search "kind" values. Kept as string constants so templates and URL
// query parameters can share the same vocabulary.
const (
	searchKindPackage   = "package"
	searchKindFile      = "file"
	searchKindInterface = "interface"
	searchKindStruct    = "struct"
	searchKindFunction  = "function"
	searchKindType      = "type"
	searchKindConst     = "const"
	searchKindVar       = "var"
	searchKindError     = "error"
)

// searchKinds is the canonical ordered list of kinds. Used by the UI to
// render the kind-filter dropdown and to validate incoming `kind` query
// parameters.
var searchKinds = []string{
	searchKindPackage,
	searchKindFile,
	searchKindInterface,
	searchKindStruct,
	searchKindFunction,
	searchKindType,
	searchKindConst,
	searchKindVar,
	searchKindError,
}

// isKnownKind reports whether k is one of the accepted kind filters.
// The empty string (no filter) is also considered valid.
func isKnownKind(k string) bool {
	if k == "" {
		return true
	}
	for _, known := range searchKinds {
		if known == k {
			return true
		}
	}
	return false
}

// searchLimit caps the number of results returned per query. The UI can
// re-query with a narrower term if it hits the cap; bounded output keeps
// HTMX fragments small and ranking stable.
const searchLimit = 50

// runSearch indexes the given packages on the fly and returns up to
// searchLimit results that match query, restricted to kind when kind is
// non-empty. Matching is case-insensitive; scoring prefers exact match
// over prefix over substring over fuzzy subsequence.
//
// The index is rebuilt per query rather than cached on State because the
// model mutates when the daemon reloads packages; per-query indexing
// also keeps this package independent of serve.State internals.
func runSearch(pkgs []domain.PackageModel, query, kind string) []searchResult {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	qLower := strings.ToLower(q)

	var out []searchResult
	add := func(r searchResult) {
		if kind != "" && r.Kind != kind {
			return
		}
		score, ok := matchScore(r.Name, qLower)
		if !ok {
			// Also check package/file fields so "serve" matches
			// "internal/serve" as a package search target.
			altScore, altOk := matchScore(r.Package, qLower)
			if altOk {
				score = altScore + 100 // alt-field matches rank below name matches
			} else {
				altScore, altOk = matchScore(r.File, qLower)
				if !altOk {
					return
				}
				score = altScore + 200
			}
		}
		r.score = score
		out = append(out, r)
	}

	for _, p := range pkgs {
		add(searchResult{
			Kind:    searchKindPackage,
			Name:    p.Path,
			Package: p.Path,
			Href:    "/packages/" + p.Path,
		})
		for _, f := range p.SourceFiles() {
			add(searchResult{
				Kind:    searchKindFile,
				Name:    f,
				Package: p.Path,
				File:    f,
				Href:    "/packages/" + p.Path + "#file-" + f,
			})
		}
		for _, iface := range p.Interfaces {
			add(searchResult{
				Kind:    searchKindInterface,
				Name:    iface.Name,
				Package: p.Path,
				File:    iface.SourceFile,
				Href:    "/packages/" + p.Path + "#interface-" + iface.Name,
			})
		}
		for _, s := range p.Structs {
			add(searchResult{
				Kind:    searchKindStruct,
				Name:    s.Name,
				Package: p.Path,
				File:    s.SourceFile,
				Href:    "/packages/" + p.Path + "#struct-" + s.Name,
			})
		}
		for _, fn := range p.Functions {
			add(searchResult{
				Kind:    searchKindFunction,
				Name:    fn.Name,
				Package: p.Path,
				File:    fn.SourceFile,
				Href:    "/packages/" + p.Path + "#function-" + fn.Name,
			})
		}
		for _, td := range p.TypeDefs {
			add(searchResult{
				Kind:    searchKindType,
				Name:    td.Name,
				Package: p.Path,
				File:    td.SourceFile,
				Href:    "/packages/" + p.Path + "#type-" + td.Name,
			})
		}
		for _, c := range p.Constants {
			add(searchResult{
				Kind:    searchKindConst,
				Name:    c.Name,
				Package: p.Path,
				File:    c.SourceFile,
				Href:    "/packages/" + p.Path + "#const-" + c.Name,
			})
		}
		for _, v := range p.Variables {
			add(searchResult{
				Kind:    searchKindVar,
				Name:    v.Name,
				Package: p.Path,
				File:    v.SourceFile,
				Href:    "/packages/" + p.Path + "#var-" + v.Name,
			})
		}
		for _, e := range p.Errors {
			add(searchResult{
				Kind:    searchKindError,
				Name:    e.Name,
				Package: p.Path,
				File:    e.SourceFile,
				Href:    "/packages/" + p.Path + "#error-" + e.Name,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Package < out[j].Package
	})

	if len(out) > searchLimit {
		out = out[:searchLimit]
	}
	return out
}

// matchScore reports whether qLower matches target and returns a rank
// score (lower is better). qLower must already be lowercased by the
// caller. The scoring tiers are:
//
//	0   exact match (case-insensitive)
//	1   target starts with query (prefix)
//	2   substring match
//	3+  fuzzy subsequence match (score grows with the gap size)
//
// Non-matches return (0, false).
func matchScore(target, qLower string) (int, bool) {
	if target == "" || qLower == "" {
		return 0, false
	}
	tLower := strings.ToLower(target)
	switch {
	case tLower == qLower:
		return 0, true
	case strings.HasPrefix(tLower, qLower):
		return 1, true
	case strings.Contains(tLower, qLower):
		return 2, true
	}
	// Fuzzy subsequence: every character of q must appear in target in
	// order. Score is 3 + total gap size so tighter matches win.
	gap, ok := subsequenceGap(tLower, qLower)
	if !ok {
		return 0, false
	}
	return 3 + gap, true
}

// subsequenceGap checks whether q occurs as a subsequence in t. If so
// it returns the number of non-matching characters between the first
// and last matched position (plus leading offset) as a "tightness"
// measure.
func subsequenceGap(t, q string) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	ti, qi := 0, 0
	firstMatch := -1
	lastMatch := -1
	for ti < len(t) && qi < len(q) {
		if t[ti] == q[qi] {
			if firstMatch < 0 {
				firstMatch = ti
			}
			lastMatch = ti
			qi++
		}
		ti++
	}
	if qi < len(q) {
		return 0, false
	}
	span := lastMatch - firstMatch + 1
	return firstMatch + (span - len(q)), true
}
