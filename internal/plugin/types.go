package plugin

import (
	"time"

	"github.com/kgatilin/archai/internal/diff"
)

// TargetMeta is the read-only descriptor of a locked target as seen
// by plugins. It mirrors target.TargetMeta with a leaner shape so
// future fields (e.g. tags, author) can be added without forcing
// every plugin to depend on internal/target.
type TargetMeta struct {
	ID          string
	BaseCommit  string
	CreatedAt   string
	Description string
}

// TargetSnapshot is a frozen view of a target: its metadata plus the
// unified Model that was current when the target was locked.
type TargetSnapshot struct {
	Meta  TargetMeta
	Model *Model
}

// ModelEvent describes a change to the live Model. Plugins that
// subscribe receive one of these per fsnotify-driven reload, overlay
// reload, or target switch. The struct stays intentionally small;
// future fields can be appended without breaking existing handlers.
type ModelEvent struct {
	// Kind identifies what changed. See ModelEventKind* constants.
	Kind ModelEventKind

	// Paths lists the package paths affected by this event. Empty
	// for non-package events (overlay reload, target switch).
	Paths []string

	// Target is the new active target id, set when Kind ==
	// ModelEventKindTargetSwitch. Empty otherwise.
	Target string

	// At is the wall-clock timestamp when the event was published.
	At time.Time
}

// ModelEventKind enumerates the broadcast event categories.
type ModelEventKind string

const (
	// ModelEventKindPackageReload fires when one or more packages
	// were re-extracted in response to .go file changes.
	ModelEventKindPackageReload ModelEventKind = "package-reload"

	// ModelEventKindOverlayReload fires when archai.yaml was reloaded.
	ModelEventKindOverlayReload ModelEventKind = "overlay-reload"

	// ModelEventKindTargetSwitch fires when the CURRENT target id
	// changed.
	ModelEventKindTargetSwitch ModelEventKind = "target-switch"
)

// Diff is the structured patch produced by Host.Diff. We re-export
// internal/diff.Diff under this package to keep the plugin contract
// self-contained — plugins don't have to depend on internal/diff.
type Diff = diff.Diff

// ValidationReport summarizes the result of Host.Validate for a
// target. Empty Violations means the current code matches the target.
type ValidationReport struct {
	// TargetID is the target id that was validated against.
	TargetID string

	// OK is true when no violations were found.
	OK bool

	// Violations is the list of structured changes between the
	// current code model and the target. Re-uses diff.Change so the
	// representation is identical to `archai validate --format yaml`.
	Violations []diff.Change
}
