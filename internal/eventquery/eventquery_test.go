package eventquery

import (
	"bytes"
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
	hits := Search(shop(), "invoice.issued", 0)
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

	hits := Search(m, "shop.invoice.issued", 0)
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
			hits := Search(shop(), tc.query, 0)
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
	if hits := Search(shop(), "   ", 0); len(hits) != 0 {
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

	hits := WithHealth(Search(m, "app.thing.done", 0), eventmodel.BuildGraph(m))
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
