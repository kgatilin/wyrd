package http

import (
	nethttp "net/http"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// bcListData is the page model for /bc (bounded context list).
type bcListData struct {
	pageData

	HasOverlay bool
	BCs        []bcSummaryView
}

// bcSummaryView is one row in the /bc list.
type bcSummaryView struct {
	Name         string
	Description  string
	Relationship string
	AggCount     int
	PkgCount     int
	Href         string
}

// bcDetailData is the page model for /bc/{name}.
type bcDetailData struct {
	pageData

	Name         string
	Description  string
	Relationship string
	Aggregates   []string
	Upstream     []bcRefView
	Downstream   []bcRefView
	Packages     []domain.PackageModel
}

// bcRefView is a reference to another bounded context in the detail view.
type bcRefView struct {
	Name string
	Href string
}

// registerBCRoutes mounts the /bc and /bc/{name} handlers plus the
// JSON graph API at /api/bc/graph.
func (s *Server) registerBCRoutes(mux *nethttp.ServeMux) {
	mux.HandleFunc("/bc", s.handleBCList)
	mux.HandleFunc("/bc/", s.handleBCDetail)
	mux.HandleFunc("/api/bc/graph", s.handleBCGraph)
}

// handleBCList renders /bc — the bounded context catalog.
func (s *Server) handleBCList(w nethttp.ResponseWriter, r *nethttp.Request) {
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	data := bcListData{
		pageData: s.basePageData(r, "Domain", "/bc"),
	}

	if snap.Overlay != nil && len(snap.Overlay.BoundedContexts) > 0 {
		data.HasOverlay = true
		data.BCs = buildBCSummaries(snap.Overlay.BoundedContexts, snap.Packages)
	}

	s.renderPage(w, "bc_list.html", data)
}

// handleBCDetail renders /bc/{name} — the detail page for one bounded context.
func (s *Server) handleBCDetail(w nethttp.ResponseWriter, r *nethttp.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/bc/")
	if name == "" {
		nethttp.Redirect(w, r, "/bc", nethttp.StatusFound)
		return
	}

	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()

	if snap.Overlay == nil {
		nethttp.NotFound(w, r)
		return
	}
	bc, ok := snap.Overlay.BoundedContexts[name]
	if !ok {
		nethttp.NotFound(w, r)
		return
	}

	pkgs := packagesInBC(name, bc.Aggregates, snap.Packages)

	upstream := make([]bcRefView, 0, len(bc.Upstream))
	for _, u := range bc.Upstream {
		upstream = append(upstream, bcRefView{Name: u, Href: "/bc/" + u})
	}
	downstream := make([]bcRefView, 0, len(bc.Downstream))
	for _, d := range bc.Downstream {
		downstream = append(downstream, bcRefView{Name: d, Href: "/bc/" + d})
	}

	aggsCopy := make([]string, len(bc.Aggregates))
	copy(aggsCopy, bc.Aggregates)
	sort.Strings(aggsCopy)

	data := bcDetailData{
		pageData:     s.basePageData(r, "Domain: "+name, "/bc"),
		Name:         name,
		Description:  bc.Description,
		Relationship: bc.Relationship,
		Aggregates:   aggsCopy,
		Upstream:     upstream,
		Downstream:   downstream,
		Packages:     pkgs,
	}

	s.renderPage(w, "bc_detail.html", data)
}

// handleBCGraph serves GET /api/bc/graph — the Cytoscape JSON for the
// bounded context graph. Nodes represent bounded contexts; edges
// represent upstream/downstream relationships.
func (s *Server) handleBCGraph(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	if snap.Overlay == nil {
		writeJSON(w, graphPayload{
			Meta:  graphMeta{View: "bc-map", Layout: "elk"},
			Nodes: []graphNode{},
			Edges: []graphEdge{},
		})
		return
	}
	payload := buildBCGraph(snap.Overlay)
	writeJSON(w, payload)
}

// buildBCGraph produces the Cytoscape payload for /api/bc/graph.
// Each bounded context becomes a node; upstream relationships become
// directed edges. The function avoids duplicate edges by only emitting
// an edge when the source is alphabetically less than the target, then
// relying on the upstream/downstream labelling for direction.
func buildBCGraph(cfg *overlay.Config) graphPayload {
	out := graphPayload{
		Meta:  graphMeta{View: "bc-map", Layout: "elk"},
		Nodes: make([]graphNode, 0, len(cfg.BoundedContexts)),
		Edges: make([]graphEdge, 0),
	}

	// Sorted names for stable output.
	names := make([]string, 0, len(cfg.BoundedContexts))
	for name := range cfg.BoundedContexts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		bc := cfg.BoundedContexts[name]
		label := name
		if bc.Description != "" {
			label = name + "\n" + bc.Description
		}
		out.Nodes = append(out.Nodes, graphNode{
			ID:    "bc:" + name,
			Label: label,
			Kind:  "bc",
		})
	}

	// Emit edges only from the upstream side to avoid duplicates.
	seen := make(map[string]struct{})
	for _, name := range names {
		bc := cfg.BoundedContexts[name]
		for _, up := range bc.Upstream {
			if _, ok := cfg.BoundedContexts[up]; !ok {
				continue
			}
			edgeID := "bc:" + up + "->bc:" + name
			reverseID := "bc:" + name + "->bc:" + up
			if _, exists := seen[edgeID]; exists {
				continue
			}
			if _, exists := seen[reverseID]; exists {
				continue
			}
			seen[edgeID] = struct{}{}
			out.Edges = append(out.Edges, graphEdge{
				Source: "bc:" + up,
				Target: "bc:" + name,
				Kind:   "upstream",
			})
		}
	}

	return out
}

// buildBCSummaries returns a sorted list of summary views for all BCs.
func buildBCSummaries(bcs map[string]overlay.BoundedContext, packages []domain.PackageModel) []bcSummaryView {
	out := make([]bcSummaryView, 0, len(bcs))
	for name, bc := range bcs {
		pkgs := packagesInBC(name, bc.Aggregates, packages)
		out = append(out, bcSummaryView{
			Name:         name,
			Description:  bc.Description,
			Relationship: bc.Relationship,
			AggCount:     len(bc.Aggregates),
			PkgCount:     len(pkgs),
			Href:         "/bc/" + name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// packagesInBC returns the packages whose Aggregate field is one of the
// named aggregates in the bounded context.
func packagesInBC(bcName string, aggregates []string, packages []domain.PackageModel) []domain.PackageModel {
	if len(aggregates) == 0 {
		return nil
	}
	in := make(map[string]struct{}, len(aggregates))
	for _, a := range aggregates {
		in[a] = struct{}{}
	}
	var out []domain.PackageModel
	for _, p := range packages {
		if _, ok := in[p.Aggregate]; ok {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
