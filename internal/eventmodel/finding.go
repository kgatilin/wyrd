package eventmodel

// FindingSeverity classifies the severity of a validation finding.
type FindingSeverity string

const (
	// SeverityError is a fatal rule breach that must be fixed.
	SeverityError FindingSeverity = "error"
	// SeverityWarning is a potential issue (starvation, orphans) that
	// may be acceptable depending on the composed set.
	SeverityWarning FindingSeverity = "warning"
)

// FindingKind classifies the rule that was breached.
type FindingKind string

const (
	// KindDuplicateOwner — two components claim overlapping owns, so the
	// authority over a namespace's schemas is ambiguous.
	KindDuplicateOwner FindingKind = "duplicate-owner"

	// KindPatternConflict — one event kind is declared on more than one
	// subject pattern across the composed set. The pattern is where the kind
	// lives on the wire, so two answers means the subscribers of one side
	// will never see what the other side appends.
	KindPatternConflict FindingKind = "kind-pattern-conflict"

	// KindSelfInputConflict — one component declares the same kind in both
	// inputs and outputs, so it triggers itself. Folding one's own output
	// into state is the legal channel for that, and it is state_events.
	KindSelfInputConflict FindingKind = "self-input-conflict"

	// KindStarvedInput — an inputs entry has no producer.
	KindStarvedInput FindingKind = "starved-input"

	// KindStarvedStateEvent — a state_events entry has no producer.
	KindStarvedStateEvent FindingKind = "starved-state-event"

	// KindOrphanEvent — an output is observed by nobody: it is neither an
	// input nor a state event anywhere in the composed set.
	KindOrphanEvent FindingKind = "orphan-event"

	// KindPartitionMismatch — a component's inputs and state events do not
	// all extract the same ordered partition key, so its fold read-set
	// cannot address one state.
	KindPartitionMismatch FindingKind = "partition-mismatch"

	// KindUnderspecifiedState — the component declares a state schema with
	// no shape (an object with no properties and no $ref): a placeholder,
	// not a projection contract.
	KindUnderspecifiedState FindingKind = "underspecified-state"

	// KindExclusiveUnhandled — a kind declared `delivery: exclusive` is
	// nobody's input. Fires ONLY under that opt-in policy; an output with no
	// observer is an orphan-event warning, not an error.
	KindExclusiveUnhandled FindingKind = "exclusive-unhandled"

	// KindExclusiveConflict — a kind declared `delivery: exclusive` is the
	// input of more than one component. Fires ONLY under that opt-in policy;
	// multiple independent observers are the event-sourced norm.
	KindExclusiveConflict FindingKind = "exclusive-conflict"

	// KindUnresolvedRef — a $ref does not resolve.
	KindUnresolvedRef FindingKind = "unresolved-ref"

	// KindRefCycle — a cross-component $ref cycle was detected.
	KindRefCycle FindingKind = "ref-cycle"

	// KindMalformedSlot — a subject pattern has invalid {slot} syntax.
	KindMalformedSlot FindingKind = "malformed-slot"
)

// Finding is a single validation result. Findings carry enough location
// information to be rendered with context (component, file, slot).
type Finding struct {
	// Severity distinguishes errors from warnings.
	Severity FindingSeverity

	// Kind classifies the rule breach.
	Kind FindingKind

	// Component is the component id where the issue was found.
	Component string

	// File is the source file path (for diagnostics).
	File string

	// Location names the slot/type entry involved (usually the event kind).
	// Empty when the finding applies to the component as a whole.
	Location string

	// Message is a human-readable description of the issue.
	Message string

	// Related holds additional context (e.g. conflicting component ids,
	// cycle path). Keys and semantics vary by Kind.
	Related map[string]any
}
