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

// nodeKinds is every kind the graph stores, in the order a message lists them
// back to a caller: the shapes first, then the rest.
var nodeKinds = []string{"iface", "class", "func", "method", "type", "const", "var", "error"}

// NodeKinds returns the kinds a filter accepts, spelled the way answers print
// them. This is the list an error offers back when a filter names something
// else.
func NodeKinds() []string {
	out := make([]string, len(nodeKinds))
	for i, k := range nodeKinds {
		out[i] = DisplayKind(k)
	}
	return out
}

// UnknownKinds returns the entries of kinds that name no kind at all, in the
// order given and without duplicates.
//
// Nothing downstream can distinguish "filtered everything out" from "matched
// nothing", so an unrecognised kind reaches the caller as an empty answer —
// which reads as "there is no such code" rather than as a typo. The check is
// here rather than at the call site because the vocabulary is here.
func UnknownKinds(kinds []string) []string {
	known := make(map[string]bool, len(nodeKinds))
	for _, k := range nodeKinds {
		known[k] = true
	}
	return unknownOf(kinds, func(word string) bool { return known[CanonicalKind(word)] })
}

// edgeKinds is every edge kind a traversal may be restricted to.
var edgeKinds = []EdgeKind{EdgeUses, EdgeReturns, EdgeImplements, EdgeCalls, EdgeReferences, EdgeContains}

// EdgeKinds returns the edge kinds expand accepts.
func EdgeKinds() []string {
	out := make([]string, len(edgeKinds))
	for i, k := range edgeKinds {
		out[i] = string(k)
	}
	return out
}

// UnknownEdgeKinds returns the entries of kinds that name no edge kind. An
// unrecognised one restricts a traversal to nothing and hands back the seeds
// alone, which looks like an unconnected symbol.
func UnknownEdgeKinds(kinds []string) []string {
	known := make(map[string]bool, len(edgeKinds))
	for _, k := range edgeKinds {
		known[string(k)] = true
	}
	return unknownOf(kinds, func(word string) bool { return known[word] })
}

// unknownOf collects the entries a recogniser rejects, preserving input order
// and reporting each distinct one once.
func unknownOf(words []string, ok func(string) bool) []string {
	var bad []string
	seen := map[string]bool{}
	for _, w := range words {
		if ok(w) || seen[w] {
			continue
		}
		seen[w] = true
		bad = append(bad, w)
	}
	return bad
}
