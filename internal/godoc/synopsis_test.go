package godoc

import (
	"strings"
	"testing"
)

func TestSynopsis_UnwrapsAndStopsAtTheSentence(t *testing.T) {
	// The case that motivated the package: a wrapped comment whose first line
	// ends mid-clause. Cutting at the line break, or at a fixed width, hands
	// the reader half a sentence.
	doc := "NewTenantStore wraps a Store as the application event bus door.\n" +
		"Tenancy is enforced here and nowhere else."
	if got, want := Synopsis(doc, 200), "NewTenantStore wraps a Store as the application event bus door."; got != want {
		t.Errorf("Synopsis = %q, want %q", got, want)
	}
}

func TestSynopsis_KeepsInitialsInsideTheSentence(t *testing.T) {
	for _, tc := range []struct{ doc, want string }{
		{"Ported from R. Pike's original. Rest follows.", "Ported from R. Pike's original."},
		{"Applies U.S. law only. Elsewhere it is a no-op.", "Applies U.S. law only."},
		{"Ends here! And continues.", "Ends here!"},
	} {
		if got := Synopsis(tc.doc, 200); got != tc.want {
			t.Errorf("Synopsis(%q) = %q, want %q", tc.doc, got, tc.want)
		}
	}
}

func TestSynopsis_StopsAtTheParagraphBreak(t *testing.T) {
	doc := "A headline with no period\n\nSecond paragraph explains it."
	if got, want := Synopsis(doc, 200), "A headline with no period"; got != want {
		t.Errorf("Synopsis = %q, want %q", got, want)
	}
}

func TestSynopsis_EmptyAndBlank(t *testing.T) {
	for _, doc := range []string{"", "   \n  \n"} {
		if got := Synopsis(doc, 200); got != "" {
			t.Errorf("Synopsis(%q) = %q, want empty", doc, got)
		}
	}
}

func TestClipWords_CutsOnAWordBoundary(t *testing.T) {
	got := ClipWords(strings.Repeat("word ", 40), 50)
	if !strings.HasSuffix(got, " …") {
		t.Errorf("clipped text should be marked: %q", got)
	}
	if strings.Contains(strings.TrimSuffix(got, " …"), "wor ") || strings.HasSuffix(strings.TrimSuffix(got, " …"), "wor") {
		t.Errorf("cut landed mid-token: %q", got)
	}
	if len(got) > 60 {
		t.Errorf("cut ran well past the budget: %q", got)
	}
	// A single token longer than the budget has no boundary to fall back to;
	// clipping it short of the budget would return nothing useful.
	long := ClipWords(strings.Repeat("w", 300), 50)
	if len(long) < 50 {
		t.Errorf("a budget-long token should still fill the budget: %q", long)
	}
	// Multi-byte runes must not be severed.
	if got := ClipWords(strings.Repeat("é", 50), 11); !strings.HasSuffix(got, " …") || strings.Contains(got, "�") {
		t.Errorf("clip corrupted a rune boundary: %q", got)
	}
}

func TestClipWords_UnderBudgetIsUntouched(t *testing.T) {
	if got := ClipWords("short", 100); got != "short" {
		t.Errorf("under-budget text altered: %q", got)
	}
}
