package eventmodel

import (
	"reflect"
	"testing"
)

func TestBuildGraphNodes(t *testing.T) {
	// Build a model with components, kinds, and types.
	billing := comp("billing", "billing")
	billing.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Inputs = []Slot{{Kind: "billing.invoice.issue", Pattern: "svc.*.billing.{account}.invoice.issue"}}
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}
	billing.StateEvents = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}
	billing.PartitionKey = []string{"account"}

	shipping := comp("shipping", "shipping")
	shipping.Inputs = []Slot{{Kind: "billing.invoice.issued", Pattern: "svc.*.billing.{account}.invoice.issued"}}

	m := model(billing, shipping)
	g := BuildGraph(m)

	// Check node counts.
	nodesByKind := make(map[NodeKind]int)
	for _, n := range g.Nodes {
		nodesByKind[n.Kind]++
	}

	if nodesByKind[NodeComponent] != 2 {
		t.Errorf("want 2 component nodes, got %d", nodesByKind[NodeComponent])
	}
	// 2 kinds: billing.invoice.issue and billing.invoice.issued
	if nodesByKind[NodeEventKind] != 2 {
		t.Errorf("want 2 kind nodes, got %d", nodesByKind[NodeEventKind])
	}
	if nodesByKind[NodeType] != 1 {
		t.Errorf("want 1 type node, got %d", nodesByKind[NodeType])
	}

	// Check component node attributes. The fold is not a node of its own, so
	// the read-set and its key ride on the component that holds them.
	var billingNode Node
	for _, n := range g.Nodes {
		if n.ID == "component:billing" {
			billingNode = n
			break
		}
	}
	if billingNode.ID == "" {
		t.Fatal("billing component node not found")
	}
	if billingNode.Attrs["owns"] != "billing" {
		t.Errorf("billing owns = %v, want billing", billingNode.Attrs["owns"])
	}
	if got := billingNode.Attrs["partition_arity"]; got != 1 {
		t.Errorf("billing partition_arity = %v, want 1", got)
	}
	// Read-set is inputs + state events; the output-only pattern is the same
	// subject as the state event here, so the deduplicated set is 2.
	subjects, _ := billingNode.Attrs["subjects"].([]string)
	if len(subjects) != 2 {
		t.Errorf("billing subjects = %v, want 2 entries", subjects)
	}
}

func TestBuildGraphEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
	billing.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
	billing.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}

	ledger := comp("ledger", "ledger")
	ledger.Inputs = []Slot{{Kind: "billing.invoice.issued"}}

	gateway := comp("gateway", "gateway")
	gateway.Outputs = []Slot{{Kind: "billing.invoice.issue"}}

	m := model(billing, ledger, gateway)
	g := BuildGraph(m)

	edgesByKind := make(map[EdgeKind]int)
	for _, e := range g.Edges {
		edgesByKind[e.Kind]++
	}

	// billing outputs billing.invoice.issued, gateway outputs billing.invoice.issue
	if edgesByKind[EdgeOutput] != 2 {
		t.Errorf("want 2 output edges, got %d", edgesByKind[EdgeOutput])
	}
	// billing is triggered by billing.invoice.issue, ledger by billing.invoice.issued
	if edgesByKind[EdgeInput] != 2 {
		t.Errorf("want 2 input edges, got %d", edgesByKind[EdgeInput])
	}
	// billing folds its own output.
	if edgesByKind[EdgeStateEvent] != 1 {
		t.Errorf("want 1 state-event edge, got %d", edgesByKind[EdgeStateEvent])
	}

	// Direction: outputs point away from the component, the other two into it.
	for _, e := range g.Edges {
		switch e.Kind {
		case EdgeOutput:
			if e.From != componentID("billing") && e.From != componentID("gateway") {
				t.Errorf("output edge should start at a component, got %s", e.From)
			}
		case EdgeInput, EdgeStateEvent:
			if e.To != componentID("billing") && e.To != componentID("ledger") {
				t.Errorf("%s edge should end at a component, got %s", e.Kind, e.To)
			}
		}
	}
}

func TestBuildGraphHealth(t *testing.T) {
	tests := []struct {
		name       string
		model      *Model
		kind       string
		wantHealth Health
	}{
		{
			name: "ok: output with a consumer",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
				func() *Component {
					c := comp("shipping", "shipping")
					c.Inputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOK,
		},
		{
			name: "orphan: output nobody observes",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOrphan,
		},
		{
			name: "starved: input with no producer",
			model: model(
				func() *Component {
					c := comp("shipping", "shipping")
					c.Inputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthStarved,
		},
		{
			// Without the exclusive opt-in, many consumers is the healthy
			// event-sourced default, not an ambiguity.
			name: "ok: broadcast kind with multiple consumers",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Outputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("billing1", "billing1")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("billing2", "billing2")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthOK,
		},
		{
			name: "ambiguous: exclusive kind with multiple consumers",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Outputs = []Slot{{Kind: "billing.invoice.issue", Delivery: DeliveryExclusive}}
					return c
				}(),
				func() *Component {
					c := comp("billing1", "billing1")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("billing2", "billing2")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthAmbiguous,
		},
		{
			// Folding is observation: a kind nobody is triggered by, but that
			// some component keeps in state, is not an orphan.
			name: "ok: output observed only as a state event",
			model: model(
				func() *Component {
					c := comp("billing", "billing")
					c.Outputs = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
				func() *Component {
					c := comp("analytics", "analytics")
					c.StateEvents = []Slot{{Kind: "billing.invoice.issued"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issued",
			wantHealth: HealthOK,
		},
		{
			// Exclusive cardinality counts inputs alone: any number of
			// components may fold the kind without competing to handle it.
			name: "ok: exclusive kind with one consumer and extra folders",
			model: model(
				func() *Component {
					c := comp("gateway", "gateway")
					c.Outputs = []Slot{{Kind: "billing.invoice.issue", Delivery: DeliveryExclusive}}
					return c
				}(),
				func() *Component {
					c := comp("billing", "billing")
					c.Inputs = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
				func() *Component {
					c := comp("audit", "audit")
					c.StateEvents = []Slot{{Kind: "billing.invoice.issue"}}
					return c
				}(),
			),
			kind:       "billing.invoice.issue",
			wantHealth: HealthOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := BuildGraph(tc.model)
			var found bool
			for _, n := range g.Nodes {
				if n.ID == kindID(tc.kind) {
					found = true
					gotHealth := Health(n.Attrs["health"].(string))
					if gotHealth != tc.wantHealth {
						t.Errorf("health = %q, want %q", gotHealth, tc.wantHealth)
					}
					break
				}
			}
			if !found {
				t.Errorf("kind node %q not found", tc.kind)
			}
		})
	}
}

func TestBuildGraphPayloadAndRefEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Types["Line"] = SchemaNode{Raw: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"product": map[string]any{"$ref": "#/types/Invoice"},
		},
	}}
	billing.Outputs = []Slot{{
		Kind:   "billing.invoice.issued",
		Schema: SchemaNode{Raw: map[string]any{"$ref": "#/types/Invoice"}},
	}}

	shipping := comp("shipping", "shipping")
	shipping.Inputs = []Slot{{Kind: "billing.invoice.issued"}}

	m := model(billing, shipping)
	g := BuildGraph(m)

	// Check for payload edge: kind --payload--> type
	var foundPayload bool
	for _, e := range g.Edges {
		if e.Kind == EdgePayload && e.From == kindID("billing.invoice.issued") {
			foundPayload = true
			if e.To != typeID("billing", "Invoice") {
				t.Errorf("payload edge to = %q, want %q", e.To, typeID("billing", "Invoice"))
			}
		}
	}
	if !foundPayload {
		t.Error("payload edge not found")
	}

	// Check for refs edge: type --refs--> type (Line refs Invoice)
	var foundRefs bool
	for _, e := range g.Edges {
		if e.Kind == EdgeRefs && e.From == typeID("billing", "Line") {
			foundRefs = true
			if e.To != typeID("billing", "Invoice") {
				t.Errorf("refs edge to = %q, want %q", e.To, typeID("billing", "Invoice"))
			}
		}
	}
	if !foundRefs {
		t.Error("refs edge not found")
	}
}

func TestBuildGraphVocabEdges(t *testing.T) {
	billing := comp("billing", "billing")
	billing.Types["Invoice"] = SchemaNode{Raw: map[string]any{"type": "object"}}
	billing.Types["Line"] = SchemaNode{Raw: map[string]any{"type": "object"}}

	m := model(billing)
	g := BuildGraph(m)

	// Should have 2 types edges: component -> type
	var typesEdges int
	for _, e := range g.Edges {
		if e.Kind == EdgeDefines && e.From == componentID("billing") {
			typesEdges++
		}
	}
	if typesEdges != 2 {
		t.Errorf("want 2 types edges, got %d", typesEdges)
	}
}

// TestBuildGraphPatternConflict: where declarations disagree about a kind's
// address the projection must pick deterministically and say so, not silently
// expose whichever pattern the map iteration happened to land on.
func TestBuildGraphPatternConflict(t *testing.T) {
	producer := comp("llm", "llm")
	producer.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}

	consumer := comp("router", "router")
	consumer.Inputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.router.{session}.message"}}

	for i := 0; i < 20; i++ {
		g := BuildGraph(model(producer, consumer))
		var node *Node
		for j := range g.Nodes {
			if g.Nodes[j].ID == kindID("llm.message") {
				node = &g.Nodes[j]
				break
			}
		}
		if node == nil {
			t.Fatal("kind node not found")
		}
		// "llm" sorts before "router", so its declaration is canonical.
		if node.Attrs["pattern"] != "svc.*.llm.{session}.message" {
			t.Fatalf("pattern = %v, want a stable llm-side pattern", node.Attrs["pattern"])
		}
		if node.Attrs["pattern_conflict"] != true {
			t.Fatalf("pattern_conflict should be set, attrs = %v", node.Attrs)
		}
	}
}

func TestBuildGraphNoPatternConflictFlagWhenConsistent(t *testing.T) {
	c := comp("llm", "llm")
	c.Outputs = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}
	c.StateEvents = []Slot{{Kind: "llm.message", Pattern: "svc.*.llm.{session}.message"}}

	g := BuildGraph(model(c))
	for _, n := range g.Nodes {
		if n.ID == kindID("llm.message") {
			if _, ok := n.Attrs["pattern_conflict"]; ok {
				t.Errorf("pattern_conflict must be absent when declarations agree: %v", n.Attrs)
			}
			return
		}
	}
	t.Fatal("kind node not found")
}

// A kind a port family puts on one address per instance has no canonical
// address, and the projection must not publish one. The first declared address
// is the first instance in sorted order and nothing more: it says the kind
// travels there, which is a third true with three instances, and it moves to a
// different instance the moment one sorts ahead of the old first — which is
// how a commit adding channel/adminui reported every kind under `channel` as
// changed. Subjects carries the whole answer, sorted, so a reader and a diff
// both see a set rather than an arrival order.
func TestBuildGraphOmitsTheAddressOfAnImportedKindThatTravelsSeveral(t *testing.T) {
	const kind = "channel.command.message.send"
	instance := func(name string) *Component {
		c := comp("channel/"+name, "channel")
		c.Source = SourceAsyncAPI
		c.Inputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.channel." + name + ".thread.{thread}.command.message.send"}}
		return c
	}

	subjectsOf := func(t *testing.T, m *Model) []string {
		t.Helper()
		g := BuildGraph(m)
		for _, n := range g.Nodes {
			if n.ID != kindID(kind) {
				continue
			}
			if pattern, ok := n.Attrs["pattern"]; ok {
				t.Errorf("pattern = %v, want none for a kind on several addresses", pattern)
			}
			subjects, _ := n.Attrs["subjects"].([]string)
			return subjects
		}
		t.Fatal("kind node not found")
		return nil
	}

	before := subjectsOf(t, model(instance("runner"), instance("slack")))
	want := []string{
		"app.{tenant}.channel.runner.thread.{thread}.command.message.send",
		"app.{tenant}.channel.slack.thread.{thread}.command.message.send",
	}
	if !reflect.DeepEqual(before, want) {
		t.Fatalf("subjects = %v, want %v", before, want)
	}

	// Adding an instance adds its address and moves nothing else.
	after := subjectsOf(t, model(instance("adminui"), instance("runner"), instance("slack")))
	want = append([]string{"app.{tenant}.channel.adminui.thread.{thread}.command.message.send"}, want...)
	if !reflect.DeepEqual(after, want) {
		t.Errorf("subjects after the instance joined = %v, want %v", after, want)
	}
}

// One address is still an address: a kind that travels one keeps it, and a
// reader of a single-instance component sees the subject rather than a
// one-entry list.
func TestBuildGraphKeepsTheAddressOfAnImportedKindThatTravelsOne(t *testing.T) {
	const kind = "tools.command.call"
	c := comp("tools/datetime", "tools")
	c.Source = SourceAsyncAPI
	c.Inputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.tools.datetime.call.{call}.command.call"}}

	g := BuildGraph(model(c))
	for _, n := range g.Nodes {
		if n.ID != kindID(kind) {
			continue
		}
		if got := n.Attrs["pattern"]; got != "app.{tenant}.tools.datetime.call.{call}.command.call" {
			t.Errorf("pattern = %v, want the one address it travels", got)
		}
		if subjects, ok := n.Attrs["subjects"]; ok {
			t.Errorf("subjects = %v, want none when there is one address", subjects)
		}
		return
	}
	t.Fatal("kind node not found")
}

// A port family is one kind on one address per instance. A producer that named
// a single instance reaches that instance — not every sibling that takes the
// same kind, which is what a kind-name-only rule draws.
func TestBuildFlowsMatchesOnAddress(t *testing.T) {
	const (
		runnerSubject = "app.{tenant}.channel.runner.thread.{thread}.command.message.send"
		slackSubject  = "app.{tenant}.channel.slack.thread.{thread}.command.message.send"
		anyChannel    = "app.{tenant}.channel.{name}.thread.{thread}.command.message.send"
		kind          = "channel.command.message.send"
	)

	runner := comp("channel/runner", "app.{tenant}.channel.runner")
	runner.Inputs = []Slot{{Kind: kind, Pattern: runnerSubject}}
	slack := comp("channel/slack", "app.{tenant}.channel.slack")
	slack.Inputs = []Slot{{Kind: kind, Pattern: slackSubject}}

	t.Run("an append to one instance reaches that instance", func(t *testing.T) {
		egress := comp("egress/router", "app.{tenant}.egress")
		egress.Outputs = []Slot{{Kind: kind, Pattern: runnerSubject}}

		flows := BuildFlows(BuildGraph(model(runner, slack, egress)))

		want := []Flow{{From: "egress/router", To: "channel/runner", Kind: kind, Trigger: true, Health: "ok"}}
		if !reflect.DeepEqual(flows, want) {
			t.Errorf("flows = %+v, want %+v", flows, want)
		}
	})

	t.Run("an append to each instance reaches each once", func(t *testing.T) {
		egress := comp("egress/router", "app.{tenant}.egress")
		egress.Outputs = []Slot{
			{Kind: kind, Pattern: runnerSubject},
			{Kind: kind, Pattern: slackSubject},
		}

		flows := BuildFlows(BuildGraph(model(runner, slack, egress)))

		want := []Flow{
			{From: "egress/router", To: "channel/runner", Kind: kind, Trigger: true, Health: "ok"},
			{From: "egress/router", To: "channel/slack", Kind: kind, Trigger: true, Health: "ok"},
		}
		if !reflect.DeepEqual(flows, want) {
			t.Errorf("flows = %+v, want %+v", flows, want)
		}
	})

	t.Run("an append with the instance left open reaches the family", func(t *testing.T) {
		egress := comp("egress/router", "app.{tenant}.egress")
		egress.Outputs = []Slot{{Kind: kind, Pattern: anyChannel}}

		flows := BuildFlows(BuildGraph(model(runner, slack, egress)))

		want := []Flow{
			{From: "egress/router", To: "channel/runner", Kind: kind, Trigger: true, Health: "ok"},
			{From: "egress/router", To: "channel/slack", Kind: kind, Trigger: true, Health: "ok"},
		}
		if !reflect.DeepEqual(flows, want) {
			t.Errorf("flows = %+v, want %+v", flows, want)
		}
	})

	t.Run("a declaration with no address still draws its flow", func(t *testing.T) {
		plain := comp("audit", "audit")
		plain.Inputs = []Slot{{Kind: "audit.event.recorded"}}
		writer := comp("ledger", "ledger")
		writer.Outputs = []Slot{{Kind: "audit.event.recorded", Pattern: "svc.ledger.recorded"}}

		flows := BuildFlows(BuildGraph(model(plain, writer)))

		want := []Flow{{From: "ledger", To: "audit", Kind: "audit.event.recorded", Trigger: true, Health: "ok"}}
		if !reflect.DeepEqual(flows, want) {
			t.Errorf("flows = %+v, want %+v", flows, want)
		}
	})
}

func TestSubjectsMatch(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "a.b.{c}", "a.b.{c}", true},
		{"a slot takes a literal", "a.{name}.c", "a.slack.c", true},
		{"two literals must agree", "a.runner.c", "a.slack.c", false},
		{"a star is open too", "svc.*.billing.issued", "svc.app.billing.issued", true},
		{"different lengths address different things", "a.b.c", "a.b.c.d", false},
		{"a missing subject matches anything", "", "a.b.c", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SubjectsMatch(tt.a, tt.b); got != tt.want {
				t.Errorf("SubjectsMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			if got := SubjectsMatch(tt.b, tt.a); got != tt.want {
				t.Errorf("SubjectsMatch(%q, %q) = %v, want %v (not symmetric)", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

// The address each append names is the address it reaches on. Collapsing the
// two sides on the kind name alone pairs every address with every observer:
// with two addresses and two observers that is four relations where the model
// declares two, and the two it invents are exactly the ones the addressing was
// there to prevent.
func TestBuildAppendsResolvesEachAddressSeparately(t *testing.T) {
	const (
		runnerSubject = "app.{tenant}.channel.runner.thread.{thread}.command.message.send"
		slackSubject  = "app.{tenant}.channel.slack.thread.{thread}.command.message.send"
		kind          = "channel.command.message.send"
	)

	runner := comp("channel/runner", "app.{tenant}.channel.runner")
	runner.Inputs = []Slot{{Kind: kind, Pattern: runnerSubject}}
	slack := comp("channel/slack", "app.{tenant}.channel.slack")
	slack.Inputs = []Slot{{Kind: kind, Pattern: slackSubject}}
	egress := comp("egress/router", "app.{tenant}.egress")
	egress.Outputs = []Slot{
		{Kind: kind, Pattern: runnerSubject},
		{Kind: kind, Pattern: slackSubject},
	}

	got := BuildAppends(BuildGraph(model(runner, slack, egress)))
	want := []Append{
		{
			From: "egress/router", Kind: kind, Subject: runnerSubject, Health: "ok",
			Reaches: []Reach{{To: "channel/runner", Trigger: true, Subject: runnerSubject}},
		},
		{
			From: "egress/router", Kind: kind, Subject: slackSubject, Health: "ok",
			Reaches: []Reach{{To: "channel/slack", Trigger: true, Subject: slackSubject}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appends = %+v, want %+v", got, want)
	}
}

// One kind, two addresses, one listener: the append nobody hears is a fact
// about that address, not about the kind. Reporting per kind hides it behind
// the sibling that works, and the kind scores healthy either way — every
// counting rule sees a producer and an observer.
func TestBuildAppendsReportsTheAddressNobodyHears(t *testing.T) {
	const (
		heard   = "app.{tenant}.channel.runner.thread.{thread}.command.message.send"
		unheard = "app.{tenant}.channel.slack.thread.{thread}.command.message.send"
		kind    = "channel.command.message.send"
	)

	runner := comp("channel/runner", "app.{tenant}.channel.runner")
	runner.Inputs = []Slot{{Kind: kind, Pattern: heard}}
	egress := comp("egress/router", "app.{tenant}.egress")
	egress.Outputs = []Slot{{Kind: kind, Pattern: heard}, {Kind: kind, Pattern: unheard}}

	got := BuildAppends(BuildGraph(model(runner, egress)))
	if len(got) != 2 {
		t.Fatalf("got %d appends, want one per address: %+v", len(got), got)
	}
	if len(got[0].Reaches) != 1 || got[0].Reaches[0].To != "channel/runner" {
		t.Errorf("append on %s reaches %+v, want channel/runner", got[0].Subject, got[0].Reaches)
	}
	if len(got[1].Reaches) != 0 {
		t.Errorf("append on %s reaches %+v, want nobody", got[1].Subject, got[1].Reaches)
	}
	// The pair-level view still says the pair is connected, because it is.
	if flows := BuildFlows(BuildGraph(model(runner, egress))); len(flows) != 1 {
		t.Errorf("flows = %+v, want one edge", flows)
	}
}

// A component folding what it just appended is recording an outcome. It is not
// a relation between two components, so it is not one of the observers an
// append reaches.
func TestBuildAppendsSkipsTheSelfFold(t *testing.T) {
	const kind = "ingress.event.message.routed"

	only := comp("ingress/router", "ingress")
	only.Outputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.ingress.event.message.routed"}}
	only.StateEvents = []Slot{{Kind: kind, Pattern: "app.{tenant}.ingress.event.message.routed"}}

	got := BuildAppends(BuildGraph(model(only)))
	if len(got) != 1 || len(got[0].Reaches) != 0 {
		t.Errorf("appends = %+v, want one append reaching nobody", got)
	}
}
