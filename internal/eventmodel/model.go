// Package eventmodel provides domain types and validation for event-driven
// architecture declarations. Each component declares its event interface in
// a .arch/events.yaml file; the reader discovers and composes these into a
// Model that the validator checks for structural integrity.
//
// # Core semantics
//
// A durable event is appended once and may be observed independently by any
// number of components. Nothing in the base model requires an event to resolve
// to a single handler: emission is an append to a log, observation is a read of
// it, and the two are not a call.
//
// A component declares three lists and nothing else:
//
//   - Inputs — the events that trigger it.
//   - Outputs — the events it appends to the log.
//   - StateEvents — the events it folds into its own state. They may be its
//     own outputs (the usual case: fold the outcome you just recorded) or
//     another component's events.
//
// The fold is not declared, it is derived: its read-set is the subject
// patterns of Inputs and StateEvents, its consumed kinds are theirs, and its
// partition key is the ordered {slot} list every one of those patterns must
// share. Outputs are deliberately not part of the read-set — appending an
// event does not subscribe you to it.
//
// One consequence is worth stating, because it is the rule the shape exists to
// make checkable: a kind in both Inputs and Outputs is a component triggering
// itself, which is an error, while a kind in both StateEvents and Outputs is
// the normal way to fold your own outcome into state.
//
// Exclusive handling — "exactly one component processes this kind" — is a
// transport/runtime policy, not a property of event sourcing. It is available
// as an explicit opt-in per slot (Delivery == DeliveryExclusive) and is the
// only thing that turns consumer cardinality into a validated rule.
//
// The package implements the built-in rules from the event-model design doc:
// single-owner namespaces, one subject pattern per kind, closure (starved
// inputs, starved state events, orphan outputs), partition coherence, opt-in
// exclusive delivery, and $ref integrity. Schema compatibility (structural
// subset check on payloads) is NOT implemented in this iteration; the opaque
// schema representation does not support it cleanly.
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
	// Version is the schema version (currently 2).
	Version int

	// ID is the stable component identifier, unique in the repo.
	ID string

	// Owns is the namespace whose vocabulary this component defines —
	// the authority over the schemas of kinds under that prefix. It is
	// NOT an exclusive right to produce or to observe those events: any
	// component may append to, or subscribe to, a namespace it does not own.
	Owns string

	// Description is a human-readable summary.
	Description string

	// Inputs are the event kinds that trigger this component. They are part
	// of its fold read-set: a component sees the events it reacts to.
	Inputs []Slot

	// Outputs are the event kinds this component appends to the durable log.
	// Appending does not subscribe: an output is in the read-set only if it
	// is also declared as a state event.
	Outputs []Slot

	// StateEvents are the event kinds this component folds into its state
	// without being triggered by them. They are commonly the component's own
	// outputs — recording the outcome it just appended — but may equally be
	// another component's events that its state has to track.
	StateEvents []Slot

	// State is the optional projection state schema: what the fold actually
	// holds. Either a full schema or a $ref to a component type. When absent,
	// no state shape is checked; when present but empty, that is an
	// underspecified-state warning.
	State SchemaNode

	// PartitionKey is the ordered list of {slot} names shared by every
	// subject pattern in Inputs and StateEvents — the address of one fold
	// state. Derived at parse time from the first pattern that carries slots;
	// the validator requires the rest to repeat it.
	PartitionKey []string

	// Types are component-local reusable JSON Schema definitions, the
	// analogue of JSON Schema's $defs. They are referenced from payload and
	// state schemas via $ref.
	Types map[string]SchemaNode

	// Extra is opaque passthrough data for templates; archai never
	// interprets it.
	Extra map[string]any

	// SourceFile is the path to the .arch/events.yaml that defined
	// this component (for diagnostics).
	SourceFile string
}

// ReadSet returns the slots that make up the component's fold read-set:
// its inputs followed by its state events, in declaration order. Outputs are
// not included — appending an event is not a subscription to it.
func (c *Component) ReadSet() []Slot {
	out := make([]Slot, 0, len(c.Inputs)+len(c.StateEvents))
	out = append(out, c.Inputs...)
	out = append(out, c.StateEvents...)
	return out
}

// Subjects returns the distinct subject patterns of the component's read-set
// in declaration order, skipping slots that declare no pattern. This is the
// transport read-set the runtime subscribes with.
func (c *Component) Subjects() []string {
	var out []string
	seen := make(map[string]bool)
	for _, slot := range c.ReadSet() {
		if slot.Pattern == "" || seen[slot.Pattern] {
			continue
		}
		seen[slot.Pattern] = true
		out = append(out, slot.Pattern)
	}
	return out
}

// Consumes returns the distinct event kinds the component folds, in
// declaration order: its inputs followed by its state events.
func (c *Component) Consumes() []string {
	var out []string
	seen := make(map[string]bool)
	for _, slot := range c.ReadSet() {
		if seen[slot.Kind] {
			continue
		}
		seen[slot.Kind] = true
		out = append(out, slot.Kind)
	}
	return out
}

// Slot represents a single input, output or state-event declaration.
type Slot struct {
	// Kind is the event kind name (e.g. "billing.invoice.issued").
	Kind string

	// Pattern is the subject the kind travels on: a NATS-style pattern with
	// {slot} tokens naming the partition key ("one state per X"). It is the
	// wire address of the kind, not its name, and the two are separate
	// because the same kind is addressed per-partition. archai parses it only
	// far enough to extract {slot} tokens; it never matches it against kinds.
	// Optional: a kind declared with no pattern contributes nothing to the
	// read-set's subjects and nothing to the partition key.
	Pattern string

	// Delivery is the optional delivery policy for this kind. The default
	// (DeliveryBroadcast) is plain event-sourced observation: 0..N consumers.
	// DeliveryExclusive opts into the RPC-like rule that exactly one
	// component must take the kind as an input, and is the only way to make
	// consumer cardinality a validated constraint.
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

// Delivery is the optional delivery policy attached to a slot. It is the
// escape hatch for systems that genuinely need a command with exactly one
// handler; the event-sourced default is broadcast.
type Delivery string

const (
	// DeliveryBroadcast is the default: the event is appended once and may
	// be observed independently by any number of components.
	DeliveryBroadcast Delivery = "broadcast"
	// DeliveryExclusive opts into single-handler semantics for this kind.
	// Only under this policy are zero-consumer and multi-consumer situations
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
