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

func TestUnknownKinds_NamesOnlyWhatItCannotResolve(t *testing.T) {
	// Both vocabularies resolve, so neither is reported.
	if bad := UnknownKinds([]string{"struct", "class", "interface", "iface", "method"}); bad != nil {
		t.Errorf("known kinds reported as unknown: %v", bad)
	}
	if bad := UnknownKinds(nil); bad != nil {
		t.Errorf("no filter should report nothing: %v", bad)
	}
	// A typo is named, in the order given, once each, and the good entries
	// alongside it are not.
	bad := UnknownKinds([]string{"nonsense", "func", "strct", "nonsense"})
	if len(bad) != 2 || bad[0] != "nonsense" || bad[1] != "strct" {
		t.Errorf("UnknownKinds = %v, want [nonsense strct]", bad)
	}
}

func TestNodeKinds_ArePrintedWordsAndAllResolve(t *testing.T) {
	words := NodeKinds()
	if len(words) != len(nodeKinds) {
		t.Fatalf("NodeKinds returned %d words for %d kinds", len(words), len(nodeKinds))
	}
	// The list an error offers back has to be a list that then works.
	if bad := UnknownKinds(words); bad != nil {
		t.Errorf("the accepted-kinds list is not itself accepted: %v", bad)
	}
	for _, w := range words {
		if w == "class" || w == "iface" {
			t.Errorf("the offered list leaks the graph's spelling: %v", words)
		}
	}
}

func TestUnknownEdgeKinds(t *testing.T) {
	if bad := UnknownEdgeKinds(EdgeKinds()); bad != nil {
		t.Errorf("the accepted edge kinds are not accepted: %v", bad)
	}
	if bad := UnknownEdgeKinds([]string{"calls", "invokes"}); len(bad) != 1 || bad[0] != "invokes" {
		t.Errorf("UnknownEdgeKinds = %v, want [invokes]", bad)
	}
}
