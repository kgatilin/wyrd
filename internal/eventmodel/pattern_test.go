package eventmodel

import "testing"

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern string
		kind    string
		want    bool
	}{
		// Literal matches.
		{"billing.invoice.issued", "billing.invoice.issued", true},
		{"billing.invoice.issued", "billing.invoice.paid", false},
		{"billing", "billing", true},
		{"billing", "ledger", false},

		// Single-segment wildcard.
		{"billing.*.issued", "billing.invoice.issued", true},
		{"billing.*.issued", "billing.credit.issued", true},
		{"billing.*.issued", "billing.invoice.paid", false},
		{"billing.*", "billing.invoice", true},
		{"billing.*", "billing", false},
		{"*", "billing", true},
		{"*", "billing.invoice", false},
		{"*.invoice", "billing.invoice", true},
		{"*.invoice", "ledger.invoice", true},
		{"*.*", "billing.invoice", true},
		{"*.*", "billing", false},

		// Tail wildcard ">".
		{"billing.>", "billing.invoice", true},
		{"billing.>", "billing.invoice.issued", true},
		{"billing.>", "billing", false}, // > requires at least one segment
		{"svc.billing.>", "svc.billing.foo.bar.baz", true},
		{">", "billing", true},
		{">", "billing.invoice.issued", true},

		// Tail wildcard "**".
		{"billing.**", "billing.invoice", true},
		{"billing.**", "billing.invoice.issued", true},
		{"billing.**", "billing", false},

		// Combined patterns.
		{"svc.*.invoice.>", "svc.billing.invoice.issued", true},
		{"svc.*.invoice.>", "svc.billing.invoice.paid.confirmed", true},
		{"svc.*.invoice.>", "svc.billing.invoice", false}, // > needs at least one
		{"*.*.>", "a.b.c", true},
		{"*.*.>", "a.b", false},

		// Edge cases.
		{"", "", true},
		{"a", "", false},
		{"", "a", false},
		{"a.b.c", "a.b", false},
		{"a.b", "a.b.c", false},
	}

	for _, tc := range cases {
		got := MatchPattern(tc.pattern, tc.kind)
		if got != tc.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v",
				tc.pattern, tc.kind, got, tc.want)
		}
	}
}
