package archreview

import (
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/overlay"
)

// grouping resolves which group owns a package. Go forbids package import
// cycles, so a cycle can only exist one level up; the group is that level.
//
// Configured review groups win, resolved exactly as the review UI resolves
// them, so a cycle the report names is a cycle between categories the reviewer
// already sees on the canvas. Without them the fallback is the directory
// prefix at depth two — internal/adapter, internal/serve, cmd.
type grouping struct {
	cfg   *overlay.Config
	cache map[string]string
}

func newGrouping(cfg *overlay.Config) *grouping {
	return &grouping{cfg: cfg, cache: map[string]string{}}
}

func (g *grouping) of(pkg string) string {
	if got, ok := g.cache[pkg]; ok {
		return got
	}
	name := directoryGroup(pkg)
	if key, child, ok := g.cfg.ReviewGroupOf(pkg); ok {
		name = key
		if child != "" {
			name += "/" + child
		}
	}
	g.cache[pkg] = name
	return name
}

// directoryGroup is the depth-2 directory prefix: "internal/adapter" for
// internal/adapter/http, "cmd" for cmd/archai. Everything under
// internal/plugins collapses to one group, because a plugin tree is one
// architectural unit however many packages it is spread over.
func directoryGroup(path string) string {
	if path == "." || path == "" {
		return "(root)"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "(root)"
	}
	if len(parts) >= 2 && parts[0] == "internal" {
		if parts[1] == "plugins" {
			return "plugins"
		}
		return "internal/" + parts[1]
	}
	return parts[0]
}

// groupEdge is one group-to-group dependency with the package edges and
// symbol-level dependencies underneath it. Weight is what makes an edge the
// cycle's weakest, and the samples name the symbol that holds it.
type groupEdge struct {
	From     string
	To       string
	Weight   int
	PkgEdges []Edge
	Samples  []string
}

// groupCycle is a strongly-connected set of two or more groups.
type groupCycle struct {
	Groups []string
	// Internal are the group edges inside the cycle, sorted weakest first,
	// so the head of the list is where to cut.
	Internal []groupEdge
}

// pkgEdges flattens every package edge the cycle is built from — what the
// canvas highlights when the row is clicked.
func (c groupCycle) pkgEdges() []Edge {
	var out []Edge
	for _, ge := range c.Internal {
		out = append(out, ge.PkgEdges...)
	}
	return out
}

// key identifies the cycle by its membership, for comparing head to base.
func (c groupCycle) key() string { return strings.Join(c.Groups, "\x00") }

// collapse folds the package graph into a group graph, keeping the package
// edges and symbol dependencies behind each group edge.
func collapse(s *side, g *grouping) map[Edge]*groupEdge {
	out := map[Edge]*groupEdge{}
	for _, e := range s.sortedEdges() {
		from, to := g.of(e.From), g.of(e.To)
		if from == to {
			continue
		}
		key := Edge{From: from, To: to}
		ge := out[key]
		if ge == nil {
			ge = &groupEdge{From: from, To: to}
			out[key] = ge
		}
		info := s.edges[e]
		ge.Weight += info.weight
		ge.PkgEdges = append(ge.PkgEdges, e)
		for _, sample := range info.samples {
			if len(ge.Samples) < edgeSampleLimit {
				ge.Samples = append(ge.Samples, sample)
			}
		}
	}
	return out
}

// cyclesOf returns every strongly-connected component of two or more groups,
// largest first. This is the one analysis written here rather than delegated:
// archmotif is handed the symbol and package graphs, never the group graph,
// which exists only because Go's own rules push cycles up a level.
func cyclesOf(s *side, g *grouping) []groupCycle {
	edges := collapse(s, g)

	adjacency := map[string][]string{}
	nodeSet := map[string]bool{}
	for _, p := range s.pkgs {
		nodeSet[g.of(p)] = true
	}
	for key := range edges {
		adjacency[key.From] = append(adjacency[key.From], key.To)
		nodeSet[key.From] = true
		nodeSet[key.To] = true
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for n := range adjacency {
		sort.Strings(adjacency[n])
	}

	var cycles []groupCycle
	for _, component := range tarjan(nodes, adjacency) {
		if len(component) < 2 {
			continue
		}
		sort.Strings(component)
		member := make(map[string]bool, len(component))
		for _, m := range component {
			member[m] = true
		}
		cycle := groupCycle{Groups: component}
		for key, ge := range edges {
			if member[key.From] && member[key.To] {
				cycle.Internal = append(cycle.Internal, *ge)
			}
		}
		sort.Slice(cycle.Internal, func(i, j int) bool {
			if cycle.Internal[i].Weight != cycle.Internal[j].Weight {
				return cycle.Internal[i].Weight < cycle.Internal[j].Weight
			}
			if cycle.Internal[i].From != cycle.Internal[j].From {
				return cycle.Internal[i].From < cycle.Internal[j].From
			}
			return cycle.Internal[i].To < cycle.Internal[j].To
		})
		cycles = append(cycles, cycle)
	}
	sort.Slice(cycles, func(i, j int) bool {
		if len(cycles[i].Groups) != len(cycles[j].Groups) {
			return len(cycles[i].Groups) > len(cycles[j].Groups)
		}
		return cycles[i].key() < cycles[j].key()
	})
	return cycles
}

// tarjan returns the strongly-connected components of the given adjacency,
// visiting nodes in the order supplied so the output is deterministic.
func tarjan(nodes []string, adjacency map[string][]string) [][]string {
	var (
		index      int
		stack      []string
		onStack    = map[string]bool{}
		indices    = map[string]int{}
		lowlink    = map[string]int{}
		components [][]string
		visit      func(string)
	)

	visit = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adjacency[v] {
			if _, seen := indices[w]; !seen {
				visit(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indices[w] < lowlink[v] {
				lowlink[v] = indices[w]
			}
		}

		if lowlink[v] != indices[v] {
			return
		}
		var component []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			component = append(component, w)
			if w == v {
				break
			}
		}
		components = append(components, component)
	}

	for _, n := range nodes {
		if _, seen := indices[n]; !seen {
			visit(n)
		}
	}
	return components
}
