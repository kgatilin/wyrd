package mcp

import (
	"context"
	"fmt"

	archmotifAdapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archmotif/pkg/localpartition"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

// diffRegionMeta describes the diff-scoped node selection so the caller knows
// how the analysis was bounded: how many seed nodes the change produced, how
// large the local region ACL grew around them, and how crisp that region's
// boundary is (conductance — lower means a more self-contained region).
type diffRegionMeta struct {
	SeedCount   int     `json:"seed_count"`
	RegionSize  int     `json:"region_size"`
	Conductance float64 `json:"conductance"`
}

// diffRegionNodes selects the archmotif node ids in the local region that the
// worktree's diff (vs the review base) pulls on. It diffs base↔worktree into a
// seed set (changed nodes ∪ changed-edge endpoints), then runs ACL local
// partitioning over the flow-edge projection to grow the region the seed sits
// in. Package/file container nodes are dropped, matching selectNodes.
//
// The third return is a non-empty human-readable message when the selection
// cannot be made (no base configured, no changes, empty region). Callers wrap
// it in errorResult — these are expected outcomes, not RPC faults.
func diffRegionNodes(ctx context.Context, state *serve.State, graph *spectralcluster.Graph, worktree []domain.PackageModel) ([]string, *diffRegionMeta, string) {
	base, err := state.BaseModels(ctx)
	if err != nil {
		return nil, nil, fmt.Sprintf("loading review base: %v", err)
	}
	if base == nil {
		return nil, nil, "diff selector needs a review base, but none is configured for this daemon (set serve.base_branch, or run as a repo-level daemon that loads the base branch)"
	}

	seeds := archmotifAdapter.SeedIDsFromDiff(base, worktree)
	if len(seeds) == 0 {
		return nil, nil, "no structural changes between the review base and this worktree — nothing to scope the analysis to"
	}

	opts := localpartition.DefaultOptions()
	opts.EdgeKinds = flowEdgeKinds
	res, err := localpartition.LocalPartition(graph, seeds, opts)
	if err != nil {
		return nil, nil, fmt.Sprintf("local partition: %v", err)
	}

	// Drop package/file container nodes — the lenses cluster symbols, not the
	// structural layout (same default as selectNodes).
	region := make([]string, 0, len(res.Region))
	for _, id := range res.Region {
		n, ok := graph.Node(id)
		if !ok {
			continue
		}
		if n.Kind == "package" || n.Kind == "file" {
			continue
		}
		region = append(region, id)
	}
	if len(region) == 0 {
		return nil, nil, fmt.Sprintf("the change seeds %d node(s) but they expand to no analyzable symbol region (isolated or container-only changes)", len(seeds))
	}

	meta := &diffRegionMeta{
		SeedCount:   res.SeedCount,
		RegionSize:  len(region),
		Conductance: roundTo(res.Conductance, 3),
	}
	return region, meta, ""
}
