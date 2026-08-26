package eventmodel

import (
	"reflect"
	"testing"
)

// The case this whole rule exists for: two components agree on the kind name
// and disagree on the address. Counting declarations by name scores both ends
// healthy, and the flow builder — which matches on the address — draws nothing,
// so before HealthUnwired a broken wire and a relation the model never had
// looked identical.
func TestUnwiredKindWhenTheEndsNameDifferentAddresses(t *testing.T) {
	const (
		kind       = "entrypoint.command.message.process"
		appendsOn  = "app.{tenant}.entrypoint.agent.session.{session}.command.message.process"
		listensOn  = "app.{tenant}.entrypoint.default.session.{session}.command.message.process"
		producerID = "ingress/router"
		consumerID = "entrypoint/default"
	)

	producer := comp(producerID, "ingress")
	producer.Outputs = []Slot{{Kind: kind, Pattern: appendsOn}}
	consumer := comp(consumerID, "entrypoint")
	consumer.Inputs = []Slot{{Kind: kind, Pattern: listensOn}}
	m := model(producer, consumer)

	if got := UnwiredKinds(m); !got[kind] {
		t.Fatalf("UnwiredKinds = %v, want %q flagged", got, kind)
	}

	g := BuildGraph(m)
	if got := kindHealth(t, g, kind); got != string(HealthUnwired) {
		t.Errorf("health = %q, want %q", got, HealthUnwired)
	}

	// The pair is drawn, not dropped. A reader who sees no line cannot tell a
	// broken wire from an absent one, which is the failure this replaces.
	want := []Flow{{
		From: producerID, To: consumerID, Kind: kind,
		Trigger: true, Health: string(HealthUnwired),
	}}
	if got := BuildFlows(g); !reflect.DeepEqual(got, want) {
		t.Errorf("flows = %+v, want %+v", got, want)
	}
}

// A component appending on one address and folding on another is the normal
// self-fold idiom seen twice, not a broken wire: BuildFlows never draws a
// self-loop, so there is no missing line to explain.
func TestUnwiredIgnoresPairsInsideOneComponent(t *testing.T) {
	const kind = "ingress.event.message.routed"

	only := comp("ingress/router", "ingress")
	only.Outputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.ingress.channel.{channel}.event.message.routed"}}
	only.StateEvents = []Slot{{Kind: kind, Pattern: "app.{tenant}.ingress.thread.{thread}.event.message.routed"}}

	if got := UnwiredKinds(model(only)); len(got) != 0 {
		t.Errorf("UnwiredKinds = %v, want none", got)
	}
}

// A port family puts one kind on one address per instance. An append aimed at
// an instance that is present must not be reported just because a sibling
// instance listens elsewhere.
func TestUnwiredIgnoresAFamilyWithAMatchingInstance(t *testing.T) {
	const kind = "channel.command.message.send"

	runner := comp("channel/runner", "app.{tenant}.channel.runner")
	runner.Inputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.channel.runner.thread.{thread}.command.message.send"}}
	slack := comp("channel/slack", "app.{tenant}.channel.slack")
	slack.Inputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.channel.slack.thread.{thread}.command.message.send"}}
	egress := comp("egress/router", "app.{tenant}.egress")
	egress.Outputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.channel.runner.thread.{thread}.command.message.send"}}

	if got := UnwiredKinds(model(runner, slack, egress)); len(got) != 0 {
		t.Errorf("UnwiredKinds = %v, want none", got)
	}
}

// Both ends must be inside the composed set. A kind nobody observes is an
// orphan and keeps saying so — the states answer different questions and the
// new one must not swallow the old.
func TestUnwiredLeavesOrphanAndStarvedAlone(t *testing.T) {
	orphaned := comp("ledger", "ledger")
	orphaned.Outputs = []Slot{{Kind: "ledger.event.posted", Pattern: "app.ledger.event.posted"}}
	hungry := comp("audit", "audit")
	hungry.Inputs = []Slot{{Kind: "audit.event.recorded", Pattern: "app.audit.event.recorded"}}
	g := BuildGraph(model(orphaned, hungry))

	if got := kindHealth(t, g, "ledger.event.posted"); got != string(HealthOrphan) {
		t.Errorf("orphan health = %q, want %q", got, HealthOrphan)
	}
	if got := kindHealth(t, g, "audit.event.recorded"); got != string(HealthStarved) {
		t.Errorf("starved health = %q, want %q", got, HealthStarved)
	}
}

// Validate reports the finding over imported documents, which every other rule
// here skips. A repository whose declarations are all AsyncAPI — the case that
// surfaced this — would otherwise validate clean with a broken wire in it.
func TestValidateReportsUnwiredAcrossImportedDocuments(t *testing.T) {
	const kind = "entrypoint.command.cancel"

	producer := comp("ingress/router", "")
	producer.Source = SourceAsyncAPI
	producer.Outputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.entrypoint.agent.session.{session}.command.cancel"}}
	consumer := comp("entrypoint/default", "")
	consumer.Source = SourceAsyncAPI
	consumer.Inputs = []Slot{{Kind: kind, Pattern: "app.{tenant}.entrypoint.default.session.{session}.command.cancel"}}

	findings := findingsByKind(Validate(model(producer, consumer)), KindUnwiredKind)
	if len(findings) != 1 {
		t.Fatalf("got %d unwired findings, want 1: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %q, want %q", f.Severity, SeverityWarning)
	}
	// Reported at the producer: that is the end whose address has to change,
	// or whose consumer has to.
	if f.Component != "ingress/router" || f.Location != kind {
		t.Errorf("reported at %s:%s, want ingress/router:%s", f.Component, f.Location, kind)
	}
	heard, _ := f.Related["observed_on"].([]string)
	want := []string{"app.{tenant}.entrypoint.default.session.{session}.command.cancel"}
	if !reflect.DeepEqual(heard, want) {
		t.Errorf("observed_on = %v, want %v", heard, want)
	}
}

func kindHealth(t *testing.T, g *Graph, kind string) string {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID != kindID(kind) {
			continue
		}
		health, _ := n.Attrs["health"].(string)
		return health
	}
	t.Fatalf("no node for kind %q", kind)
	return ""
}

// Two components claiming one namespace is legal among imported documents —
// nothing grades them — so OwnerOf still has to answer, and no claimant is the
// answer. Instances of a port family each claim the family's namespace by
// being instances of it; none of them defines it for the others.
func TestOwnerOfNamesTheNamespaceWhenTheClaimIsTied(t *testing.T) {
	runner := comp("channel/runner", "channel")
	runner.Source = SourceAsyncAPI
	slack := comp("channel/slack", "channel")
	slack.Source = SourceAsyncAPI
	m := model(runner, slack)

	if got := OwnerOf(m, "channel.command.message.send"); got != "channel" {
		t.Errorf("owner = %q, want the tied namespace", got)
	}
}

// The answer has to hold still across a commit that adds an instance. Naming a
// claimant did not: the first-in-sorted-order tiebreak moved from
// channel/runner to channel/adminui the moment adminui existed, and the model
// diff then reported every kind under `channel` as changed against a base that
// declared none of them differently — including the ones only slack declares.
func TestOwnerOfHoldsWhenAnInstanceSortingFirstJoins(t *testing.T) {
	instance := func(id string) *Component {
		c := comp(id, "channel")
		c.Source = SourceAsyncAPI
		return c
	}
	before := model(instance("channel/runner"), instance("channel/slack"))
	after := model(instance("channel/adminui"), instance("channel/runner"), instance("channel/slack"))

	for _, kind := range []string{"channel.command.message.send", "channel.event.slack.delivery.failed"} {
		was, now := OwnerOf(before, kind), OwnerOf(after, kind)
		if was != now {
			t.Errorf("owner of %s moved from %q to %q when an instance joined", kind, was, now)
		}
	}
}

// A longer claim still wins over a shorter one; sorting is only the tiebreak.
func TestOwnerOfPrefersTheLongestClaim(t *testing.T) {
	broad := comp("aaa/broad", "channel")
	narrow := comp("zzz/narrow", "channel.slack")
	if got := OwnerOf(model(broad, narrow), "channel.slack.event.sent"); got != "zzz/narrow" {
		t.Errorf("owner = %q, want the longer prefix even though it sorts last", got)
	}
}
