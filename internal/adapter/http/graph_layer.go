package http

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// buildLayerGraph produces the cytoscape payload for the Layers page.
// It places every layer as a compound parent node and each package as a
// child. Cross-layer edges come straight from buildLayerEdges with the
// colour/kind mapping the Layers CSS uses.
//
// The mini=true variant drops package children and violations so the
// dashboard preview stays readable.
func buildLayerGraph(cfg *overlay.Config, packages []domain.PackageModel, mini bool) graphPayload {
	out := graphPayload{
		Meta: graphMeta{
			View:   "layer-map",
			Layout: "elk",
			Title:  "Layer map",
		},
	}
	if mini {
		out.Meta.View = "layer-map-mini"
	}
	if cfg == nil || len(cfg.Layers) == 0 {
		return out
	}

	pkgLayer := computePackageLayers(cfg, packages)
	pkgCount := make(map[string]int)
	for _, p := range packages {
		if l := pkgLayer[p.Path]; l != "" {
			pkgCount[l]++
		}
	}
	layerNames := sortedLayerNames(cfg)

	for _, name := range layerNames {
		label := name
		if !mini {
			label = fmt.Sprintf("%s (%d)", name, pkgCount[name])
		}
		out.Nodes = append(out.Nodes, graphNode{
			ID:    "layer:" + name,
			Label: label,
			Kind:  "layer",
		})
	}

	if !mini {
		// Package children, parented by their layer.
		for _, p := range packages {
			l := pkgLayer[p.Path]
			if l == "" {
				continue
			}
			out.Nodes = append(out.Nodes, graphNode{
				ID:     "pkg:" + p.Path,
				Label:  p.Path,
				Kind:   "package",
				Parent: "layer:" + l,
			})
		}
		// Deterministic child ordering.
		sort.Slice(out.Nodes, func(i, j int) bool {
			if out.Nodes[i].Kind != out.Nodes[j].Kind {
				// layers first, then packages — stable and intuitive.
				return out.Nodes[i].Kind == "layer"
			}
			return out.Nodes[i].ID < out.Nodes[j].ID
		})
	}

	violations, allowed, declared := buildLayerEdges(cfg, packages)
	addEdge := func(from, to, kind string, details []string) {
		label := ""
		if !mini {
			label = kind
		}
		out.Edges = append(out.Edges, graphEdge{
			Source:  "layer:" + from,
			Target:  "layer:" + to,
			Label:   label,
			Kind:    kind,
			Details: details,
		})
	}
	// Declared-but-unused: drawn first (gray dashed).
	if !mini {
		for _, e := range declared {
			addEdge(e.From, e.To, "declared", e.Details)
		}
	}
	for _, e := range allowed {
		addEdge(e.From, e.To, "allowed", e.Details)
	}
	if !mini {
		for _, e := range violations {
			addEdge(e.From, e.To, "violation", e.Details)
		}
	}
	return out
}

func sortedLayerNames(cfg *overlay.Config) []string {
	names := make([]string, 0, len(cfg.Layers))
	for n := range cfg.Layers {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, knownI := layerDisplayRank(names[i])
		rj, knownJ := layerDisplayRank(names[j])
		if knownI != knownJ {
			return knownI
		}
		if knownI && ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

func layerDisplayRank(name string) (int, bool) {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "_")) {
	case "domain":
		return 0, true
	case "application", "app", "service", "services", "model_ops":
		return 1, true
	case "infrastructure", "infra", "runtime", "storage":
		return 2, true
	case "adapter", "adapters", "transport", "source_adapter", "cli":
		return 3, true
	default:
		return 0, false
	}
}
