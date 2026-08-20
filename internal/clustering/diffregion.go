package clustering

import (
	"fmt"

	archmotifAdapter "github.com/kgatilin/archai/internal/adapter/archmotif"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archmotif/pkg/localpartition"
	"github.com/kgatilin/archmotif/pkg/spectralcluster"
)

// FlowEdgeKinds are the behavioral dependency edges the analyses run on — the
// same projection trophic_layers uses. The structural `contains`/file edges are
// excluded: they describe where a symbol is written, not what depends on it.
var FlowEdgeKinds = []string{"calls", "usesType", "returns", "implements"}

// DiffRegion describes how a diff-scoped selection was bounded: how many seed
// nodes the change produced, how large the local region ACL grew around them,
// and how crisp that region's boundary is (conductance — lower is more
// self-contained).
type DiffRegion struct {
	SeedCount   int     `json:"seed_count"`
	RegionSize  int     `json:"region_size"`
	Conductance float64 `json:"conductance"`
}

// DiffRegionNodes selects the archmotif node ids in the local region the
// worktree's diff (vs the review base) pulls on. It diffs base↔worktree into a
// seed set (changed nodes ∪ changed-edge endpoints), then runs ACL local
// partitioning over the flow-edge projection to grow the region around the
// seed. Package and file containers are dropped, matching SelectNodes.
//
// base must be the review-base models; graph and worktree describe the active
// snapshot. An empty selection is returned as an error carrying a message a
// caller can show verbatim — "nothing changed" is an expected outcome, not a
// fault.
func DiffRegionNodes(graph *spectralcluster.Graph, base, worktree []domain.PackageModel) ([]string, *DiffRegion, error) {
	seeds := archmotifAdapter.SeedIDsFromDiff(base, worktree)
	if len(seeds) == 0 {
		return nil, nil, fmt.Errorf("no structural changes between the review base and this worktree — nothing to scope the analysis to")
	}

	opts := localpartition.DefaultOptions()
	opts.EdgeKinds = FlowEdgeKinds
	res, err := localpartition.LocalPartition(graph, seeds, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("local partition: %w", err)
	}

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
		return nil, nil, fmt.Errorf("the change seeds %d node(s) but they expand to no analyzable symbol region (isolated or container-only changes)", len(seeds))
	}

	return region, &DiffRegion{
		SeedCount:   res.SeedCount,
		RegionSize:  len(region),
		Conductance: roundTo(res.Conductance, 3),
	}, nil
}
