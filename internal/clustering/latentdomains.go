package clustering

import (
	"errors"
	"fmt"
	"sort"

	archmotifAdapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

// LatentDomains clusters one node set twice — structurally (dependency edges)
// and semantically (embedding similarity) — and measures how much the two
// partitions agree. When semantics splits into balanced domains while structure
// collapses into a single blob, the code holds real domains fused by a
// cross-cutting concern, and the lens names the glue: the symbols every domain
// depends on.
//
// It is the analysis behind the MCP `latent_domains` tool and the review UI's
// domains canvas. Both call this; neither owns a copy of the maths.

// Input is everything the analysis needs. Vectors is a port so the caller
// supplies the embeddings rather than this package reaching for a service.
type Input struct {
	// Packages is the worktree's parsed model.
	Packages []domain.PackageModel
	// Overlay is the project's archai.yaml, or nil.
	Overlay *overlay.Config
	// Base is the review base's model, required only by a diff selector.
	Base []domain.PackageModel
	// Vectors supplies the embedding of each selected symbol.
	Vectors Vectors
	// Selector says which nodes to analyse.
	Selector Selector
	// K is the cluster count; 0 chooses it from the spectrum.
	K int
	// KNN is the neighbour count of the semantic similarity graph; 0 means 8.
	KNN int
}

// Partition is one side's clustering, encoded as a label per node rather than
// as a list of members per cluster: the node ids are shared by both sides and
// are carried once, in Result.Nodes, with Labels indexed by that same position.
// Repeating every id per side is how a partition of a real repository grows to
// hundreds of kilobytes.
type Partition struct {
	K             int     `json:"k"`
	ClusterCount  int     `json:"cluster_count"`
	DominantShare float64 `json:"dominant_share"` // largest cluster / total — high means degenerate
	Modularity    float64 `json:"modularity"`     // Newman Q — low on the structural side is a hairball
	// Labels[i] is the cluster of Result.Nodes[i], or -1 when this side did
	// not place that node.
	Labels []int `json:"labels"`
}

// GlueNode is one symbol holding the structural blob together.
type GlueNode struct {
	Node            string `json:"node"`
	FanIn           int    `json:"fan_in"` // incoming flow edges from within the analysed set
	SemanticCluster int    `json:"semantic_cluster"`
}

// Agreement is how much the two partitions say the same thing.
type Agreement struct {
	AMI     float64 `json:"ami"`     // adjusted mutual information — corrected for chance/K, drives the verdict
	NMI     float64 `json:"nmi"`     // raw normalized mutual information, for reference (inflates with K)
	Verdict string  `json:"verdict"` // aligned | diverging | latent_domains_glued
}

// Glue names what fuses the structural clusters together.
type Glue struct {
	TopFanIn    []GlueNode `json:"top_fan_in"`
	GlueCluster int        `json:"glue_cluster"` // semantic cluster concentrating the most fan-in (-1 if none)
	Note        string     `json:"note"`
}

// Result is the analysis, and the payload the browser endpoint serves verbatim.
type Result struct {
	// Nodes are the analysed symbols, as archmotif node ids, in the order both
	// Partition.Labels arrays are indexed by.
	Nodes        []string    `json:"nodes"`
	NodeCount    int         `json:"node_count"`
	Structural   Partition   `json:"structural"`
	Semantic     Partition   `json:"semantic"`
	Agreement    Agreement   `json:"agreement"`
	Glue         Glue        `json:"glue"`
	DroppedNodes int         `json:"dropped_nodes"`         // selected nodes with no embedding
	DiffRegion   *DiffRegion `json:"diff_region,omitempty"` // present when the diff selector scoped the analysis
}

// Verdicts.
const (
	VerdictAligned = "aligned"
	VerdictGlued   = "latent_domains_glued"
	VerdictDiverge = "diverging"
)

// defaultKNN is the neighbour count of the semantic similarity graph.
const defaultKNN = 8

// minComparableNodes is the smallest node set two partitions can be compared
// over. Below it the agreement figure says nothing.
const minComparableNodes = 4

// glueLimit caps the named glue nodes. The finding is which few symbols every
// domain depends on, so a longer list would not be a longer finding.
const glueLimit = 10

// LatentDomains runs the analysis. Every error it returns carries a message a
// caller can show verbatim: an empty selection or a missing review base are
// expected outcomes of a question, not faults.
func LatentDomains(in Input) (Result, error) {
	if len(in.Packages) == 0 {
		return Result{}, errors.New("no packages loaded")
	}
	if in.Vectors == nil {
		return Result{}, errors.New("vector index not available — embedder may not be configured or refresh needed")
	}

	graph, err := archmotifAdapter.ToArchmotifGraph(in.Packages, in.Overlay)
	if err != nil {
		return Result{}, fmt.Errorf("building graph: %w", err)
	}

	var selected []string
	var diffMeta *DiffRegion
	if in.Selector.Diff {
		if len(in.Base) == 0 {
			return Result{}, errors.New("diff selector needs a review base, but none is configured for this daemon (set serve.base_branch, or run as a repo-level daemon that loads the base branch)")
		}
		selected, diffMeta, err = DiffRegionNodes(graph, in.Base, in.Packages)
		if err != nil {
			return Result{}, err
		}
	} else {
		selected = SelectNodes(graph, in.Packages, in.Selector)
	}
	if len(selected) == 0 {
		return Result{}, errors.New("no nodes match the selector")
	}

	// Both clusterings must run on the identical node set, so a symbol without
	// an embedding leaves the analysis rather than the semantic side alone.
	nodes, dropped := SemanticNodes(selected, in.Vectors)
	if len(nodes) < minComparableNodes {
		return Result{}, fmt.Errorf("only %d nodes have embeddings (need at least %d to compare partitions); %d dropped",
			len(nodes), minComparableNodes, dropped)
	}

	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ArchmotifID
	}

	knn := in.KNN
	if knn < 1 {
		knn = defaultKNN
	}

	// Semantic side: a kNN graph over embedding similarity.
	semanticGraph, _, err := SemanticGraph(nodes, knn, 0.0)
	if err != nil {
		return Result{}, fmt.Errorf("building semantic graph: %w", err)
	}
	semOpts := spectralcluster.DefaultOptions()
	semOpts.K = in.K
	semOpts.NodeIDs = ids
	semOpts.EdgeKinds = []string{"references"}
	semResult, err := spectralcluster.SpectralCluster(semanticGraph, semOpts)
	if err != nil {
		return Result{}, fmt.Errorf("semantic clustering failed: %w", err)
	}

	// Structural side: same node set, dependency-flow edges, mirroring the K
	// the semantic side settled on so the two partitions are comparable.
	structOpts := spectralcluster.DefaultOptions()
	structOpts.K = semResult.ChosenK
	structOpts.NodeIDs = ids
	structOpts.EdgeKinds = FlowEdgeKinds
	structResult, err := spectralcluster.SpectralCluster(graph, structOpts)
	if err != nil {
		return Result{}, fmt.Errorf("structural clustering failed: %w", err)
	}

	semLabelOf := labelMap(semResult.Clusters)
	structLabelOf := labelMap(structResult.Clusters)
	semLabels := labelVector(ids, semLabelOf)
	structLabels := labelVector(ids, structLabelOf)

	// The agreement is measured over the nodes both sides placed.
	var sa, sb []int
	for i := range ids {
		if structLabels[i] >= 0 && semLabels[i] >= 0 {
			sa = append(sa, structLabels[i])
			sb = append(sb, semLabels[i])
		}
	}
	nmi := normalizedMutualInfo(sa, sb)
	ami := adjustedMutualInfo(sa, sb)

	fanIn := structuralFanIn(graph, ids)
	topFanIn, glueCluster := rankGlue(fanIn, semLabelOf)

	structShare := dominantShare(structResult.Clusters)
	semShare := dominantShare(semResult.Clusters)
	verdict, note := latentVerdict(ami, structShare, semShare, semResult.ChosenK)

	return Result{
		Nodes:     ids,
		NodeCount: len(ids),
		Structural: Partition{
			K:             structResult.ChosenK,
			ClusterCount:  len(structResult.Clusters),
			DominantShare: roundTo(structShare, 3),
			Modularity:    structResult.Modularity,
			Labels:        structLabels,
		},
		Semantic: Partition{
			K:             semResult.ChosenK,
			ClusterCount:  len(semResult.Clusters),
			DominantShare: roundTo(semShare, 3),
			Modularity:    semResult.Modularity,
			Labels:        semLabels,
		},
		Agreement:    Agreement{AMI: roundTo(ami, 3), NMI: roundTo(nmi, 3), Verdict: verdict},
		Glue:         Glue{TopFanIn: topFanIn, GlueCluster: glueCluster, Note: note},
		DroppedNodes: dropped,
		DiffRegion:   diffMeta,
	}, nil
}

// Members returns the node ids a side placed in cluster id, in Result.Nodes
// order. It is how a caller that wants membership recovers it from the label
// encoding. A negative cluster is the unplaced marker, not a cluster, and has
// no members.
func (r Result) Members(p Partition, cluster int) []string {
	if cluster < 0 {
		return nil
	}
	var out []string
	for i, label := range p.Labels {
		if label == cluster && i < len(r.Nodes) {
			out = append(out, r.Nodes[i])
		}
	}
	return out
}

// ClusterIDs returns the clusters a side actually placed nodes in, ascending.
func (p Partition) ClusterIDs() []int {
	seen := map[int]bool{}
	var out []int
	for _, label := range p.Labels {
		if label < 0 || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	sort.Ints(out)
	return out
}

// structuralFanIn counts, for each analysed node, the flow edges arriving from
// another analysed node. A symbol every domain calls is what holds a structural
// blob together, and fan-in is what that looks like in the graph.
func structuralFanIn(graph *spectralcluster.Graph, ids []string) map[string]int {
	inSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}
	flow := make(map[string]bool, len(FlowEdgeKinds))
	for _, kind := range FlowEdgeKinds {
		flow[kind] = true
	}
	fanIn := make(map[string]int, len(ids))
	for _, e := range graph.Edges() {
		if !flow[string(e.Kind)] || !inSet[e.From] || !inSet[e.To] {
			continue
		}
		fanIn[e.To]++
	}
	return fanIn
}

// rankGlue names the top fan-in nodes and the semantic cluster carrying the
// most fan-in mass — the domain the glue is drawn from.
func rankGlue(fanIn map[string]int, semLabelOf map[string]int) ([]GlueNode, int) {
	ranked := make([]string, 0, len(fanIn))
	for id := range fanIn {
		ranked = append(ranked, id)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if fanIn[ranked[i]] != fanIn[ranked[j]] {
			return fanIn[ranked[i]] > fanIn[ranked[j]]
		}
		return ranked[i] < ranked[j]
	})

	top := make([]GlueNode, 0, glueLimit)
	for _, id := range ranked {
		if len(top) >= glueLimit {
			break
		}
		top = append(top, GlueNode{Node: id, FanIn: fanIn[id], SemanticCluster: semLabelOf[id]})
	}

	clusterFanIn := map[int]int{}
	for id, f := range fanIn {
		if label, ok := semLabelOf[id]; ok {
			clusterFanIn[label] += f
		}
	}
	glueCluster := -1
	bestMass := -1
	for label, mass := range clusterFanIn {
		if mass > bestMass || (mass == bestMass && label < glueCluster) {
			bestMass = mass
			glueCluster = label
		}
	}
	return top, glueCluster
}

// latentVerdict classifies the structural/semantic divergence. It is driven by
// AMI (adjusted for chance/K, so it does not drift as K grows) plus the
// ABSOLUTE structural degeneracy — a dominant structural cluster swallowing
// >=45% of the nodes is a blob regardless of K, whereas the raw share gap
// shrinks mechanically as K rises.
func latentVerdict(ami, structShare, semShare float64, semK int) (verdict, note string) {
	switch {
	case ami >= 0.5:
		return VerdictAligned, "Structural and semantic decompositions agree — module boundaries match what the code is about."
	case structShare >= 0.45 && structShare > semShare && semK >= 2:
		return VerdictGlued, fmt.Sprintf(
			"Semantics splits into %d balanced domains (largest %.0f%%) but structure collapses into one blob (largest %.0f%%): real domains fused by a cross-cutting concern. The top fan-in nodes are the glue — pull them to a thin boundary and the domains separate.",
			semK, semShare*100, structShare*100)
	default:
		return VerdictDiverge, "Structural and semantic decompositions disagree, but no single dominant glue blob — boundaries are fuzzy rather than fused."
	}
}

// labelMap assigns each member node its cluster label.
func labelMap(clusters []spectralcluster.Cluster) map[string]int {
	out := map[string]int{}
	for _, c := range clusters {
		for _, m := range c.Members {
			out[m] = c.ID
		}
	}
	return out
}

// labelVector turns the per-node lookup into an array indexed by node position,
// with -1 for a node the clustering did not place.
func labelVector(ids []string, labelOf map[string]int) []int {
	out := make([]int, len(ids))
	for i, id := range ids {
		if label, ok := labelOf[id]; ok {
			out[i] = label
			continue
		}
		out[i] = -1
	}
	return out
}

// dominantShare returns the largest cluster as a fraction of all clustered nodes.
func dominantShare(clusters []spectralcluster.Cluster) float64 {
	total, largest := 0, 0
	for _, c := range clusters {
		n := len(c.Members)
		total += n
		if n > largest {
			largest = n
		}
	}
	if total == 0 {
		return 0
	}
	return float64(largest) / float64(total)
}
