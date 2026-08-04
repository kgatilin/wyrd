// Package eventmodel provides domain types and validation for event-driven
// architecture declarations. Each component declares its event interface in
// a .arch/events.yaml file; the reader discovers and composes these into a
// Model that the validator checks for structural integrity.
//
// The package implements the built-in rules from the event-model design doc:
// role x ownership matrix, single-owner namespaces, closure (starved receives,
// orphan facts), call resolution, and $ref integrity. Schema compatibility
// (structural subset check on call-out payloads) is NOT implemented in this
// iteration; the opaque schema representation does not support it cleanly.
package eventmodel

// Model is the composed set of event components discovered under a repo root.
// It is the aggregate over which validation runs.
type Model struct {
	// Components keyed by component id (stable, unique in the repo).
	Components map[string]*Component
}

// Component is one event-model declaration parsed from .arch/events.yaml.
// It is a pure data container — no behavior, no dependencies.
type Component struct {
	// Version is the schema version (currently 1).
	Version int

	// ID is the stable component identifier, unique in the repo.
	ID string

	// Owns is the namespace whose vocabulary this component defines.
	// A component may only emit facts and receive actions in its owned
	// namespace.
	Owns string

	// Description is a human-readable summary.
	Description string

	// Receives are the event kinds this component handles.
	Receives []Slot

	// Emits are the event kinds this component produces.
	Emits []Slot

	// Folds are projections maintained over event patterns.
	Folds []Fold

	// Vocab are component-local shared schema shapes.
	Vocab map[string]SchemaNode

	// Extra is opaque passthrough data for templates; archai never
	// interprets it.
	Extra map[string]any

	// SourceFile is the path to the .arch/events.yaml that defined
	// this component (for diagnostics).
	SourceFile string
}

// Slot represents a single receive or emit declaration.
type Slot struct {
	// Kind is the event kind name (e.g. "billing.invoice.issued").
	Kind string

	// Role distinguishes actions (commands) from facts (events).
	Role Role

	// Description is a human-readable summary.
	Description string

	// Exposure are free-form tags indicating API surface (e.g. "public_api").
	Exposure []string

	// Schema is the payload schema (opaque structured data).
	Schema SchemaNode

	// Extra is opaque passthrough data for templates.
	Extra map[string]any
}

// Role classifies an event kind.
type Role string

const (
	// RoleAction is a command — an instruction to do something.
	RoleAction Role = "action"
	// RoleFact is an event — a record that something happened.
	RoleFact Role = "fact"
)

// Valid reports whether r is a recognized role value.
func (r Role) Valid() bool {
	return r == RoleAction || r == RoleFact
}

// Fold represents a projection maintained over an event pattern.
//
// The subject and consumes fields serve distinct purposes and operate in
// different alphabets:
//
//   - Subject is the transport read-set: a NATS-style subject pattern with
//     {slot} tokens that declares the partition key layout ("one state per X").
//     archai does not match it against kinds; it carries the pattern through
//     for codegen and parses only enough to extract {slot} tokens.
//
//   - Consumes lists the event kinds the reducer actually folds. These are
//     kind globs in the kind alphabet (MatchPattern applies). Starvation is
//     checked here, not on the subject.
//
// The distinction matters: a subject pattern matches events the reducer may
// ignore (wrong kind in the namespace), and a consumes entry may be emitted
// on subjects the fold does not subscribe to. Conflating them (as the old
// "pattern" field did) produces false starved-fold warnings.
type Fold struct {
	// Name is the fold identifier (e.g. "billing.open-invoices").
	Name string

	// Subject is a NATS-style transport pattern with {slot} tokens.
	// Example: "svc.*.billing.{account}.invoice.>"
	// archai validates {slot} syntax but does not match this against kinds.
	Subject string

	// PartitionArity is the number of {slot} tokens in Subject.
	// Derived at parse time; zero if Subject is empty or has no slots.
	PartitionArity int

	// Consumes lists the event kinds the reducer folds, as kind globs.
	// Example: ["billing.invoice.*", "billing.credit.issued"]
	// MatchPattern applies; starvation is checked per entry.
	Consumes []string

	// State is the projection state schema (opaque structured data).
	State SchemaNode

	// Extra is opaque passthrough data for templates.
	Extra map[string]any
}

// SchemaNode is an opaque representation of a JSON Schema fragment written
// as YAML. It preserves the structure for traversal ($ref resolution, property
// walking, deprecated detection) but does NOT validate payloads against it.
// The underlying representation is map[string]any for objects, []any for
// arrays, and scalar values otherwise.
type SchemaNode struct {
	Raw any
}

// IsZero reports whether the node is empty (nil or no content).
func (n SchemaNode) IsZero() bool {
	return n.Raw == nil
}

// AsMap returns the underlying map if the node is an object, or nil otherwise.
func (n SchemaNode) AsMap() map[string]any {
	if m, ok := n.Raw.(map[string]any); ok {
		return m
	}
	return nil
}

// Get retrieves a nested value by key (only valid for object nodes).
func (n SchemaNode) Get(key string) SchemaNode {
	if m := n.AsMap(); m != nil {
		return SchemaNode{Raw: m[key]}
	}
	return SchemaNode{}
}

// Ref returns the $ref value if present, empty string otherwise.
func (n SchemaNode) Ref() string {
	if m := n.AsMap(); m != nil {
		if ref, ok := m["$ref"].(string); ok {
			return ref
		}
	}
	return ""
}

// Deprecated returns true if the schema fragment has deprecated: true.
func (n SchemaNode) Deprecated() bool {
	if m := n.AsMap(); m != nil {
		if v, ok := m["deprecated"].(bool); ok {
			return v
		}
	}
	return false
}

// Properties returns the properties map if present, nil otherwise.
func (n SchemaNode) Properties() map[string]SchemaNode {
	if m := n.AsMap(); m != nil {
		if props, ok := m["properties"].(map[string]any); ok {
			result := make(map[string]SchemaNode, len(props))
			for k, v := range props {
				result[k] = SchemaNode{Raw: v}
			}
			return result
		}
	}
	return nil
}
