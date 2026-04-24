package http

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	nethttp "net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/target"
)

// diffPageData is the template model for the Diff view.
type diffPageData struct {
	pageData
	ActiveTarget string
	HasActive    bool
	Targets      []target.TargetMeta
	// Error is set when the diff could not be computed (no target,
	// missing snapshot, read failure, etc.). When non-empty, the
	// template shows the banner instead of the diff body.
	Error   string
	Summary diffSummary
	Kinds   []kindFilter
	Groups  []diffGroup
	// Filter is the currently-selected kind filter ("" = all).
	Filter string
}

// comparePageData is the template model for the cross-target compare
// fragment served at /targets/compare.
type comparePageData struct {
	A       string
	B       string
	Error   string
	Summary diffSummary
	Kinds   []kindFilter
	Groups  []diffGroup
	Filter  string
}

// targetsPageData is the template model for the Targets view.
type targetsPageData struct {
	pageData
	ActiveTarget string
	HasActive    bool
	Targets      []target.TargetMeta
	// Error, if non-empty, is rendered as a banner at the top of the
	// page (e.g. reading .arch/targets/ failed).
	Error string
}

// diffSummary holds aggregate counts plus per-kind counts for the
// sidebar filter.
type diffSummary struct {
	Additions int
	Removals  int
	Changes   int
	Total     int
}

// kindFilter represents one entry in the kind-filter sidebar. Count is
// the number of changes of that kind in the current diff (used to hide
// entirely-empty kinds and to show badges).
type kindFilter struct {
	Kind     string
	Label    string
	Count    int
	Selected bool
}

// diffGroup is a contiguous group of changes sharing the same Op,
// ordered as they will appear in the rendered view. We keep Op-grouping
// at the top level (adds, then changes, then removes) so the viewer can
// scan by semantic category.
type diffGroup struct {
	Op      string // "add" | "remove" | "change"
	Label   string // "Additions" | "Removals" | "Changes"
	CSSTag  string // "added" | "removed" | "changed" (matches CSS)
	Entries []diffEntry
}

// diffEntry is a single change rendered in the diff view.
type diffEntry struct {
	Op     string
	Kind   string
	Path   string
	Before string
	After  string
}

// kindOrder fixes the display order of kind filters. Every kind
// referenced in internal/diff must appear here so the sidebar is stable
// even when a kind has zero changes.
var kindOrder = []struct {
	Kind  diff.Kind
	Label string
}{
	{diff.KindPackage, "Packages"},
	{diff.KindInterface, "Interfaces"},
	{diff.KindStruct, "Structs"},
	{diff.KindFunction, "Functions"},
	{diff.KindMethod, "Methods"},
	{diff.KindField, "Fields"},
	{diff.KindTypeDef, "TypeDefs"},
	{diff.KindConst, "Constants"},
	{diff.KindVar, "Variables"},
	{diff.KindError, "Errors"},
	{diff.KindDep, "Dependencies"},
	{diff.KindLayerRule, "Layer rules"},
}

// opOrder fixes the order of change groups in the rendered view.
var opOrder = []struct {
	Op    diff.Op
	Label string
	CSS   string
}{
	{diff.OpAdd, "Additions", "added"},
	{diff.OpChange, "Changes", "changed"},
	{diff.OpRemove, "Removals", "removed"},
}

// registerDiffTargetsRoutes installs the handlers added by M7e. Kept as
// a separate function so handlers.go's routes() stays uncluttered.
func (s *Server) registerDiffTargetsRoutes(mux *nethttp.ServeMux) {
	// Replace the placeholder pageHandler registrations with the real
	// M7e handlers. net/http's ServeMux panics on duplicate
	// registration so the caller must ensure this runs instead of the
	// old pageHandler("diff.html"/"targets.html") lines.
	mux.HandleFunc("/diff", s.handleDiff)
	mux.HandleFunc("/targets", s.handleTargets)
	mux.HandleFunc("/targets/use", s.handleTargetsUse)
	mux.HandleFunc("/targets/compare", s.handleTargetsCompare)
}

// handleDiff renders the Diff page: current code vs active target,
// filtered by kind when ?kind=... is set. Without HX-Request, it
// returns the full page; with HX-Request it returns only the diff
// fragment (no nav, no layout).
func (s *Server) handleDiff(w nethttp.ResponseWriter, r *nethttp.Request) {
	ctx := r.Context()
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	filter := r.URL.Query().Get("kind")

	data := diffPageData{
		pageData:     s.basePageData(r, "Diff", "/diff"),
		ActiveTarget: snap.CurrentTarget,
		HasActive:    snap.CurrentTarget != "",
		Filter:       filter,
	}

	// Always populate the target list — used both by the header select
	// box and the "switch target" link when no CURRENT is set.
	metas, err := target.List(snap.Root)
	if err != nil {
		data.Error = fmt.Sprintf("listing targets: %v", err)
		s.renderDiff(w, r, data)
		return
	}
	data.Targets = metas

	if snap.CurrentTarget == "" {
		data.Error = "No active target. Lock one with `archai target lock <id>` or pick one from the Targets view."
		s.renderDiff(w, r, data)
		return
	}

	current, tgt, err := loadDiffSides(ctx, snap.Root, snap.CurrentTarget)
	if err != nil {
		data.Error = err.Error()
		s.renderDiff(w, r, data)
		return
	}

	d := diff.Compute(current, tgt)
	data.Summary = summarize(d)
	data.Kinds = buildKindFilters(d, filter)
	data.Groups = buildGroups(d, filter)
	s.renderDiff(w, r, data)
}

// renderDiff picks full page vs fragment based on HX-Request.
func (s *Server) renderDiff(w nethttp.ResponseWriter, r *nethttp.Request, data diffPageData) {
	if isHTMX(r) {
		s.renderFragment(w, "diff.html", "diff-fragment", data)
		return
	}
	s.renderPage(w, "diff.html", data)
}

// handleTargets renders the Targets list page.
func (s *Server) handleTargets(w nethttp.ResponseWriter, r *nethttp.Request) {
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	data := targetsPageData{
		pageData:     s.basePageData(r, "Targets", "/targets"),
		ActiveTarget: snap.CurrentTarget,
		HasActive:    snap.CurrentTarget != "",
	}

	metas, err := target.List(snap.Root)
	if err != nil {
		data.Error = fmt.Sprintf("listing targets: %v", err)
		s.renderPage(w, "targets.html", data)
		return
	}
	data.Targets = metas

	if isHTMX(r) {
		s.renderFragment(w, "targets.html", "targets-fragment", data)
		return
	}
	s.renderPage(w, "targets.html", data)
}

// handleTargetsUse handles POST /targets/use?id=<id>. It validates the
// id exists on disk, persists it to .arch/targets/CURRENT, updates the
// in-memory state, and returns the refreshed targets list fragment
// (HTMX) or redirects to /targets for non-HTMX callers.
func (s *Server) handleTargetsUse(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodPost {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		nethttp.Error(w, "parse form: "+err.Error(), nethttp.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}
	if id == "" {
		nethttp.Error(w, "missing id", nethttp.StatusBadRequest)
		return
	}

	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	root := state.Root()
	if err := target.Use(root, id); err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusBadRequest)
		return
	}
	if err := state.SwitchTarget(id); err != nil {
		nethttp.Error(w, err.Error(), nethttp.StatusInternalServerError)
		return
	}

	// After a switch, re-render the targets fragment so HTMX can swap
	// the whole list (active indicator moves). Non-HTMX POSTs redirect
	// back to the list.
	if !isHTMX(r) {
		nethttp.Redirect(w, r, s.navPrefix(r)+"/targets", nethttp.StatusSeeOther)
		return
	}
	// Delegate to handleTargets so we don't duplicate snapshot logic.
	// We explicitly force a GET for the downstream render by rewriting
	// the method on a cloned request.
	getReq := r.Clone(r.Context())
	getReq.Method = nethttp.MethodGet
	s.handleTargets(w, getReq)
}

// handleTargetsCompare handles GET /targets/compare?a=<id>&b=<id>&kind=<kind>.
// It returns a fragment with a cross-target diff. Used by the compare
// form on the Targets view (HTMX) and also works as a plain page when
// loaded directly.
func (s *Server) handleTargetsCompare(w nethttp.ResponseWriter, r *nethttp.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	a := q.Get("a")
	b := q.Get("b")
	filter := q.Get("kind")

	data := comparePageData{
		A:      a,
		B:      b,
		Filter: filter,
	}
	if a == "" || b == "" {
		data.Error = "both a and b must be provided"
		s.renderFragment(w, "targets.html", "compare-fragment", data)
		return
	}

	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	root := state.Root()
	aModel, err := loadTargetFromDisk(ctx, root, a)
	if err != nil {
		data.Error = fmt.Sprintf("loading %q: %v", a, err)
		s.renderFragment(w, "targets.html", "compare-fragment", data)
		return
	}
	bModel, err := loadTargetFromDisk(ctx, root, b)
	if err != nil {
		data.Error = fmt.Sprintf("loading %q: %v", b, err)
		s.renderFragment(w, "targets.html", "compare-fragment", data)
		return
	}

	d := diff.Compute(aModel, bModel)
	data.Summary = summarize(d)
	data.Kinds = buildKindFilters(d, filter)
	data.Groups = buildGroups(d, filter)
	s.renderFragment(w, "targets.html", "compare-fragment", data)
}

// --- helpers ---

// isHTMX reports whether the request was issued by HTMX (carries the
// HX-Request header). We use this to decide between full page renders
// and partial fragment renders.
func isHTMX(r *nethttp.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// summarize tallies add / remove / change counts for the header strip.
func summarize(d *diff.Diff) diffSummary {
	var s diffSummary
	if d == nil {
		return s
	}
	for _, c := range d.Changes {
		switch c.Op {
		case diff.OpAdd:
			s.Additions++
		case diff.OpRemove:
			s.Removals++
		case diff.OpChange:
			s.Changes++
		}
	}
	s.Total = s.Additions + s.Removals + s.Changes
	return s
}

// buildKindFilters produces the sidebar filter list for d, marking the
// currently-selected entry. The "All" pseudo-filter is injected first.
func buildKindFilters(d *diff.Diff, selected string) []kindFilter {
	counts := make(map[diff.Kind]int, len(kindOrder))
	total := 0
	if d != nil {
		for _, c := range d.Changes {
			counts[c.Kind]++
			total++
		}
	}

	out := make([]kindFilter, 0, len(kindOrder)+1)
	out = append(out, kindFilter{
		Kind:     "",
		Label:    "All",
		Count:    total,
		Selected: selected == "",
	})
	for _, k := range kindOrder {
		out = append(out, kindFilter{
			Kind:     string(k.Kind),
			Label:    k.Label,
			Count:    counts[k.Kind],
			Selected: selected == string(k.Kind),
		})
	}
	return out
}

// buildGroups arranges d's changes into ordered op-groups suitable for
// the template. When filter is non-empty, only changes whose Kind
// matches are included.
func buildGroups(d *diff.Diff, filter string) []diffGroup {
	if d == nil {
		return nil
	}
	byOp := make(map[diff.Op][]diffEntry, 3)
	for _, c := range d.Changes {
		if filter != "" && string(c.Kind) != filter {
			continue
		}
		byOp[c.Op] = append(byOp[c.Op], diffEntry{
			Op:     string(c.Op),
			Kind:   string(c.Kind),
			Path:   c.Path,
			Before: formatSide(c.Before),
			After:  formatSide(c.After),
		})
	}

	// Stable sort entries inside each group by path for deterministic
	// output (Compute already sorts, but filtering preserves only that
	// relative order; re-sorting here is cheap and defensive).
	for op := range byOp {
		sort.SliceStable(byOp[op], func(i, j int) bool {
			return byOp[op][i].Path < byOp[op][j].Path
		})
	}

	out := make([]diffGroup, 0, len(opOrder))
	for _, o := range opOrder {
		entries := byOp[o.Op]
		if len(entries) == 0 {
			continue
		}
		out = append(out, diffGroup{
			Op:      string(o.Op),
			Label:   o.Label,
			CSSTag:  o.CSS,
			Entries: entries,
		})
	}
	return out
}

// formatSide renders Before/After values as a short human-readable
// string. The underlying values are heterogeneous (domain types,
// map[string]any for package summaries, nil), so we use fmt %v and
// trim; the primary information in the UI is the path + op + kind.
func formatSide(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	// Keep tooltips/cells readable.
	const max = 240
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// loadDiffSides loads current + target models for the Diff page. The
// "current" side prefers per-package .arch/*.yaml specs if available,
// falling back to nil (empty model) — the HTTP daemon should not invoke
// the Go parser on a live user project as part of a page render.
func loadDiffSides(ctx context.Context, root, targetID string) ([]domain.PackageModel, []domain.PackageModel, error) {
	current, err := loadCurrentFromDisk(ctx, root)
	if err != nil {
		return nil, nil, fmt.Errorf("loading current model: %w", err)
	}
	tgt, err := loadTargetFromDisk(ctx, root, targetID)
	if err != nil {
		return nil, nil, fmt.Errorf("loading target %q: %w", targetID, err)
	}
	return current, tgt, nil
}

// loadCurrentFromDisk returns the current project model from per-package
// .arch/*.yaml specs. Returns an empty slice (not an error) when the
// project has no yaml specs yet — the diff view will then show
// everything in the target as "add".
func loadCurrentFromDisk(ctx context.Context, root string) ([]domain.PackageModel, error) {
	files, err := findProjectYAMLSpecs(root)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	return yamlAdapter.NewReader().Read(ctx, files)
}

// loadTargetFromDisk loads a locked target's frozen model.
func loadTargetFromDisk(ctx context.Context, root, id string) ([]domain.PackageModel, error) {
	if id == "" {
		return nil, errors.New("empty target id")
	}
	meta, _, err := target.Show(root, id)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("target %q missing meta", id)
	}

	// Walk the target's model/ tree for YAML files and hand them to the
	// YAML reader. We intentionally duplicate the walker (rather than
	// export the CLI helper) because adapter packages must not import
	// cmd/.
	modelDir := filepath.Join(root, ".arch", "targets", id, "model")
	files, err := walkYAML(modelDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("target %q has no model files", id)
	}
	return yamlAdapter.NewReader().Read(ctx, files)
}

// findProjectYAMLSpecs locates per-package .arch/*.yaml files, skipping
// the .arch/targets/ subtree. Mirrors the CLI helper in cmd/archai but
// lives here to keep adapter/http free of a cmd/ dependency.
func findProjectYAMLSpecs(root string) ([]string, error) {
	targetsTree := filepath.Join(root, ".arch", "targets")
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == targetsTree || strings.HasPrefix(path, targetsTree+string(os.PathSeparator)) {
				return filepath.SkipDir
			}
			name := d.Name()
			if path != root && strings.HasPrefix(name, ".") && name != ".arch" {
				return filepath.SkipDir
			}
			return nil
		}
		if !hasYAMLExt(path) {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) != ".arch" {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// walkYAML returns every .yaml/.yml file under root. Returns an empty
// slice (not an error) if root does not exist.
func walkYAML(root string) ([]string, error) {
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if hasYAMLExt(path) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func hasYAMLExt(p string) bool {
	e := filepath.Ext(p)
	return e == ".yaml" || e == ".yml"
}
