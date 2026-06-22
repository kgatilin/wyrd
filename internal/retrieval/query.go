package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Result represents a search result with scoring and context.
type Result struct {
	NodeID  string  `json:"node_id"`
	Kind    string  `json:"kind"`
	File    string  `json:"file"`
	Doc     string  `json:"doc"`
	Snippet string  `json:"snippet"`
	Score   float32 `json:"score"`
}

// Subgraph represents an induced subgraph from expand/search_graph operations.
type Subgraph struct {
	Nodes []NodeInfo `json:"nodes"`
	Edges []EdgeInfo `json:"edges"`
}

// NodeInfo is a lightweight node representation for subgraph responses.
type NodeInfo struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Package   string `json:"package"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Doc       string `json:"doc,omitempty"`
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

// rrfK is the constant used in reciprocal rank fusion scoring.
const rrfK = 60

// Search performs hybrid search: dense (if available) + BM25, fused with RRF.
// Returns results sorted by fused score descending.
func (s *Service) Search(ctx context.Context, query string, k int, filters Filters) ([]Result, bool, error) {
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
		vecs, err := s.embedder.Embed(ctx, []string{query})
		if err == nil && len(vecs) > 0 {
			denseResults = s.vindex.Search(vecs[0], fetchK)
			denseUsed = true
		}
	}

	// Lexical search (always available)
	lexicalResults := s.lindex.Search(query, fetchK)

	// Fuse with RRF
	fused := rrfFuse(denseResults, lexicalResults, rrfK)

	// Apply filters and limit
	var results []Result
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

		result := Result{
			NodeID:  node.ID,
			Kind:    node.Kind,
			File:    node.Span.File,
			Doc:     truncateString(node.Doc, 200),
			Snippet: buildSnippet(node, s.root),
			Score:   item.Score,
		}
		results = append(results, result)

		if len(results) >= k {
			break
		}
	}

	return results, denseUsed, nil
}

// SearchGraph performs Search then expands the results into a subgraph.
func (s *Service) SearchGraph(ctx context.Context, query string, k, hops int) (Subgraph, bool, error) {
	if hops < 1 {
		hops = 1
	}

	results, denseUsed, err := s.Search(ctx, query, k, Filters{})
	if err != nil {
		return Subgraph{}, denseUsed, err
	}

	// Collect seed node IDs from search results
	seedIDs := make([]string, len(results))
	for i, r := range results {
		seedIDs[i] = r.NodeID
	}

	subgraph, err := s.Expand(ctx, seedIDs, hops, nil)
	return subgraph, denseUsed, err
}

// Expand performs BFS from the given node IDs up to hops steps.
// edgeKinds filters which edge types to traverse; empty means all.
func (s *Service) Expand(ctx context.Context, nodeIDs []string, hops int, edgeKinds []string) (Subgraph, error) {
	s.mu.RLock()
	graph := s.graph
	s.mu.RUnlock()

	if graph == nil {
		return Subgraph{Nodes: []NodeInfo{}, Edges: []EdgeInfo{}}, nil
	}

	// Convert edge kind strings
	var kinds []EdgeKind
	for _, k := range edgeKinds {
		kinds = append(kinds, EdgeKind(k))
	}

	// BFS to find neighbor nodes
	nodeSet := graph.NeighborNodes(nodeIDs, hops, kinds)

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
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})

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
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})

	return NodeDetail{
		NodeID:    node.ID,
		Kind:      node.Kind,
		Package:   node.Package,
		Name:      node.Name,
		File:      node.Span.File,
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

// rrfFuse combines dense and lexical results using Reciprocal Rank Fusion.
func rrfFuse(dense, lexical []Scored, k float32) []Scored {
	scores := make(map[string]float32)

	// RRF score from dense results
	for rank, r := range dense {
		scores[r.ID] += 1.0 / (k + float32(rank+1))
	}

	// RRF score from lexical results
	for rank, r := range lexical {
		scores[r.ID] += 1.0 / (k + float32(rank+1))
	}

	// Convert to sorted slice
	var fused []Scored
	for id, score := range scores {
		fused = append(fused, Scored{ID: id, Score: score})
	}

	sort.Slice(fused, func(i, j int) bool {
		return fused[i].Score > fused[j].Score
	})

	return fused
}

// buildSnippet creates a snippet from signature + doc + first lines of body.
func buildSnippet(node Node, root string) string {
	var parts []string

	if node.Signature != "" {
		parts = append(parts, node.Signature)
	}
	if node.Doc != "" {
		parts = append(parts, truncateString(node.Doc, 100))
	}

	// Add first lines of body if available
	if node.Span.IsValid() {
		filePath := filepath.Join(root, node.Span.File)
		data, err := os.ReadFile(filePath)
		if err == nil && node.Span.StartByte >= 0 && node.Span.EndByte <= len(data) {
			body := string(data[node.Span.StartByte:node.Span.EndByte])
			lines := strings.SplitN(body, "\n", 4)
			if len(lines) > 3 {
				lines = lines[:3]
			}
			bodySnippet := strings.Join(lines, "\n")
			if len(bodySnippet) > 150 {
				bodySnippet = bodySnippet[:150] + "..."
			}
			parts = append(parts, bodySnippet)
		}
	}

	return strings.Join(parts, "\n")
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
