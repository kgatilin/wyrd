package d2

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/sequence"
)

// SequenceOptions controls D2 sequence diagram generation.
type SequenceOptions struct {
	Mode            OverviewMode
	MaxDepth        int
	IncludeRootOnly bool
}

// SequenceDiagram is one D2 sequence diagram rooted at a function or method.
type SequenceDiagram struct {
	Label    string
	Start    domain.SymbolRef
	Tree     *sequence.Node
	Source   string
	HasCalls bool
}

func normalizeSequenceOptions(opts SequenceOptions) SequenceOptions {
	opts.Mode = opts.Mode.Normalize()
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 4
	}
	return opts
}

// BuildSequenceSource renders a call tree as D2 sequence_diagram source.
func BuildSequenceSource(root *sequence.Node) string {
	if root == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("shape: sequence_diagram\n")
	writeSequenceD2Edges(&sb, root)
	return sb.String()
}

// BuildSequenceForTarget builds a single target-rooted sequence diagram.
func BuildSequenceForTarget(models []domain.PackageModel, start domain.SymbolRef, maxDepth int) SequenceDiagram {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	tree := sequence.Build(models, start, maxDepth)
	return SequenceDiagram{
		Label:    start.Symbol,
		Start:    start,
		Tree:     tree,
		Source:   BuildSequenceSource(tree),
		HasCalls: tree != nil && len(tree.Children) > 0,
	}
}

// BuildTypeSequenceSources builds one D2 sequence diagram per visible method.
func BuildTypeSequenceSources(models []domain.PackageModel, pkgPath, typeName string, methods []domain.MethodDef, maxDepth int) []SequenceDiagram {
	if maxDepth <= 0 {
		maxDepth = 4
	}
	out := make([]SequenceDiagram, 0, len(methods))
	for _, m := range methods {
		if !m.IsExported {
			continue
		}
		start := domain.SymbolRef{Package: pkgPath, Symbol: typeName + "." + m.Name}
		tree := sequence.Build(models, start, maxDepth)
		out = append(out, SequenceDiagram{
			Label:    typeName + "." + m.Name,
			Start:    start,
			Tree:     tree,
			Source:   BuildSequenceSource(tree),
			HasCalls: len(m.Calls) > 0,
		})
	}
	return out
}

// BuildPackageSequenceSources builds one D2 sequence diagram per package
// entry point, matching the browser package Overview sequence list.
func BuildPackageSequenceSources(models []domain.PackageModel, pkg domain.PackageModel, opts SequenceOptions) []SequenceDiagram {
	opts = normalizeSequenceOptions(opts)
	includeUnexported := opts.Mode.includesUnexported()

	type candidate struct {
		diagram  SequenceDiagram
		priority int // 0 = constructor/factory, 1 = function, 2 = method
	}
	var cands []candidate

	for _, fn := range pkg.Functions {
		if !fn.IsExported && !includeUnexported {
			continue
		}
		if !opts.IncludeRootOnly && len(fn.Calls) == 0 {
			continue
		}
		start := domain.SymbolRef{Package: pkg.Path, Symbol: fn.Name}
		tree := sequence.Build(models, start, opts.MaxDepth)
		priority := 1
		if fn.Stereotype == domain.StereotypeFactory || strings.HasPrefix(fn.Name, "New") {
			priority = 0
		}
		cands = append(cands, candidate{
			diagram: SequenceDiagram{
				Label:    fn.Name,
				Start:    start,
				Tree:     tree,
				Source:   BuildSequenceSource(tree),
				HasCalls: len(fn.Calls) > 0,
			},
			priority: priority,
		})
	}

	for _, st := range pkg.Structs {
		if !st.IsExported && !includeUnexported {
			continue
		}
		for _, m := range st.Methods {
			if !m.IsExported && !includeUnexported {
				continue
			}
			if !opts.IncludeRootOnly && len(m.Calls) == 0 {
				continue
			}
			start := domain.SymbolRef{Package: pkg.Path, Symbol: st.Name + "." + m.Name}
			tree := sequence.Build(models, start, opts.MaxDepth)
			cands = append(cands, candidate{
				diagram: SequenceDiagram{
					Label:    st.Name + "." + m.Name,
					Start:    start,
					Tree:     tree,
					Source:   BuildSequenceSource(tree),
					HasCalls: len(m.Calls) > 0,
				},
				priority: 2,
			})
		}
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].priority != cands[j].priority {
			return cands[i].priority < cands[j].priority
		}
		return cands[i].diagram.Label < cands[j].diagram.Label
	})

	out := make([]SequenceDiagram, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.diagram)
	}
	return out
}

func writeSequenceD2Edges(sb *strings.Builder, n *sequence.Node) {
	caller := sequenceActorName(n.Symbol)
	for _, c := range n.Children {
		callee := sequenceActorName(c.Symbol)
		label := sequenceMethodLabel(c.Symbol)
		if c.Via != "" {
			label += " [via " + c.Via + "]"
		}
		switch {
		case c.Cycle:
			label += " (cycle)"
		case c.NotFound:
			label += " (unresolved)"
		case c.DepthLimit:
			label += " (depth limit)"
		}
		fmt.Fprintf(sb, "%s -> %s: %s\n",
			sequenceD2Ident(caller), sequenceD2Ident(callee), sequenceD2Label(label))
		if !c.Cycle && !c.NotFound && !c.DepthLimit {
			writeSequenceD2Edges(sb, c)
		}
	}
}

func sequenceActorName(ref domain.SymbolRef) string {
	leaf := sequencePkgLeaf(ref.Package)
	typ, _ := sequenceSplitMethodSymbol(ref.Symbol)
	if typ != "" {
		if leaf == "" {
			return typ
		}
		return leaf + "." + typ
	}
	if leaf == "" {
		return ref.Symbol
	}
	return leaf + "." + ref.Symbol
}

func sequenceMethodLabel(ref domain.SymbolRef) string {
	_, method := sequenceSplitMethodSymbol(ref.Symbol)
	if method != "" {
		return method
	}
	return ref.Symbol
}

func sequenceSplitMethodSymbol(sym string) (typ, method string) {
	dot := strings.Index(sym, ".")
	if dot < 0 {
		return "", ""
	}
	return sym[:dot], sym[dot+1:]
}

func sequencePkgLeaf(pkg string) string {
	if pkg == "" {
		return ""
	}
	slash := strings.LastIndex(pkg, "/")
	if slash < 0 {
		return pkg
	}
	return pkg[slash+1:]
}

func sequenceD2Ident(s string) string {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '.':
		default:
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
}

func sequenceD2Label(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '.':
		default:
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
}
