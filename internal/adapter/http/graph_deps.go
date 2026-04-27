package http

import (
	"sort"

	"github.com/kgatilin/archai/internal/domain"
)

const (
	depsModeInbound  = "inbound"
	depsModeOutbound = "outbound"
	depsModeBoth     = "both"
)

func parseDepsMode(s string) string {
	switch s {
	case depsModeInbound, depsModeOutbound, depsModeBoth:
		return s
	default:
		return depsModeBoth
	}
}

// buildPackageDepsGraph produces the cytoscape payload for the
// dedicated package dependency diagram (#89). Only project-internal
// packages appear as nodes; external dependencies are excluded from
// the graph (they are listed separately via externalDepCount /
// packageDetailData.Externals).
//
// mode controls which peer set is rendered:
//   - "outbound" — packages that the subject imports
//   - "inbound"  — packages that import the subject
//   - "both"     — both (default)
func buildPackageDepsGraph(pkg domain.PackageModel, allPkgs []domain.PackageModel, mode string) graphPayload {
	mode = parseDepsMode(mode)
	out := graphPayload{
		Meta: graphMeta{
			View:   "package-deps",
			Layout: "dagre",
			Title:  pkg.Path,
			Mode:   mode,
		},
	}

	rootID := "pkg:" + pkg.Path
	out.Nodes = append(out.Nodes, graphNode{
		ID:    rootID,
		Label: shortName(pkg.Path),
		Kind:  "package",
		Root:  true,
	})

	known := knownPackagePaths(allPkgs)
	// Sorted copy for deterministic inbound walk.
	sortedPkgs := append([]domain.PackageModel(nil), allPkgs...)
	sort.Slice(sortedPkgs, func(i, j int) bool { return sortedPkgs[i].Path < sortedPkgs[j].Path })

	outboundPeers := make(map[string]struct{})
	if mode == depsModeOutbound || mode == depsModeBoth {
		for _, d := range pkg.Dependencies {
			if d.To.External || d.To.Package == "" {
				continue
			}
			if d.To.Package == pkg.Path {
				continue
			}
			if _, ok := known[d.To.Package]; !ok {
				continue
			}
			outboundPeers[d.To.Package] = struct{}{}
		}
	}

	inboundPeers := make(map[string]struct{})
	if mode == depsModeInbound || mode == depsModeBoth {
		for _, src := range sortedPkgs {
			if src.Path == pkg.Path {
				continue
			}
			for _, d := range src.Dependencies {
				if d.To.Package == pkg.Path {
					inboundPeers[src.Path] = struct{}{}
					break
				}
			}
		}
	}

	// Assign node kinds: symmetric peers (both in- and outbound) get
	// plain "package" so neither arrow direction dominates visually.
	peerKinds := make(map[string]string)
	for p := range outboundPeers {
		peerKinds[p] = "package-out"
	}
	for p := range inboundPeers {
		if _, ok := peerKinds[p]; ok {
			peerKinds[p] = "package"
		} else {
			peerKinds[p] = "package-in"
		}
	}

	peers := make([]string, 0, len(peerKinds))
	for p := range peerKinds {
		peers = append(peers, p)
	}
	sort.Strings(peers)
	for _, p := range peers {
		out.Nodes = append(out.Nodes, graphNode{
			ID:    "pkg:" + p,
			Label: shortName(p),
			Kind:  peerKinds[p],
		})
	}

	type edgeKey struct{ src, tgt, kind string }
	seenEdge := make(map[edgeKey]struct{})
	addEdge := func(src, tgt, kind string) {
		k := edgeKey{src, tgt, kind}
		if _, dup := seenEdge[k]; dup {
			return
		}
		seenEdge[k] = struct{}{}
		out.Edges = append(out.Edges, graphEdge{Source: src, Target: tgt, Kind: kind})
	}

	if mode == depsModeOutbound || mode == depsModeBoth {
		outs := make([]string, 0, len(outboundPeers))
		for p := range outboundPeers {
			outs = append(outs, p)
		}
		sort.Strings(outs)
		for _, p := range outs {
			addEdge(rootID, "pkg:"+p, "outbound")
		}
	}
	if mode == depsModeInbound || mode == depsModeBoth {
		ins := make([]string, 0, len(inboundPeers))
		for p := range inboundPeers {
			ins = append(ins, p)
		}
		sort.Strings(ins)
		for _, p := range ins {
			addEdge("pkg:"+p, rootID, "inbound")
		}
	}

	return out
}

// externalDepCount returns the number of distinct external packages
// that pkg directly imports. Used to populate the externals summary
// badge in the UI.
func externalDepCount(pkg domain.PackageModel) int {
	seen := make(map[string]struct{})
	for _, d := range pkg.Dependencies {
		if !d.To.External || d.To.Package == "" {
			continue
		}
		seen[d.To.Package] = struct{}{}
	}
	return len(seen)
}
