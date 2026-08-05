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

	// KindRoleConflict — one event kind is declared with more than one role
	// across the composed set. Role is a global property of the kind, not of
	// a declaration site.
	KindRoleConflict FindingKind = "kind-role-conflict"

	// KindSelfReceiveConflict — one component declares the same kind in both
	// emits and receives. Observing one's own event statefully is a fold.
	KindSelfReceiveConflict FindingKind = "self-receive-conflict"

	// KindStarvedReceive — a receives slot has no producer.
	KindStarvedReceive FindingKind = "starved-receive"

	// KindStarvedFold — a fold consumes entry matches no emitted kind.
	KindStarvedFold FindingKind = "starved-fold"

	// KindOrphanEvent — an emitted event (of either role) is observed by
	// nobody: no receives slot and no fold consumes it.
	KindOrphanEvent FindingKind = "orphan-event"

	// KindPartitionMismatch — a fold's subjects do not extract the same
	// ordered partition key, so they cannot address one fold state.
	KindPartitionMismatch FindingKind = "partition-mismatch"

	// KindUnderspecifiedState — a fold declares a state schema with no
	// shape (an object with no properties and no $ref): a placeholder,
	// not a projection contract.
	KindUnderspecifiedState FindingKind = "underspecified-state"

	// KindExclusiveUnhandled — a kind declared `delivery: exclusive` has no
	// receiver. Fires ONLY under that opt-in policy; broadcast events with
	// no observer are orphan-event warnings, not errors.
	KindExclusiveUnhandled FindingKind = "exclusive-unhandled"

	// KindExclusiveConflict — a kind declared `delivery: exclusive` has more
	// than one receiver. Fires ONLY under that opt-in policy; multiple
	// independent observers are the event-sourced norm, not a defect.
	KindExclusiveConflict FindingKind = "exclusive-conflict"

	// KindUnresolvedRef — a $ref does not resolve.
	KindUnresolvedRef FindingKind = "unresolved-ref"

	// KindRefCycle — a cross-component $ref cycle was detected.
	KindRefCycle FindingKind = "ref-cycle"

	// KindMalformedSlot — a fold subject has invalid {slot} syntax.
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

	// Location names the slot/fold/vocab entry involved (e.g. the event
	// kind or fold name). Empty when the finding applies to the component
	// as a whole.
	Location string

	// Message is a human-readable description of the issue.
	Message string

	// Related holds additional context (e.g. conflicting component ids,
	// cycle path). Keys and semantics vary by Kind.
	Related map[string]any
}
