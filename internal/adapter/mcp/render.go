package mcp

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kgatilin/archai/internal/clustering"
)

// Compact text rendering for the LLM-facing tool results.
//
// The graph/search tools return a summary an agent reads, not a document a
// program parses, so the payloads are rendered as compact indented text
// instead of JSON. The wins over the old JSON shape:
//   - the analysed package is named once in a header; same-package symbols are
//     rendered bare (id = header-package + "." + name), so the package path is
//     not repeated on every node, edge endpoint, and signature type;
//   - edges are folded by (source, kind) with an arrow and a fan-out list
//     instead of one {from,to,kind} object each;
//   - relevance scores are dropped — results are already emitted in rank order
//     and the scores were never a threshold;
//   - snippets that duplicated the signature/doc are dropped.
//
// Cross-package symbols keep enough of their path to round-trip back into
// get_node/expand: node lists show them under their own package header, and
// edge endpoints shorten to <lastsegment>.<Name> which the node list resolves.

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

// foldEdges groups edges by (from, kind) into `from --kind--> a, b, c` lines,
// endpoints rendered via short. Lines and fan-out lists are sorted, duplicate
// targets removed, for deterministic output.
func foldEdges(edges []edgeInfo, short func(string) string) []string {
	type key struct{ from, kind string }
	groups := map[key]map[string]bool{}
	for _, e := range edges {
		k := key{short(e.From), e.Kind}
		if groups[k] == nil {
			groups[k] = map[string]bool{}
		}
		groups[k][short(e.To)] = true
	}
	lines := make([]string, 0, len(groups))
	for k, tos := range groups {
		targets := make([]string, 0, len(tos))
		for t := range tos {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		lines = append(lines, fmt.Sprintf("  %s --%s--> %s", k.from, k.kind, strings.Join(targets, ", ")))
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
		// reported next to the size the reader would otherwise judge by.
		fmt.Fprintf(&b, "cut  conductance %.3f (0 = isolated community, 1 = no boundary)  ·  %d seeds\n",
			r.Conductance, r.SeedCount)
	}
	if r.Truncated {
		b.WriteString("(region truncated to the node cap — narrow the query or lower k/hops)\n")
	}
	if home != "" {
		fmt.Fprintf(&b, "home %s   · bare names are in this package\n", home)
	}

	b.WriteString("\nhits  (matched the query text)\n")
	writeHitGroups(&b, seeds, home)
	if len(region) > 0 {
		b.WriteString("\nregion  (what the hits diffuse into)\n")
		writeHitGroups(&b, region, home)
	}

	if len(r.Edges) > 0 {
		b.WriteString("\nedges  (src --kind--> dst)\n")
		for _, line := range foldEdges(r.Edges, func(id string) string { return shortEndpoint(id, home) }) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// writeHitGroups prints hits grouped by package, the home package first and
// unlabelled, each group keeping the order it arrived in — which is the order
// the search ranked them. The groups keep it too: a package appears where its
// best hit did, so reading top to bottom is reading the ranking.
func writeHitGroups(b *strings.Builder, hits []searchHit, home string) {
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
			sig := sigTail(simplifyType(h.Signature, home), h.Name)
			if sig == "" {
				sig = h.Kind
			}
			fmt.Fprintf(b, "  %s  %s   %s\n", h.Name, sig, baseLoc(h.File, h.Line))
			if d := firstLine(h.Doc, maxSubgraphDoc); d != "" {
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
		fmt.Fprintf(&b, "home %s   · bare names are in this package\n", home)
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
			sig := sigTail(simplifyType(n.Signature, home), n.Name)
			loc := baseLoc(n.File, n.Line)
			if sig == "" {
				sig = n.Kind
			}
			fmt.Fprintf(&b, "  %s  %s   %s\n", n.Name, sig, loc)
			if d := firstLine(n.Doc, maxSubgraphDoc); d != "" {
				fmt.Fprintf(&b, "    · %s\n", d)
			}
		}
	}

	if len(r.Edges) > 0 {
		b.WriteString("\nedges  (src --kind--> dst)\n")
		for _, line := range foldEdges(r.Edges, func(id string) string { return shortEndpoint(id, home) }) {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// --- get_node ---

func renderNodeDetail(d nodeDetailResult) string {
	var b strings.Builder
	home := d.Package
	fmt.Fprintf(&b, "%s  ·  %s  ·  %s  ·  %s\n", d.Name, d.Kind, d.Package, locOf(d.File, d.Line))
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
	short := func(id string) string { return shortEndpoint(id, home) }
	if len(out) > 0 {
		b.WriteString("\nout  (this --kind--> X)\n")
		for _, line := range foldEdges(out, short) {
			b.WriteString(strings.Replace(line, "  "+d.Name+" --", "  --", 1) + "\n")
		}
	}
	if len(in) > 0 {
		b.WriteString("\nin  (X --kind--> this)\n")
		// Fold by (from,kind) but render as reverse arrows grouped per source.
		for _, line := range foldEdges(in, short) {
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
	c := d.Counts
	fmt.Fprintf(&b, "counts  iface %d  struct %d  func %d  type %d  const %d  var %d  err %d  = %d\n",
		c.Interfaces, c.Structs, c.Functions, c.TypeDefs, c.Constants, c.Variables, c.Errors, c.Total)
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
	if d := firstLine(s.Doc, digestDocBytes); d != "" {
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

func renderPackageList(sums []PackageSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "packages  ·  %d\n", len(sums))
	for _, s := range sums {
		layer := ""
		if s.Layer != "" {
			layer = "  layer=" + s.Layer
		}
		fmt.Fprintf(&b, "  %s   iface %d  struct %d  func %d%s\n",
			s.Path, s.InterfaceCount, s.StructCount, s.FunctionCount, layer)
	}
	return b.String()
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
