package archgraph

import (
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
)

// ProjectPackages reconstructs a []domain.PackageModel slice from
// the graph. Round-trip property: for any models input, BuildGraph
// followed by ProjectPackages returns an equivalent slice modulo
// the deterministic sort order applied below.
//
// Equivalence is defined by domain field equality on every field
// of PackageModel — projection consumes the typed Payload on each
// symbol-kind node and the side-table payloads stashed on the Graph
// for Dependencies / Implementations / Calls.
func (g *Graph) ProjectPackages() []domain.PackageModel {
	// Index nodes by package path.
	pkgs := make(map[string]*domain.PackageModel)
	pkgOrder := make([]string, 0)

	for _, n := range g.Nodes {
		if n.Kind != NodeKindPackage {
			continue
		}
		p := &domain.PackageModel{
			Path:      n.Package,
			Name:      n.Name,
			Layer:     n.Attrs["layer"],
			Aggregate: n.Attrs["aggregate"],
		}
		pkgs[n.Package] = p
		pkgOrder = append(pkgOrder, n.Package)
	}

	// Walk symbol-kind nodes and append to their owning package's
	// slice. We pre-bucket interface/struct method+field payloads
	// because those live on the containing type, not as standalone
	// slice entries on PackageModel.
	for _, n := range g.Nodes {
		p, ok := pkgs[n.Package]
		if !ok {
			continue
		}
		switch n.Kind {
		case NodeKindInterface:
			if v, ok := payloadInterface(n); ok {
				p.Interfaces = append(p.Interfaces, v)
			}
		case NodeKindStruct:
			if v, ok := payloadStruct(n); ok {
				p.Structs = append(p.Structs, v)
			}
		case NodeKindTypeDef:
			if v, ok := payloadTypeDef(n); ok {
				p.TypeDefs = append(p.TypeDefs, v)
			}
		case NodeKindFunction:
			if v, ok := payloadFunction(n); ok {
				p.Functions = append(p.Functions, v)
			}
		case NodeKindConst:
			if v, ok := payloadConst(n); ok {
				p.Constants = append(p.Constants, v)
			}
		case NodeKindVar:
			if v, ok := payloadVar(n); ok {
				p.Variables = append(p.Variables, v)
			}
		case NodeKindError:
			if v, ok := payloadError(n); ok {
				p.Errors = append(p.Errors, v)
			}
		}
	}

	// Restore Dependencies on each owning package from the side
	// table. Side-table keys carry the original "from" package id
	// implicitly via the edge.From node id — we re-parse the package
	// out of From.
	for _, d := range g.depPayloads {
		p, ok := pkgs[d.From.Package]
		if !ok {
			// Dependencies whose owning package wasn't a loaded
			// package can't be projected; this matches the
			// expectation that PackageModel.Dependencies only
			// contains edges owned by that package.
			continue
		}
		p.Dependencies = append(p.Dependencies, d)
	}

	// Restore Implementations on each interface-side package.
	for _, impl := range g.implPayloads {
		p, ok := pkgs[impl.Interface.Package]
		if !ok {
			continue
		}
		p.Implementations = append(p.Implementations, impl)
	}

	// Restore Calls on functions / methods inside packages.
	for _, cp := range g.callPayloads {
		owner, name, recv := parseCallerID(cp.fromID)
		p, ok := pkgs[owner]
		if !ok {
			continue
		}
		if recv == "" {
			// package-level function
			for i := range p.Functions {
				if p.Functions[i].Name == name {
					p.Functions[i].Calls = append(p.Functions[i].Calls, cp.edge)
					break
				}
			}
			continue
		}
		// method on struct or interface
		matched := false
		for i := range p.Structs {
			if p.Structs[i].Name != recv {
				continue
			}
			for j := range p.Structs[i].Methods {
				if p.Structs[i].Methods[j].Name == name {
					p.Structs[i].Methods[j].Calls = append(p.Structs[i].Methods[j].Calls, cp.edge)
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			continue
		}
		for i := range p.Interfaces {
			if p.Interfaces[i].Name != recv {
				continue
			}
			for j := range p.Interfaces[i].Methods {
				if p.Interfaces[i].Methods[j].Name == name {
					p.Interfaces[i].Methods[j].Calls = append(p.Interfaces[i].Methods[j].Calls, cp.edge)
					break
				}
			}
			break
		}
	}

	// Final deterministic ordering.
	sort.Strings(pkgOrder)
	out := make([]domain.PackageModel, 0, len(pkgOrder))
	for _, path := range pkgOrder {
		p := pkgs[path]
		normalizePackage(p)
		out = append(out, *p)
	}
	return out
}

// parseCallerID splits a function or method id into (package, name,
// receiver). For a function id "fn:pkg.X" it returns (pkg, X, "").
// For a method id "method:pkg.Recv.M" it returns (pkg, M, Recv).
// Unknown ids return zero values.
func parseCallerID(id string) (pkg, name, recv string) {
	switch {
	case strings.HasPrefix(id, "fn:"):
		rest := strings.TrimPrefix(id, "fn:")
		// rest is "<pkg>.<name>", split on the LAST dot because
		// package paths may contain dots (rare but allowed).
		idx := strings.LastIndex(rest, ".")
		if idx <= 0 {
			return "", rest, ""
		}
		return rest[:idx], rest[idx+1:], ""
	case strings.HasPrefix(id, "method:"):
		rest := strings.TrimPrefix(id, "method:")
		// rest is "<pkg>.<recv>.<name>". Split off the last two
		// dotted segments. The package can contain dots, so we walk
		// from the right.
		idxName := strings.LastIndex(rest, ".")
		if idxName <= 0 {
			return "", "", ""
		}
		head := rest[:idxName]
		name = rest[idxName+1:]
		idxRecv := strings.LastIndex(head, ".")
		if idxRecv <= 0 {
			return "", name, head
		}
		return head[:idxRecv], name, head[idxRecv+1:]
	}
	return "", "", ""
}

// normalizePackage sorts each slice on a PackageModel in a stable,
// content-derived order. This is what makes round-trip work: the
// same package projected twice yields the same field values in the
// same order.
func normalizePackage(p *domain.PackageModel) {
	sort.SliceStable(p.Interfaces, func(i, j int) bool {
		return p.Interfaces[i].Name < p.Interfaces[j].Name
	})
	for i := range p.Interfaces {
		sortMethods(p.Interfaces[i].Methods)
	}
	sort.SliceStable(p.Structs, func(i, j int) bool {
		return p.Structs[i].Name < p.Structs[j].Name
	})
	for i := range p.Structs {
		sort.SliceStable(p.Structs[i].Fields, func(a, b int) bool {
			return p.Structs[i].Fields[a].Name < p.Structs[i].Fields[b].Name
		})
		sortMethods(p.Structs[i].Methods)
	}
	sort.SliceStable(p.Functions, func(i, j int) bool {
		return p.Functions[i].Name < p.Functions[j].Name
	})
	for i := range p.Functions {
		sortCalls(p.Functions[i].Calls)
	}
	sort.SliceStable(p.TypeDefs, func(i, j int) bool {
		return p.TypeDefs[i].Name < p.TypeDefs[j].Name
	})
	sort.SliceStable(p.Constants, func(i, j int) bool {
		return p.Constants[i].Name < p.Constants[j].Name
	})
	sort.SliceStable(p.Variables, func(i, j int) bool {
		return p.Variables[i].Name < p.Variables[j].Name
	})
	sort.SliceStable(p.Errors, func(i, j int) bool {
		return p.Errors[i].Name < p.Errors[j].Name
	})
	sort.SliceStable(p.Dependencies, func(i, j int) bool {
		return depKey(p.Dependencies[i]) < depKey(p.Dependencies[j])
	})
	sort.SliceStable(p.Implementations, func(i, j int) bool {
		return implKey(p.Implementations[i]) < implKey(p.Implementations[j])
	})
}

func sortMethods(ms []domain.MethodDef) {
	sort.SliceStable(ms, func(i, j int) bool {
		return ms[i].Name < ms[j].Name
	})
	for i := range ms {
		sortCalls(ms[i].Calls)
	}
}

func sortCalls(cs []domain.CallEdge) {
	sort.SliceStable(cs, func(i, j int) bool {
		ki := cs[i].To.QualifiedName() + "|" + cs[i].Via
		kj := cs[j].To.QualifiedName() + "|" + cs[j].Via
		return ki < kj
	})
}

func depKey(d domain.Dependency) string {
	return d.From.QualifiedName() + "->" + d.To.QualifiedName() + ":" + string(d.Kind)
}

func implKey(i domain.Implementation) string {
	return i.Concrete.QualifiedName() + "=>" + i.Interface.QualifiedName()
}

// NormalizePackages applies the same deterministic ordering used
// internally by ProjectPackages to an input slice, so callers can
// compare BuildGraph round-trips against the original input.
func NormalizePackages(models []domain.PackageModel) []domain.PackageModel {
	out := make([]domain.PackageModel, len(models))
	copy(out, models)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	for i := range out {
		normalizePackage(&out[i])
	}
	return out
}
