package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kgatilin/archai/internal/clustering"
	"github.com/kgatilin/archai/internal/serve"
)

// The latent_domains tool. The analysis lives in internal/clustering, which the
// review UI's domains endpoint calls too; this file is the MCP surface of it —
// argument decoding, readiness gating, and the sampled text verdict an agent
// reads.

type latentDomainsArgs struct {
	Selector spectralSelector `json:"selector"`
	K        any              `json:"k"`   // "auto" or integer; applied to the semantic side, mirrored on the structural side
	KNN      int              `json:"knn"` // k nearest neighbors for the semantic similarity graph
}

type latentDomainsPartition struct {
	K             int                   `json:"k"`
	ClusterCount  int                   `json:"cluster_count"`
	DominantShare float64               `json:"dominant_share"` // largest cluster / total — high means degenerate
	Modularity    float64               `json:"modularity"`     // Newman Q — low on the structural side = hairball
	Clusters      []spectralClusterInfo `json:"clusters"`
}

type latentDomainsResponse struct {
	NodeCount    int                    `json:"node_count"`
	Structural   latentDomainsPartition `json:"structural"`
	Semantic     latentDomainsPartition `json:"semantic"`
	Agreement    clustering.Agreement   `json:"agreement"`
	Glue         clustering.Glue        `json:"glue"`
	DroppedNodes int                    `json:"dropped_nodes"`         // selected nodes without embeddings
	DiffRegion   *clustering.DiffRegion `json:"diff_region,omitempty"` // present when selector.diff scoped the analysis
}

// handleLatentDomains runs the analysis and renders its verdict.
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
	if tr, gated := indexingGate(svc); gated {
		return tr, nil
	}
	vidx := svc.VectorIndexWithLookup()
	if vidx == nil {
		return errorResult("vector index not available — embedder may not be configured or refresh needed"), nil
	}

	k, rpcErr := parseClusterK(args.K)
	if rpcErr != nil {
		return ToolResult{}, rpcErr
	}

	snap := state.Snapshot()
	in := clustering.Input{
		Packages: snap.Packages,
		Overlay:  snap.Overlay,
		Vectors:  vidx,
		Selector: args.Selector.domain(),
		K:        k,
		KNN:      args.KNN,
	}
	// The base is only loaded for a diff-scoped question; loading it otherwise
	// would make every call pay for a checkout nothing reads.
	if in.Selector.Diff {
		base, err := state.BaseModels(context.Background())
		if err != nil {
			return errorResult(fmt.Sprintf("loading review base: %v", err)), nil
		}
		in.Base = base
	}

	res, err := clustering.LatentDomains(in)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	return text(renderLatentDomains(latentDomainsResponse{
		NodeCount: res.NodeCount,
		Structural: latentDomainsPartition{
			K:             res.Structural.K,
			ClusterCount:  res.Structural.ClusterCount,
			DominantShare: res.Structural.DominantShare,
			Modularity:    res.Structural.Modularity,
			Clusters:      clusterSummaries(res, res.Structural),
		},
		Semantic: latentDomainsPartition{
			K:             res.Semantic.K,
			ClusterCount:  res.Semantic.ClusterCount,
			DominantShare: res.Semantic.DominantShare,
			Modularity:    res.Semantic.Modularity,
			Clusters:      clusterSummaries(res, res.Semantic),
		},
		Agreement:    res.Agreement,
		Glue:         res.Glue,
		DroppedNodes: res.DroppedNodes,
		DiffRegion:   res.DiffRegion,
	}))
}

// clusterSummaries is the latent_domains variant of buildClusterInfos. This
// lens emits clusters on BOTH the structural and semantic sides, so the full
// member dump (up to clusterMembersFullLimit per cluster, the spectral_cluster
// default) compounds 2×K and blows the response past the result budget on a
// large region. latent_domains is a verdict lens — its product is the AMI
// verdict + glue + modularity, not membership — so it ALWAYS samples. A caller
// who needs full membership uses spectral_cluster / semantic_cluster; a client
// that needs BOTH partitions in full reads GET /api/archmotif/domains, which
// carries the partition as label arrays and is not squeezed through an agent's
// context budget.
func clusterSummaries(res clustering.Result, p clustering.Partition) []spectralClusterInfo {
	ids := p.ClusterIDs()
	out := make([]spectralClusterInfo, 0, len(ids))
	for _, id := range ids {
		members := res.Members(p, id)
		info := spectralClusterInfo{ID: id, Size: len(members)}
		if len(members) <= clusterMembersSample {
			info.Members = members
		} else {
			info.MembersSample = sampleStrings(members, clusterMembersSample)
			info.Truncated = true
		}
		out = append(out, info)
	}
	return out
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
