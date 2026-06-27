package http

import (
	"encoding/json"
	nethttp "net/http"
	"strconv"
	"strings"

	d2adapter "github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/mermaid"
)

// sequenceAPIEntry is one entry-point sequence diagram for a package,
// rendered as Mermaid `sequenceDiagram` source so a front-end can draw it
// without a D2 toolchain.
type sequenceAPIEntry struct {
	Label    string `json:"label"`
	Mermaid  string `json:"mermaid"`
	HasCalls bool   `json:"hasCalls"`
}

// sequenceAPIResponse is the payload returned by /api/sequence.
type sequenceAPIResponse struct {
	Package string             `json:"package"`
	Mode    string             `json:"mode"`
	Entries []sequenceAPIEntry `json:"entries"`
}

// handleSequenceJSON returns the package's call-sequence diagrams as Mermaid
// source, projected to the type-interaction level.
//
//	GET /api/sequence?package=<path>&depth=<n>
//
// Lifelines are types; an edge is drawn only for a cross-type call (caller
// type ≠ callee type), so intra-type calls are collapsed and you see how the
// package's types are wired at the interface level. Entry points are the
// package's public API (exported funcs/methods), but a public-rooted flow that
// hops through an unexported method of *another* type is still drawn — only
// same-type internal chatter is hidden.
func (s *Server) handleSequenceJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	pkgPath := strings.Trim(r.URL.Query().Get("package"), "/")
	if pkgPath == "" {
		nethttp.Error(w, "missing package", nethttp.StatusBadRequest)
		return
	}

	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	pkgs := applyOverlay(snap.Packages, snap.Overlay)

	pkg, ok := findPackage(pkgs, pkgPath)
	if !ok {
		nethttp.NotFound(w, r)
		return
	}

	depth := 4
	if d, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil && d > 0 && d <= 12 {
		depth = d
	}

	diagrams := d2adapter.BuildPackageSequenceSources(pkgs, pkg, d2adapter.SequenceOptions{
		Mode:                        d2adapter.OverviewModePublic,
		MaxDepth:                    depth,
		IncludeInternalInteractions: true,
	})

	entries := make([]sequenceAPIEntry, 0, len(diagrams))
	for _, d := range diagrams {
		if !d.HasCalls {
			continue
		}
		src := mermaid.BuildSequenceSource(d.Tree)
		if strings.TrimSpace(src) == "" {
			continue
		}
		entries = append(entries, sequenceAPIEntry{
			Label:    d.Label,
			Mermaid:  src,
			HasCalls: d.HasCalls,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sequenceAPIResponse{
		Package: pkg.Path,
		Mode:    "public-interaction",
		Entries: entries,
	})
}
