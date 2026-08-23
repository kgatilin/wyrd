package retrieval

// Node kinds have two spellings, and both reach this package.
//
// The graph stores `class` and `iface`, inherited from the generic code model
// it is projected through — Go has neither word. Everything a reader sees says
// `struct` and `interface`: the signature the extractor built, the package
// digest, and Go itself. So a caller who filters by what an answer printed is
// typing the other vocabulary, and used to get a silent zero for it.
//
// The mapping lives next to the kinds it translates rather than in each
// surface that would otherwise guess at it.

// displayKinds maps a graph node kind onto the Go word for it. Kinds the two
// vocabularies already agree on (func, method, type, const, var, error) are
// absent and pass through.
var displayKinds = map[string]string{
	"class": "struct",
	"iface": "interface",
}

// canonicalKinds is displayKinds inverted: the word a caller types onto the
// kind the graph stores.
var canonicalKinds = map[string]string{
	"struct":    "class",
	"interface": "iface",
}

// DisplayKind returns the Go word for a graph node kind. Renderers print this,
// never the stored kind — `class` names nothing in a Go codebase.
func DisplayKind(kind string) string {
	if word, ok := displayKinds[kind]; ok {
		return word
	}
	return kind
}

// CanonicalKind returns the stored kind for a word a caller supplied. Either
// vocabulary is accepted: a filter built from what an answer printed has to
// match the nodes that answer came from.
func CanonicalKind(word string) string {
	if kind, ok := canonicalKinds[word]; ok {
		return kind
	}
	return word
}

// canonicalKindSet builds the lookup a kind filter tests against. A nil result
// means "no filter", which is not the same as an empty one.
func canonicalKindSet(kinds []string) map[string]bool {
	if len(kinds) == 0 {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[CanonicalKind(k)] = true
	}
	return set
}
