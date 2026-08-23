package eventquery

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/eventmodel"
)

// shop is a three-component model with each of the three ways to observe a
// kind: a trigger, a self-fold, and a cross-component fold — plus one output
// nobody takes at all.
func shop() *eventmodel.Model {
	return &eventmodel.Model{Components: map[string]*eventmodel.Component{
		"orders/api": {
			ID:      "orders/api",
			Owns:    "shop.orders",
			Outputs: []eventmodel.Slot{{Kind: "shop.order.placed", Pattern: "shop.orders.{order}.placed", Description: "A customer placed an order"}},
		},
		"billing/ledger": {
			ID:           "billing/ledger",
			Owns:         "shop.billing",
			PartitionKey: []string{"order"},
			Inputs:       []eventmodel.Slot{{Kind: "shop.order.placed", Pattern: "shop.orders.{order}.placed"}},
			Outputs: []eventmodel.Slot{
				{Kind: "shop.invoice.issued", Pattern: "shop.billing.{order}.issued"},
				{Kind: "shop.audit.noted", Pattern: "shop.billing.{order}.noted"},
			},
			StateEvents: []eventmodel.Slot{{Kind: "shop.invoice.issued", Pattern: "shop.billing.{order}.issued"}},
		},
		"reporting/rollup": {
			ID:          "reporting/rollup",
			Owns:        "shop.reporting",
			StateEvents: []eventmodel.Slot{{Kind: "shop.invoice.issued", Pattern: "shop.billing.{order}.issued"}},
		},
	}}
}

func TestSearchFindsAKindWithEverythingItIsWiredTo(t *testing.T) {
	hits, _ := Search(shop(), "invoice.issued", 0)
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
	}

	hit := hits[0]
	if hit.Type != "kind" || hit.Name != "shop.invoice.issued" {
		t.Fatalf("wrong hit: %+v", hit)
	}
	if got := strings.Join(hit.Producers, ","); got != "billing/ledger" {
		t.Errorf("producers = %q", got)
	}
	if len(hit.Triggers) != 0 {
		t.Errorf("triggers = %v, want none", hit.Triggers)
	}
	if got := strings.Join(hit.Folders, ","); got != "billing/ledger,reporting/rollup" {
		t.Errorf("folders = %q", got)
	}
}

// A reader who types a name in full means that thing, not the six entries
// whose description mentions it.
func TestSearchRanksAnExactNameFirst(t *testing.T) {
	m := shop()
	m.Components["orders/api"].Description = "everything about shop.invoice.issued"

	hits, _ := Search(m, "shop.invoice.issued", 0)
	if len(hits) < 2 {
		t.Fatalf("got %d hits, want the kind and the component: %+v", len(hits), hits)
	}
	if hits[0].Name != "shop.invoice.issued" || hits[0].Matched != "name" {
		t.Errorf("first hit = %s (matched %s), want the exact kind", hits[0].Name, hits[0].Matched)
	}
}

func TestSearchMatchesComponentsAndSubjects(t *testing.T) {
	for _, tc := range []struct {
		query, wantName, wantMatched string
	}{
		{"billing/ledger", "billing/ledger", "id"},
		{"shop.reporting", "reporting/rollup", "owns"},
		{"orders.{order}", "shop.order.placed", "subject"},
		{"customer placed", "shop.order.placed", "description"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			hits, _ := Search(shop(), tc.query, 0)
			if len(hits) == 0 {
				t.Fatalf("no hits for %q", tc.query)
			}
			if hits[0].Name != tc.wantName || hits[0].Matched != tc.wantMatched {
				t.Errorf("first hit = %s (matched %s), want %s (matched %s)",
					hits[0].Name, hits[0].Matched, tc.wantName, tc.wantMatched)
			}
		})
	}
}

func TestSearchOnAnEmptyQueryReturnsNothing(t *testing.T) {
	if hits, _ := Search(shop(), "   ", 0); len(hits) != 0 {
		t.Errorf("blank query returned %d hits", len(hits))
	}
}

// Health is the graph's verdict, and a search result carries it so a caller
// does not have to run the validator separately to learn a wire is broken.
func TestWithHealthStampsTheGraphsVerdict(t *testing.T) {
	m := &eventmodel.Model{Components: map[string]*eventmodel.Component{
		"a": {ID: "a", Owns: "app.a", Outputs: []eventmodel.Slot{{Kind: "app.thing.done", Pattern: "app.a.{x}.done"}}},
		"b": {ID: "b", Owns: "app.b", Inputs: []eventmodel.Slot{{Kind: "app.thing.done", Pattern: "app.b.{x}.done"}}},
	}}

	hits, _ := Search(m, "app.thing.done", 0)
	hits = WithHealth(hits, eventmodel.BuildGraph(m))
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Health != string(eventmodel.HealthUnwired) {
		t.Errorf("health = %q, want %q", hits[0].Health, eventmodel.HealthUnwired)
	}
}

func TestWiringDrawsEachFlowOnceAndNamesTheUnobserved(t *testing.T) {
	var buf bytes.Buffer
	Wiring(shop(), eventmodel.BuildGraph(shop()), &buf)
	out := buf.String()

	for _, want := range []string{
		"3 components  3 kinds", // the header counts what was read
		"→ billing/ledger",      // triggered
		"⇢ reporting/rollup",    // folded elsewhere
		"(nobody)",              // shop.audit.noted is observed by no one
	} {
		if !strings.Contains(out, want) {
			t.Errorf("wiring is missing %q:\n%s", want, out)
		}
	}

	// A component folding its own output records an outcome; it is not an
	// edge, and drawing it would put every component in a loop with itself.
	if strings.Contains(out, "⇢ billing/ledger          shop.invoice.issued") {
		t.Errorf("self-fold drawn as an edge:\n%s", out)
	}
	if got := strings.Count(out, "shop.order.placed"); got != 1 {
		t.Errorf("shop.order.placed appears %d times, want 1:\n%s", got, out)
	}
}

func TestWiringOnAnEmptyModel(t *testing.T) {
	var buf bytes.Buffer
	Wiring(&eventmodel.Model{Components: map[string]*eventmodel.Component{}}, nil, &buf)
	if !strings.Contains(buf.String(), "No event declarations") {
		t.Errorf("unhelpful output: %q", buf.String())
	}
}

// ports is one kind published on one address per instance — the fan-out
// addressing exists for. Joining producers to observers on the kind name
// alone draws every producer address at every observer of the kind: four
// lines for two relations, two of which say a component is called on an
// address it never listens to.
func ports() *eventmodel.Model {
	const (
		kind    = "channel.command.message.send"
		runner  = "app.{tenant}.channel.runner.thread.{thread}.command.message.send"
		slack   = "app.{tenant}.channel.slack.thread.{thread}.command.message.send"
		report  = "channel.command.progress.report"
		runnerP = "app.{tenant}.channel.runner.thread.{thread}.command.progress.report"
		slackP  = "app.{tenant}.channel.slack.thread.{thread}.command.progress.report"
	)
	return &eventmodel.Model{Components: map[string]*eventmodel.Component{
		"channel/runner": {
			ID: "channel/runner", Owns: "app.{tenant}.channel.runner",
			Inputs: []eventmodel.Slot{
				{Kind: kind, Pattern: runner},
				{Kind: report, Pattern: runnerP},
			},
		},
		"channel/slack": {
			ID: "channel/slack", Owns: "app.{tenant}.channel.slack",
			Inputs: []eventmodel.Slot{
				{Kind: kind, Pattern: slack},
				{Kind: report, Pattern: slackP},
			},
		},
		"egress/router": {
			ID: "egress/router", Owns: "app.{tenant}.egress",
			Outputs: []eventmodel.Slot{
				{Kind: kind, Pattern: runner},
				{Kind: kind, Pattern: slack},
				{Kind: report, Pattern: runnerP},
				{Kind: report, Pattern: slackP},
			},
		},
	}}
}

func TestWiringSendsEachAddressToItsOwnSubscriber(t *testing.T) {
	var buf bytes.Buffer
	Wiring(ports(), eventmodel.BuildGraph(ports()), &buf)
	out := buf.String()

	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  →") || strings.HasPrefix(line, "  ⇢") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 4 {
		t.Errorf("got %d flow lines, want 4 (one per address):\n%s", len(lines), out)
	}
	for _, line := range lines {
		to, subject := strings.Fields(line)[1], line[strings.Index(line, " on ")+4:]
		instance := strings.TrimPrefix(to, "channel/")
		if !strings.Contains(subject, "channel."+instance+".") {
			t.Errorf("%s is addressed at another instance: %s", to, line)
		}
	}
}

// The address is on the line only where it settles something. Every append in
// the shop model takes one address, so naming it would widen every line to
// disambiguate nothing.
func TestWiringNamesTheAddressOnlyWhenAKindTakesSeveral(t *testing.T) {
	var buf bytes.Buffer
	Wiring(shop(), eventmodel.BuildGraph(shop()), &buf)
	if strings.Contains(buf.String(), " on ") {
		t.Errorf("single-address model printed addresses:\n%s", buf.String())
	}
}

// One kind, two addresses, one listener. The kind is healthy by every count —
// it has a producer and an observer — so the silent address is only visible
// per append.
func TestWiringNamesTheAddressNobodyHears(t *testing.T) {
	m := ports()
	delete(m.Components, "channel/slack")

	var buf bytes.Buffer
	Wiring(m, eventmodel.BuildGraph(m), &buf)
	out := buf.String()

	if got := strings.Count(out, "→ (nobody)"); got != 2 {
		t.Errorf("got %d unheard appends, want 2 (both slack addresses):\n%s", got, out)
	}
	if !strings.Contains(out, "(nobody)               channel.command.message.send  on app.{tenant}.channel.slack") {
		t.Errorf("the unheard append does not name its address:\n%s", out)
	}
}

// A page that reports its own length as the total is wrong in the one way a
// reader cannot catch.
func TestFormatHitsSaysWhatItWithheld(t *testing.T) {
	hits, total := Search(shop(), "shop", 2)
	if total <= len(hits) {
		t.Fatalf("got total %d for %d hits, want the query to overflow the limit", total, len(hits))
	}

	var buf bytes.Buffer
	FormatHits(&buf, hits, total, "shop")
	if want := fmt.Sprintf("showing %d of %d match(es)", len(hits), total); !strings.Contains(buf.String(), want) {
		t.Errorf("header does not say %q:\n%s", want, buf.String())
	}

	all, total := Search(shop(), "shop", 0)
	buf.Reset()
	FormatHits(&buf, all, total, "shop")
	if strings.Contains(buf.String(), "showing") {
		t.Errorf("an untruncated result claims truncation:\n%s", buf.String())
	}
}

// A component hit that gives only counts ends the enquiry it started.
func TestSearchAnswersWhichKindsAComponentIsWiredTo(t *testing.T) {
	hits, _ := Search(shop(), "billing/ledger", 0)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	hit := hits[0]
	if got := strings.Join(hit.Takes, ","); got != "shop.order.placed" {
		t.Errorf("takes = %q", got)
	}
	if got := strings.Join(hit.Appends, ","); got != "shop.audit.noted,shop.invoice.issued" {
		t.Errorf("appends = %q", got)
	}
	if got := strings.Join(hit.Folds, ","); got != "shop.invoice.issued" {
		t.Errorf("folds = %q", got)
	}

	var buf bytes.Buffer
	FormatHits(&buf, hits, len(hits), "billing/ledger")
	for _, want := range []string{"triggered by shop.order.placed", "appends      shop.audit.noted", "folds        shop.invoice.issued"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("component hit is missing %q:\n%s", want, buf.String())
		}
	}
}
