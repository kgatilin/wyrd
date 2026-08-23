package events

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kgatilin/wyrd/internal/adapter/git"
	"github.com/kgatilin/wyrd/internal/eventmodel"
	"github.com/kgatilin/wyrd/internal/overlay"
)

// baseModel is the event model as it stood at one commit, with the commit it
// was read from.
type baseModel struct {
	Rev   string
	Model *eventmodel.Model
}

// loadBaseModel reads the event model at merge-base(baseRef, HEAD).
//
// Three-dot like every other base in this UI: it starts at the merge base, so a
// branch still reads correctly after the base branch moves ahead, and the
// comparison answers "what did this branch do" rather than "how do these two
// tips differ".
//
// Declarations are read out of the commit a file at a time rather than by
// materializing the tree. The Go model reader needs a real checkout and pays
// for one (internal/serve.baseTrees); an event model is a handful of YAML
// files, and exporting a large repository to read nine of them is the cost of
// a feature nobody would turn on.
//
// An unresolvable ref yields (nil, nil), not an error. It is the same call
// ReviewBase makes and for the same reason: a base that cannot be resolved must
// read as "no comparison", never as a fallback to the base branch's tip, which
// produces a large, confident, backwards diff.
func loadBaseModel(root, baseRef string, current []eventmodel.Location) (*baseModel, error) {
	if root == "" || baseRef == "" {
		return nil, nil
	}
	rev, ok := git.MergeBase(root, baseRef)
	if !ok {
		return nil, nil
	}

	// The union of both sides' source declarations. Each alone is wrong in one
	// direction: read the base with only the branch's sources and a source the
	// branch deleted hides the components it used to find, so they never show
	// as removed; read it with only the base's and a source the branch added
	// makes every component under it look new.
	locations := unionLocations(current, baseSourceLocations(root, rev))

	paths, err := git.ListTree(root, rev)
	if err != nil {
		return nil, err
	}
	candidates := eventmodel.DeclarationCandidates(paths, locations)

	dir, err := os.MkdirTemp("", "wyrd-events-base-")
	if err != nil {
		return nil, fmt.Errorf("events: base tree: %w", err)
	}
	defer os.RemoveAll(dir)

	// Every declared source directory has to exist even when the commit put no
	// files in it: ReadSources treats a missing source as a typo and fails the
	// whole read, so a branch that introduced the directory would take its own
	// diff down.
	for _, loc := range locations {
		if loc.Path == "" || filepath.IsAbs(loc.Path) {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(loc.Path)), 0o755); err != nil {
			return nil, fmt.Errorf("events: base tree: %w", err)
		}
	}

	for _, p := range candidates {
		data, err := git.ShowFile(root, rev, p)
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, fmt.Errorf("events: base tree: %w", err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return nil, fmt.Errorf("events: base tree: %w", err)
		}
	}

	model, err := eventmodel.ReadSources(dir, locations)
	if err != nil {
		return nil, fmt.Errorf("events: base %s: %w", rev, err)
	}
	rebaseSourceFiles(model, dir, root)
	return &baseModel{Rev: rev, Model: model}, nil
}

// rebaseSourceFiles rewrites each component's SourceFile from the scratch
// directory it was read out of to where that file lives in the worktree.
//
// Without it every component differs from its own current self on a temp path
// that changes per request, so a diff of nothing at all comes back as "all
// components changed" — and the one field that would have told a reader a
// declaration moved becomes noise instead.
func rebaseSourceFiles(m *eventmodel.Model, from, to string) {
	for _, comp := range m.Components {
		rel, err := filepath.Rel(from, comp.SourceFile)
		if err != nil || rel == "" {
			continue
		}
		comp.SourceFile = filepath.Join(to, rel)
	}
}

// baseSourceLocations reads events.sources out of the project overlay as it was
// at rev. A commit with no overlay, an overlay this build cannot parse, or one
// that declares no sources all mean the same thing here — nothing to add to the
// union — so none of them is an error.
func baseSourceLocations(root, rev string) []eventmodel.Location {
	for _, name := range []string{overlay.FileName, overlay.LegacyFileName} {
		data, err := git.ShowFile(root, rev, name)
		if err != nil {
			continue
		}
		cfg, err := overlay.Decode(name, data)
		if err != nil {
			return nil
		}
		return overlayLocations(cfg)
	}
	return nil
}

// unionLocations concatenates source declarations, dropping exact repeats. The
// two sides normally state the same thing, and reading a directory twice would
// only be caught later, by ReadSources rejecting the duplicate component ids it
// produced.
func unionLocations(sets ...[]eventmodel.Location) []eventmodel.Location {
	seen := make(map[eventmodel.Location]bool)
	var out []eventmodel.Location
	for _, set := range sets {
		for _, loc := range set {
			if seen[loc] {
				continue
			}
			seen[loc] = true
			out = append(out, loc)
		}
	}
	return out
}
