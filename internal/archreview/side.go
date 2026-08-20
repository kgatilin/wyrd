package archreview

import (
	"fmt"
	"math"
	"sort"
	"strings"

	archmotifadapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	archmotifimport "github.com/kgatilin/archmotif/pkg/archmotifimport"
	"github.com/kgatilin/archmotif/pkg/components"
	"github.com/kgatilin/archmotif/pkg/filestats"
	"github.com/kgatilin/archmotif/pkg/trophic"
)

// Node-id kind prefixes assigned by internal/adapter/archmotif.
const (
	kindPrefixType   = "type:"
	kindPrefixFn     = "fn:"
	kindPrefixMethod = "method:"
	kindPrefixFile   = "file:"
	kindPrefixPkg    = "pkg:"
)

// dependsOnKind is the coarse package-to-package edge the exporter emits, and
// the one the group-level trophic solve runs on. It is not in trophic's
// default (symbol-level) edge set, so it has to be named explicitly.
const dependsOnKind = "dependsOn"

// symbol is one graph-visible declaration. Constants, variables and struct
// fields are deliberately absent: the graph records no incoming edge for them
// (a field's coupling is recorded on its struct), so an "is anything using
// this?" question has no evidence to answer with, and reporting them would be
// guessing dressed as a finding.
type symbol struct {
	node     string // archmotif node id
	pkg      string
	name     string // declared name; for a method, the method name
	recv     string // receiver or interface type; empty for top-level symbols
	kind     string
	exported bool
	span     domain.Span
}

// isMember reports whether the symbol is a method of a type rather than a
// top-level declaration.
func (s symbol) isMember() bool { return s.recv != "" }

// label is the symbol's fully qualified name, as a row reads it.
func (s symbol) label() string {
	if s.isMember() {
		return s.pkg + "." + s.recv + "." + s.name
	}
	return s.pkg + "." + s.name
}

// target is the symbol's click target in uigraph id conventions: a method
// resolves to its receiver's card row plus the member id, because that is what
// the wiring panel anchors on.
func (s symbol) target() Target {
	if s.isMember() {
		return Target{
			ComponentID: s.pkg,
			InternalID:  s.pkg + "." + s.recv,
			MemberID:    s.pkg + "." + s.recv + "." + s.name,
		}
	}
	return Target{ComponentID: s.pkg, InternalID: s.pkg + "." + s.name}
}

// edgeInfo is one package-to-package edge with the symbol-level dependencies
// that hold it up. The count is what makes an edge "weakest"; the samples are
// what lets a row name the symbol to cut.
type edgeInfo struct {
	weight  int
	samples []string
}

// side is one model set — head or base — with every measurement the sections
// read. Both modes build one for head; review mode builds a second for base
// and subtracts.
type side struct {
	models      []domain.PackageModel
	overlay     *overlay.Config
	byPath      map[string]domain.PackageModel
	pkgs        []string // sorted package paths
	graph       *archmotifimport.Graph
	symbols     map[string]symbol // by archmotif node id
	order       []string          // symbol node ids, sorted
	symbolNodes []string          // symbol node ids the graph actually carries

	edges    map[Edge]*edgeInfo        // package-level edges
	inbound  map[string]int            // node id -> incoming flow edges, any package
	inFrom   map[string]map[string]int // node id -> source package -> incoming flow edges
	viaIface map[string]bool           // method node ids reached through an implemented interface

	symTrophic trophic.Result
	pkgComps   components.Result
	files      filestats.Result

	// declsByFile counts top-level declarations per module-relative file
	// path, read off the same filestats run that flags god-files.
	declsByFile map[string]int
	// fileCutoff is filestats' own outlier threshold, kept as the float it
	// computes: rounding it to an int here would let the growth section
	// flag a file the god-file section calls ordinary.
	fileCutoff float64
}

// newSide measures one model set. A graph that cannot be built yields a nil
// *side and an error the caller reports as a warning rather than as silence.
func newSide(models []domain.PackageModel, cfg *overlay.Config) (*side, error) {
	graph, err := archmotifadapter.ToArchmotifGraph(models, cfg)
	if err != nil {
		return nil, fmt.Errorf("build archmotif graph: %w", err)
	}

	s := &side{
		models:      models,
		overlay:     cfg,
		byPath:      make(map[string]domain.PackageModel, len(models)),
		graph:       graph,
		symbols:     map[string]symbol{},
		edges:       map[Edge]*edgeInfo{},
		inbound:     map[string]int{},
		inFrom:      map[string]map[string]int{},
		viaIface:    map[string]bool{},
		declsByFile: map[string]int{},
	}
	for _, m := range models {
		if m.Path == "" {
			continue
		}
		if _, seen := s.byPath[m.Path]; seen {
			continue
		}
		s.byPath[m.Path] = m
		s.pkgs = append(s.pkgs, m.Path)
	}
	sort.Strings(s.pkgs)

	s.collectSymbols()
	s.collectEdges()
	s.runLenses()
	return s, nil
}

// collectSymbols enumerates every declaration the archmotif graph carries a
// node for, keyed by that node's id so graph edges resolve back to a symbol.
func (s *side) collectSymbols() {
	add := func(sym symbol) {
		s.symbols[sym.node] = sym
		s.order = append(s.order, sym.node)
	}
	for _, m := range s.models {
		for _, iface := range m.Interfaces {
			add(symbol{
				node: kindPrefixType + m.Path + "." + iface.Name, pkg: m.Path,
				name: iface.Name, kind: "interface", exported: iface.IsExported, span: iface.Span,
			})
			for _, method := range iface.Methods {
				add(symbol{
					node: kindPrefixMethod + m.Path + "." + iface.Name + "." + method.Name, pkg: m.Path,
					name: method.Name, recv: iface.Name, kind: "method",
					exported: iface.IsExported && method.IsExported, span: methodSpan(method.Span, iface.Span),
				})
			}
		}
		for _, st := range m.Structs {
			add(symbol{
				node: kindPrefixType + m.Path + "." + st.Name, pkg: m.Path,
				name: st.Name, kind: "struct", exported: st.IsExported, span: st.Span,
			})
			for _, method := range st.Methods {
				add(symbol{
					node: kindPrefixMethod + m.Path + "." + st.Name + "." + method.Name, pkg: m.Path,
					name: method.Name, recv: st.Name, kind: "method",
					exported: st.IsExported && method.IsExported, span: methodSpan(method.Span, st.Span),
				})
			}
		}
		for _, td := range m.TypeDefs {
			add(symbol{
				node: kindPrefixType + m.Path + "." + td.Name, pkg: m.Path,
				name: td.Name, kind: "typedef", exported: td.IsExported, span: td.Span,
			})
		}
		for _, fn := range m.Functions {
			add(symbol{
				node: kindPrefixFn + m.Path + "." + fn.Name, pkg: m.Path,
				name: fn.Name, kind: "func", exported: fn.IsExported, span: fn.Span,
			})
		}
	}
	sort.Strings(s.order)
}

// methodSpan falls back to the declaring type's span when the reader did not
// record one for the method, so a changed hunk still lands on something.
func methodSpan(method, owner domain.Span) domain.Span {
	if method.IsValid() {
		return method
	}
	return owner
}

// collectEdges walks the graph's flow edges once and derives everything read
// off edge direction: package-level edges with the symbol dependencies behind
// them, per-symbol fan-in, and which methods are reached through an interface.
func (s *side) collectEdges() {
	// A method of a type that implements an interface declaring the same
	// method is reached through the interface, so its lack of a direct
	// caller says nothing. The graph records the implements edge between
	// the types only; the method-level pairing is synthesized here, the
	// same way the review UI's wiring panel synthesizes it.
	methodsOf := map[string][]symbol{}
	for _, node := range s.order {
		sym := s.symbols[node]
		if sym.isMember() {
			methodsOf[sym.pkg+"."+sym.recv] = append(methodsOf[sym.pkg+"."+sym.recv], sym)
		}
	}
	for _, e := range s.graph.Edges() {
		if string(e.Kind) != "implements" {
			continue
		}
		iface, okIface := s.symbols[e.To]
		concrete, okConcrete := s.symbols[e.From]
		if !okIface || !okConcrete {
			continue
		}
		for _, m := range methodsOf[concrete.pkg+"."+concrete.name] {
			declared := kindPrefixMethod + iface.pkg + "." + iface.name + "." + m.name
			if _, ok := s.symbols[declared]; ok {
				s.viaIface[m.node] = true
			}
		}
	}

	for _, e := range s.graph.Edges() {
		if !isFlowKind(string(e.Kind)) {
			continue
		}
		from, okFrom := s.symbols[e.From]
		to, okTo := s.symbols[e.To]
		if !okFrom || !okTo {
			continue
		}
		s.inbound[e.To]++
		if from.pkg == to.pkg {
			continue
		}
		if s.inFrom[e.To] == nil {
			s.inFrom[e.To] = map[string]int{}
		}
		s.inFrom[e.To][from.pkg]++
		key := Edge{From: from.pkg, To: to.pkg}
		info := s.edges[key]
		if info == nil {
			info = &edgeInfo{}
			s.edges[key] = info
		}
		info.weight++
		if len(info.samples) < edgeSampleLimit {
			info.samples = append(info.samples, from.label()+" → "+to.label())
		}
	}
}

const edgeSampleLimit = 3

// isFlowKind reports whether an edge kind carries dependency direction. It is
// the projection trophic runs on: structural containment is not a dependency.
func isFlowKind(kind string) bool {
	switch kind {
	case "calls", "usesType", "returns", "implements", "embeds":
		return true
	}
	return false
}

// runLenses hands the graph to archmotif's analyses. Components run on a
// second, package-only graph built through the same public import shim,
// because the symbol projection would otherwise drown the coarse dependsOn
// edges a package-granular question is asked of.
func (s *side) runLenses() {
	var fileNodes []string
	for _, n := range s.graph.Nodes() {
		switch n.Kind {
		case "file":
			fileNodes = append(fileNodes, n.ID)
		case "package", "external", "field":
			// Structural containers and leaves carry no flow edges.
		default:
			if _, ok := s.symbols[n.ID]; ok {
				s.symbolNodes = append(s.symbolNodes, n.ID)
			}
		}
	}
	sort.Strings(s.symbolNodes)
	sort.Strings(fileNodes)

	if len(s.symbolNodes) > 0 {
		s.symTrophic = trophic.Analyze(s.graph, trophic.Options{NodeIDs: s.symbolNodes})
	}
	if len(fileNodes) > 0 {
		s.files = filestats.Analyze(s.graph, filestats.Options{NodeIDs: fileNodes})
		s.fileCutoff = godFileCutoff(s.files.MedianSymbols)
		for _, f := range s.files.Files {
			s.declsByFile[strings.TrimPrefix(f.File, kindPrefixFile)] = f.SymbolCount
		}
	}

	pkgGraph, pkgNodes := s.packageGraph()
	if pkgGraph != nil && len(pkgNodes) > 0 {
		s.pkgComps = components.Analyze(pkgGraph, pkgNodes)
	}
}

// godFileCutoff is the file-hotspot threshold the filestats lens established:
// three times the median, never below twenty.
func godFileCutoff(median float64) float64 {
	if cutoff := 3 * median; cutoff > 20 {
		return cutoff
	}
	return 20
}

// cutoffDeclarations is the smallest whole declaration count that reaches the
// god-file threshold — the number a row quotes.
func (s *side) cutoffDeclarations() int { return int(math.Ceil(s.fileCutoff)) }

// packageGraph builds a package-only archmotif graph from the derived package
// edges, so components can be run at the granularity the section asks about
// without being reimplemented here.
func (s *side) packageGraph() (*archmotifimport.Graph, []string) {
	b := archmotifimport.NewBuilder()
	nodes := make([]string, 0, len(s.pkgs))
	for _, p := range s.pkgs {
		id := kindPrefixPkg + p
		if err := b.AddPackage(id, s.byPath[p].Layer, s.byPath[p].Aggregate); err != nil {
			return nil, nil
		}
		nodes = append(nodes, id)
	}
	for _, e := range s.sortedEdges() {
		if err := b.AddDependency(kindPrefixPkg+e.From, kindPrefixPkg+e.To, archmotifimport.DependencyDependsOn); err != nil {
			return nil, nil
		}
	}
	g, err := b.Build()
	if err != nil {
		return nil, nil
	}
	return g, nodes
}

// sortedEdges returns the package edges in a stable order.
func (s *side) sortedEdges() []Edge {
	out := make([]Edge, 0, len(s.edges))
	for e := range s.edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// isolatedSymbols returns the declarations with no edge, of any kind, to
// another declaration — the singletons of the symbol projection, in the order
// symbolNodes carries.
//
// This is a degree test, not an analysis, which is why it is written here
// rather than delegated. archmotif's components lens answers the same question,
// but it also solves an eigenvector centrality per component, and a codebase's
// symbols form one large component: that is a dense n×n power iteration whose
// centre this section then discards — on a few thousand symbols, a second of
// work for an answer a single pass over the edges gives. Self-loops are skipped
// so a recursive function still reads as isolated, matching the undirected
// projection the lens builds.
func (s *side) isolatedSymbols() []symbol {
	inGraph := make(map[string]bool, len(s.symbolNodes))
	for _, node := range s.symbolNodes {
		inGraph[node] = true
	}
	linked := make(map[string]bool, len(s.symbolNodes))
	for _, e := range s.graph.Edges() {
		if e.From == e.To || !inGraph[e.From] || !inGraph[e.To] {
			continue
		}
		linked[e.From] = true
		linked[e.To] = true
	}
	out := make([]symbol, 0, len(s.symbolNodes))
	for _, node := range s.symbolNodes {
		if linked[node] {
			continue
		}
		if sym, ok := s.symbols[node]; ok {
			out = append(out, sym)
		}
	}
	return out
}

// crossIn is how many flow edges reach the node from another package.
func (s *side) crossIn(node string) int {
	total := 0
	for _, n := range s.inFrom[node] {
		total += n
	}
	return total
}

// packageOfFile maps a module-relative file path back to the package that
// holds it, or "" when no loaded package does.
func (s *side) packageOfFile(path string) string {
	dir := "."
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		dir = path[:i]
	}
	if _, ok := s.byPath[dir]; ok {
		return dir
	}
	return ""
}

// backwardEdges keys the symbol-level inversions by their endpoints so review
// mode can subtract the base's set from head's.
func (s *side) backwardEdges() map[Edge]float64 {
	out := make(map[Edge]float64, len(s.symTrophic.BackwardEdges))
	for _, be := range s.symTrophic.BackwardEdges {
		out[Edge{From: be.From, To: be.To}] = be.Span
	}
	return out
}

// packageDegree is a package's fan-in plus fan-out over distinct neighbours.
func (s *side) packageDegree() map[string]int {
	in := map[string]int{}
	out := map[string]int{}
	for e := range s.edges {
		out[e.From]++
		in[e.To]++
	}
	degree := make(map[string]int, len(s.pkgs))
	for _, p := range s.pkgs {
		degree[p] = in[p] + out[p]
	}
	return degree
}
