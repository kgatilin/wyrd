package retrieval

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/kgatilin/archmotif/pkg/localpartition"
)

// SearchOptions parameterizes the one search operation.
type SearchOptions struct {
	// K is how many text hits are taken as seeds. They are what the query
	// itself matched, so they always come back in the answer, whatever the
	// diffusion around them does.
	K int

	// Hops is a hard radius on the region grown around the seeds: nothing
	// farther than this many undirected steps from a seed is returned, whatever
	// mass it collected.
	Hops int

	// Filters constrain what the text channels may match. They shape the seeds,
	// not the region: the community a seed sits in is the wiring the caller
	// asked the graph for, and filtering that would answer a different question.
	Filters Filters
}

// Hit is one node of a search answer, carrying which side of the operation put
// it there: Seed marks what the query text matched, the rest is what the
// diffusion from those seeds reached.
type Hit struct {
	NodeInfo

	// Seed is true when the text channels matched this node — it seeded the
	// diffusion instead of being found by it.
	Seed bool `json:"seed,omitempty"`

	// TextScore is the calibrated relevance mass the text channels gave a seed.
	// Zero on a node the diffusion reached: nothing in the query text matched
	// it, the graph did.
	TextScore float64 `json:"text_score,omitempty"`
}

// SearchResult is a whole search answer: the seeds and the community around
// them as one node list, the edges induced among those nodes, and the
// diagnostics of the cut that chose them.
type SearchResult struct {
	// Hits are the seeds first, in text-relevance order, then the nodes the
	// diffusion reached, in diffusion-mass order. Two orderings because there
	// are two signals; every hit carries both scores, so a caller that wants
	// one order for the whole list can impose it.
	Hits  []Hit      `json:"hits"`
	Edges []EdgeInfo `json:"edges"`

	// Dense reports whether the vector channel contributed to the seeds. False
	// means they are BM25-only — a statement about recall, not validity.
	Dense bool `json:"dense"`

	// Conductance is the fraction of edge weight crossing the region's
	// boundary, in [0,1]. Low means the region really does separate from the
	// rest of the graph — the query found something coherent; high means the
	// best available cut still runs through the middle of a hairball, which is
	// the signal that the answer is thin however many nodes came back.
	Conductance float64 `json:"conductance"`

	// Truncated reports that MaxGraphNodes cut the region down, so what came
	// back is the highest-mass part of a larger community. Seeds are never cut.
	Truncated bool `json:"truncated"`

	// SeedCount is how many distinct hits actually seeded the diffusion.
	SeedCount int `json:"seed_count"`
}

// Subgraph represents an induced subgraph from an expand operation.
type Subgraph struct {
	Nodes []NodeInfo `json:"nodes"`
	Edges []EdgeInfo `json:"edges"`
}

// NodeInfo is a lightweight node representation for subgraph responses.
type NodeInfo struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Package   string  `json:"package"`
	Name      string  `json:"name"`
	File      string  `json:"file,omitempty"`
	Line      int     `json:"line,omitempty"`
	Signature string  `json:"signature,omitempty"`
	Doc       string  `json:"doc,omitempty"`
	Score     float64 `json:"score,omitempty"`
}

// EdgeInfo is an edge representation for subgraph responses.
type EdgeInfo struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// NodeDetail is the full detail for a single node including body and edges.
type NodeDetail struct {
	NodeID    string     `json:"node_id"`
	Kind      string     `json:"kind"`
	Package   string     `json:"package"`
	Name      string     `json:"name"`
	File      string     `json:"file"`
	Line      int        `json:"line"`
	Signature string     `json:"signature,omitempty"`
	Doc       string     `json:"doc,omitempty"`
	Body      string     `json:"body,omitempty"`
	Edges     []EdgeInfo `json:"edges"`
}

// Filters constrain search results.
type Filters struct {
	Kinds         []string `json:"kinds,omitempty"`
	PackagePrefix string   `json:"package_prefix,omitempty"`
}

// scoredNode is one text-channel match: the node the query hit and the
// calibrated mass the fusion gave it.
type scoredNode struct {
	node Node
	mass float64
}

// textHits runs the two text channels and fuses them into a single calibrated
// relevance mass per node, sorted by mass descending. This is the signal — the
// y of the design — that the graph stage diffuses; it is not an answer of its
// own, which is why it stays inside the package.
func (s *Service) textHits(ctx context.Context, query string, k int, filters Filters) ([]scoredNode, bool, error) {
	if k < 1 {
		k = 10
	}

	// Fetch more candidates than k for fusion
	fetchK := k * 3
	if fetchK < 50 {
		fetchK = 50
	}

	var denseResults []Scored
	denseUsed := false

	// Dense search if available
	if s.DenseAvailable() && s.embedder != nil && s.vindex != nil {
		vec, err := s.embedder.EmbedQuery(ctx, query)
		if err == nil && len(vec) > 0 {
			denseResults = s.vindex.Search(vec, fetchK)
			denseUsed = true
		}
	}

	// Lexical search (always available)
	lexicalResults := s.lindex.Search(query, fetchK)

	// Fuse the two channels into one calibrated relevance mass
	fused := s.fuseCalibrated(query, denseResults, lexicalResults)

	// Apply filters and limit
	var hits []scoredNode
	for _, item := range fused {
		node, ok := s.getNode(item.ID)
		if !ok {
			continue
		}

		// Kind filter
		if len(filters.Kinds) > 0 && !containsString(filters.Kinds, node.Kind) {
			continue
		}

		// Package prefix filter
		if filters.PackagePrefix != "" && !strings.HasPrefix(node.Package, filters.PackagePrefix) {
			continue
		}

		hits = append(hits, scoredNode{node: node, mass: float64(item.Score)})

		if len(hits) >= k {
			break
		}
	}

	return hits, denseUsed, nil
}

// Search is the whole search operation, and the only one: the text channels
// score what the query matches, and that score is the weight those matches
// carry into a diffusion over the code graph, which hands back the community
// they sit in. "Where does this live" and "how does it connect" are the two
// ends of one answer rather than two tools — the hits are in it, marked, and so
// is the wiring around them.
//
// The push spreads each seed's mass along the weighted edges and stops pushing
// where the residual falls under DiffusionEpsilon, so the walk only ever visits
// the neighbourhood the mass actually reaches — the size of the repository does
// not enter into it. The sweep then walks the mass-ordered prefixes of what was
// reached and keeps the one whose boundary is cheapest, which is what makes the
// region size query-adaptive: a narrow question cuts a small dense region, a
// broad one keeps going until the boundary stops getting cheaper. The price of
// that boundary is the conductance, and it travels back as the quality signal
// on the whole answer.
//
// Three bounds sit on top of the diffusion. Hops keeps the meaning its name
// promises — alpha and epsilon shape the region but say nothing about graph
// distance, so the sweep set is intersected with the nodes within Hops
// undirected steps of the seeds. MaxGraphNodes is the payload cap, applied last
// and by mass, and it is reported instead of silently applied. Neither touches
// the seeds: a symbol the query named is in the answer even when it is isolated
// from everything the diffusion found, because a search that hides its own hits
// is not a search.
func (s *Service) Search(ctx context.Context, query string, opts SearchOptions) (SearchResult, error) {
	hops := opts.Hops
	if hops < 1 {
		hops = 1
	}

	seeds, denseUsed, err := s.textHits(ctx, query, opts.K, opts.Filters)
	if err != nil {
		return SearchResult{}, err
	}

	s.mu.RLock()
	graph := s.graph
	params := s.params
	s.mu.RUnlock()

	result := SearchResult{Hits: []Hit{}, Edges: []EdgeInfo{}, Dense: denseUsed}
	if len(seeds) == 0 {
		return result, nil
	}

	// A candidate that ended up with no mass is not evidence of anything, and
	// localpartition reads a non-positive seed weight as "unweighted" (1) — the
	// exact inversion that would make the weakest hit the strongest seed. It
	// stays in the answer as a hit; it just does not personalize the diffusion.
	lpSeeds := make([]localpartition.Seed, 0, len(seeds))
	seedIDs := make([]string, 0, len(seeds))
	isSeed := make(map[string]bool, len(seeds))
	for _, sd := range seeds {
		isSeed[sd.node.ID] = true
		if sd.mass <= 0 {
			continue
		}
		lpSeeds = append(lpSeeds, localpartition.Seed{Name: sd.node.ID, Weight: sd.mass})
		seedIDs = append(seedIDs, sd.node.ID)
	}

	if graph == nil || len(lpSeeds) == 0 {
		// Nothing to diffuse over: the hits are the whole answer, and their own
		// text mass is the only score there is.
		result.Hits, result.Edges = s.buildAnswer(graph, seeds, nil, nil)
		return result, nil
	}

	edges := diffusionEdges(graph, params.EdgeKindWeights)
	part, err := localpartition.LocalPartitionWeighted(edges, lpSeeds, localpartition.Options{
		Alpha:      params.DiffusionAlpha,
		Epsilon:    params.DiffusionEpsilon,
		HubDamping: params.HubDamping,
	})
	if err != nil {
		return SearchResult{}, fmt.Errorf("local partition: %w", err)
	}

	// The seeds are listed on their own terms, so the region is what the
	// diffusion added to them — and the payload cap is spent on that alone.
	region := make([]string, 0, len(part.Region))
	for _, id := range withinHops(edges, seedIDs, part.Region, hops) {
		if !isSeed[id] {
			region = append(region, id)
		}
	}
	region, truncated := capByMass(region, part.Weights, params.MaxGraphNodes)

	result.Hits, result.Edges = s.buildAnswer(graph, seeds, region, part.Weights)
	result.Conductance = part.Conductance
	result.SeedCount = part.SeedCount
	result.Truncated = truncated
	return result, nil
}

// buildAnswer materialises the answer: the seeds in text-relevance order, then
// the region in mass order, followed by the edges induced among all of them.
//
// Score is the diffusion mass throughout, so one number orders the whole list;
// a seed the diffusion never placed (no graph, or a symbol with no
// participating edges) falls back to its text mass rather than sorting to the
// bottom of an answer it is the point of. A region node the graph cannot
// resolve is skipped — edges may name a symbol that never became a node of its
// own.
func (s *Service) buildAnswer(graph *Graph, seeds []scoredNode, region []string, weights map[string]float64) ([]Hit, []EdgeInfo) {
	if graph == nil {
		region = nil
	}
	hits := make([]Hit, 0, len(seeds)+len(region))
	keep := make(map[string]bool, len(seeds)+len(region))

	for _, sd := range seeds {
		if keep[sd.node.ID] {
			continue
		}
		keep[sd.node.ID] = true
		mass, ok := weights[sd.node.ID]
		if !ok {
			mass = sd.mass
		}
		hits = append(hits, Hit{
			NodeInfo:  nodeInfoOf(sd.node, mass),
			Seed:      true,
			TextScore: sd.mass,
		})
	}

	for _, id := range region {
		if keep[id] {
			continue
		}
		node, ok := graph.NodesByID[id]
		if !ok {
			continue
		}
		keep[id] = true
		hits = append(hits, Hit{NodeInfo: nodeInfoOf(node, weights[id])})
	}

	if graph == nil {
		return hits, []EdgeInfo{}
	}

	rawEdges := graph.InducedEdges(keep)
	edges := make([]EdgeInfo, len(rawEdges))
	for i, e := range rawEdges {
		edges[i] = EdgeInfo{From: e.From, To: e.To, Kind: string(e.Kind)}
	}
	sortEdgeInfos(edges)
	return hits, edges
}

// nodeInfoOf projects a graph node onto the lightweight shape responses carry.
func nodeInfoOf(node Node, score float64) NodeInfo {
	return NodeInfo{
		ID:        node.ID,
		Kind:      node.Kind,
		Package:   node.Package,
		Name:      node.Name,
		File:      node.Span.File,
		Line:      node.Span.StartLine,
		Signature: node.Signature,
		Doc:       truncateString(node.Doc, 200),
		Score:     score,
	}
}

// diffusionEdges projects the code graph onto the weighted edge list the
// diffusion walks.
//
// EdgeKindWeights is the participation list, not merely a scale: a kind the map
// does not mention contributes no edge at all. That is how a relation is kept
// out of the diffusion entirely — the design excludes containment on exactly
// these grounds — without a second masking parameter that could disagree with
// the weights. A kind mapped to a non-positive weight is read the same way,
// since localpartition treats such a weight as 1 and would otherwise smuggle
// the edge back in at full strength.
//
// Only Outgoing is walked. Incoming holds the same edges seen from the other
// end, and localpartition symmetrizes anyway, so iterating both maps would
// double every weight. The result is sorted so that the weight accumulation
// inside localpartition happens in the same order on every run, which Go's
// randomized map iteration would otherwise leave free to differ in the last
// bits — enough to flip a sweep tie.
func diffusionEdges(graph *Graph, kindWeights map[string]float64) []localpartition.WeightedEdge {
	var edges []localpartition.WeightedEdge
	for _, out := range graph.Outgoing {
		for _, e := range out {
			w, ok := kindWeights[string(e.Kind)]
			if !ok || w <= 0 {
				continue
			}
			edges = append(edges, localpartition.WeightedEdge{From: e.From, To: e.To, Weight: w})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Weight < edges[j].Weight
	})
	return edges
}

// withinHops drops the region nodes that lie farther than hops undirected steps
// from the seeds.
//
// The diffusion has no notion of distance: alpha decides how fast mass decays
// with each step, not how many steps it may take, so a well-connected region
// can reach several hops out at any alpha. The hops argument is a promise about
// radius, and this is where it is kept.
//
// The walk is over the edge list handed to the diffusion rather than over the
// Graph's own adjacency, so the two can never measure different graphs: whatever
// diffusionEdges decides participates is what a hop is counted along.
func withinHops(edges []localpartition.WeightedEdge, seedIDs, region []string, hops int) []string {
	adj := make(map[string][]string, len(edges))
	for _, e := range edges {
		if e.From == e.To {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	reachable := make(map[string]bool, len(seedIDs))
	frontier := make([]string, 0, len(seedIDs))
	for _, id := range seedIDs {
		if !reachable[id] {
			reachable[id] = true
			frontier = append(frontier, id)
		}
	}
	for step := 0; step < hops && len(frontier) > 0; step++ {
		var next []string
		for _, id := range frontier {
			for _, nbr := range adj[id] {
				if reachable[nbr] {
					continue
				}
				reachable[nbr] = true
				next = append(next, nbr)
			}
		}
		frontier = next
	}

	kept := make([]string, 0, len(region))
	for _, id := range region {
		if reachable[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

// capByMass orders the region by diffusion mass and keeps at most max of it,
// reporting whether anything was dropped. Ordering is part of the contract, not
// just of the cap: consumers render and re-truncate the node list as given, so
// the heaviest nodes have to come first. Ties break on node ID, so the same
// query returns the same nodes run to run.
func capByMass(region []string, weights map[string]float64, max int) ([]string, bool) {
	ordered := make([]string, len(region))
	copy(ordered, region)
	sort.Slice(ordered, func(i, j int) bool {
		wi, wj := weights[ordered[i]], weights[ordered[j]]
		if wi != wj {
			return wi > wj
		}
		return ordered[i] < ordered[j]
	})
	if max > 0 && len(ordered) > max {
		return ordered[:max], true
	}
	return ordered, false
}

// emptySubgraph is the shape a query with nothing to say returns: empty, but
// never nil, so a caller can marshal it without a nil-versus-[] special case.
func emptySubgraph() Subgraph {
	return Subgraph{Nodes: []NodeInfo{}, Edges: []EdgeInfo{}}
}

// sortEdgeInfos orders edges deterministically by endpoints then kind.
func sortEdgeInfos(edges []EdgeInfo) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
}

// Expand performs BFS from the given node IDs up to hops steps.
// edgeKinds filters which edge types to traverse; empty means all.
func (s *Service) Expand(ctx context.Context, nodeIDs []string, hops int, edgeKinds []string) (Subgraph, error) {
	s.mu.RLock()
	graph := s.graph
	s.mu.RUnlock()

	if graph == nil {
		return emptySubgraph(), nil
	}

	// Convert edge kind strings
	var kinds []EdgeKind
	for _, k := range edgeKinds {
		kinds = append(kinds, EdgeKind(k))
	}

	// BFS to find neighbor nodes (uncapped: the explicit expand endpoint keeps
	// its full-neighbourhood contract).
	nodeSet := graph.NeighborNodes(nodeIDs, hops, kinds, 0)

	// Build node info list
	var nodes []NodeInfo
	for id := range nodeSet {
		if node, ok := graph.NodesByID[id]; ok {
			nodes = append(nodes, NodeInfo{
				ID:        node.ID,
				Kind:      node.Kind,
				Package:   node.Package,
				Name:      node.Name,
				Signature: node.Signature,
				Doc:       truncateString(node.Doc, 200),
			})
		}
	}

	// Sort for determinism
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	// Build induced edges
	rawEdges := graph.InducedEdges(nodeSet)
	edges := make([]EdgeInfo, len(rawEdges))
	for i, e := range rawEdges {
		edges[i] = EdgeInfo{From: e.From, To: e.To, Kind: string(e.Kind)}
	}
	sortEdgeInfos(edges)

	return Subgraph{Nodes: nodes, Edges: edges}, nil
}

// Node returns full detail for a single node including body read via Span.
func (s *Service) Node(ctx context.Context, id string) (NodeDetail, error) {
	s.mu.RLock()
	graph := s.graph
	s.mu.RUnlock()

	if graph == nil {
		return NodeDetail{}, nil
	}

	node, ok := graph.NodesByID[id]
	if !ok {
		return NodeDetail{}, nil
	}

	// Read body from file via Span
	body := ""
	if node.Span.IsValid() {
		filePath := filepath.Join(s.root, node.Span.File)
		data, err := os.ReadFile(filePath)
		if err == nil {
			if node.Span.StartByte >= 0 && node.Span.EndByte <= len(data) && node.Span.StartByte < node.Span.EndByte {
				body = string(data[node.Span.StartByte:node.Span.EndByte])
			}
		}
	}

	// Collect incident edges
	var edges []EdgeInfo
	for _, e := range graph.Outgoing[id] {
		edges = append(edges, EdgeInfo{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	for _, e := range graph.Incoming[id] {
		edges = append(edges, EdgeInfo{From: e.From, To: e.To, Kind: string(e.Kind)})
	}
	sortEdgeInfos(edges)

	return NodeDetail{
		NodeID:    node.ID,
		Kind:      node.Kind,
		Package:   node.Package,
		Name:      node.Name,
		File:      node.Span.File,
		Line:      node.Span.StartLine,
		Signature: node.Signature,
		Doc:       node.Doc,
		Body:      body,
		Edges:     edges,
	}, nil
}

// getNode retrieves a node from the graph by ID.
func (s *Service) getNode(id string) (Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.graph == nil {
		return Node{}, false
	}
	node, ok := s.graph.NodesByID[id]
	return node, ok
}

// fuseCalibrated combines the dense and lexical candidate lists into a single
// calibrated relevance mass per candidate, sorted by mass descending.
//
// A cosine and a BM25 score sit on unrelated scales, so each channel is first
// softmaxed into a probability mass over its own candidates; the two masses are
// then mixed convexly with DenseWeight. The result is a distribution over the
// candidate pool, which keeps the margin between hits — how far ahead the top
// one actually was — and is what the graph diffusion downstream takes as its
// personalization vector.
//
// When only one channel produced candidates it carries the full mass rather
// than its convex share, so the distribution still sums to 1.
func (s *Service) fuseCalibrated(query string, dense, lexical []Scored) []Scored {
	denseMass := softmax(dense, s.params.DenseTemp)
	lexMass := softmax(lexical, s.params.LexTemp)

	var mass map[string]float64
	switch {
	case len(denseMass) == 0:
		mass = lexMass
	case len(lexMass) == 0:
		mass = denseMass
	default:
		beta := s.params.DenseWeight
		mass = make(map[string]float64, len(denseMass)+len(lexMass))
		for id, m := range denseMass {
			mass[id] += beta * m
		}
		for id, m := range lexMass {
			mass[id] += (1 - beta) * m
		}
	}

	s.applyExactNameFloor(mass, query)

	fused := make([]Scored, 0, len(mass))
	for id, m := range mass {
		fused = append(fused, Scored{ID: id, Score: float32(m)})
	}
	sort.Slice(fused, func(i, j int) bool {
		if fused[i].Score != fused[j].Score {
			return fused[i].Score > fused[j].Score
		}
		return fused[i].ID < fused[j].ID
	})

	return fused
}

// softmax turns a channel's raw scores into a probability mass over its
// candidates. temp controls sharpness: a low temperature concentrates the mass
// on the leading scores, a high one flattens toward uniform. The maximum score
// is subtracted before exponentiating, the standard overflow guard.
func softmax(scored []Scored, temp float64) map[string]float64 {
	if len(scored) == 0 {
		return nil
	}
	if temp <= 0 {
		temp = 1
	}

	top := math.Inf(-1)
	for _, sc := range scored {
		if v := float64(sc.Score); v > top {
			top = v
		}
	}

	mass := make(map[string]float64, len(scored))
	for _, sc := range scored {
		mass[sc.ID] = math.Exp((float64(sc.Score) - top) / temp)
	}

	total := 0.0
	for _, m := range mass {
		total += m
	}
	if total <= 0 {
		return mass
	}
	for id := range mass {
		mass[id] /= total
	}
	return mass
}

// applyExactNameFloor lifts every candidate whose symbol name is literally a
// token of the query to at least ExactNameBoost, then renormalizes the mass
// back to 1. A softmax over a wide candidate pool can bury an exactly named
// symbol under its semantic neighbours; the floor keeps a name the caller typed
// verbatim in view without pinning it to the top.
func (s *Service) applyExactNameFloor(mass map[string]float64, query string) {
	floor := s.params.ExactNameBoost
	if floor <= 0 || len(mass) == 0 {
		return
	}
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return
	}

	lifted := false
	for id, m := range mass {
		if m >= floor {
			continue
		}
		node, ok := s.getNode(id)
		if !ok || !namedBy(tokens, node.Name) {
			continue
		}
		mass[id] = floor
		lifted = true
	}
	if !lifted {
		return
	}

	total := 0.0
	for _, m := range mass {
		total += m
	}
	if total <= 0 {
		return
	}
	for id := range mass {
		mass[id] /= total
	}
}

// namedBy reports whether the query named this symbol. A method's Name carries
// its receiver ("Service.Search"), and callers type the method alone at least
// as often as they type the pair, so the bare member name counts as naming it
// too — otherwise the one kind of symbol whose name is qualified would be the
// one kind the floor never lifts.
func namedBy(tokens map[string]bool, name string) bool {
	if tokens[strings.ToLower(name)] {
		return true
	}
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return tokens[strings.ToLower(name[dot+1:])]
	}
	return false
}

// queryTokens splits a query on everything that cannot appear in a Go
// identifier and lowercases what is left, so "NewService(ctx)" yields
// {newservice, ctx}.
func queryTokens(query string) map[string]bool {
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	tokens := make(map[string]bool, len(parts))
	for _, p := range parts {
		tokens[strings.ToLower(p)] = true
	}
	return tokens
}

// truncateString truncates s to maxLen characters with "..." suffix if needed.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// containsString checks if needle is in haystack.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
