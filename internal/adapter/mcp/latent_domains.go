package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	archmotifAdapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

// latent_domains compares the STRUCTURAL clustering (dependency edges) with the
// SEMANTIC clustering (embedding similarity) of the same node set. When the two
// disagree sharply — structure collapses into one glued blob while semantics
// splits into balanced domains — the package holds latent domains fused by a
// cross-cutting concern (shared helpers, a god-dispatcher). The lens names the
// glue: the high structural fan-in nodes that every domain depends on.
//
// This is the lens that surfaces, on its own, what otherwise needs a human to
// notice by eyeballing spectral_cluster and semantic_cluster side by side.

type latentDomainsArgs struct {
	Selector spectralSelector `json:"selector"`
	K        any              `json:"k"`   // "auto" or integer; applied to the semantic side, mirrored on the structural side
	KNN      int              `json:"knn"` // k nearest neighbors for the semantic similarity graph
}

type latentDomainsPartition struct {
	K             int                   `json:"k"`
	ClusterCount  int                   `json:"cluster_count"`
	DominantShare float64               `json:"dominant_share"` // largest cluster / total — high means degenerate
	Clusters      []spectralClusterInfo `json:"clusters"`
}

type glueNode struct {
	Node            string `json:"node"`
	FanIn           int    `json:"fan_in"` // incoming flow edges from within the analyzed set
	SemanticCluster int    `json:"semantic_cluster"`
}

type latentDomainsAgreement struct {
	NMI     float64 `json:"nmi"`     // normalized mutual information of the two partitions, [0,1]
	Verdict string  `json:"verdict"` // aligned | diverging | latent_domains_glued
}

type latentDomainsGlue struct {
	TopFanIn    []glueNode `json:"top_fan_in"`
	GlueCluster int        `json:"glue_cluster"` // semantic cluster concentrating the most fan-in (-1 if none)
	Note        string     `json:"note"`
}

type latentDomainsResponse struct {
	NodeCount    int                    `json:"node_count"`
	Structural   latentDomainsPartition `json:"structural"`
	Semantic     latentDomainsPartition `json:"semantic"`
	Agreement    latentDomainsAgreement `json:"agreement"`
	Glue         latentDomainsGlue      `json:"glue"`
	DroppedNodes int                    `json:"dropped_nodes"` // selected nodes without embeddings
}

// flowEdgeKinds are the behavioral dependency edges used for the structural
// clustering and the fan-in (glue) measure — the same projection trophic_layers
// runs on. structural contains/file edges are excluded.
var flowEdgeKinds = []string{"calls", "usesType", "returns", "implements"}

// handleLatentDomains runs structural and semantic clustering over the same
// node set, measures their agreement, and identifies the glue.
func handleLatentDomains(state *serve.State, rawArgs json.RawMessage) (ToolResult, *RPCError) {
	var args latentDomainsArgs
	if rpcErr := unmarshalArgs(rawArgs, &args); rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	if state == nil {
		return errorResult("no state available"), nil
	}

	svc := state.Retrieval()
	if svc == nil {
		return errorResult("retrieval not initialized — call refresh first"), nil
	}
	vidx := svc.VectorIndexWithLookup()
	if vidx == nil {
		return errorResult("vector index not available — embedder may not be configured or refresh needed"), nil
	}

	snap := state.Snapshot()
	if len(snap.Packages) == 0 {
		return errorResult("no packages loaded"), nil
	}

	graph, err := archmotifAdapter.ToArchmotifGraph(snap.Packages, snap.Overlay)
	if err != nil {
		return errorResult(fmt.Sprintf("building graph: %v", err)), nil
	}

	archmotifNodeIDs := selectNodes(graph, snap.Packages, args.Selector)
	if len(archmotifNodeIDs) == 0 {
		return errorResult("no nodes match the selector"), nil
	}

	// Keep only nodes that carry an embedding so both clusterings run on the
	// identical node set (the comparison is meaningless otherwise).
	var nodesWithVectors []semanticNode
	droppedCount := 0
	for _, amid := range archmotifNodeIDs {
		rid := archmotifIDToRetrievalID(amid)
		if rid == "" {
			droppedCount++
			continue
		}
		vec, ok := vidx.Vector(rid)
		if !ok || len(vec) == 0 {
			droppedCount++
			continue
		}
		nodesWithVectors = append(nodesWithVectors, semanticNode{archmotifID: amid, retrievalID: rid, vec: vec})
	}
	if len(nodesWithVectors) < 4 {
		return errorResult(fmt.Sprintf("only %d nodes have embeddings (need at least 4 to compare partitions); %d dropped",
			len(nodesWithVectors), droppedCount)), nil
	}

	commonIDs := make([]string, len(nodesWithVectors))
	for i, nv := range nodesWithVectors {
		commonIDs[i] = nv.archmotifID
	}

	k, rpcErr := parseClusterK(args.K)
	if rpcErr != nil {
		return ToolResult{}, rpcErr
	}
	knn := args.KNN
	if knn < 1 {
		knn = 8
	}

	// Semantic side: kNN graph over embedding similarity.
	semanticGraph, _, err := buildSemanticKNNGraph(nodesWithVectors, knn, 0.0)
	if err != nil {
		return errorResult(fmt.Sprintf("building semantic graph: %v", err)), nil
	}
	semOpts := spectralcluster.DefaultOptions()
	semOpts.K = k
	semOpts.NodeIDs = commonIDs
	semOpts.EdgeKinds = []string{"references"}
	semResult, err := spectralcluster.SpectralCluster(semanticGraph, semOpts)
	if err != nil {
		return errorResult(fmt.Sprintf("semantic clustering failed: %v", err)), nil
	}

	// Structural side: same node set, dependency-flow edges, mirror the k the
	// semantic side settled on so the two partitions are comparable.
	structOpts := spectralcluster.DefaultOptions()
	structOpts.K = semResult.ChosenK
	structOpts.NodeIDs = commonIDs
	structOpts.EdgeKinds = flowEdgeKinds
	structResult, err := spectralcluster.SpectralCluster(graph, structOpts)
	if err != nil {
		return errorResult(fmt.Sprintf("structural clustering failed: %v", err)), nil
	}

	semLabelOf := labelMap(semResult.Clusters)
	structLabelOf := labelMap(structResult.Clusters)

	// Aligned label vectors over nodes present in both partitions.
	var sa, sb []int
	for _, id := range commonIDs {
		la, oka := structLabelOf[id]
		lb, okb := semLabelOf[id]
		if oka && okb {
			sa = append(sa, la)
			sb = append(sb, lb)
		}
	}
	nmi := normalizedMutualInfo(sa, sb)

	// Glue: structural fan-in (incoming flow edges from within the set).
	inSet := make(map[string]bool, len(commonIDs))
	for _, id := range commonIDs {
		inSet[id] = true
	}
	flow := make(map[string]bool, len(flowEdgeKinds))
	for _, kk := range flowEdgeKinds {
		flow[kk] = true
	}
	fanIn := make(map[string]int, len(commonIDs))
	for _, e := range graph.Edges() {
		if !flow[string(e.Kind)] || !inSet[e.From] || !inSet[e.To] {
			continue
		}
		fanIn[e.To]++
	}

	// Top glue nodes by fan-in.
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
	const glueLimit = 10
	topFanIn := make([]glueNode, 0, glueLimit)
	for _, id := range ranked {
		if len(topFanIn) >= glueLimit {
			break
		}
		topFanIn = append(topFanIn, glueNode{Node: id, FanIn: fanIn[id], SemanticCluster: semLabelOf[id]})
	}

	// The semantic cluster carrying the most fan-in mass is the glue domain.
	clusterFanIn := map[int]int{}
	for id, f := range fanIn {
		if lbl, ok := semLabelOf[id]; ok {
			clusterFanIn[lbl] += f
		}
	}
	glueCluster := -1
	bestMass := -1
	for lbl, mass := range clusterFanIn {
		if mass > bestMass || (mass == bestMass && lbl < glueCluster) {
			bestMass = mass
			glueCluster = lbl
		}
	}

	structShare := dominantShare(structResult.Clusters)
	semShare := dominantShare(semResult.Clusters)

	verdict, note := latentVerdict(nmi, structShare, semShare, semResult.ChosenK)

	resp := latentDomainsResponse{
		NodeCount: len(commonIDs),
		Structural: latentDomainsPartition{
			K:             structResult.ChosenK,
			ClusterCount:  len(structResult.Clusters),
			DominantShare: roundTo(structShare, 3),
			Clusters:      buildClusterInfos(structResult.Clusters),
		},
		Semantic: latentDomainsPartition{
			K:             semResult.ChosenK,
			ClusterCount:  len(semResult.Clusters),
			DominantShare: roundTo(semShare, 3),
			Clusters:      buildClusterInfos(semResult.Clusters),
		},
		Agreement: latentDomainsAgreement{NMI: roundTo(nmi, 3), Verdict: verdict},
		Glue: latentDomainsGlue{
			TopFanIn:    topFanIn,
			GlueCluster: glueCluster,
			Note:        note,
		},
		DroppedNodes: droppedCount,
	}
	return textResult(resp)
}

// latentVerdict classifies the structural/semantic divergence.
func latentVerdict(nmi, structShare, semShare float64, semK int) (verdict, note string) {
	switch {
	case nmi >= 0.5:
		return "aligned", "Structural and semantic decompositions agree — module boundaries match what the code is about."
	case structShare-semShare >= 0.15 && semK >= 2:
		return "latent_domains_glued", fmt.Sprintf(
			"Semantics splits into %d balanced domains (largest %.0f%%) but structure collapses into one blob (largest %.0f%%): real domains fused by a cross-cutting concern. The top fan-in nodes are the glue — pull them to a thin boundary and the domains separate.",
			semK, semShare*100, structShare*100)
	default:
		return "diverging", "Structural and semantic decompositions disagree, but no single dominant glue blob — boundaries are fuzzy rather than fused."
	}
}

// labelMap assigns each member node a cluster label.
func labelMap(clusters []spectralcluster.Cluster) map[string]int {
	out := map[string]int{}
	for _, c := range clusters {
		for _, m := range c.Members {
			out[m] = c.ID
		}
	}
	return out
}

// dominantShare returns the largest cluster as a fraction of all clustered nodes.
func dominantShare(clusters []spectralcluster.Cluster) float64 {
	total, max := 0, 0
	for _, c := range clusters {
		n := len(c.Members)
		total += n
		if n > max {
			max = n
		}
	}
	if total == 0 {
		return 0
	}
	return float64(max) / float64(total)
}

// parseClusterK reads the shared "auto"|int K argument; 0 means auto.
func parseClusterK(v any) (int, *RPCError) {
	if v == nil {
		return 0, nil
	}
	switch t := v.(type) {
	case string:
		if t != "auto" {
			return 0, &RPCError{Code: ErrInvalidParams, Message: fmt.Sprintf("invalid k value: %q (use \"auto\" or an integer)", t)}
		}
		return 0, nil
	case float64:
		if int(t) < 1 {
			return 0, &RPCError{Code: ErrInvalidParams, Message: "k must be >= 1"}
		}
		return int(t), nil
	case int:
		if t < 1 {
			return 0, &RPCError{Code: ErrInvalidParams, Message: "k must be >= 1"}
		}
		return t, nil
	default:
		return 0, &RPCError{Code: ErrInvalidParams, Message: fmt.Sprintf("invalid k type: %T", v)}
	}
}

// normalizedMutualInfo computes NMI of two label vectors (same index = same
// node), in [0,1]. 1 = identical partitions; ~0 = independent. Both empty or
// single-cluster on both sides counts as full agreement.
func normalizedMutualInfo(a, b []int) float64 {
	n := len(a)
	if n == 0 || len(b) != n {
		return 0
	}
	countA := map[int]float64{}
	countB := map[int]float64{}
	countAB := map[[2]int]float64{}
	for i := 0; i < n; i++ {
		countA[a[i]]++
		countB[b[i]]++
		countAB[[2]int{a[i], b[i]}]++
	}
	N := float64(n)

	mi := 0.0
	for pair, nab := range countAB {
		pab := nab / N
		pa := countA[pair[0]] / N
		pb := countB[pair[1]] / N
		mi += pab * math.Log(pab/(pa*pb))
	}

	ha := entropy(countA, N)
	hb := entropy(countB, N)
	if ha == 0 && hb == 0 {
		return 1 // both are a single cluster — trivially identical
	}
	if ha == 0 || hb == 0 {
		return 0 // one side is a single cluster, the other is not
	}
	nmi := mi / math.Sqrt(ha*hb)
	if nmi < 0 {
		nmi = 0
	}
	if nmi > 1 {
		nmi = 1
	}
	return nmi
}

func entropy(counts map[int]float64, N float64) float64 {
	h := 0.0
	for _, c := range counts {
		p := c / N
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return h
}

func roundTo(v float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(v*pow) / pow
}
