// Package eventmodel provides domain types and validation for event-driven
// architecture declarations. Each component declares its event interface in
// a .arch/events.yaml file; the reader discovers and composes these into a
// Model that the validator checks for structural integrity.
//
// # Core semantics
//
// A durable event is published once and may be observed independently by any
// number of components and folds. Nothing in the base model requires an event
// to resolve to a single handler: emission is an append to a log, reception is
// an observation of it, and the two are not a call. Role (action | fact) is a
// semantic classification of the event, not a delivery contract.
//
// Exclusive handling — "exactly one component processes this kind" — is a
// transport/runtime policy, not a property of event sourcing. It is available
// as an explicit opt-in per slot (Delivery == DeliveryExclusive) and is the
// only thing that turns receiver cardinality into a validated rule.
//
// The package implements the built-in rules from the event-model design doc:
// single-owner namespaces, closure (starved receives, starved folds, orphan
// events), fold partition coherence, opt-in exclusive delivery, and $ref
// integrity. Schema compatibility (structural subset check on payloads) is NOT
// implemented in this iteration; the opaque schema representation does not
// support it cleanly.
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

	// Owns is the namespace whose vocabulary this component defines —
	// the authority over the schemas of kinds under that prefix. It is
	// NOT an exclusive right to produce or to observe those events: any
	// component may emit into, or subscribe to, a namespace it does not own.
	Owns string

	// Description is a human-readable summary.
	Description string

	// Receives are the event kinds this component observes (stateless
	// observation). A kind may have 0..N receivers; observation carries no
	// cardinality contract unless a slot opts into exclusive delivery.
	Receives []Slot

	// Emits are the event kinds this component appends to the durable log.
	Emits []Slot

	// Folds are stateful observations: projections maintained over event
	// patterns. Several folds — in the same or different components — may
	// consume the same kind independently.
	Folds []Fold

	// Types are component-local reusable JSON Schema definitions, the
	// analogue of JSON Schema's $defs. They are referenced from payload and
	// fold-state schemas via $ref.
	Types map[string]SchemaNode

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

	// Role classifies the event semantically: an action expresses intent,
	// a fact records what happened. It carries no delivery contract — an
	// action is not an RPC and does not require a single handler.
	Role Role

	// Delivery is the optional delivery policy for this kind. The default
	// (DeliveryBroadcast) is plain event-sourced observation: 0..N receivers.
	// DeliveryExclusive opts into the RPC-like rule that exactly one
	// component must receive the kind, and is the only way to make receiver
	// cardinality a validated constraint.
	Delivery Delivery

	// Description is a human-readable summary.
	Description string

	// Exposure are free-form tags indicating API surface (e.g. "public_api").
	Exposure []string

	// Schema is the payload schema (opaque structured data).
	Schema SchemaNode

	// Extra is opaque passthrough data for templates.
	Extra map[string]any
}

// Role classifies an event kind semantically. It is documentation and a
// rendering axis, never a cardinality contract: an action may be observed by
// zero, one, or many components exactly like a fact.
//
// Role is a property of the KIND, held globally across the composed set — not
// a property of a declaration site. Every producer and every observer of a
// kind must agree on it, and payload variants never change it. Where the same
// name would carry both an intent and its outcome, those are two kinds
// (e.g. `x.thing.do` and `x.thing.done`), not one kind read two ways.
// Disagreement is a kind-role-conflict error.
type Role string

const (
	// RoleAction expresses intent — "do this". It is not an RPC.
	RoleAction Role = "action"
	// RoleFact records that something happened.
	RoleFact Role = "fact"
)

// Valid reports whether r is a recognized role value.
func (r Role) Valid() bool {
	return r == RoleAction || r == RoleFact
}

// Delivery is the optional delivery policy attached to a slot. It is the
// escape hatch for systems that genuinely need a command with exactly one
// handler; the event-sourced default is broadcast.
type Delivery string

const (
	// DeliveryBroadcast is the default: the event is appended once and may
	// be observed independently by any number of components and folds.
	DeliveryBroadcast Delivery = "broadcast"
	// DeliveryExclusive opts into single-handler semantics for this kind.
	// Only under this policy are zero-receiver and multi-receiver situations
	// validation errors.
	DeliveryExclusive Delivery = "exclusive"
)

// Valid reports whether d is a recognized delivery value. The empty value is
// valid and means DeliveryBroadcast.
func (d Delivery) Valid() bool {
	return d == "" || d == DeliveryBroadcast || d == DeliveryExclusive
}

// IsExclusive reports whether the slot opted into single-handler delivery.
func (d Delivery) IsExclusive() bool { return d == DeliveryExclusive }

// Fold is a stateful observation: a projection maintained over event facts.
// Folds are independent of one another and of receives — several folds may
// consume the same kind, and a kind consumed by a fold need not be received
// anywhere.
//
// The subjects and consumes fields serve distinct purposes and operate in
// different alphabets:
//
//   - Subjects is the transport read-set: NATS-style subject patterns with
//     {slot} tokens that declare the partition key layout ("one state per X")
//     and wire subscriptions. archai does not match them against kinds; it
//     carries the patterns through for codegen and parses only enough to
//     extract {slot} tokens.
//
//   - Consumes lists the event kinds the reducer actually folds. These are
//     kind globs in the kind alphabet (MatchPattern applies). Starvation is
//     checked here, not on the subjects.
//
// The distinction matters: a subject pattern matches events the reducer may
// ignore (wrong kind in the namespace), and a consumes entry may be emitted
// on subjects the fold does not subscribe to. Conflating them (as the old
// "pattern" field did) produces false starved-fold warnings.
type Fold struct {
	// Name is the fold identifier (e.g. "billing.open-invoices").
	Name string

	// Subjects are NATS-style transport patterns with {slot} tokens.
	// Example: ["svc.*.billing.{account}.invoice.>",
	//           "svc.*.billing.{account}.credit.>"]
	// A fold may read from several subjects, but all of them must extract
	// the same ordered partition key — one fold instance holds one state,
	// so every subject it reads must identify that state identically.
	// archai validates {slot} syntax but does not match these against kinds.
	Subjects []string

	// PartitionKey is the ordered list of {slot} names shared by every
	// entry in Subjects. Derived at parse time; nil when no subject is
	// declared or the subjects carry no slots.
	PartitionKey []string

	// Consumes lists the event kinds the reducer folds, as kind globs.
	// Example: ["billing.invoice.*", "billing.credit.issued"]
	// MatchPattern applies; starvation is checked per entry.
	Consumes []string

	// State is the projection state schema. It is required: a fold without a
	// declared state shape is an unfinished declaration, not a valid one.
	// Either a full schema or a $ref to a component type.
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
