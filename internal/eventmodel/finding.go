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
	// KindOwnershipViolation — emitting facts or receiving actions outside
	// the owned namespace.
	KindOwnershipViolation FindingKind = "ownership-violation"

	// KindDuplicateOwner — two components claim overlapping owns.
	KindDuplicateOwner FindingKind = "duplicate-owner"

	// KindStarvedReceive — a receives slot has no producer.
	KindStarvedReceive FindingKind = "starved-receive"

	// KindStarvedFold — a fold pattern matches no emitted kind.
	KindStarvedFold FindingKind = "starved-fold"

	// KindOrphanFact — an emitted fact has no consumer.
	KindOrphanFact FindingKind = "orphan-fact"

	// KindAmbiguousCall — an emitted action resolves to multiple receivers.
	KindAmbiguousCall FindingKind = "ambiguous-call"

	// KindUnresolvedCall — an emitted action resolves to zero receivers.
	KindUnresolvedCall FindingKind = "unresolved-call"

	// KindUnresolvedRef — a $ref does not resolve.
	KindUnresolvedRef FindingKind = "unresolved-ref"

	// KindRefCycle — a cross-component $ref cycle was detected.
	KindRefCycle FindingKind = "ref-cycle"
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
