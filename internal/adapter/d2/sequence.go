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

type sequenceSymbolInfo struct {
	Exported bool
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
	symbols := buildSequenceSymbolIndex(models)
	out := make([]SequenceDiagram, 0, len(methods))
	for _, m := range methods {
		if !m.IsExported {
			continue
		}
		start := domain.SymbolRef{Package: pkgPath, Symbol: typeName + "." + m.Name}
		tree := sequence.Build(models, start, maxDepth)
		tree = buildInteractionTree(tree, symbols, false)
		if tree == nil {
			continue
		}
		out = append(out, SequenceDiagram{
			Label:    typeName + "." + m.Name,
			Start:    start,
			Tree:     tree,
			Source:   BuildSequenceSource(tree),
			HasCalls: len(tree.Children) > 0,
		})
	}
	return out
}

// BuildPackageSequenceSources builds one D2 sequence diagram per package
// entry point, matching the browser package Overview sequence list.
func BuildPackageSequenceSources(models []domain.PackageModel, pkg domain.PackageModel, opts SequenceOptions) []SequenceDiagram {
	opts = normalizeSequenceOptions(opts)
	includeUnexported := opts.Mode.includesUnexported()
	symbols := buildSequenceSymbolIndex(models)

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
		tree = buildInteractionTree(tree, symbols, includeUnexported)
		if tree == nil && !opts.IncludeRootOnly {
			continue
		}
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
				HasCalls: tree != nil && len(tree.Children) > 0,
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
			tree = buildInteractionTree(tree, symbols, includeUnexported)
			if tree == nil && !opts.IncludeRootOnly {
				continue
			}
			cands = append(cands, candidate{
				diagram: SequenceDiagram{
					Label:    st.Name + "." + m.Name,
					Start:    start,
					Tree:     tree,
					Source:   BuildSequenceSource(tree),
					HasCalls: tree != nil && len(tree.Children) > 0,
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

func buildInteractionTree(root *sequence.Node, symbols map[string]sequenceSymbolInfo, includeUnexported bool) *sequence.Node {
	if root == nil {
		return nil
	}
	out := cloneSequenceNode(root)
	for _, child := range root.Children {
		appendInteractionChild(out, root.Symbol, child, symbols, includeUnexported)
	}
	if len(out.Children) == 0 {
		return nil
	}
	return out
}

func buildSequenceSymbolIndex(models []domain.PackageModel) map[string]sequenceSymbolInfo {
	idx := make(map[string]sequenceSymbolInfo)
	for _, model := range models {
		for _, fn := range model.Functions {
			idx[sequenceSymbolKey(domain.SymbolRef{Package: model.Path, Symbol: fn.Name})] = sequenceSymbolInfo{
				Exported: fn.IsExported,
			}
		}
		for _, st := range model.Structs {
			for _, m := range st.Methods {
				idx[sequenceSymbolKey(domain.SymbolRef{Package: model.Path, Symbol: st.Name + "." + m.Name})] = sequenceSymbolInfo{
					Exported: st.IsExported && m.IsExported,
				}
			}
		}
	}
	return idx
}

func appendInteractionChild(
	parent *sequence.Node,
	caller domain.SymbolRef,
	child *sequence.Node,
	symbols map[string]sequenceSymbolInfo,
	includeUnexported bool,
) {
	if child == nil {
		return
	}
	if sequenceVisible(child.Symbol, symbols, includeUnexported) && sequenceActorName(caller) != sequenceActorName(child.Symbol) {
		cloned := cloneSequenceNode(child)
		for _, grandchild := range child.Children {
			appendInteractionChild(cloned, child.Symbol, grandchild, symbols, includeUnexported)
		}
		parent.Children = append(parent.Children, cloned)
		return
	}
	for _, grandchild := range child.Children {
		appendInteractionChild(parent, caller, grandchild, symbols, includeUnexported)
	}
}

func cloneSequenceNode(n *sequence.Node) *sequence.Node {
	if n == nil {
		return nil
	}
	return &sequence.Node{
		Symbol:     n.Symbol,
		Via:        n.Via,
		Cycle:      n.Cycle,
		DepthLimit: n.DepthLimit,
		NotFound:   n.NotFound,
	}
}

func sequenceVisible(ref domain.SymbolRef, symbols map[string]sequenceSymbolInfo, includeUnexported bool) bool {
	info, ok := symbols[sequenceSymbolKey(ref)]
	if !ok {
		return false
	}
	return includeUnexported || info.Exported
}

func sequenceSymbolKey(ref domain.SymbolRef) string {
	return ref.Package + "|" + ref.Symbol
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
		if ref.Package != "" {
			return ref.Package
		}
		return ref.Symbol
	}
	return leaf
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
	if s == "" {
		return `""`
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
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
