// Package eventsrc answers one question: where does a project keep its event
// declarations?
//
// It exists so the daemon and the CI gate cannot disagree about that. Both read
// `events.sources` out of the same overlay, and a copy of this projection on
// each side would drift the first time a source field is added — with the only
// symptom being a canvas and a build log that describe different repositories.
package eventsrc

import (
	"fmt"

	"github.com/kgatilin/wyrd/internal/eventmodel"
	"github.com/kgatilin/wyrd/internal/overlay"
)

// Locations reads events.sources out of the project overlay at root.
//
// A project with no overlay, or one that declares no sources, returns none:
// the reader falls back to the `.arch/` convention, and "no overlay" is not an
// error a project has to fix before its event map can be read.
func Locations(root string) ([]eventmodel.Location, error) {
	path := overlay.DiscoverPath(root)
	if path == "" {
		return nil, nil
	}
	cfg, err := overlay.Load(path)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	return From(cfg), nil
}

// From projects an overlay's events.sources onto reader locations.
//
// Separate from Locations because the base model of a diff reads its overlay
// out of a git revision rather than off disk, and both paths have to agree
// about what a source declaration means.
func From(cfg *overlay.Config) []eventmodel.Location {
	if cfg == nil {
		return nil
	}
	out := make([]eventmodel.Location, 0, len(cfg.Events.Sources))
	for _, src := range cfg.Events.Sources {
		out = append(out, eventmodel.Location{
			Path:    src.Path,
			Include: src.Include,
			Format:  eventmodel.Format(src.Format),
		})
	}
	return out
}
