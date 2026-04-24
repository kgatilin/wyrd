package http

import (
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/sequence"
)

// typeKind classifies the detail page so the template can render the
// right subset of sections (interfaces have methods but no fields;
// typedefs have an underlying type instead).
type typeKind string

const (
	typeKindStruct    typeKind = "struct"
	typeKindInterface typeKind = "interface"
	typeKindTypeDef   typeKind = "type"
)

// typeRef names a type found in the loaded model, scoped to its owning
// package so two types with the same short name in different packages
// remain distinct.
type typeRef struct {
	Package string
	Name    string
}

// id returns the route-id used in /types/{id} URLs. The leading
// component is the package path (may contain slashes), the trailing
// component after the final "." is the type name.
func (r typeRef) id() string { return r.Package + "." + r.Name }

// parseTypeID parses the id produced by typeRef.id — the last "." in
// the final path segment separates package from type name. Returns
// (ref, true) on success.
func parseTypeID(id string) (typeRef, bool) {
	if id == "" {
		return typeRef{}, false
	}
	// The id may contain slashes in the package part. The type name is
	// after the LAST dot in the final slash-separated segment.
	slash := strings.LastIndex(id, "/")
	var prefix, rest string
	if slash >= 0 {
		prefix = id[:slash+1]
		rest = id[slash+1:]
	} else {
		rest = id
	}
	dot := strings.LastIndex(rest, ".")
	if dot <= 0 || dot == len(rest)-1 {
		return typeRef{}, false
	}
	return typeRef{
		Package: prefix + rest[:dot],
		Name:    rest[dot+1:],
	}, true
}

// fieldView is one row in the type-detail Fields table. Type carries
// a pre-formatted TypeRef so the template only has to print a string.
type fieldView struct {
	Name       string
	Type       string
	Tag        string
	IsExported bool
}

// methodView is one entry in the type-detail Methods list.
type methodView struct {
	Name       string
	Signature  string
	IsExported bool
}

// relatedView names another type related to the subject type — either
// an interface it implements or a concrete type that implements it.
// Href points at the /types/{id} detail page for the related type.
type relatedView struct {
	Package string
	Name    string
	Href    string
}

// usedByView is one entry in the Used-by list: a package path with a
// link to /packages/{pkg}. Count is how many references that package
// has to the subject type.
type usedByView struct {
	Package string
	Href    string
	Count   int
}

// graphJSON is the JSON payload consumed by the browser-side Cytoscape
// renderer. Nodes have ids + labels + a "kind" attribute for styling;
// edges connect node ids and carry an optional label.
type graphJSON struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

type graphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Root  bool   `json:"root,omitempty"`
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

// sequenceEntry is one public-method entry point for which a static
// call tree is pre-computed on page load.
type sequenceEntry struct {
	Method string    // MethodDef.Name
	Label  string    // "TypeName.Method"
	Graph  graphJSON // cytoscape nodes/edges for the tree
	HasM6  bool      // true when CallEdge data was available
}

// typePageData is the page model for /types/{id}.
type typePageData struct {
	pageData

	Kind       typeKind
	Package    string
	Name       string
	IsExported bool
	SourceFile string
	Doc        string
	Stereotype string

	// Typedef-only: underlying type rendered for display.
	Underlying string

	Fields  []fieldView
	Methods []methodView

	Implements    []relatedView // interfaces this concrete type implements
	ImplementedBy []relatedView // concrete types implementing this interface
	UsedBy        []usedByView  // packages referencing this type
	PackageHref   string        // /packages/<pkg> link for the owning package
	RelGraph      graphJSON     // Cytoscape payload for the relationship graph
	RelGraphJSON  string        // serialized graphJSON ready for embedding
	RelGraphEmpty bool          // true when the graph has only the root node
	Sequences     []sequenceEntry
	HasSequences  bool
}

// handleType renders /types/{id}. Missing types return 404.
func (s *Server) handleType(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/types/")
	ref, ok := parseTypeID(id)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}

	snap := s.state.Snapshot()
	data, found := buildTypePage(snap.Packages, ref)
	if !found {
		nethttp.NotFound(w, r)
		return
	}
	data.pageData = pageData{
		Title:      ref.Name + " — Types",
		ActivePath: "/packages",
		NavItems:   buildNav("/packages"),
	}

	s.renderPage(w, "type_detail.html", data)
}

// handleTypeGraph serves the cytoscape nodes/edges for /types/{id} as
// JSON. This is the same payload embedded in the page; exposing it
// separately lets the frontend refresh the graph without reloading
// the whole page once the daemon's auto-reload lands.
func (s *Server) handleTypeGraph(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/types/")
	id = strings.TrimSuffix(id, "/graph")
	ref, ok := parseTypeID(id)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}

	snap := s.state.Snapshot()
	data, found := buildTypePage(snap.Packages, ref)
	if !found {
		nethttp.NotFound(w, r)
		return
	}
	writeJSON(w, data.RelGraph)
}

// buildTypePage locates the type identified by ref and assembles its
// detail-page model. Returns (data, false) when no such type exists.
func buildTypePage(packages []domain.PackageModel, ref typeRef) (typePageData, bool) {
	var data typePageData
	data.Package = ref.Package
	data.Name = ref.Name
	data.PackageHref = "/packages/" + ref.Package

	var (
		owner  *domain.PackageModel
		found  bool
		fields []domain.FieldDef
		meths  []domain.MethodDef
	)

	for i := range packages {
		if packages[i].Path != ref.Package {
			continue
		}
		owner = &packages[i]
		// Look for the type in structs / interfaces / typedefs.
		for _, s := range owner.Structs {
			if s.Name == ref.Name {
				data.Kind = typeKindStruct
				data.IsExported = s.IsExported
				data.SourceFile = s.SourceFile
				data.Doc = s.Doc
				data.Stereotype = string(s.Stereotype)
				fields = s.Fields
				meths = s.Methods
				found = true
				break
			}
		}
		if !found {
			for _, iface := range owner.Interfaces {
				if iface.Name == ref.Name {
					data.Kind = typeKindInterface
					data.IsExported = iface.IsExported
					data.SourceFile = iface.SourceFile
					data.Doc = iface.Doc
					data.Stereotype = string(iface.Stereotype)
					meths = iface.Methods
					found = true
					break
				}
			}
		}
		if !found {
			for _, td := range owner.TypeDefs {
				if td.Name == ref.Name {
					data.Kind = typeKindTypeDef
					data.IsExported = td.IsExported
					data.SourceFile = td.SourceFile
					data.Doc = td.Doc
					data.Stereotype = string(td.Stereotype)
					data.Underlying = td.UnderlyingType.String()
					found = true
					break
				}
			}
		}
		break
	}

	if !found {
		return data, false
	}

	data.Fields = toFieldViews(fields)
	data.Methods = toMethodViews(meths)
	data.Implements = collectImplements(packages, ref)
	data.ImplementedBy = collectImplementedBy(packages, ref)
	data.UsedBy = collectUsedBy(packages, ref)
	data.RelGraph = buildRelationshipGraph(ref, data.Implements, data.ImplementedBy, data.UsedBy)
	data.RelGraphEmpty = len(data.RelGraph.Edges) == 0
	if buf, err := json.Marshal(data.RelGraph); err == nil {
		data.RelGraphJSON = string(buf)
	}

	// Sequence trees from public methods when M6 CallEdge data is
	// available. A method without any Calls produces a root-only tree
	// which is still useful (shows the entry point), but we tag
	// HasM6=false in that case so the template can note it.
	data.Sequences = buildSequenceEntries(packages, ref, meths)
	data.HasSequences = len(data.Sequences) > 0

	return data, true
}

// toFieldViews converts domain FieldDefs into the flat view rows used
// by the template. Ordering is preserved (source order).
func toFieldViews(fields []domain.FieldDef) []fieldView {
	out := make([]fieldView, 0, len(fields))
	for _, f := range fields {
		out = append(out, fieldView{
			Name:       f.Name,
			Type:       f.Type.String(),
			Tag:        f.Tag,
			IsExported: f.IsExported,
		})
	}
	return out
}

// toMethodViews formats method signatures for the detail page.
func toMethodViews(methods []domain.MethodDef) []methodView {
	out := make([]methodView, 0, len(methods))
	for _, m := range methods {
		out = append(out, methodView{
			Name:       m.Name,
			Signature:  m.Signature(),
			IsExported: m.IsExported,
		})
	}
	return out
}

// collectImplements returns the interfaces implemented by a concrete
// type. Uses the cross-package PackageModel.Implementations slices.
func collectImplements(packages []domain.PackageModel, ref typeRef) []relatedView {
	var out []relatedView
	seen := make(map[string]bool)
	for _, p := range packages {
		for _, impl := range p.Implementations {
			if impl.Concrete.Package != ref.Package || impl.Concrete.Symbol != ref.Name {
				continue
			}
			ifaceRef := typeRef{Package: impl.Interface.Package, Name: impl.Interface.Symbol}
			key := ifaceRef.id()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, relatedView{
				Package: ifaceRef.Package,
				Name:    ifaceRef.Name,
				Href:    "/types/" + ifaceRef.id(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// collectImplementedBy returns the concrete types that implement the
// subject interface.
func collectImplementedBy(packages []domain.PackageModel, ref typeRef) []relatedView {
	var out []relatedView
	seen := make(map[string]bool)
	for _, p := range packages {
		for _, impl := range p.Implementations {
			if impl.Interface.Package != ref.Package || impl.Interface.Symbol != ref.Name {
				continue
			}
			concrete := typeRef{Package: impl.Concrete.Package, Name: impl.Concrete.Symbol}
			key := concrete.id()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, relatedView{
				Package: concrete.Package,
				Name:    concrete.Name,
				Href:    "/types/" + concrete.id(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// collectUsedBy counts how many times each package references the
// subject type via Dependencies. We exclude the owning package (a type
// is always "used by" itself) and external symbols.
func collectUsedBy(packages []domain.PackageModel, ref typeRef) []usedByView {
	counts := make(map[string]int)
	for _, p := range packages {
		for _, dep := range p.Dependencies {
			if dep.To.External {
				continue
			}
			if dep.To.Package != ref.Package || dep.To.Symbol != ref.Name {
				continue
			}
			if dep.From.Package == ref.Package {
				// Internal reference — don't surface the type's own
				// package under "Used by".
				continue
			}
			counts[dep.From.Package]++
		}
	}
	out := make([]usedByView, 0, len(counts))
	for pkg, n := range counts {
		out = append(out, usedByView{
			Package: pkg,
			Href:    "/packages/" + pkg,
			Count:   n,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Package < out[j].Package
	})
	return out
}

// buildRelationshipGraph produces the Cytoscape payload for the type
// detail page. It places the subject type at the center and connects
// implements / implemented-by / used-by relations as labeled edges.
func buildRelationshipGraph(ref typeRef, impls, implBy []relatedView, usedBy []usedByView) graphJSON {
	root := graphNode{
		ID:    "type:" + ref.id(),
		Label: ref.Name,
		Kind:  "type",
		Root:  true,
	}
	out := graphJSON{Nodes: []graphNode{root}}

	addNode := func(n graphNode) {
		for _, existing := range out.Nodes {
			if existing.ID == n.ID {
				return
			}
		}
		out.Nodes = append(out.Nodes, n)
	}

	for _, r := range impls {
		id := "type:" + r.Package + "." + r.Name
		addNode(graphNode{ID: id, Label: r.Name, Kind: "interface"})
		out.Edges = append(out.Edges, graphEdge{
			Source: root.ID,
			Target: id,
			Label:  "implements",
			Kind:   "implements",
		})
	}
	for _, r := range implBy {
		id := "type:" + r.Package + "." + r.Name
		addNode(graphNode{ID: id, Label: r.Name, Kind: "struct"})
		out.Edges = append(out.Edges, graphEdge{
			Source: id,
			Target: root.ID,
			Label:  "implements",
			Kind:   "implemented-by",
		})
	}
	for _, u := range usedBy {
		id := "pkg:" + u.Package
		addNode(graphNode{ID: id, Label: shortName(u.Package), Kind: "package"})
		out.Edges = append(out.Edges, graphEdge{
			Source: id,
			Target: root.ID,
			Label:  fmt.Sprintf("uses ×%d", u.Count),
			Kind:   "uses",
		})
	}
	return out
}

// buildSequenceEntries computes a sequence tree from each public
// method of the subject type (when the type is a struct/interface with
// methods). Depth is capped at 4 to keep the rendered graph readable.
// Methods with no captured Calls still produce a root-only tree which
// the template renders as "(no recorded calls)".
func buildSequenceEntries(packages []domain.PackageModel, ref typeRef, meths []domain.MethodDef) []sequenceEntry {
	const maxDepth = 4
	var out []sequenceEntry
	for _, m := range meths {
		if !m.IsExported {
			continue
		}
		start := domain.SymbolRef{Package: ref.Package, Symbol: ref.Name + "." + m.Name}
		node := sequence.Build(packages, start, maxDepth)
		entry := sequenceEntry{
			Method: m.Name,
			Label:  ref.Name + "." + m.Name,
			Graph:  sequenceNodeToGraph(node),
			HasM6:  len(m.Calls) > 0,
		}
		out = append(out, entry)
	}
	return out
}

// sequenceNodeToGraph converts a sequence.Node tree into the flat
// cytoscape graph payload the page renders. Node ids are the
// "pkg|Symbol" key plus a stable breadth-first index so repeat visits
// in different subtrees get distinct nodes.
func sequenceNodeToGraph(root *sequence.Node) graphJSON {
	if root == nil {
		return graphJSON{}
	}
	var out graphJSON
	var walk func(n *sequence.Node, parentID string, counter *int)
	walk = func(n *sequence.Node, parentID string, counter *int) {
		if n == nil {
			return
		}
		id := fmt.Sprintf("seq-%d", *counter)
		*counter++
		label := n.Symbol.Symbol
		if n.Symbol.Package != "" {
			label = shortName(n.Symbol.Package) + "." + label
		}
		kind := "call"
		switch {
		case n.Cycle:
			kind = "cycle"
		case n.DepthLimit:
			kind = "depth-limit"
		case n.NotFound:
			kind = "not-found"
		case parentID == "":
			kind = "root"
		}
		out.Nodes = append(out.Nodes, graphNode{ID: id, Label: label, Kind: kind, Root: parentID == ""})
		if parentID != "" {
			out.Edges = append(out.Edges, graphEdge{Source: parentID, Target: id, Label: n.Via})
		}
		for _, child := range n.Children {
			walk(child, id, counter)
		}
	}
	counter := 0
	walk(root, "", &counter)
	return out
}

// writeJSON marshals v as application/json. On error a 500 is emitted
// — the payload is always a plain struct so failures are exceptional.
func writeJSON(w nethttp.ResponseWriter, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		nethttp.Error(w, fmt.Sprintf("json marshal: %v", err), nethttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(buf)
}
