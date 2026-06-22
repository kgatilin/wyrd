package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"strings"

	"github.com/kgatilin/archai/internal/retrieval"
)

// registerRetrievalRoutes wires the retrieval API endpoints.
// These are registered both at top-level and under /w/{name}/ in multi-worktree mode.
func (s *Server) registerRetrievalRoutes(mux *nethttp.ServeMux) {
	mux.HandleFunc("/api/search", s.handleAPISearch)
	mux.HandleFunc("/api/search_graph", s.handleAPISearchGraph)
	mux.HandleFunc("/api/expand", s.handleAPIExpand)
	mux.HandleFunc("/api/node/", s.handleAPINode)
	mux.HandleFunc("/api/refresh", s.handleAPIRefresh)
}

// searchRequest is the JSON body for POST /api/search.
type searchRequest struct {
	Query   string            `json:"query"`
	K       int               `json:"k,omitempty"`
	Filters retrieval.Filters `json:"filters,omitempty"`
}

// searchResponse is the JSON response for POST /api/search.
type searchResponse struct {
	Results []retrieval.Result `json:"results"`
	Dense   bool               `json:"dense"`
}

// handleAPISearch handles POST /api/search.
func (s *Server) handleAPISearch(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		w.Header().Set("Allow", nethttp.MethodPost)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	var req searchRequest
	if err := readJSONBodyInto(r, &req); err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	if req.Query == "" {
		writeJSONErrorText(w, nethttp.StatusBadRequest, "missing query")
		return
	}

	state := s.stateFor(r)
	if state == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "no state available")
		return
	}

	svc := state.Retrieval()
	if svc == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "retrieval not initialized")
		return
	}

	k := req.K
	if k <= 0 {
		k = 10
	}

	results, denseUsed, err := svc.Search(r.Context(), req.Query, k, req.Filters)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusInternalServerError, err.Error())
		return
	}

	if results == nil {
		results = []retrieval.Result{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(searchResponse{
		Results: results,
		Dense:   denseUsed,
	})
}

// searchGraphRequest is the JSON body for POST /api/search_graph.
type searchGraphRequest struct {
	Query string `json:"query"`
	K     int    `json:"k,omitempty"`
	Hops  int    `json:"hops,omitempty"`
}

// searchGraphResponse is the JSON response for POST /api/search_graph.
type searchGraphResponse struct {
	Nodes []retrieval.NodeInfo `json:"nodes"`
	Edges []retrieval.EdgeInfo `json:"edges"`
	Dense bool                 `json:"dense"`
}

// handleAPISearchGraph handles POST /api/search_graph.
func (s *Server) handleAPISearchGraph(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		w.Header().Set("Allow", nethttp.MethodPost)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	var req searchGraphRequest
	if err := readJSONBodyInto(r, &req); err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	if req.Query == "" {
		writeJSONErrorText(w, nethttp.StatusBadRequest, "missing query")
		return
	}

	state := s.stateFor(r)
	if state == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "no state available")
		return
	}

	svc := state.Retrieval()
	if svc == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "retrieval not initialized")
		return
	}

	k := req.K
	if k <= 0 {
		k = 10
	}
	hops := req.Hops
	if hops <= 0 {
		hops = 1
	}

	subgraph, denseUsed, err := svc.SearchGraph(r.Context(), req.Query, k, hops)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusInternalServerError, err.Error())
		return
	}

	if subgraph.Nodes == nil {
		subgraph.Nodes = []retrieval.NodeInfo{}
	}
	if subgraph.Edges == nil {
		subgraph.Edges = []retrieval.EdgeInfo{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(searchGraphResponse{
		Nodes: subgraph.Nodes,
		Edges: subgraph.Edges,
		Dense: denseUsed,
	})
}

// expandRequest is the JSON body for POST /api/expand.
type expandRequest struct {
	NodeIDs   []string `json:"node_ids"`
	Hops      int      `json:"hops,omitempty"`
	EdgeKinds []string `json:"edges,omitempty"`
}

// expandResponse is the JSON response for POST /api/expand.
type expandResponse struct {
	Nodes []retrieval.NodeInfo `json:"nodes"`
	Edges []retrieval.EdgeInfo `json:"edges"`
}

// handleAPIExpand handles POST /api/expand.
func (s *Server) handleAPIExpand(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		w.Header().Set("Allow", nethttp.MethodPost)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	var req expandRequest
	if err := readJSONBodyInto(r, &req); err != nil {
		writeJSONErrorText(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	if len(req.NodeIDs) == 0 {
		writeJSONErrorText(w, nethttp.StatusBadRequest, "missing node_ids")
		return
	}

	state := s.stateFor(r)
	if state == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "no state available")
		return
	}

	svc := state.Retrieval()
	if svc == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "retrieval not initialized")
		return
	}

	hops := req.Hops
	if hops <= 0 {
		hops = 1
	}

	subgraph, err := svc.Expand(r.Context(), req.NodeIDs, hops, req.EdgeKinds)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusInternalServerError, err.Error())
		return
	}

	if subgraph.Nodes == nil {
		subgraph.Nodes = []retrieval.NodeInfo{}
	}
	if subgraph.Edges == nil {
		subgraph.Edges = []retrieval.EdgeInfo{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(expandResponse{
		Nodes: subgraph.Nodes,
		Edges: subgraph.Edges,
	})
}

// handleAPINode handles GET /api/node/{id}.
func (s *Server) handleAPINode(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		w.Header().Set("Allow", nethttp.MethodGet)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	// Extract node ID from path: /api/node/{id}
	const prefix = "/api/node/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		nethttp.NotFound(w, r)
		return
	}
	nodeID := strings.TrimPrefix(r.URL.Path, prefix)
	if nodeID == "" {
		writeJSONErrorText(w, nethttp.StatusBadRequest, "missing node id")
		return
	}

	state := s.stateFor(r)
	if state == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "no state available")
		return
	}

	svc := state.Retrieval()
	if svc == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "retrieval not initialized")
		return
	}

	detail, err := svc.Node(r.Context(), nodeID)
	if err != nil {
		writeJSONErrorText(w, nethttp.StatusInternalServerError, err.Error())
		return
	}

	if detail.NodeID == "" {
		writeJSONErrorText(w, nethttp.StatusNotFound, "node not found")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(detail)
}

// refreshResponse is the JSON response for POST /api/refresh.
type refreshResponse struct {
	Reindexed int  `json:"reindexed"`
	Removed   int  `json:"removed"`
	Dense     bool `json:"dense"`
}

// handleAPIRefresh handles POST /api/refresh.
func (s *Server) handleAPIRefresh(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		w.Header().Set("Allow", nethttp.MethodPost)
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}

	state := s.stateFor(r)
	if state == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "no state available")
		return
	}

	svc := state.Retrieval()
	if svc == nil {
		writeJSONErrorText(w, nethttp.StatusServiceUnavailable, "retrieval not initialized")
		return
	}

	// Get current model snapshot and rebuild index
	snap := state.Snapshot()
	models := snap.Packages

	// Count nodes before
	oldCount := 0
	if svc.Graph() != nil {
		oldCount = len(svc.Graph().NodesByID)
	}

	// Reindex from models (this builds nodes, graph, and indexes)
	ctx := context.Background()
	if err := svc.IndexFromModels(ctx, models); err != nil {
		writeJSONErrorText(w, nethttp.StatusInternalServerError, err.Error())
		return
	}

	// Save indexes
	if err := svc.Save(); err != nil {
		writeJSONErrorText(w, nethttp.StatusInternalServerError, err.Error())
		return
	}

	// Count nodes after
	newCount := 0
	if svc.Graph() != nil {
		newCount = len(svc.Graph().NodesByID)
	}

	// Calculate removed (rough estimate)
	removed := 0
	if oldCount > newCount {
		removed = oldCount - newCount
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(refreshResponse{
		Reindexed: newCount,
		Removed:   removed,
		Dense:     svc.DenseAvailable(),
	})
}

// readJSONBodyInto reads and unmarshals the request body into v.
func readJSONBodyInto(r *nethttp.Request, v interface{}) error {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	b = trimBOM(b)
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil // empty body is OK, leaves v at zero value
	}
	return json.Unmarshal(b, v)
}
