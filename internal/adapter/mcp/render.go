package mcp

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kgatilin/wyrd/internal/clustering"
	"github.com/kgatilin/wyrd/internal/retrieval"
)

// Compact text rendering for the LLM-facing tool results.
//
// The graph/search tools return a summary an agent reads, not a document a
// program parses, so the payloads are rendered as compact indented text
// instead of JSON. The wins over the old JSON shape:
//   - the analysed package is named once in a header; same-package symbols are
//     rendered bare (id = header-package + "." + name), so the package path is
//     not repeated on every node, edge endpoint, and signature type;
//   - edges are folded onto whichever endpoint carries the most of them, as
//     an arrow and a fan-out list, instead of one {from,to,kind} object each;
//   - relevance scores are dropped — results are already emitted in rank order
//     and the scores were never a threshold;
//   - snippets that duplicated the signature/doc are dropped.
//
// Cross-package symbols keep enough of their path to round-trip back into
// get_node/expand: node lists show them under their own package header, and
// edge endpoints shorten to <lastsegment>.<Name> which the node list resolves.
//
// The format is documented once, in the tool descriptions the client receives
// with the tool list — not re-explained in the body of every result. A legend
// on each response is a per-call tax on something the reader already knows by
// the second call.

// text wraps a pre-rendered string as a plain-text tool result (no JSON).
func text(s string) (ToolResult, *RPCError) {
	return ToolResult{Content: []ToolResultContent{{Type: "text", Text: s}}}, nil
}

// lastSeg returns the final path segment of a package import path.
func lastSeg(pkgPath string) string {
	if i := strings.LastIndex(pkgPath, "/"); i >= 0 {
		return pkgPath[i+1:]
	}
	return pkgPath
}

// splitNodeID splits a retrieval node id (pkgpath.Symbol, where Symbol may be
// Recv.Method) into its package path and symbol name. The boundary is the
// first '.' following the last '/': package path segments are '/'-separated and
// carry no '.', so the first dot after the final slash starts the symbol.
func splitNodeID(id string) (pkg, name string) {
	slash := strings.LastIndex(id, "/")
	rel := id[slash+1:]
	dot := strings.IndexByte(rel, '.')
	if dot < 0 {
		return id, ""
	}
	cut := slash + 1 + dot
	return id[:cut], id[cut+1:]
}

// importPathRe matches an import-path-qualified type (path contains at least one
// '/') immediately before its identifier, e.g. `encoding/json.RawMessage`.
var importPathRe = regexp.MustCompile(`([\w.]+(?:/[\w.]+)+)\.(\w)`)

// simplifyType strips the home package prefix from a type or signature string
// and shortens any other import-path-qualified type to its last path segment:
//
//	(state *internal/serve.State) (internal/adapter/mcp.ToolResult)
//	-> (state *serve.State) (ToolResult)          [home = internal/adapter/mcp]
func simplifyType(s, home string) string {
	if s == "" {
		return s
	}
	if home != "" {
		s = strings.ReplaceAll(s, home+".", "")
	}
	return importPathRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := importPathRe.FindStringSubmatch(m)
		return lastSeg(sm[1]) + "." + sm[2]
	})
}

// sigTail drops the leading symbol name (and a `type Name ` prefix) from a
// signature so it reads as a tail after the anchor name: `Foo(a int) error` ->
// `(a int) error`, `type Bar struct` -> `struct`.
func sigTail(sig, name string) string {
	sig = strings.TrimSpace(sig)
	if t := strings.TrimPrefix(sig, "type "+name+" "); t != sig {
		return strings.TrimSpace(t)
	}
	if t := strings.TrimPrefix(sig, name); t != sig {
		return strings.TrimSpace(t)
	}
	return sig
}

// shortEndpoint renders an edge endpoint id relative to home: same-package ids
// become bare names; cross-package ids become <lastsegment>.<Name>.
//
// The last segment is not a unique name. `impl` names a dozen packages in a
// layered repository, so two symbols in two of them read identically here —
// which is why edge rendering goes through newEndpointShortener rather than
// calling this directly on each id.
func shortEndpoint(id, home string) string {
	pkg, name := splitNodeID(id)
	if name == "" {
		return id
	}
	if pkg == home {
		return name
	}
	return lastSeg(pkg) + "." + name
}

// endpointForm renders an endpoint id carrying depth package segments: 0 is
// the bare name (home package only), 1 is <lastsegment>.<Name>, and each step
// prepends one more segment until the path is whole.
func endpointForm(id string, depth int, home string) string {
	pkg, name := splitNodeID(id)
	if name == "" {
		return id
	}
	if pkg != home {
		depth++
	}
	if depth <= 0 {
		return name
	}
	return pkgTail(pkg, depth) + "." + name
}

// endpointDepthCap is the depth past which endpointForm stops changing: the
// whole path is already in the name.
func endpointDepthCap(id, home string) int {
	pkg, name := splitNodeID(id)
	if name == "" {
		return 0
	}
	segments := strings.Count(pkg, "/") + 1
	if pkg != home {
		segments--
	}
	return segments
}

// pkgTail returns the last n segments of a package path.
func pkgTail(pkg string, n int) string {
	segs := strings.Split(pkg, "/")
	if n >= len(segs) {
		return pkg
	}
	return strings.Join(segs[len(segs)-n:], "/")
}

// newEndpointShortener renders each of ids as briefly as it can while still
// naming one symbol, and returns the renderer.
//
// Brevity per id is not the goal — distinctness across the set is. A short
// name that two ids share does not shorten the answer, it deletes half of it:
// `impl.reduce` under a node's callers is one line whether one package calls
// it or three, and a reader who folds an edge list into a set of names never
// learns which. Ids that collide grow a package segment at a time until they
// stand apart, so the disambiguation is paid for only where it is needed.
func newEndpointShortener(ids []string, home string) func(string) string {
	depth := make(map[string]int, len(ids))
	for _, id := range ids {
		depth[id] = 0
	}
	for {
		claims := make(map[string][]string, len(depth))
		for id, d := range depth {
			form := endpointForm(id, d, home)
			claims[form] = append(claims[form], id)
		}
		grew := false
		for _, group := range claims {
			if len(group) < 2 {
				continue
			}
			for _, id := range group {
				if depth[id] < endpointDepthCap(id, home) {
					depth[id]++
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}
	return func(id string) string {
		if d, ok := depth[id]; ok {
			return endpointForm(id, d, home)
		}
		return shortEndpoint(id, home)
	}
}

// edgeEndpoints lists every id an edge set touches, which is the set an
// endpoint renderer has to keep apart.
func edgeEndpoints(edges []edgeInfo) []string {
	seen := make(map[string]bool, 2*len(edges))
	out := make([]string, 0, 2*len(edges))
	for _, e := range edges {
		for _, id := range [2]string{e.From, e.To} {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// shortArchmotifID renders a lens member/center id (`fn:path.Name`,
// `type:path.Recv.Method`, ...) relative to home. The kind prefix is dropped;
// same-package symbols become bare names, others become <lastsegment>.<Name>.
func shortArchmotifID(id, home string) string {
	pkg := clustering.PackagePathOf(id)
	body := id
	if i := strings.IndexByte(body, ':'); i >= 0 {
		body = body[i+1:]
	}
	name := body
	if pkg != "" {
		name = strings.TrimPrefix(body, pkg+".")
	}
	if pkg == "" || pkg == home {
		return name
	}
	return lastSeg(pkg) + "." + name
}

// shortArchmotifIDs maps shortArchmotifID over a slice.
func shortArchmotifIDs(ids []string, home string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = shortArchmotifID(id, home)
	}
	return out
}

// foldKey identifies one folded edge group: a hub node, an edge kind, and
// which side of the arrow the hub sits on.
type foldKey struct {
	hub     string
	kind    string
	reverse bool // hub is the target: `hub <--kind-- a, b, c`
}

// lessFoldKey orders fold keys so the greedy choice below is deterministic
// when two groups are the same size. Forward wins ties, since a singleton
// group reads more naturally as src --kind--> dst.
func lessFoldKey(a, b foldKey) bool {
	if a.reverse != b.reverse {
		return !a.reverse
	}
	if a.hub != b.hub {
		return a.hub < b.hub
	}
	return a.kind < b.kind
}

// foldEdges collapses an edge list into hub lines, folding in whichever
// direction pays: `from --kind--> a, b, c` where one source fans out, and
// `to <--kind-- a, b, c` where many sources converge on one target.
//
// Folding by (source, kind) alone — what this replaced — leaves the converging
// case unfolded, and that case is the common one: every implementation of an
// interface emits its own `X --implements--> Iface` line, so a package with
// twenty implementations spent twenty lines saying one thing. Groups are
// chosen greedily, largest first, so each edge is printed exactly once under
// whichever hub carries the most of its siblings.
func foldEdges(edges []edgeInfo, short func(string) string) []string {
	type edge struct{ from, kind, to string }
	remaining := make(map[edge]bool, len(edges))
	for _, e := range edges {
		remaining[edge{short(e.From), e.Kind, short(e.To)}] = true
	}

	var lines []string
	for len(remaining) > 0 {
		counts := map[foldKey]int{}
		for e := range remaining {
			counts[foldKey{e.from, e.kind, false}]++
			counts[foldKey{e.to, e.kind, true}]++
		}
		var best foldKey
		bestN := 0
		for k, n := range counts {
			if n > bestN || (n == bestN && lessFoldKey(k, best)) {
				best, bestN = k, n
			}
		}

		var peers []string
		for e := range remaining {
			if e.kind != best.kind {
				continue
			}
			switch {
			case best.reverse && e.to == best.hub:
				peers = append(peers, e.from)
			case !best.reverse && e.from == best.hub:
				peers = append(peers, e.to)
			default:
				continue
			}
			delete(remaining, e)
		}
		sort.Strings(peers)
		lines = append(lines, foldLine(best, peers))
	}
	sort.Strings(lines)
	return lines
}

// foldLine renders one folded group.
func foldLine(k foldKey, peers []string) string {
	if k.reverse {
		return fmt.Sprintf("  %s <--%s-- %s", k.hub, k.kind, strings.Join(peers, ", "))
	}
	return fmt.Sprintf("  %s --%s--> %s", k.hub, k.kind, strings.Join(peers, ", "))
}

// foldIncident folds a single node's incident edges by kind alone. Every edge
// already shares that node as one endpoint — the section header names it — so
// only the kind and the far side need printing.
func foldIncident(edges []edgeInfo, far func(edgeInfo) string, reverse bool) []string {
	byKind := map[string]map[string]bool{}
	for _, e := range edges {
		if byKind[e.Kind] == nil {
			byKind[e.Kind] = map[string]bool{}
		}
		byKind[e.Kind][far(e)] = true
	}
	lines := make([]string, 0, len(byKind))
	for kind, set := range byKind {
		peers := make([]string, 0, len(set))
		for p := range set {
			peers = append(peers, p)
		}
		sort.Strings(peers)
		if reverse {
			lines = append(lines, fmt.Sprintf("  <--%s-- %s", kind, strings.Join(peers, ", ")))
		} else {
			lines = append(lines, fmt.Sprintf("  --%s--> %s", kind, strings.Join(peers, ", ")))
		}
	}
	sort.Strings(lines)
	return lines
}

// dominantPackage returns the most common package among node ids (the home
// package a subgraph header declares).
func dominantPackage(ids []string) string {
	counts := map[string]int{}
	for _, id := range ids {
		pkg, _ := splitNodeID(id)
		counts[pkg]++
	}
	best, bestN := "", 0
	for pkg, n := range counts {
		if n > bestN || (n == bestN && pkg < best) {
			best, bestN = pkg, n
		}
	}
	return best
}

// --- search ---

func renderSearch(query string, r searchResult) string {
	var b strings.Builder
	var seeds, region []searchHit
	for _, h := range r.Hits {
		if h.Seed {
			seeds = append(seeds, h)
		} else {
			region = append(region, h)
		}
	}

	// Home is the top hit's package — the answer's anchor. It is read off the
	// hits and not the whole answer, since the region can be larger than what
	// the query matched and letting it decide which names print bare would
	// rename the answer around its own periphery. Nor is it the *commonest*
	// package among the hits: groups print home first, so a query with one
	// decisive hit and a tail of weak ones elsewhere would lead with the tail —
	// which reads as a wrong answer even when the ranking underneath is right.
	home := ""
	if len(seeds) > 0 {
		home = seeds[0].Package
	}

	tag := ""
	if r.Dense {
		tag = "  ·  dense"
	}
	fmt.Fprintf(&b, "search %q  ·  %d hits  ·  region %d nodes / %d edges%s\n",
		query, len(seeds), len(region), len(r.Edges), tag)
	if len(r.Hits) == 0 {
		b.WriteString("(no matches — broaden the query or check filters)\n")
		return b.String()
	}
	if r.SeedCount > 0 {
		// Conductance is the headline quality number: it says whether the
		// region is a community or an arbitrary slice of a hairball, so it is
		// reported next to the size the reader would otherwise judge by. What
		// the number means is in the tool description, not on every line.
		fmt.Fprintf(&b, "cut  conductance %.3f  ·  %d seeds\n", r.Conductance, r.SeedCount)
	}
	if r.Truncated {
		b.WriteString("(region truncated to the node cap — narrow the query or lower k/hops)\n")
	}
	if home != "" {
		fmt.Fprintf(&b, "home %s\n", home)
	}

	b.WriteString("\nhits\n")
	writeHitGroups(&b, seeds, home, true)
	if len(region) > 0 {
		// The region is context, not the answer: a reader scanning it wants to
		// know what is nearby and where to look next, and follows up on the
		// one or two entries that matter with get_node. Printing every
		// neighbour's full signature here spends the response's bulk on
		// parameter lists nobody reads.
		b.WriteString("\nregion\n")
		writeHitGroups(&b, region, home, false)
	}

	if len(r.Edges) > 0 {
		// Which edges these are, said once: the ones running between nodes
		// that came back. A reader who takes them for a symbol's full wiring
		// concludes a caller does not exist because the ranking placed it
		// outside the region.
		b.WriteString("\nedges  (between the nodes above; get_node <id> for all of one symbol's)\n")
		for _, line := range foldEdges(r.Edges, newEndpointShortener(edgeEndpoints(r.Edges), home)) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// writeHitGroups prints hits grouped by package, the home package first and
// unlabelled, each group keeping the order it arrived in — which is the order
// the search ranked them. The groups keep it too: a package appears where its
// best hit did, so reading top to bottom is reading the ranking.
//
// withSig prints each entry's signature; without it an entry is name, kind and
// location — enough to decide whether to open it.
func writeHitGroups(b *strings.Builder, hits []searchHit, home string, withSig bool) {
	var order []string
	byPkg := map[string][]searchHit{}
	for _, h := range hits {
		if _, seen := byPkg[h.Package]; !seen {
			order = append(order, h.Package)
		}
		byPkg[h.Package] = append(byPkg[h.Package], h)
	}
	sort.SliceStable(order, func(i, j int) bool {
		return order[i] == home && order[j] != home
	})
	for _, pkg := range order {
		if pkg != home {
			fmt.Fprintf(b, " %s\n", pkg)
		}
		for _, h := range byPkg[pkg] {
			detail := retrieval.DisplayKind(h.Kind)
			if withSig {
				if sig := sigTail(simplifyType(h.Signature, home), h.Name); sig != "" {
					detail = sig
				}
			}
			b.WriteString(symbolLine(h.Name, detail, baseLoc(h.File, h.Line)))
			if d := synopsis(h.Doc, maxSubgraphDoc); d != "" {
				fmt.Fprintf(b, "    · %s\n", d)
			}
		}
	}
}

// --- expand ---

func renderSubgraph(seed string, r subgraphResult) string {
	var b strings.Builder
	ids := make([]string, len(r.Nodes))
	for i, n := range r.Nodes {
		ids[i] = n.ID
	}
	home := dominantPackage(ids)

	head := "subgraph"
	if seed != "" {
		head += fmt.Sprintf(" %q", seed)
	}
	tag := ""
	if r.Dense {
		tag = "  ·  dense"
	}
	nc, ec := r.NodeCount, r.EdgeCount
	if nc == 0 {
		nc = len(r.Nodes)
	}
	if ec == 0 {
		ec = len(r.Edges)
	}
	fmt.Fprintf(&b, "%s  ·  %d nodes / %d edges%s\n", head, nc, ec, tag)
	if r.Truncated {
		b.WriteString("(truncated to the node cap — narrow the query or lower k/hops)\n")
	}
	if home != "" {
		fmt.Fprintf(&b, "home %s\n", home)
	}

	// Nodes grouped by package, home package first.
	var order []string
	byPkg := map[string][]nodeInfo{}
	for _, n := range r.Nodes {
		if _, seen := byPkg[n.Package]; !seen {
			order = append(order, n.Package)
		}
		byPkg[n.Package] = append(byPkg[n.Package], n)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i] == home {
			return true
		}
		if order[j] == home {
			return false
		}
		return order[i] < order[j]
	})
	b.WriteString("\nnodes\n")
	for _, pkg := range order {
		if pkg != home {
			fmt.Fprintf(&b, " %s\n", pkg)
		}
		for _, n := range byPkg[pkg] {
			detail := sigTail(simplifyType(n.Signature, home), n.Name)
			if detail == "" {
				detail = retrieval.DisplayKind(n.Kind)
			}
			b.WriteString(symbolLine(n.Name, detail, baseLoc(n.File, n.Line)))
			if d := synopsis(n.Doc, maxSubgraphDoc); d != "" {
				fmt.Fprintf(&b, "    · %s\n", d)
			}
		}
	}

	if len(r.Edges) > 0 {
		// Which edges these are, said once: the ones running between nodes
		// that came back. A reader who takes them for a symbol's full wiring
		// concludes a caller does not exist because the ranking placed it
		// outside the region.
		b.WriteString("\nedges  (between the nodes above; get_node <id> for all of one symbol's)\n")
		for _, line := range foldEdges(r.Edges, newEndpointShortener(edgeEndpoints(r.Edges), home)) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// --- get_node ---

func renderNodeDetail(d nodeDetailResult) string {
	var b strings.Builder
	home := d.Package
	fmt.Fprintf(&b, "%s  ·  %s  ·  %s  ·  %s\n", d.Name, retrieval.DisplayKind(d.Kind), d.Package, locOf(d.File, d.Line))
	if sig := simplifyType(d.Signature, home); sig != "" {
		fmt.Fprintf(&b, "signature  %s\n", sig)
	}
	if doc := strings.TrimSpace(d.Doc); doc != "" {
		fmt.Fprintf(&b, "doc  %s\n", doc)
	}

	var out, in []edgeInfo
	for _, e := range d.Edges {
		if e.To == d.NodeID && e.From != d.NodeID {
			in = append(in, e)
		} else {
			out = append(out, e)
		}
	}
	short := newEndpointShortener(edgeEndpoints(d.Edges), home)
	if len(out) > 0 {
		b.WriteString("\nout\n")
		for _, line := range foldIncident(out, func(e edgeInfo) string { return short(e.To) }, false) {
			b.WriteString(line + "\n")
		}
	}
	if len(in) > 0 {
		// One line per kind, not per caller: a hub symbol has hundreds of
		// callers and one `<--calls-- a, b, c` line says the same thing the
		// old per-source folding said in hundreds.
		b.WriteString("\nin\n")
		for _, line := range foldIncident(in, func(e edgeInfo) string { return short(e.From) }, true) {
			b.WriteString(line + "\n")
		}
	}
	if d.EdgesTruncated {
		fmt.Fprintf(&b, "(edges truncated to %d of %d)\n", maxNodeEdges, d.EdgeCount)
	}

	if body := strings.TrimRight(d.Body, "\n"); body != "" {
		b.WriteString("\nbody\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

// --- get_package ---

func renderPackageDigest(d packageDigest) string {
	var b strings.Builder
	name := d.Name
	if name != "" && name != lastSeg(d.Path) {
		name = " (" + name + ")"
	} else {
		name = ""
	}
	fmt.Fprintf(&b, "package %s%s\n", d.Path, name)
	line := fmt.Sprintf("counts  = %d", d.Counts.Total)
	if cols := countCols(censusOf(d.Counts), digestKinds); cols != "" {
		line += "   " + cols
	}
	b.WriteString(line + "\n")
	meta := []string{}
	if d.Layer != "" {
		meta = append(meta, "layer="+d.Layer)
	}
	if d.Aggregate != "" {
		meta = append(meta, "aggregate="+d.Aggregate)
	}
	if len(meta) > 0 {
		fmt.Fprintf(&b, "%s\n", strings.Join(meta, "  "))
	}
	if len(d.SourceFiles) > 0 {
		fmt.Fprintf(&b, "files  %s\n", strings.Join(d.SourceFiles, ", "))
	}

	// Symbols grouped by kind, in a stable kind order.
	kindOrder := []struct{ key, head string }{
		{"interface", "interfaces"},
		{"struct", "structs"},
		{"func", "funcs"},
		{"type", "types"},
		{"const", "consts"},
		{"var", "vars"},
		{"error", "errors"},
	}
	byKind := map[string][]symbolDigest{}
	for _, s := range d.Symbols {
		byKind[s.Kind] = append(byKind[s.Kind], s)
	}
	for _, ko := range kindOrder {
		syms := byKind[ko.key]
		if len(syms) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", ko.head)
		for _, s := range syms {
			b.WriteString(renderSymbolDigest(s, d.Path))
		}
	}

	if len(d.Implementations) > 0 {
		b.WriteString("\nimplements\n")
		for _, im := range d.Implementations {
			ptr := ""
			if im.Pointer {
				ptr = "*"
			}
			fmt.Fprintf(&b, "  %s%s -> %s\n", ptr, im.Concrete, im.Interface)
		}
	}

	if d.Pagination != nil {
		p := d.Pagination
		if p.Truncated {
			fmt.Fprintf(&b, "\npage  %d–%d of %d  ·  next_offset=%d\n",
				p.Offset, p.Offset+p.Returned, p.Total, p.NextOffset)
		} else if p.Total > p.Returned {
			fmt.Fprintf(&b, "\npage  %d–%d of %d\n", p.Offset, p.Offset+p.Returned, p.Total)
		}
	}
	if d.Hint != "" {
		fmt.Fprintf(&b, "hint  %s\n", d.Hint)
	}
	return b.String()
}

// renderSymbolDigest renders one package-surface symbol line (+ members/doc).
func renderSymbolDigest(s symbolDigest, home string) string {
	var b strings.Builder
	loc := locOf(s.File, s.Line)
	head := s.Name
	switch s.Kind {
	case "func":
		head = s.Name + sigTailSpace(sigTail(simplifyType(s.Signature, home), s.Name))
	case "const":
		v := s.Value
		if t := simplifyType(s.Signature, home); t != "" {
			v = t + " = " + v
		}
		head = s.Name + "  " + strings.TrimSpace(v)
	case "var":
		if t := simplifyType(s.Signature, home); t != "" {
			head = s.Name + "  " + t
		}
	case "type":
		if s.Underlying != "" {
			head = s.Name + "  " + simplifyType(s.Underlying, home)
		}
	case "error":
		if s.Message != "" {
			head = s.Name + "  " + fmt.Sprintf("%q", s.Message)
		}
	}
	star := ""
	if s.Stereotype != "" && s.Stereotype != "none" {
		star = "  «" + s.Stereotype + "»"
	}
	fmt.Fprintf(&b, "  %s%s   %s\n", head, star, loc)
	if d := synopsis(s.Doc, digestDocBytes); d != "" {
		fmt.Fprintf(&b, "    · %s\n", d)
	}
	for _, m := range s.Methods {
		fmt.Fprintf(&b, "      %s\n", simplifyType(m, home))
	}
	for _, f := range s.Fields {
		fmt.Fprintf(&b, "      %s\n", simplifyType(f, home))
	}
	for _, ev := range s.EnumValues {
		fmt.Fprintf(&b, "      = %s\n", ev)
	}
	return b.String()
}

// sigTailSpace prefixes a non-empty signature tail with a space so it abuts the
// symbol name (`Foo` + `(a int)` -> `Foo(a int)`).
func sigTailSpace(tail string) string {
	if tail == "" {
		return ""
	}
	if strings.HasPrefix(tail, "(") {
		return tail
	}
	return " " + tail
}

// --- list_packages ---

// renderPackageList prints the repo's package census, sorted by path. The
// order is the point: the daemon holds its packages in a map, and a few
// hundred lines in hash order are unreadable as a map of the repository —
// sorted, sibling packages sit together and the tree shows through.
func renderPackageList(sums []PackageSummary) string {
	ordered := make([]PackageSummary, len(sums))
	copy(ordered, sums)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	var b strings.Builder
	fmt.Fprintf(&b, "packages  ·  %d\n", len(ordered))
	for _, s := range ordered {
		line := "  " + s.Path
		if cols := countCols(map[string]int{
			"interface": s.InterfaceCount, "struct": s.StructCount, "func": s.FunctionCount,
		}, digestKinds); cols != "" {
			line += "   " + cols
		}
		if s.Layer != "" {
			line += "   layer=" + s.Layer
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// censusOf projects a digest's counts onto that vocabulary.
func censusOf(c symbolCounts) map[string]int {
	return map[string]int{
		"interface": c.Interfaces, "struct": c.Structs, "func": c.Functions,
		"type": c.TypeDefs, "const": c.Constants, "var": c.Variables, "error": c.Errors,
	}
}

// countCols renders the non-zero entries of a symbol census in the given key
// order, as `label n` pairs. Zero counts say nothing a missing column doesn't.
func countCols(counts map[string]int, order []string) string {
	parts := make([]string, 0, len(order))
	for _, k := range order {
		if n := counts[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", k, n))
		}
	}
	return strings.Join(parts, "  ")
}

// symbolLine renders one `name  detail  location` entry. Absent columns are
// dropped rather than padded: a node without a location used to leave three
// trailing spaces on every line of the answer.
func symbolLine(name, detail, loc string) string {
	line := "  " + name
	if detail != "" {
		line += "  " + detail
	}
	if loc != "" {
		line += "   " + loc
	}
	return line + "\n"
}

// locOf renders a file:line locator, omitting an absent line.
func locOf(file string, line int) string {
	if file == "" {
		return ""
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

// baseLoc renders a locator with only the file basename — used where the node
// is already grouped under its package header, so the directory is redundant.
func baseLoc(file string, line int) string {
	if file == "" {
		return ""
	}
	return locOf(path.Base(file), line)
}

// num formats a float to 3 significant figures with trailing zeros dropped
// (0.07599… -> "0.076", 0.42 -> "0.42", 0 -> "0"): enough precision for a
// verdict/metric, none of the 17-digit noise.
func num(x float64) string {
	return strconv.FormatFloat(x, 'g', 3, 64)
}

// --- analysis-lens rendering ---

// dominantArchmotifPackage returns the most common package among archmotif node
// ids (`fn:path.Name`, `type:path.Recv.Method`, ...) — the home a lens header
// declares and shortens member names against.
func dominantArchmotifPackage(idSets ...[]string) string {
	counts := map[string]int{}
	for _, set := range idSets {
		for _, id := range set {
			if p := clustering.PackagePathOf(id); p != "" {
				counts[p]++
			}
		}
	}
	best, bestN := "", 0
	for p, n := range counts {
		if n > bestN || (n == bestN && p < best) {
			best, bestN = p, n
		}
	}
	return best
}

// clusterMemberIDs flattens the member/sample ids of a set of clusters.
func clusterMemberIDs(clusters []spectralClusterInfo) []string {
	var ids []string
	for _, c := range clusters {
		ids = append(ids, c.Members...)
		ids = append(ids, c.MembersSample...)
	}
	return ids
}

// membersLine renders a capped member list with a "(+N more)" remainder note.
func membersLine(shown []string, total int, home string) string {
	names := shortArchmotifIDs(shown, home)
	s := strings.Join(names, ", ")
	if total > len(shown) {
		s += fmt.Sprintf(" … (+%d more)", total-len(shown))
	}
	return s
}

func renderComponents(pkg string, r componentsResponse) string {
	var b strings.Builder
	home := pkg
	fmt.Fprintf(&b, "components  %s  ·  %d nodes / %d edges  ·  %d component(s)\n",
		pkg, r.NodeCount, r.EdgeCount, r.ComponentCount)

	// Size histogram, largest size first.
	sizes := make([]int, 0, len(r.SizeHistogram))
	for sz := range r.SizeHistogram {
		sizes = append(sizes, sz)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
	parts := make([]string, 0, len(sizes))
	for _, sz := range sizes {
		parts = append(parts, fmt.Sprintf("%d×%d", sz, r.SizeHistogram[sz]))
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, "sizes (size×count)  %s\n", strings.Join(parts, "  "))
	}

	// Non-singleton components in detail; singletons (missing-edge orphans)
	// collapsed to one line.
	var singletons []string
	idx := 0
	for _, c := range r.Components {
		if c.Size == 1 {
			singletons = append(singletons, shortArchmotifID(c.Center, home))
			continue
		}
		idx++
		members := c.Members
		total := c.Size
		if len(members) == 0 {
			members = c.MembersSample
		}
		line := fmt.Sprintf("  #%d  ×%d   center %s", idx, c.Size, shortArchmotifID(c.Center, home))
		if len(members) > 0 {
			line += "   " + membersLine(members, total, home)
		}
		b.WriteString(line + "\n")
	}
	if len(singletons) > 0 {
		sort.Strings(singletons)
		fmt.Fprintf(&b, "  singletons (size-1, likely missing edges)  %s\n", strings.Join(singletons, ", "))
	}
	if r.ComponentsTrunc {
		b.WriteString("(component list truncated to the largest; histogram covers all)\n")
	}
	return b.String()
}

func renderTrophic(pkg string, r trophicLayersResponse) string {
	var b strings.Builder
	home := pkg
	fmt.Fprintf(&b, "trophic_layers  %s  ·  %d nodes / %d edges  ·  edges %s\n",
		pkg, r.NodeCount, r.EdgeCount, strings.Join(r.EdgeKindsUsed, ","))
	fmt.Fprintf(&b, "coherence  F0=%s  %s  ·  backward_edges %d\n",
		num(r.Coherence.F0), r.Coherence.Verdict, r.BackwardEdgeCount)

	if len(r.Layers) > 0 {
		top := r.Layers[len(r.Layers)-1].Level
		fmt.Fprintf(&b, "layers  (L0=foundation … L%d=entry)\n", top)
		for _, l := range r.Layers {
			line := fmt.Sprintf("  L%d  ×%d   center %s", l.Level, l.Size, shortArchmotifID(l.Center, home))
			if len(l.Sample) > 0 {
				line += "   " + membersLine(l.Sample, l.Size, home)
			}
			b.WriteString(line + "\n")
		}
	}

	if len(r.BackwardEdges) > 0 {
		b.WriteString("inversions  (backward edges up the hierarchy, worst first)\n")
		for _, e := range r.BackwardEdges {
			fmt.Fprintf(&b, "  %s --> %s   span %s\n",
				shortArchmotifID(e.From, home), shortArchmotifID(e.To, home), num(e.Span))
		}
	}
	if len(r.Cycles) > 0 {
		b.WriteString("cycles\n")
		for _, c := range r.Cycles {
			line := fmt.Sprintf("  ×%d   center %s", c.Size, shortArchmotifID(c.Center, home))
			if len(c.MembersSample) > 0 {
				line += "   " + membersLine(c.MembersSample, c.Size, home)
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// renderClusters renders spectral/semantic cluster membership under a `label`
// prefix (C for structural, M for semantic), each `Cx  ×N  members…`.
func renderClusters(clusters []spectralClusterInfo, label, home string) string {
	var b strings.Builder
	for _, c := range clusters {
		members := c.Members
		if len(members) == 0 {
			members = c.MembersSample
		}
		line := fmt.Sprintf("  %s%d  ×%d", label, c.ID, c.Size)
		if len(members) > 0 {
			line += "   " + membersLine(members, c.Size, home)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderSpectralCore(tool string, r spectralClusterResponse) string {
	var b strings.Builder
	home := dominantArchmotifPackage(clusterMemberIDs(r.Clusters), r.BoundarySymbols)
	fmt.Fprintf(&b, "%s  %s  ·  %d nodes / %d edges  ·  K=%d (%s)  ·  Q=%s\n",
		tool, home, r.NodeCount, r.EdgeCount, r.CutAnalysis.ChosenK, r.CutAnalysis.KSource, num(r.Modularity))

	if len(r.Eigenvalues) > 0 {
		const maxEig = 8
		ev := r.Eigenvalues
		suffix := ""
		if len(ev) > maxEig {
			ev = ev[:maxEig]
			suffix = " …"
		}
		nums := make([]string, len(ev))
		for i, v := range ev {
			nums[i] = num(v)
		}
		fmt.Fprintf(&b, "eigenvalues  %s%s\n", strings.Join(nums, ", "), suffix)
	}
	if len(r.CutAnalysis.Candidates) > 0 {
		cands := make([]string, 0, len(r.CutAnalysis.Candidates))
		for _, c := range r.CutAnalysis.Candidates {
			cands = append(cands, fmt.Sprintf("K%d gap%s Q%s", c.K, num(c.Gap), num(c.Modularity)))
		}
		fmt.Fprintf(&b, "candidates  %s\n", strings.Join(cands, "  ·  "))
	}

	b.WriteString("clusters\n")
	b.WriteString(renderClusters(r.Clusters, "C", home))

	if r.BoundarySymbolCount > 0 {
		shown, _ := r.BoundarySymbols, r.BoundarySymbolCount
		fmt.Fprintf(&b, "boundary  ×%d   %s\n", r.BoundarySymbolCount, membersLine(shown, r.BoundarySymbolCount, home))
	}
	return b.String()
}

func renderSemanticCluster(r semanticClusterResponse) string {
	out := renderSpectralCore("semantic_cluster", r.spectralClusterResponse)
	if r.DroppedNodes > 0 {
		out += fmt.Sprintf("dropped  %d (no embedding)\n", r.DroppedNodes)
	}
	return out
}

func renderFileHotspots(pkg string, r fileHotspotsResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "file_hotspots  %s  ·  %d files / %d symbols  ·  median %s  max %d  outliers %d\n",
		pkg, r.FileCount, r.TotalSymbols, num(r.MedianSymbols), r.MaxSymbols, r.OutlierCount)
	for _, f := range r.Files {
		flag := ""
		if f.Outlier {
			flag = "   OUTLIER"
		}
		fmt.Fprintf(&b, "  %-32s ×%d%s\n", path.Base(f.File), f.SymbolCount, flag)
	}
	return b.String()
}

func renderLatentDomains(r latentDomainsResponse) string {
	var b strings.Builder
	home := dominantArchmotifPackage(
		clusterMemberIDs(r.Structural.Clusters),
		clusterMemberIDs(r.Semantic.Clusters),
	)
	fmt.Fprintf(&b, "latent_domains  %s  ·  %d nodes  ·  verdict %s\n", home, r.NodeCount, r.Agreement.Verdict)
	fmt.Fprintf(&b, "agreement  AMI=%s  NMI=%s   (AMI drives the verdict)\n",
		num(r.Agreement.AMI), num(r.Agreement.NMI))
	fmt.Fprintf(&b, "structural  K=%d  Q=%s  dominant %s%%\n",
		r.Structural.K, num(r.Structural.Modularity), num(r.Structural.DominantShare*100))
	fmt.Fprintf(&b, "semantic    K=%d  Q=%s  dominant %s%%\n",
		r.Semantic.K, num(r.Semantic.Modularity), num(r.Semantic.DominantShare*100))

	if len(r.Glue.TopFanIn) > 0 {
		fmt.Fprintf(&b, "glue  (top structural fan-in — pull to a thin boundary; semantic cluster %d)\n", r.Glue.GlueCluster)
		parts := make([]string, 0, len(r.Glue.TopFanIn))
		for _, g := range r.Glue.TopFanIn {
			parts = append(parts, fmt.Sprintf("%s ×%d", shortArchmotifID(g.Node, home), g.FanIn))
		}
		fmt.Fprintf(&b, "  %s\n", strings.Join(parts, "  ·  "))
	}
	if r.Glue.Note != "" {
		fmt.Fprintf(&b, "note  %s\n", r.Glue.Note)
	}
	b.WriteString("structural clusters (sample)\n")
	b.WriteString(renderClusters(r.Structural.Clusters, "S", home))
	b.WriteString("semantic clusters (sample)\n")
	b.WriteString(renderClusters(r.Semantic.Clusters, "M", home))
	if r.DroppedNodes > 0 {
		fmt.Fprintf(&b, "dropped  %d (no embedding)\n", r.DroppedNodes)
	}
	return b.String()
}

// --- extract ---

// renderExtractIndex prints the no-paths survey: one line per package with the
// non-zero parts of its symbol census. Zero-valued kinds are dropped — an
// index of a few hundred packages otherwise spends most of its width printing
// `const 0  var 0  err 0`.
func renderExtractIndex(pkgs []packageDigest, hint string) string {
	ordered := make([]packageDigest, len(pkgs))
	copy(ordered, pkgs)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })

	var b strings.Builder
	fmt.Fprintf(&b, "extract index  ·  %d packages  ·  no symbols\n", len(ordered))
	for _, d := range ordered {
		line := fmt.Sprintf("  %s   = %d", d.Path, d.Counts.Total)
		if cols := countCols(censusOf(d.Counts), digestKinds); cols != "" {
			line += "   " + cols
		}
		if d.Layer != "" {
			line += "   layer=" + d.Layer
		}
		b.WriteString(line + "\n")
	}
	if hint != "" {
		fmt.Fprintf(&b, "hint  %s\n", hint)
	}
	return b.String()
}

// renderExtractDigests prints the with-paths form: the same per-package digest
// get_package renders, one after another. Sharing the renderer is the point —
// extract's own JSON shape repeated every package path, every field name, and
// every kind key on the wire, for text a reader already knows how to read.
func renderExtractDigests(pkgs []packageDigest) string {
	if len(pkgs) == 0 {
		return "extract  ·  0 packages matched the requested paths\n"
	}
	parts := make([]string, 0, len(pkgs))
	for _, d := range pkgs {
		parts = append(parts, renderPackageDigest(d))
	}
	return strings.Join(parts, "\n")
}

// --- daemon state ---

// renderStatus prints the daemon's readiness in the same text register as
// every other tool, rather than as a JSON object the reader has to parse by
// eye. The machine-readable form lives at GET /api/mcp/status, which is what
// the review UI polls.
func renderStatus(r statusResult) string {
	var b strings.Builder
	state := "lexical-only"
	switch {
	case r.Indexing:
		state = "indexing"
	case r.Ready:
		state = "ready"
	}
	fmt.Fprintf(&b, "status  %s\n", state)

	counts := []string{fmt.Sprintf("packages %d", r.Packages), fmt.Sprintf("nodes %d", r.Nodes)}
	if r.Embeddable > 0 {
		counts = append(counts, fmt.Sprintf("embedded %d/%d", r.Embedded, r.Embeddable))
	}
	if r.Pending > 0 {
		counts = append(counts, fmt.Sprintf("pending %d", r.Pending))
	}
	fmt.Fprintf(&b, "counts  %s\n", strings.Join(counts, "  "))
	if r.Embedder != "" {
		fmt.Fprintf(&b, "embedder  %s\n", r.Embedder)
	}
	if r.Message != "" {
		fmt.Fprintf(&b, "%s\n", r.Message)
	}
	return b.String()
}

// renderRefresh prints what a reindex changed.
func renderRefresh(r refreshResult) string {
	dense := "lexical-only"
	if r.Dense {
		dense = "dense"
	}
	return fmt.Sprintf("refresh  ·  reindexed %d  removed %d  ·  %s\n", r.Reindexed, r.Removed, dense)
}

// renderEmbeddingCoverage prints dense-embedding coverage as a census plus a
// per-kind breakdown, one line each.
func renderEmbeddingCoverage(r embeddingCoverageResult) string {
	var b strings.Builder
	dense := "lexical-only"
	if r.DenseAvailable {
		dense = "dense"
	}
	fmt.Fprintf(&b, "embedding_coverage  ·  %d nodes  ·  embeddable %d  embedded %d  missing %d  ·  %s\n",
		r.TotalNodes, r.EmbeddableNodes, r.EmbeddedNodes, r.MissingNodes, dense)

	kinds := make([]string, 0, len(r.ByKind))
	for k := range r.ByKind {
		kinds = append(kinds, k)
	}
	// Ordered by the word the line prints, so the list reads alphabetically.
	sort.Slice(kinds, func(i, j int) bool {
		return retrieval.DisplayKind(kinds[i]) < retrieval.DisplayKind(kinds[j])
	})
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		c, word := r.ByKind[k], retrieval.DisplayKind(k)
		if c.Embeddable == 0 {
			// Non-embeddable kinds are excluded from dense search by design;
			// their totals are noise next to the ones that can be missing.
			parts = append(parts, fmt.Sprintf("%s %d (n/a)", word, c.Total))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", word, c.Embedded, c.Embeddable))
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, "by kind  %s\n", strings.Join(parts, "  "))
	}
	if len(r.MissingSample) > 0 {
		fmt.Fprintf(&b, "missing  %s\n", membersLine(r.MissingSample, r.MissingNodes, ""))
	}
	return b.String()
}

// --- bounded contexts ---

// renderBCList prints one line per bounded context plus its description, in
// place of the JSON array that repeated six field names per context.
func renderBCList(bcs []BCSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "bounded_contexts  ·  %d\n", len(bcs))
	for _, bc := range bcs {
		line := "  " + bc.Name
		if bc.Relationship != "" {
			line += "  «" + bc.Relationship + "»"
		}
		line += relatedCols(bc)
		b.WriteString(line + "\n")
		if d := synopsis(bc.Description, digestDocBytes); d != "" {
			fmt.Fprintf(&b, "    · %s\n", d)
		}
	}
	return b.String()
}

// relatedCols renders a bounded context's aggregate and context-map columns,
// omitting the empty ones.
func relatedCols(bc BCSummary) string {
	out := ""
	for _, col := range []struct {
		label string
		items []string
	}{
		{"aggregates", bc.Aggregates},
		{"up", bc.Upstream},
		{"down", bc.Downstream},
	} {
		if len(col.items) > 0 {
			out += fmt.Sprintf("   %s %s", col.label, strings.Join(col.items, ", "))
		}
	}
	return out
}

// renderBCDetail prints one bounded context with its member packages.
func renderBCDetail(d bcDetail) string {
	var b strings.Builder
	head := "bounded_context  " + d.Name
	if d.Relationship != "" {
		head += "  «" + d.Relationship + "»"
	}
	b.WriteString(head + "\n")
	if desc := strings.TrimSpace(d.Description); desc != "" {
		fmt.Fprintf(&b, "  · %s\n", desc)
	}
	for _, row := range []struct {
		label string
		items []string
	}{
		{"aggregates", d.Aggregates},
		{"upstream", d.Upstream},
		{"downstream", d.Downstream},
		{"packages", d.Packages},
	} {
		if len(row.items) > 0 {
			fmt.Fprintf(&b, "%-11s %s\n", row.label, strings.Join(row.items, ", "))
		}
	}
	return b.String()
}
