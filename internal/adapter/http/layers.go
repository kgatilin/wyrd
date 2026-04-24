package http

import (
	"fmt"
	nethttp "net/http"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// layersData is the model passed to layers.html. When the project has
// no overlay Overlay=false and the template renders an empty-state
// explanation instead of the diagram.
type layersData struct {
	pageData

	HasOverlay bool
	Module     string

	Layers          []layerView // one entry per layer in lexical order
	Violations      []edgeView  // forbidden cross-layer edges (deduplicated)
	AllowedEdges    []edgeView  // allowed cross-layer edges observed in code
	DeclaredEdges   []edgeView  // declared-but-unused edges from LayerRules
	ViolationsCount int         // total package-pairs with violations
}

// layerView describes one layer on the Layers page.
type layerView struct {
	Name     string
	Packages []layerPackage // packages in this layer, sorted by relative path
}

// layerPackage represents a package inside a layer. Rel is the
// module-relative path (used as anchor target); Name is the trailing
// component to show as the link label.
type layerPackage struct {
	Rel  string
	Name string
}

// edgeView describes a directed edge between two layers. Color is
// "green" for allowed edges and "red" for violations so the template
// can style them without branching on meaning.
type edgeView struct {
	From    string
	To      string
	Color   string
	Details []string // human-readable "pkg -> pkg" details backing the edge
}

// handleLayers renders the Layers page. It reads a snapshot of the
// state and derives layer membership, allowed/violating edges, and a
// D2 diagram from the overlay config.
func (s *Server) handleLayers(w nethttp.ResponseWriter, r *nethttp.Request) {
	snap := s.state.Snapshot()
	data := layersData{
		pageData: pageData{
			Title:      "Layers",
			ActivePath: "/layers",
			NavItems:   buildNav("/layers"),
		},
	}

	if snap.Overlay == nil || len(snap.Overlay.Layers) == 0 {
		s.renderPage(w, "layers.html", data)
		return
	}

	data.HasOverlay = true
	data.Module = snap.Overlay.Module
	data.Layers = buildLayerViews(snap.Overlay, snap.Packages)
	data.Violations, data.AllowedEdges, data.DeclaredEdges = buildLayerEdges(snap.Overlay, snap.Packages)
	for _, v := range data.Violations {
		data.ViolationsCount += len(v.Details)
	}

	// M8 (#46): the Layer map is rendered client-side from /api/layers,
	// so we no longer emit a server-side D2 → SVG render for the display
	// diagram. The D2 source is still available via /view/layers/d2 for
	// export and the `archai sequence` CLI path is unchanged.
	s.renderPage(w, "layers.html", data)
}

// buildLayerViews constructs one layerView per layer, with packages
// assigned to that layer via overlay globs. The layer ordering is
// lexical so reloads produce stable HTML.
func buildLayerViews(cfg *overlay.Config, packages []domain.PackageModel) []layerView {
	layerNames := make([]string, 0, len(cfg.Layers))
	for name := range cfg.Layers {
		layerNames = append(layerNames, name)
	}
	sort.Strings(layerNames)

	// Assign each package to the first layer whose glob it matches
	// (same rule as overlay.Merge, kept local to avoid mutating the
	// caller's models slice just to read membership).
	pkgLayer := computePackageLayers(cfg, packages)

	views := make([]layerView, 0, len(layerNames))
	for _, name := range layerNames {
		var pkgs []layerPackage
		for _, p := range packages {
			rel := moduleRel(cfg.Module, p.Path)
			if pkgLayer[p.Path] != name {
				continue
			}
			pkgs = append(pkgs, layerPackage{
				Rel:  rel,
				Name: shortName(rel),
			})
		}
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Rel < pkgs[j].Rel })
		views = append(views, layerView{Name: name, Packages: pkgs})
	}
	return views
}

// buildLayerEdges derives three edge groups for the Layers page:
//   - violations: cross-layer edges observed in code that LayerRules forbid.
//   - allowed:    cross-layer edges observed in code that LayerRules allow.
//   - declared:   edges declared in LayerRules but not observed in code yet.
func buildLayerEdges(cfg *overlay.Config, packages []domain.PackageModel) (violations, allowed, declared []edgeView) {
	pkgLayer := computePackageLayers(cfg, packages)

	allowSet := make(map[string]map[string]struct{}, len(cfg.LayerRules))
	for src, targets := range cfg.LayerRules {
		inner := make(map[string]struct{}, len(targets))
		for _, t := range targets {
			inner[t] = struct{}{}
		}
		allowSet[src] = inner
	}

	type edgeKey struct{ from, to string }
	type edgeAcc struct {
		details map[string]struct{}
	}
	addDetail := func(m map[edgeKey]*edgeAcc, k edgeKey, detail string) {
		if _, ok := m[k]; !ok {
			m[k] = &edgeAcc{details: make(map[string]struct{})}
		}
		m[k].details[detail] = struct{}{}
	}

	violationAcc := make(map[edgeKey]*edgeAcc)
	allowedAcc := make(map[edgeKey]*edgeAcc)

	for _, p := range packages {
		fromLayer := pkgLayer[p.Path]
		if fromLayer == "" {
			continue
		}
		for _, dep := range p.Dependencies {
			if dep.To.External {
				continue
			}
			// Normalize the import target to a module-relative path so
			// it lines up with PackageModel.Path (which is how pkgLayer
			// is keyed). Dependencies from the Go reader are
			// fully-qualified; from the YAML reader they may already
			// be relative — the helper handles both.
			toRel := moduleRel(cfg.Module, dep.To.Package)
			if toRel == p.Path {
				continue
			}
			toLayer, ok := pkgLayer[toRel]
			if !ok || toLayer == "" || toLayer == fromLayer {
				continue
			}
			key := edgeKey{from: fromLayer, to: toLayer}
			detail := fmt.Sprintf("%s -> %s", p.Path, toRel)

			rules, hasRule := allowSet[fromLayer]
			if !hasRule {
				// No outbound rules declared: every cross-layer dep is a violation.
				addDetail(violationAcc, key, detail)
				continue
			}
			if _, ok := rules[toLayer]; ok {
				addDetail(allowedAcc, key, detail)
				continue
			}
			addDetail(violationAcc, key, detail)
		}
	}

	// Declared-but-unused edges: every LayerRules entry that wasn't
	// observed as "allowed" in code.
	declaredSeen := make(map[edgeKey]struct{})
	for src, targets := range cfg.LayerRules {
		for _, t := range targets {
			k := edgeKey{from: src, to: t}
			if _, obs := allowedAcc[k]; obs {
				continue
			}
			declaredSeen[k] = struct{}{}
		}
	}

	flatten := func(m map[edgeKey]*edgeAcc, color string) []edgeView {
		out := make([]edgeView, 0, len(m))
		for k, v := range m {
			details := make([]string, 0, len(v.details))
			for d := range v.details {
				details = append(details, d)
			}
			sort.Strings(details)
			out = append(out, edgeView{
				From:    k.from,
				To:      k.to,
				Color:   color,
				Details: details,
			})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].From != out[j].From {
				return out[i].From < out[j].From
			}
			return out[i].To < out[j].To
		})
		return out
	}

	violations = flatten(violationAcc, "red")
	allowed = flatten(allowedAcc, "green")

	declared = make([]edgeView, 0, len(declaredSeen))
	for k := range declaredSeen {
		declared = append(declared, edgeView{
			From:  k.from,
			To:    k.to,
			Color: "gray",
		})
	}
	sort.Slice(declared, func(i, j int) bool {
		if declared[i].From != declared[j].From {
			return declared[i].From < declared[j].From
		}
		return declared[i].To < declared[j].To
	})
	return violations, allowed, declared
}

// buildLayerMapD2 assembles a compact D2 source for the layer map.
// When detailed=true each layer node shows a package count and inter-
// layer edges are annotated with their kind (allowed / violation).
// When detailed=false only layers + allowed edges are drawn, suitable
// for the small dashboard preview.
func buildLayerMapD2(cfg *overlay.Config, packages []domain.PackageModel, detailed bool) string {
	var b strings.Builder

	pkgLayer := computePackageLayers(cfg, packages)
	pkgCount := make(map[string]int)
	for _, p := range packages {
		if l := pkgLayer[p.Path]; l != "" {
			pkgCount[l]++
		}
	}

	layerNames := make([]string, 0, len(cfg.Layers))
	for name := range cfg.Layers {
		layerNames = append(layerNames, name)
	}
	sort.Strings(layerNames)

	for _, name := range layerNames {
		id := d2ID(name)
		label := name
		if detailed {
			label = fmt.Sprintf("%s (%d)", name, pkgCount[name])
		}
		fmt.Fprintf(&b, "%s: %q\n", id, label)
	}

	violations, allowed, declared := buildLayerEdges(cfg, packages)

	// Declared (gray dashed) — drawn first so code-backed edges layer on top.
	if detailed {
		for _, e := range declared {
			fmt.Fprintf(&b, "%s -> %s: {style.stroke: \"#9ca3af\"; style.stroke-dash: 3}\n",
				d2ID(e.From), d2ID(e.To))
		}
	}
	// Allowed (green).
	for _, e := range allowed {
		fmt.Fprintf(&b, "%s -> %s: {style.stroke: \"#16a34a\"}\n", d2ID(e.From), d2ID(e.To))
	}
	// Violations (red). Only drawn in detailed view so the dashboard
	// preview stays clean; the full Layers page is where violations live.
	if detailed {
		for _, e := range violations {
			fmt.Fprintf(&b, "%s -> %s: violation {style.stroke: \"#dc2626\"; style.stroke-width: 2}\n",
				d2ID(e.From), d2ID(e.To))
		}
	}

	return b.String()
}

// computePackageLayers returns a map from PackageModel.Path to the
// layer name assigned by the overlay. Packages that match no layer
// are absent from the map.
func computePackageLayers(cfg *overlay.Config, packages []domain.PackageModel) map[string]string {
	out := make(map[string]string, len(packages))
	layerNames := make([]string, 0, len(cfg.Layers))
	for name := range cfg.Layers {
		layerNames = append(layerNames, name)
	}
	sort.Strings(layerNames)

	for _, p := range packages {
		rel := moduleRel(cfg.Module, p.Path)
		for _, layer := range layerNames {
			if matchLayerGlobs(cfg.Layers[layer], rel) {
				out[p.Path] = layer
				break
			}
		}
	}
	return out
}

// matchLayerGlobs returns true when any of globs matches pkgPath.
// Duplicates overlay.matchGlob semantics (trailing "..." = recursive
// prefix; trailing "*" = single-segment wildcard; else exact).
// Kept local so the http adapter stays decoupled from overlay internals.
func matchLayerGlobs(globs []string, pkgPath string) bool {
	for _, g := range globs {
		if g == "..." {
			return true
		}
		if strings.HasSuffix(g, "/...") {
			prefix := strings.TrimSuffix(g, "/...")
			if pkgPath == prefix || strings.HasPrefix(pkgPath, prefix+"/") {
				return true
			}
			continue
		}
		if strings.HasSuffix(g, "/*") {
			prefix := strings.TrimSuffix(g, "/*")
			if !strings.HasPrefix(pkgPath, prefix+"/") {
				continue
			}
			rest := pkgPath[len(prefix)+1:]
			if rest != "" && !strings.Contains(rest, "/") {
				return true
			}
			continue
		}
		if g == pkgPath {
			return true
		}
	}
	return false
}

// moduleRel strips the module prefix from a fully-qualified package
// path. An input without the module prefix is returned unchanged,
// which is the format used throughout the archai model.
func moduleRel(module, pkgPath string) string {
	if module == "" {
		return pkgPath
	}
	if pkgPath == module {
		return ""
	}
	if strings.HasPrefix(pkgPath, module+"/") {
		return strings.TrimPrefix(pkgPath, module+"/")
	}
	return pkgPath
}

// shortName returns the trailing path component of rel, suitable as a
// human-readable label in the layer view.
func shortName(rel string) string {
	if rel == "" {
		return "."
	}
	if idx := strings.LastIndex(rel, "/"); idx >= 0 {
		return rel[idx+1:]
	}
	return rel
}

// d2ID returns a D2-safe node id for s. D2 accepts most unicode in ids
// but chokes on spaces, dashes at the start, and reserved punctuation
// — a simple alphanumeric-with-underscores encoding is safest.
func d2ID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
