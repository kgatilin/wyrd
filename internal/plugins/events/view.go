package events

import (
	"sort"

	"github.com/kgatilin/wyrd/internal/eventmodel"
)

// The wire shape of the event model, owned here rather than by the domain
// types. A canvas draws components and the flows between them, and asks about
// one kind when the reader clicks it; the bipartite graph is the wrong unit for
// all three, and Component's Go field names are the wrong spelling.

// modelView is the whole response: the nodes, the edges between them, and the
// vocabulary they carry.
type modelView struct {
	Components []componentView `json:"components"`
	Flows      []flowView      `json:"flows"`
	Kinds      []kindView      `json:"kinds"`
}

// componentView is one node of the canvas.
type componentView struct {
	ID          string `json:"id"`
	Owns        string `json:"owns,omitempty"`
	Description string `json:"description,omitempty"`
	// Source is "asyncapi" for an imported document and absent for a native
	// declaration. The canvas marks imported nodes: they are not validated, and
	// a reader should know which half of the picture is graded and which is
	// taken on the publisher's word.
	Source       string   `json:"source,omitempty"`
	SourceFile   string   `json:"source_file,omitempty"`
	PartitionKey []string `json:"partition_key,omitempty"`
	HasState     bool     `json:"has_state"`
	// Instances names the port family a single document describes. A port is
	// one node here, so this is how the family is visible at all.
	Instances   []string   `json:"instances,omitempty"`
	Inputs      []slotView `json:"inputs,omitempty"`
	Outputs     []slotView `json:"outputs,omitempty"`
	StateEvents []slotView `json:"state_events,omitempty"`
}

// slotView is one entry of one of the three lists.
type slotView struct {
	Kind        string `json:"kind"`
	Pattern     string `json:"pattern,omitempty"`
	Description string `json:"description,omitempty"`
	Delivery    string `json:"delivery,omitempty"`
	// Role is the imported declaration's own word for the entry — command,
	// event, call or observe. The three lists cannot tell an event from a
	// call, and the reader wants to see the difference.
	Role      string   `json:"role,omitempty"`
	Instances []string `json:"instances,omitempty"`
}

// flowView is one producer-to-observer edge.
type flowView struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	// Trigger separates the two ways of observing: driving the target, or
	// only being folded into its state.
	Trigger bool   `json:"trigger"`
	Health  string `json:"health,omitempty"`
}

// kindView is one event kind, with everywhere it appears. This is what the
// canvas shows when a reader selects an edge.
type kindView struct {
	Name         string   `json:"name"`
	Pattern      string   `json:"pattern,omitempty"`
	Description  string   `json:"description,omitempty"`
	PartitionKey []string `json:"partition_key,omitempty"`
	Delivery     string   `json:"delivery,omitempty"`
	Health       string   `json:"health,omitempty"`
	// Class is the imported vocabulary's command/event split, written out by
	// the publisher so nobody has to parse the kind name to recover it.
	Class     string   `json:"class,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Producers []string `json:"producers,omitempty"`
	Triggers  []string `json:"triggers,omitempty"`
	Folders   []string `json:"folders,omitempty"`
	Schema    any      `json:"schema,omitempty"`
	// Example is one instance of Schema, rendered so a reader can see the
	// payload as an object rather than compile the schema in their head.
	// Derived here rather than in the canvas because a `$ref` resolves
	// against the declaring component, which the wire shape does not carry.
	Example any `json:"example,omitempty"`
}

// buildModelView projects the composed model and its graph onto the wire shape.
func buildModelView(m *eventmodel.Model, g *eventmodel.Graph) modelView {
	view := modelView{
		Components: componentViews(m),
		Flows:      flowViews(g),
		Kinds:      kindViews(m, g),
	}
	if view.Components == nil {
		view.Components = []componentView{}
	}
	if view.Flows == nil {
		view.Flows = []flowView{}
	}
	if view.Kinds == nil {
		view.Kinds = []kindView{}
	}
	return view
}

func componentViews(m *eventmodel.Model) []componentView {
	ids := make([]string, 0, len(m.Components))
	for id := range m.Components {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]componentView, 0, len(ids))
	for _, id := range ids {
		comp := m.Components[id]
		out = append(out, componentView{
			ID:           id,
			Owns:         comp.Owns,
			Description:  comp.Description,
			Source:       string(comp.Source),
			SourceFile:   comp.SourceFile,
			PartitionKey: comp.PartitionKey,
			HasState:     !comp.State.IsZero(),
			Instances:    extraStrings(comp.Extra, "port_instances"),
			Inputs:       slotViews(comp.Inputs),
			Outputs:      slotViews(comp.Outputs),
			StateEvents:  slotViews(comp.StateEvents),
		})
	}
	return out
}

func slotViews(slots []eventmodel.Slot) []slotView {
	if len(slots) == 0 {
		return nil
	}
	out := make([]slotView, 0, len(slots))
	for _, slot := range slots {
		out = append(out, slotView{
			Kind:        slot.Kind,
			Pattern:     slot.Pattern,
			Description: slot.Description,
			Delivery:    string(slot.Delivery),
			Role:        extraString(slot.Extra, "role"),
			Instances:   extraStrings(slot.Extra, "instances"),
		})
	}
	return out
}

func flowViews(g *eventmodel.Graph) []flowView {
	flows := eventmodel.BuildFlows(g)
	out := make([]flowView, 0, len(flows))
	for _, f := range flows {
		out = append(out, flowView{
			From:    f.From,
			To:      f.To,
			Kind:    f.Kind,
			Trigger: f.Trigger,
			Health:  f.Health,
		})
	}
	return out
}

func kindViews(m *eventmodel.Model, g *eventmodel.Graph) []kindView {
	// Attributes the graph already computed — health, the canonical pattern,
	// the partition coordinates — rather than a second derivation of each.
	attrs := make(map[string]map[string]any)
	for _, node := range g.Nodes {
		if node.Kind == eventmodel.NodeEventKind {
			attrs[node.ID] = node.Attrs
		}
	}

	producers := make(map[string][]string)
	triggers := make(map[string][]string)
	folders := make(map[string][]string)
	descriptions := make(map[string]string)
	classes := make(map[string]string)
	schemas := make(map[string]any)
	// The component a kind's schema came from, kept because $ref resolution
	// is relative to it.
	schemaOwners := make(map[string]*eventmodel.Component)

	ids := make([]string, 0, len(m.Components))
	for id := range m.Components {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		comp := m.Components[id]
		collect := func(slots []eventmodel.Slot, into map[string][]string) {
			for _, slot := range slots {
				into[slot.Kind] = append(into[slot.Kind], id)
				if descriptions[slot.Kind] == "" {
					descriptions[slot.Kind] = slot.Description
				}
				if classes[slot.Kind] == "" {
					classes[slot.Kind] = extraString(slot.Extra, "class")
				}
				if _, seen := schemas[slot.Kind]; !seen && !slot.Schema.IsZero() {
					schemas[slot.Kind] = slot.Schema.Raw
					schemaOwners[slot.Kind] = comp
				}
			}
		}
		collect(comp.Outputs, producers)
		collect(comp.Inputs, triggers)
		collect(comp.StateEvents, folders)
	}

	names := make([]string, 0, len(attrs))
	for id := range attrs {
		names = append(names, trimKindNode(id))
	}
	sort.Strings(names)

	out := make([]kindView, 0, len(names))
	for _, name := range names {
		a := attrs["kind:"+name]
		out = append(out, kindView{
			Name:         name,
			Pattern:      attrString(a, "pattern"),
			Description:  descriptions[name],
			PartitionKey: attrStrings(a, "partition_key"),
			Delivery:     attrString(a, "delivery"),
			Health:       attrString(a, "health"),
			Class:        classes[name],
			Owner:        eventmodel.OwnerOf(m, name),
			Producers:    producers[name],
			Triggers:     triggers[name],
			Folders:      folders[name],
			Schema:       schemas[name],
			Example:      eventmodel.ExampleOf(m, schemaOwners[name], eventmodel.SchemaNode{Raw: schemas[name]}),
		})
	}
	return out
}

func trimKindNode(id string) string {
	const prefix = "kind:"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
}

func attrString(attrs map[string]any, key string) string {
	s, _ := attrs[key].(string)
	return s
}

func attrStrings(attrs map[string]any, key string) []string {
	out, _ := attrs[key].([]string)
	return out
}

func extraString(extra map[string]any, key string) string {
	s, _ := extra[key].(string)
	return s
}

// extraStrings reads a string list out of opaque Extra data, which arrives as
// []string from a typed decode and as []any from a YAML one.
func extraStrings(extra map[string]any, key string) []string {
	switch typed := extra[key].(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
