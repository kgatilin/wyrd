package retrieval

import "testing"

func TestDisplayKind_UsesGoWords(t *testing.T) {
	for stored, want := range map[string]string{
		"class": "struct", "iface": "interface",
		"func": "func", "method": "method", "type": "type",
		"const": "const", "var": "var", "error": "error",
	} {
		if got := DisplayKind(stored); got != want {
			t.Errorf("DisplayKind(%q) = %q, want %q", stored, got, want)
		}
	}
}

func TestCanonicalKind_AcceptsEitherVocabulary(t *testing.T) {
	// The invariant: whatever word an answer printed for a kind is a word a
	// filter accepts, and it resolves to the kind the graph stores.
	for _, stored := range []string{"class", "iface", "func", "method", "type", "const", "var", "error"} {
		if got := CanonicalKind(DisplayKind(stored)); got != stored {
			t.Errorf("round trip for %q: printed %q, resolved to %q", stored, DisplayKind(stored), got)
		}
		// The graph's own spelling keeps working, so nothing already written breaks.
		if got := CanonicalKind(stored); got != stored {
			t.Errorf("CanonicalKind(%q) = %q, want it unchanged", stored, got)
		}
	}
}

func TestCanonicalKindSet_NilMeansNoFilter(t *testing.T) {
	if got := canonicalKindSet(nil); got != nil {
		t.Errorf("no kinds should mean no filter, got %v", got)
	}
	set := canonicalKindSet([]string{"struct", "iface"})
	if !set["class"] || !set["iface"] {
		t.Errorf("both vocabularies should resolve into the set: %v", set)
	}
	if set["struct"] {
		t.Errorf("the set is tested against stored kinds, not printed words: %v", set)
	}
}

func TestCanonicalKind_PassesThroughUnknownWords(t *testing.T) {
	// An unknown kind is the caller's typo to see in an empty result, not
	// something to silently rewrite into a kind that exists.
	if got := CanonicalKind("struckt"); got != "struckt" {
		t.Errorf("CanonicalKind rewrote an unknown word to %q", got)
	}
}
