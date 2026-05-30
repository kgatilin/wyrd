// Package uigraph projects archai's domain model + overlay + diff into the
// UIGraph JSON shape consumed by the POC review UI. Pure data + a pure
// projection function; no I/O, no behavior on the types.
package uigraph

import (
	"path"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

const Schema = "archai.uigraph/v0"

type UIGraph struct {
	Schema          string           `json:"schema"`
	PR              *PR              `json:"pr,omitempty"`
	BoundedContexts []BoundedContext `json:"boundedContexts"`
	Components      []Component      `json:"components"`
	Edges           []Edge           `json:"edges"`
	Comments        []Comment        `json:"comments"`
}

type PR struct {
	Title   string `json:"title"`
	Branch  string `json:"branch"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Stats   Stats  `json:"stats"`
}

type Stats struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Changed  int `json:"changed"`
	Comments int `json:"comments"`
}

type BoundedContext struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Component struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Tech      string     `json:"tech"`
	Desc      string     `json:"desc"`
	BC        string     `json:"bc"`
	Diff      string     `json:"diff,omitempty"` // added|removed|changed
	Internals []Internal `json:"internals"`
	Ports     []Port     `json:"ports"`
}

type Internal struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // class|iface
	Name    string   `json:"name"`
	Diff    string   `json:"diff,omitempty"`
	Members []Member `json:"members"`
}

type Member struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // method|prop
	Name string `json:"name"`
	Diff string `json:"diff,omitempty"`
}

type Port struct {
	ID   string `json:"id"`
	Side string `json:"side"` // left|right
	Kind string `json:"kind"` // in|out
	Name string `json:"name"`
	Diff string `json:"diff,omitempty"`
}

type Edge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	FromPort string `json:"fromPort"`
	ToPort   string `json:"toPort"`
	Label    string `json:"label"`
	Diff     string `json:"diff,omitempty"`
}

type Comment struct {
	ID     string        `json:"id"`
	Target CommentTarget `json:"target"`
	Body   string        `json:"body"`
}

type CommentTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Project transforms archai's domain model + overlay + diff into a UIGraph.
// This is a pure function: no I/O, no globals.
//
// Mapping rules:
//   - boundedContexts: from cfg.BoundedContexts if non-nil; else derive from
//     package Layer; else a single {id:"all", name:"All"}.
//   - one Component per PackageModel: id=pkg.Path, name=last path segment,
//     tech="Go", desc=first line of doc.
//   - internals: each InterfaceDef (kind:"iface") and StructDef (kind:"class");
//     id = pkg.Path + "." + Name.
//   - members: each method (kind:"method", name="Name()") and field
//     (kind:"prop", name="Name : Type").
//   - ports: one "in" port per exported interface (side:"left"), one "out"
//     port per distinct outbound dependency target (side:"right").
//   - edges: one per dependency between two packages present in the model.
//   - diff: for each diff.Change, parseChangePath(change.Path) -> stamp
//     diffWord(op) on the matching component/internal/member.
//   - When d == nil, emit no diff flags and PR == nil.
func Project(models []domain.PackageModel, cfg *overlay.Config, d *diff.Diff) (UIGraph, error) {
	g := UIGraph{
		Schema:          Schema,
		BoundedContexts: []BoundedContext{},
		Components:      []Component{},
		Edges:           []Edge{},
		Comments:        []Comment{},
	}

	// Build bounded contexts
	g.BoundedContexts = buildBoundedContexts(models, cfg)

	// Build component lookup for edges
	pkgSet := make(map[string]bool)
	for _, m := range models {
		pkgSet[m.Path] = true
	}

	// Build diff lookup: path -> op string
	diffMap := buildDiffMap(d)

	// Build components
	for _, m := range models {
		comp := buildComponent(m, cfg, diffMap)
		g.Components = append(g.Components, comp)
	}

	// Build edges from dependencies
	g.Edges = buildEdges(models, pkgSet, diffMap)

	// Build PR if diff is non-empty
	if d != nil && !d.IsEmpty() {
		g.PR = buildPR(d)
	}

	return g, nil
}

func buildBoundedContexts(models []domain.PackageModel, cfg *overlay.Config) []BoundedContext {
	var bcs []BoundedContext

	if cfg != nil && len(cfg.BoundedContexts) > 0 {
		// Use overlay bounded contexts
		for id, bc := range cfg.BoundedContexts {
			name := bc.Name
			if name == "" {
				name = id
			}
			bcs = append(bcs, BoundedContext{ID: id, Name: name})
		}
		// Sort for determinism
		sort.Slice(bcs, func(i, j int) bool { return bcs[i].ID < bcs[j].ID })
		return bcs
	}

	// Derive from package layers
	layerSet := make(map[string]bool)
	for _, m := range models {
		if m.Layer != "" {
			layerSet[m.Layer] = true
		}
	}

	if len(layerSet) > 0 {
		var layers []string
		for l := range layerSet {
			layers = append(layers, l)
		}
		sort.Strings(layers)
		for _, l := range layers {
			bcs = append(bcs, BoundedContext{ID: l, Name: l})
		}
		return bcs
	}

	// Default single BC
	return []BoundedContext{{ID: "all", Name: "All"}}
}

func buildComponent(m domain.PackageModel, cfg *overlay.Config, diffMap map[string]string) Component {
	comp := Component{
		ID:        m.Path,
		Name:      path.Base(m.Path),
		Tech:      "Go",
		Desc:      firstLine(m.Interfaces, m.Structs),
		BC:        resolveBC(m, cfg),
		Internals: []Internal{},
		Ports:     []Port{},
	}

	// Apply diff at component level (package changes)
	if op, ok := diffMap[m.Path]; ok {
		comp.Diff = op
	}

	// Build internals from interfaces
	for _, iface := range m.Interfaces {
		internal := Internal{
			ID:      m.Path + "." + iface.Name,
			Kind:    "iface",
			Name:    iface.Name,
			Members: []Member{},
		}
		// Apply diff at internal level
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		// Build members from methods
		for _, method := range iface.Methods {
			member := Member{
				ID:   internal.ID + "." + method.Name,
				Kind: "method",
				Name: method.Name + "()",
			}
			// Apply diff at member level
			if op, ok := diffMap[member.ID]; ok {
				member.Diff = op
			}
			internal.Members = append(internal.Members, member)
		}
		comp.Internals = append(comp.Internals, internal)

		// Create "in" port for exported interfaces
		if iface.IsExported {
			port := Port{
				ID:   m.Path + ":in:" + iface.Name,
				Side: "left",
				Kind: "in",
				Name: iface.Name,
			}
			comp.Ports = append(comp.Ports, port)
		}
	}

	// Build internals from structs
	for _, s := range m.Structs {
		internal := Internal{
			ID:      m.Path + "." + s.Name,
			Kind:    "class",
			Name:    s.Name,
			Members: []Member{},
		}
		// Apply diff at internal level
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		// Build members from fields
		for _, f := range s.Fields {
			member := Member{
				ID:   internal.ID + "." + f.Name,
				Kind: "prop",
				Name: f.Name + " : " + f.Type.String(),
			}
			// Apply diff at member level
			if op, ok := diffMap[member.ID]; ok {
				member.Diff = op
			}
			internal.Members = append(internal.Members, member)
		}
		// Build members from struct methods
		for _, method := range s.Methods {
			member := Member{
				ID:   internal.ID + "." + method.Name,
				Kind: "method",
				Name: method.Name + "()",
			}
			if op, ok := diffMap[member.ID]; ok {
				member.Diff = op
			}
			internal.Members = append(internal.Members, member)
		}
		comp.Internals = append(comp.Internals, internal)
	}

	// Build "out" ports from dependencies
	outTargets := make(map[string]bool)
	for _, dep := range m.Dependencies {
		if dep.To.Package != "" && dep.To.Package != m.Path {
			outTargets[dep.To.Package] = true
		}
	}
	var targets []string
	for t := range outTargets {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		port := Port{
			ID:   m.Path + ":out:" + t,
			Side: "right",
			Kind: "out",
			Name: "use " + path.Base(t),
		}
		comp.Ports = append(comp.Ports, port)
	}

	return comp
}

func buildEdges(models []domain.PackageModel, pkgSet map[string]bool, diffMap map[string]string) []Edge {
	// Collect unique edges
	edgeMap := make(map[string]Edge)

	for _, m := range models {
		for _, dep := range m.Dependencies {
			targetPkg := dep.To.Package
			// Only include edges to packages in our model
			if targetPkg == "" || targetPkg == m.Path || !pkgSet[targetPkg] {
				continue
			}

			edgeID := "e:" + m.Path + "->" + targetPkg
			if _, exists := edgeMap[edgeID]; !exists {
				edge := Edge{
					ID:       edgeID,
					From:     m.Path,
					To:       targetPkg,
					FromPort: m.Path + ":out:" + targetPkg,
					ToPort:   targetPkg + ":in:...",
					Label:    string(dep.Kind),
				}
				edgeMap[edgeID] = edge
			}
		}
	}

	// Convert to slice and sort for determinism
	var edges []Edge
	for _, e := range edgeMap {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return edges
}

func buildDiffMap(d *diff.Diff) map[string]string {
	m := make(map[string]string)
	if d == nil {
		return m
	}
	for _, c := range d.Changes {
		word := diffWord(string(c.Op))
		if word != "" {
			m[c.Path] = word
		}
	}
	return m
}

func buildPR(d *diff.Diff) *PR {
	stats := Stats{}
	for _, c := range d.Changes {
		switch c.Op {
		case diff.OpAdd:
			stats.Added++
		case diff.OpRemove:
			stats.Removed++
		case diff.OpChange:
			stats.Changed++
		}
	}
	return &PR{
		Title:   "Architecture Changes",
		Branch:  "",
		Agent:   "",
		Summary: "",
		Stats:   stats,
	}
}

func resolveBC(m domain.PackageModel, cfg *overlay.Config) string {
	if cfg != nil && len(cfg.BoundedContexts) > 0 {
		// Try to find a BC that contains this package's aggregate
		for id, bc := range cfg.BoundedContexts {
			for _, aggName := range bc.Aggregates {
				if agg, ok := cfg.Aggregates[aggName]; ok {
					// Check if the aggregate root is in this package
					if strings.HasPrefix(agg.Root, m.Path+".") {
						return id
					}
				}
			}
		}
	}
	// Fall back to layer if set
	if m.Layer != "" {
		return m.Layer
	}
	return "all"
}

func firstLine(ifaces []domain.InterfaceDef, structs []domain.StructDef) string {
	// Try interfaces first
	for _, iface := range ifaces {
		if iface.Doc != "" {
			lines := strings.SplitN(iface.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	// Try structs
	for _, s := range structs {
		if s.Doc != "" {
			lines := strings.SplitN(s.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	return ""
}
