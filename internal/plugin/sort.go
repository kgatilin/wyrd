package plugin

import "sort"

func sortLayers(xs []*Layer) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
}

func sortLayerRules(xs []*LayerRule) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Layer < xs[j].Layer })
}

func sortAggregates(xs []*Aggregate) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
}

func sortBoundedContexts(xs []*BoundedContext) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
}
