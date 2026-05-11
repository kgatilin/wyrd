package archgraph

import (
	"sort"

	"github.com/kgatilin/archai/internal/domain"
)

// BuildGraph constructs an architecture graph from a slice of
// domain.PackageModel and an optional domain.Module. The module
// argument may be nil; when non-nil, a single module node is added
// and every package node is connected to it with a contains edge.
//
// BuildGraph is a pure function: same input → byte-identical Graph.
// It does no I/O and never mutates its inputs.
func BuildGraph(models []domain.PackageModel, mod *domain.Module) (*Graph, error) {
	b := newBuilder()

	if mod != nil {
		b.addModule(mod.Path)
	}

	// Snapshot package paths in sorted order so the outer loop is
	// deterministic regardless of caller ordering.
	pkgByPath := make(map[string]*domain.PackageModel, len(models))
	paths := make([]string, 0, len(models))
	for i := range models {
		p := &models[i]
		pkgByPath[p.Path] = p
		paths = append(paths, p.Path)
	}
	sort.Strings(paths)

	// Pass 1: package nodes, file nodes, symbol nodes, containment.
	for _, path := range paths {
		b.addPackageWithContents(pkgByPath[path])
	}

	// Pass 2: implements relationships (interface-side declarations).
	for _, path := range paths {
		b.addImplementations(pkgByPath[path])
	}

	// Pass 3: symbol-level dependency edges (uses/returns/extends/...).
	for _, path := range paths {
		b.addDependencies(pkgByPath[path])
	}

	// Pass 4: call edges from function / method bodies.
	for _, path := range paths {
		b.addCallEdges(pkgByPath[path])
	}

	return b.finalize(), nil
}

// builder accumulates nodes/edges with dedup so callers can emit
// the same id more than once safely (first write wins for payload).
//
// implPayloads / depPayloads / callPayloads are side tables keyed by
// edge id. They retain the original domain values so projection can
// reconstruct the input Dependencies / Implementations / Calls slices
// without re-deriving fields from edge Attrs. They are not exposed
// outside the package.
type builder struct {
	moduleID     string
	nodes        map[string]Node
	edges        map[string]Edge
	implPayloads map[string]domain.Implementation
	depPayloads  map[string]domain.Dependency
	callPayloads map[string]callPayload
}

func newBuilder() *builder {
	return &builder{
		nodes:        make(map[string]Node),
		edges:        make(map[string]Edge),
		implPayloads: make(map[string]domain.Implementation),
		depPayloads:  make(map[string]domain.Dependency),
		callPayloads: make(map[string]callPayload),
	}
}

func (b *builder) addNode(n Node) {
	if _, ok := b.nodes[n.ID]; ok {
		return
	}
	b.nodes[n.ID] = n
}

func (b *builder) addEdge(e Edge) {
	if _, ok := b.edges[e.ID]; ok {
		return
	}
	b.edges[e.ID] = e
}

func (b *builder) addModule(path string) {
	id := ModuleID(path)
	b.moduleID = id
	b.addNode(Node{
		ID:   id,
		Kind: NodeKindModule,
		Name: path,
		Attrs: map[string]string{
			"path": path,
		},
	})
}

func (b *builder) addPackageWithContents(p *domain.PackageModel) {
	pkgID := PackageID(p.Path)
	attrs := map[string]string{
		"path": p.Path,
		"name": p.Name,
	}
	if p.Layer != "" {
		attrs["layer"] = p.Layer
	}
	if p.Aggregate != "" {
		attrs["aggregate"] = p.Aggregate
	}
	b.addNode(Node{
		ID:      pkgID,
		Kind:    NodeKindPackage,
		Name:    p.Name,
		Package: p.Path,
		Attrs:   attrs,
	})

	if b.moduleID != "" {
		b.addEdge(Edge{
			ID:   edgeID(EdgeKindContains, b.moduleID, pkgID),
			Kind: EdgeKindContains,
			From: b.moduleID,
			To:   pkgID,
		})
	}

	// Overlay annotation edges. Edges (not node attrs) per the
	// ticket vocabulary; node attrs are kept too for quick lookup.
	if p.Layer != "" {
		// We don't materialize layer nodes; the layer is a string
		// label. The edge target is a synthetic id so consumers
		// that want a node-based view can add it later without
		// changing this code.
		layerID := "layer:" + p.Layer
		b.addNode(Node{
			ID:   layerID,
			Kind: NodeKindExternal,
			Name: p.Layer,
			Attrs: map[string]string{
				"kind": "layer",
				"name": p.Layer,
			},
		})
		b.addEdge(Edge{
			ID:   edgeID(EdgeKindBelongsToLayer, pkgID, layerID),
			Kind: EdgeKindBelongsToLayer,
			From: pkgID,
			To:   layerID,
		})
	}
	if p.Aggregate != "" {
		aggID := "domain:" + p.Aggregate
		b.addNode(Node{
			ID:   aggID,
			Kind: NodeKindExternal,
			Name: p.Aggregate,
			Attrs: map[string]string{
				"kind": "aggregate",
				"name": p.Aggregate,
			},
		})
		b.addEdge(Edge{
			ID:   edgeID(EdgeKindBelongsToDomain, pkgID, aggID),
			Kind: EdgeKindBelongsToDomain,
			From: pkgID,
			To:   aggID,
		})
	}

	// File nodes — one per distinct source file. They get a
	// contains edge from the package and a contains edge to each
	// symbol declared in that file.
	for _, file := range p.SourceFiles() {
		fid := FileID(p.Path, file)
		b.addNode(Node{
			ID:      fid,
			Kind:    NodeKindFile,
			Name:    file,
			Package: p.Path,
			File:    file,
			Attrs: map[string]string{
				"path": p.Path,
				"file": file,
			},
		})
		b.addEdge(Edge{
			ID:   edgeID(EdgeKindContains, pkgID, fid),
			Kind: EdgeKindContains,
			From: pkgID,
			To:   fid,
		})
	}

	// Interfaces. The payload stripped of method-level Calls so the
	// dedicated call-edge pass owns the calls-roundtrip; otherwise
	// projection would emit each call twice (once from payload,
	// once from edge).
	for _, iface := range p.Interfaces {
		tid := TypeID(p.Path, iface.Name)
		b.addNode(Node{
			ID:      tid,
			Kind:    NodeKindInterface,
			Name:    iface.Name,
			Package: p.Path,
			File:    iface.SourceFile,
			Attrs:   typeAttrs(iface.IsExported, iface.Stereotype, iface.SourceFile, iface.Doc),
			Payload: stripInterfaceCalls(iface),
		})
		b.addContainsFromFileAndPackage(p.Path, iface.SourceFile, tid)
		b.addRoleEdge(tid, iface.Stereotype)
		for _, m := range iface.Methods {
			mid := MethodID(p.Path, iface.Name, m.Name)
			b.addNode(Node{
				ID:      mid,
				Kind:    NodeKindMethod,
				Name:    m.Name,
				Package: p.Path,
				File:    iface.SourceFile,
				Attrs: map[string]string{
					"receiver": iface.Name,
					"exported": boolStr(m.IsExported),
				},
				Payload: m,
			})
			b.addEdge(Edge{
				ID:   edgeID(EdgeKindContains, tid, mid),
				Kind: EdgeKindContains,
				From: tid,
				To:   mid,
			})
		}
	}

	// Structs. Method-level Calls are stripped from the payload —
	// see Interfaces comment.
	for _, s := range p.Structs {
		tid := TypeID(p.Path, s.Name)
		b.addNode(Node{
			ID:      tid,
			Kind:    NodeKindStruct,
			Name:    s.Name,
			Package: p.Path,
			File:    s.SourceFile,
			Attrs:   typeAttrs(s.IsExported, s.Stereotype, s.SourceFile, s.Doc),
			Payload: stripStructCalls(s),
		})
		b.addContainsFromFileAndPackage(p.Path, s.SourceFile, tid)
		b.addRoleEdge(tid, s.Stereotype)
		for _, f := range s.Fields {
			fid := FieldID(p.Path, s.Name, f.Name)
			b.addNode(Node{
				ID:      fid,
				Kind:    NodeKindField,
				Name:    f.Name,
				Package: p.Path,
				File:    s.SourceFile,
				Attrs: map[string]string{
					"struct":   s.Name,
					"type":     f.Type.String(),
					"exported": boolStr(f.IsExported),
					"tag":      f.Tag,
				},
				Payload: f,
			})
			b.addEdge(Edge{
				ID:   edgeID(EdgeKindContains, tid, fid),
				Kind: EdgeKindContains,
				From: tid,
				To:   fid,
			})
		}
		for _, m := range s.Methods {
			mid := MethodID(p.Path, s.Name, m.Name)
			b.addNode(Node{
				ID:      mid,
				Kind:    NodeKindMethod,
				Name:    m.Name,
				Package: p.Path,
				File:    s.SourceFile,
				Attrs: map[string]string{
					"receiver": s.Name,
					"exported": boolStr(m.IsExported),
				},
				Payload: m,
			})
			b.addEdge(Edge{
				ID:   edgeID(EdgeKindContains, tid, mid),
				Kind: EdgeKindContains,
				From: tid,
				To:   mid,
			})
		}
	}

	// TypeDefs (treated as type nodes with kind=typedef).
	for _, td := range p.TypeDefs {
		tid := TypeID(p.Path, td.Name)
		b.addNode(Node{
			ID:      tid,
			Kind:    NodeKindTypeDef,
			Name:    td.Name,
			Package: p.Path,
			File:    td.SourceFile,
			Attrs: map[string]string{
				"underlying": td.UnderlyingType.String(),
				"exported":   boolStr(td.IsExported),
				"stereotype": td.Stereotype.String(),
				"source":     td.SourceFile,
			},
			Payload: td,
		})
		b.addContainsFromFileAndPackage(p.Path, td.SourceFile, tid)
		b.addRoleEdge(tid, td.Stereotype)
	}

	// Package-level functions.
	for _, fn := range p.Functions {
		fid := FunctionID(p.Path, fn.Name)
		b.addNode(Node{
			ID:      fid,
			Kind:    NodeKindFunction,
			Name:    fn.Name,
			Package: p.Path,
			File:    fn.SourceFile,
			Attrs: map[string]string{
				"exported":   boolStr(fn.IsExported),
				"stereotype": fn.Stereotype.String(),
				"source":     fn.SourceFile,
			},
			Payload: stripFunctionCalls(fn),
		})
		b.addContainsFromFileAndPackage(p.Path, fn.SourceFile, fid)
		b.addRoleEdge(fid, fn.Stereotype)
	}

	// Constants.
	for _, c := range p.Constants {
		cid := ConstID(p.Path, c.Name)
		b.addNode(Node{
			ID:      cid,
			Kind:    NodeKindConst,
			Name:    c.Name,
			Package: p.Path,
			File:    c.SourceFile,
			Attrs: map[string]string{
				"type":     c.Type.String(),
				"value":    c.Value,
				"exported": boolStr(c.IsExported),
			},
			Payload: c,
		})
		b.addContainsFromFileAndPackage(p.Path, c.SourceFile, cid)
	}

	// Variables.
	for _, v := range p.Variables {
		vid := VarID(p.Path, v.Name)
		b.addNode(Node{
			ID:      vid,
			Kind:    NodeKindVar,
			Name:    v.Name,
			Package: p.Path,
			File:    v.SourceFile,
			Attrs: map[string]string{
				"type":     v.Type.String(),
				"exported": boolStr(v.IsExported),
			},
			Payload: v,
		})
		b.addContainsFromFileAndPackage(p.Path, v.SourceFile, vid)
	}

	// Errors.
	for _, e := range p.Errors {
		eid := ErrorID(p.Path, e.Name)
		b.addNode(Node{
			ID:      eid,
			Kind:    NodeKindError,
			Name:    e.Name,
			Package: p.Path,
			File:    e.SourceFile,
			Attrs: map[string]string{
				"message":  e.Message,
				"exported": boolStr(e.IsExported),
			},
			Payload: e,
		})
		b.addContainsFromFileAndPackage(p.Path, e.SourceFile, eid)
	}
}

// addContainsFromFileAndPackage emits package→symbol and (if a
// source file is known) file→symbol contains edges.
func (b *builder) addContainsFromFileAndPackage(pkgPath, file, symbolID string) {
	pkgID := PackageID(pkgPath)
	b.addEdge(Edge{
		ID:   edgeID(EdgeKindContains, pkgID, symbolID),
		Kind: EdgeKindContains,
		From: pkgID,
		To:   symbolID,
	})
	if file == "" {
		return
	}
	fid := FileID(pkgPath, file)
	b.addEdge(Edge{
		ID:   edgeID(EdgeKindContains, fid, symbolID),
		Kind: EdgeKindContains,
		From: fid,
		To:   symbolID,
	})
}

// addRoleEdge attaches a has_role annotation edge to a synthetic role
// node when the stereotype is non-empty.
func (b *builder) addRoleEdge(symbolID string, s domain.Stereotype) {
	if s.IsEmpty() {
		return
	}
	roleID := "role:" + s.String()
	b.addNode(Node{
		ID:   roleID,
		Kind: NodeKindExternal,
		Name: s.String(),
		Attrs: map[string]string{
			"kind": "role",
			"name": s.String(),
		},
	})
	b.addEdge(Edge{
		ID:   edgeID(EdgeKindHasRole, symbolID, roleID),
		Kind: EdgeKindHasRole,
		From: symbolID,
		To:   roleID,
	})
}

// addImplementations emits implements edges declared on this
// package's Implementations slice. Both endpoints become nodes:
// the interface (already added by addPackageWithContents above)
// and the concrete (added here as an external if not loaded).
func (b *builder) addImplementations(p *domain.PackageModel) {
	for _, impl := range p.Implementations {
		ifaceID := symbolRefToNodeID(impl.Interface, NodeKindInterface)
		concID := symbolRefToNodeID(impl.Concrete, NodeKindStruct)
		// Ensure concrete node exists even when it lives in a
		// package we did not load (or when the concrete is in a
		// different package and Pass 1 hasn't reached it — both
		// orderings are fine because of dedup).
		if _, ok := b.nodes[concID]; !ok {
			b.addNode(Node{
				ID:      concID,
				Kind:    classifyExternal(impl.Concrete),
				Name:    impl.Concrete.Symbol,
				Package: impl.Concrete.Package,
				Attrs: map[string]string{
					"external": boolStr(impl.Concrete.External),
				},
			})
		}
		if _, ok := b.nodes[ifaceID]; !ok {
			b.addNode(Node{
				ID:      ifaceID,
				Kind:    classifyExternal(impl.Interface),
				Name:    impl.Interface.Symbol,
				Package: impl.Interface.Package,
				Attrs: map[string]string{
					"external": boolStr(impl.Interface.External),
				},
			})
		}
		e := Edge{
			ID:   edgeID(EdgeKindImplements, concID, ifaceID),
			Kind: EdgeKindImplements,
			From: concID,
			To:   ifaceID,
			Attrs: map[string]string{
				"is_pointer":     boolStr(impl.IsPointer),
				"iface_package":  impl.Interface.Package,
				"iface_file":     impl.Interface.File,
				"iface_external": boolStr(impl.Interface.External),
				"conc_package":   impl.Concrete.Package,
				"conc_file":      impl.Concrete.File,
				"conc_external":  boolStr(impl.Concrete.External),
			},
		}
		// Stash the original on the edge so projection is exact.
		b.edges[e.ID] = e
		b.implPayloads[e.ID] = impl
	}
}

// addDependencies emits one edge per Dependency. The kind maps 1:1
// to EdgeKind. Implements dependencies are also recorded so projection
// can rebuild the full Dependencies slice; the structural Implements
// pass above is the canonical implementation-relationship source for
// graph consumers, but PackageModel.Dependencies may also carry the
// edge separately and we round-trip both.
func (b *builder) addDependencies(p *domain.PackageModel) {
	for _, d := range p.Dependencies {
		kind := dependencyKindToEdgeKind(d.Kind)
		fromID := symbolRefToNodeID(d.From, "")
		toID := symbolRefToNodeID(d.To, "")
		// Ensure endpoint nodes exist (external dependencies often
		// don't have nodes from the contents pass).
		if _, ok := b.nodes[fromID]; !ok {
			b.addNode(Node{
				ID:      fromID,
				Kind:    classifyExternal(d.From),
				Name:    d.From.Symbol,
				Package: d.From.Package,
				File:    d.From.File,
				Attrs: map[string]string{
					"external": boolStr(d.From.External),
				},
			})
		}
		if _, ok := b.nodes[toID]; !ok {
			b.addNode(Node{
				ID:      toID,
				Kind:    classifyExternal(d.To),
				Name:    d.To.Symbol,
				Package: d.To.Package,
				File:    d.To.File,
				Attrs: map[string]string{
					"external": boolStr(d.To.External),
				},
			})
		}
		e := Edge{
			ID:   edgeID(kind, fromID, toID),
			Kind: kind,
			From: fromID,
			To:   toID,
			Attrs: map[string]string{
				"through_exported": boolStr(d.ThroughExported),
				"from_package":     d.From.Package,
				"from_file":        d.From.File,
				"from_external":    boolStr(d.From.External),
				"to_package":       d.To.Package,
				"to_file":          d.To.File,
				"to_external":      boolStr(d.To.External),
			},
		}
		b.edges[e.ID] = e
		b.depPayloads[e.ID] = d
	}
}

// addCallEdges emits one edge per CallEdge on every function/method
// in the package. The call payload is stashed for exact round-trip
// because the same (from,to) edge id collapses Via variants, while
// PackageModel preserves them as separate slice entries.
func (b *builder) addCallEdges(p *domain.PackageModel) {
	emit := func(fromID string, calls []domain.CallEdge) {
		for _, c := range calls {
			toID := symbolRefToNodeID(c.To, "")
			if _, ok := b.nodes[toID]; !ok {
				b.addNode(Node{
					ID:      toID,
					Kind:    classifyExternal(c.To),
					Name:    c.To.Symbol,
					Package: c.To.Package,
					File:    c.To.File,
					Attrs: map[string]string{
						"external": boolStr(c.To.External),
					},
				})
			}
			// Edge id includes Via so interface-dispatched fanout
			// keeps each variant as its own edge.
			id := edgeID(EdgeKindCalls, fromID, toID)
			if c.Via != "" {
				id = id + "|via=" + c.Via
			}
			e := Edge{
				ID:   id,
				Kind: EdgeKindCalls,
				From: fromID,
				To:   toID,
				Attrs: map[string]string{
					"via":         c.Via,
					"to_package":  c.To.Package,
					"to_file":     c.To.File,
					"to_external": boolStr(c.To.External),
				},
			}
			b.edges[e.ID] = e
			b.callPayloads[e.ID] = callPayload{
				fromID: fromID,
				edge:   c,
			}
		}
	}

	for _, fn := range p.Functions {
		emit(FunctionID(p.Path, fn.Name), fn.Calls)
	}
	for _, s := range p.Structs {
		for _, m := range s.Methods {
			emit(MethodID(p.Path, s.Name, m.Name), m.Calls)
		}
	}
	for _, iface := range p.Interfaces {
		for _, m := range iface.Methods {
			emit(MethodID(p.Path, iface.Name, m.Name), m.Calls)
		}
	}
}

// finalize sorts everything by id and returns the graph.
func (b *builder) finalize() *Graph {
	nodes := make([]Node, 0, len(b.nodes))
	for _, n := range b.nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	edges := make([]Edge, 0, len(b.edges))
	for _, e := range b.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	return &Graph{
		ModuleID:     b.moduleID,
		Nodes:        nodes,
		Edges:        edges,
		implPayloads: b.implPayloads,
		depPayloads:  b.depPayloads,
		callPayloads: b.callPayloads,
	}
}

// typeAttrs builds the common attribute set for interface/struct
// nodes.
func typeAttrs(isExported bool, s domain.Stereotype, source, doc string) map[string]string {
	return map[string]string{
		"exported":   boolStr(isExported),
		"stereotype": s.String(),
		"source":     source,
		"doc":        doc,
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// dependencyKindToEdgeKind maps domain.DependencyKind onto EdgeKind.
func dependencyKindToEdgeKind(k domain.DependencyKind) EdgeKind {
	switch k {
	case domain.DependencyUses:
		return EdgeKindUses
	case domain.DependencyReturns:
		return EdgeKindReturns
	case domain.DependencyImplements:
		return EdgeKindImplements
	case domain.DependencyExtends:
		return EdgeKindExtends
	case domain.DependencyNestedIn:
		return EdgeKindNestedIn
	default:
		return EdgeKindUses
	}
}

// classifyExternal returns the node kind for a SymbolRef that does
// not already have a typed node. External refs become NodeKindExternal;
// otherwise we conservatively return NodeKindStruct (the most common
// concrete-type kind). This is only used to backfill placeholder
// nodes; real classification is done in the contents pass.
func classifyExternal(r domain.SymbolRef) NodeKind {
	if r.External {
		return NodeKindExternal
	}
	return NodeKindStruct
}

// symbolRefToNodeID converts a SymbolRef to the node id used in the
// graph. For external refs we always use ExternalID; for module
// internal refs we use TypeID by default but allow callers to hint.
func symbolRefToNodeID(r domain.SymbolRef, hint NodeKind) string {
	if r.External {
		return ExternalID(r.Package, r.Symbol)
	}
	// Inside the loaded module, the symbol kind isn't recorded on
	// SymbolRef so we use the same TypeID prefix as interfaces and
	// structs. This is fine because all symbols sharing a package
	// name space already share the type: namespace in our id scheme.
	_ = hint
	return TypeID(r.Package, r.Symbol)
}

// stripInterfaceCalls returns a shallow copy of iface with the
// per-method Calls slices nil. The call edges are stored separately
// (see addCallEdges) and re-attached on projection; keeping them on
// the payload too would double them after round-trip.
func stripInterfaceCalls(iface domain.InterfaceDef) domain.InterfaceDef {
	out := iface
	out.Methods = make([]domain.MethodDef, len(iface.Methods))
	for i, m := range iface.Methods {
		m.Calls = nil
		out.Methods[i] = m
	}
	return out
}

// stripStructCalls returns a shallow copy of s with the per-method
// Calls slices nil.
func stripStructCalls(s domain.StructDef) domain.StructDef {
	out := s
	out.Methods = make([]domain.MethodDef, len(s.Methods))
	for i, m := range s.Methods {
		m.Calls = nil
		out.Methods[i] = m
	}
	return out
}

// stripFunctionCalls returns a shallow copy of fn with Calls nil.
func stripFunctionCalls(fn domain.FunctionDef) domain.FunctionDef {
	out := fn
	out.Calls = nil
	return out
}

// callPayload retains the original CallEdge plus the from-side
// caller id so projection can place the edge on the right
// function/method when rebuilding Calls slices.
type callPayload struct {
	fromID string
	edge   domain.CallEdge
}
