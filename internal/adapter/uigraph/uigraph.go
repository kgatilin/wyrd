// Package uigraph projects archai's domain model + overlay + diff into the
// UIGraph JSON shape consumed by the POC review UI. Pure data + a pure
// projection function; no I/O, no behavior on the types.
package uigraph

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/publicapi"
)

const Schema = "archai.uigraph/v0"

type UIGraph struct {
	Schema             string            `json:"schema"`
	Repo               *Repo             `json:"repo,omitempty"`
	Worktrees          []Worktree        `json:"worktrees,omitempty"`
	PR                 *PR               `json:"pr,omitempty"`
	ReviewScopes       []ReviewScope     `json:"reviewScopes,omitempty"`
	ReviewViews        []ReviewView      `json:"reviewViews,omitempty"`
	ReviewGroupings    []ReviewGrouping  `json:"reviewGroupings,omitempty"`
	DefaultReviewView  string            `json:"defaultReviewView,omitempty"`
	DefaultReviewScope string            `json:"defaultReviewScope,omitempty"`
	DefaultGrouping    string            `json:"defaultGrouping,omitempty"`
	PolicyViolations   []PolicyViolation `json:"policyViolations,omitempty"`
	BoundedContexts    []BoundedContext  `json:"boundedContexts"`
	Components         []Component       `json:"components"`
	Edges              []Edge            `json:"edges"`
	Relations          []SymbolRelation  `json:"relations,omitempty"`
	Comments           []Comment         `json:"comments"`
}

type Repo struct {
	Root           string `json:"root,omitempty"`
	ActiveWorktree string `json:"activeWorktree,omitempty"`
	BaseRef        string `json:"baseRef,omitempty"`
	BaseWorktree   string `json:"baseWorktree,omitempty"`
	Compare        string `json:"compare,omitempty"`
}

type Worktree struct {
	Name    string `json:"name"`
	Branch  string `json:"branch,omitempty"`
	Head    string `json:"head,omitempty"`
	Current bool   `json:"current,omitempty"`
	Base    bool   `json:"base,omitempty"`
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

type ReviewScope struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ReviewView struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	DefaultScope     string   `json:"defaultScope"`
	DefaultExpansion string   `json:"defaultExpansion,omitempty"`
	GroupBy          string   `json:"groupBy,omitempty"`
	ComponentIDs     []string `json:"componentIds"`
	ComponentCount   int      `json:"componentCount"`
}

type ReviewGrouping struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Groups []ReviewGroup `json:"groups"`
}

type ReviewGroup struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	ComponentIDs   []string `json:"componentIds"`
	ComponentCount int      `json:"componentCount"`
}

type PolicyViolation struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	SourceComponentID string `json:"sourceComponentId"`
	TargetComponentID string `json:"targetComponentId"`
	SourceLayer       string `json:"sourceLayer,omitempty"`
	TargetLayer       string `json:"targetLayer,omitempty"`
	Message           string `json:"message"`
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
	ID         string   `json:"id"`
	Kind       string   `json:"kind"` // class|iface
	Name       string   `json:"name"`
	SourceFile string   `json:"sourceFile,omitempty"`
	Exported   bool     `json:"exported,omitempty"`
	Diff       string   `json:"diff,omitempty"`
	DiffBefore string   `json:"diffBefore,omitempty"`
	DiffAfter  string   `json:"diffAfter,omitempty"`
	Members    []Member `json:"members"`
}

type Member struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // method|prop
	Name       string `json:"name"`
	SourceFile string `json:"sourceFile,omitempty"`
	Exported   bool   `json:"exported,omitempty"`
	Diff       string `json:"diff,omitempty"`
	DiffBefore string `json:"diffBefore,omitempty"`
	DiffAfter  string `json:"diffAfter,omitempty"`
}

type Port struct {
	ID     string `json:"id"`
	Side   string `json:"side"` // left|right
	Kind   string `json:"kind"` // in|out
	Name   string `json:"name"`
	Public bool   `json:"public,omitempty"`
	Diff   string `json:"diff,omitempty"`
}

type Edge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	FromPort string `json:"fromPort"`
	ToPort   string `json:"toPort"`
	Label    string `json:"label"`
	Public   bool   `json:"public,omitempty"`
	Diff     string `json:"diff,omitempty"`
}

type SymbolRelation struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	FromComponentID string `json:"fromComponentId"`
	FromInternalID  string `json:"fromInternalId,omitempty"`
	FromMemberID    string `json:"fromMemberId,omitempty"`
	FromLabel       string `json:"fromLabel,omitempty"`
	ToComponentID   string `json:"toComponentId"`
	ToInternalID    string `json:"toInternalId,omitempty"`
	ToMemberID      string `json:"toMemberId,omitempty"`
	ToLabel         string `json:"toLabel,omitempty"`
	Public          bool   `json:"public,omitempty"`
	Diff            string `json:"diff,omitempty"`
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
	return ProjectWithPublicDiff(models, cfg, d, nil)
}

// ProjectWithPublicDiff enriches the coarse package-model diff with semantic
// public API changes. The public API diff carries member-level changes that the
// package-model diff intentionally collapses to the containing interface/struct.
func ProjectWithPublicDiff(
	models []domain.PackageModel,
	cfg *overlay.Config,
	d *diff.Diff,
	publicDiff *publicapi.Diff,
) (UIGraph, error) {
	models = append([]domain.PackageModel(nil), models...)
	if cfg != nil {
		merged, _, err := overlay.Merge(models, cfg)
		if err != nil {
			return UIGraph{}, err
		}
		models = merged
	}

	g := UIGraph{
		Schema:          Schema,
		ReviewScopes:    defaultReviewScopes(),
		BoundedContexts: []BoundedContext{},
		Components:      []Component{},
		Edges:           []Edge{},
		Relations:       []SymbolRelation{},
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
	legacyDiffs := buildLegacyDiffDetails(d)
	publicDiffMap := buildPublicDiffMap(publicDiff)
	publicIndex := publicapi.Project(models).Index()

	// Build components
	for _, m := range models {
		comp := buildComponent(m, cfg, diffMap, legacyDiffs, publicIndex, publicDiffMap)
		g.Components = append(g.Components, comp)
	}

	// Build edges from dependencies
	g.Edges = buildEdges(models, pkgSet, buildDependencyDiffMap(d), publicIndex)
	g.Relations = buildRelations(models, pkgSet, buildRelationDiffMap(d), publicIndex)
	g.PolicyViolations = buildPolicyViolations(models, cfg)

	g.ReviewViews = buildReviewViews(models, cfg)
	g.ReviewGroupings = buildReviewGroupings(models, cfg, g.ReviewViews, g.BoundedContexts)
	if len(g.ReviewViews) > 0 {
		defaultView := g.ReviewViews[0]
		for _, view := range g.ReviewViews {
			if view.ComponentCount > 0 {
				defaultView = view
				break
			}
		}
		g.DefaultReviewView = defaultView.ID
		g.DefaultReviewScope = defaultView.DefaultScope
		g.DefaultGrouping = defaultGroupingForView(defaultView, g.ReviewGroupings)
	}

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

func buildComponent(
	m domain.PackageModel,
	cfg *overlay.Config,
	diffMap map[string]string,
	legacyDiffs legacyDiffDetails,
	publicIndex publicapi.Index,
	publicDiffs map[string]publicDiffAnnotation,
) Component {
	comp := Component{
		ID:        m.Path,
		Name:      path.Base(m.Path),
		Tech:      "Go",
		Desc:      firstLine(m),
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
			ID:         m.Path + "." + iface.Name,
			Kind:       "iface",
			Name:       iface.Name,
			SourceFile: iface.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + iface.Name),
			Members:    []Member{},
		}
		// Apply diff at internal level
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
		// Build members from methods
		for _, method := range iface.Methods {
			member := Member{
				ID:         internal.ID + "." + method.Name,
				Kind:       "method",
				Name:       method.Name + "()",
				SourceFile: iface.SourceFile,
				Exported:   publicIndex.HasMemberID(internal.ID + "." + method.Name),
			}
			// Apply diff at member level
			if op, ok := diffMap[member.ID]; ok {
				member.Diff = op
			}
			applyLegacyDiffDetailToMember(&member, legacyDiffs)
			if op, ok := publicMemberDiff(publicDiffs, member.ID); ok {
				member.Diff = op
			}
			applyPublicDiffDetailToMember(&member, publicDiffs)
			internal.Members = append(internal.Members, member)
		}
		internal.Members = appendLegacyDiffMembers(internal.Members, internal.ID, iface.SourceFile, legacyDiffs)
		internal.Members = appendPublicDiffMembers(internal.Members, internal.ID, iface.SourceFile, publicDiffs)
		comp.Internals = append(comp.Internals, internal)

		// Create "in" port for exported interfaces
		if iface.IsExported {
			port := Port{
				ID:     m.Path + ":in:" + iface.Name,
				Side:   "left",
				Kind:   "in",
				Name:   iface.Name,
				Public: publicIndex.HasSymbolID(m.Path + "." + iface.Name),
			}
			comp.Ports = append(comp.Ports, port)
		}
	}

	// Build internals from structs
	for _, s := range m.Structs {
		internal := Internal{
			ID:         m.Path + "." + s.Name,
			Kind:       "class",
			Name:       s.Name,
			SourceFile: s.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + s.Name),
			Members:    []Member{},
		}
		// Apply diff at internal level
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
		// Build members from fields
		for _, f := range s.Fields {
			member := Member{
				ID:         internal.ID + "." + f.Name,
				Kind:       "prop",
				Name:       f.Name + " : " + f.Type.String(),
				SourceFile: s.SourceFile,
				Exported:   publicIndex.HasMemberID(internal.ID + "." + f.Name),
			}
			// Apply diff at member level
			if op, ok := diffMap[member.ID]; ok {
				member.Diff = op
			}
			applyLegacyDiffDetailToMember(&member, legacyDiffs)
			if op, ok := publicMemberDiff(publicDiffs, member.ID); ok {
				member.Diff = op
			}
			applyPublicDiffDetailToMember(&member, publicDiffs)
			internal.Members = append(internal.Members, member)
		}
		// Build members from struct methods
		for _, method := range s.Methods {
			member := Member{
				ID:         internal.ID + "." + method.Name,
				Kind:       "method",
				Name:       method.Name + "()",
				SourceFile: s.SourceFile,
				Exported:   publicIndex.HasMemberID(internal.ID + "." + method.Name),
			}
			if op, ok := diffMap[member.ID]; ok {
				member.Diff = op
			}
			applyLegacyDiffDetailToMember(&member, legacyDiffs)
			if op, ok := publicMemberDiff(publicDiffs, member.ID); ok {
				member.Diff = op
			}
			applyPublicDiffDetailToMember(&member, publicDiffs)
			internal.Members = append(internal.Members, member)
		}
		internal.Members = appendLegacyDiffMembers(internal.Members, internal.ID, s.SourceFile, legacyDiffs)
		internal.Members = appendPublicDiffMembers(internal.Members, internal.ID, s.SourceFile, publicDiffs)
		comp.Internals = append(comp.Internals, internal)
	}

	for _, fn := range m.Functions {
		internal := Internal{
			ID:         m.Path + "." + fn.Name,
			Kind:       "func",
			Name:       fn.Signature(),
			SourceFile: fn.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + fn.Name),
			Members:    []Member{},
		}
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
		comp.Internals = append(comp.Internals, internal)
	}

	for _, td := range m.TypeDefs {
		internal := Internal{
			ID:         m.Path + "." + td.Name,
			Kind:       "type",
			Name:       td.Name + " : " + td.UnderlyingType.String(),
			SourceFile: td.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + td.Name),
			Members:    []Member{},
		}
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
		for _, c := range td.Constants {
			member := Member{
				ID:         internal.ID + "." + c,
				Kind:       "const",
				Name:       c,
				SourceFile: td.SourceFile,
				Exported:   publicIndex.HasMemberID(internal.ID + "." + c),
			}
			if op, ok := publicMemberDiff(publicDiffs, member.ID); ok {
				member.Diff = op
			}
			applyLegacyDiffDetailToMember(&member, legacyDiffs)
			applyPublicDiffDetailToMember(&member, publicDiffs)
			internal.Members = append(internal.Members, member)
		}
		internal.Members = appendLegacyDiffMembers(internal.Members, internal.ID, td.SourceFile, legacyDiffs)
		internal.Members = appendPublicDiffMembers(internal.Members, internal.ID, td.SourceFile, publicDiffs)
		comp.Internals = append(comp.Internals, internal)
	}

	for _, c := range m.Constants {
		internal := Internal{
			ID:         m.Path + "." + c.Name,
			Kind:       "const",
			Name:       constDisplayName(c),
			SourceFile: c.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + c.Name),
			Members:    []Member{},
		}
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
		comp.Internals = append(comp.Internals, internal)
	}

	for _, v := range m.Variables {
		internal := Internal{
			ID:         m.Path + "." + v.Name,
			Kind:       "var",
			Name:       varDisplayName(v),
			SourceFile: v.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + v.Name),
			Members:    []Member{},
		}
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
		comp.Internals = append(comp.Internals, internal)
	}

	for _, e := range m.Errors {
		internal := Internal{
			ID:         m.Path + "." + e.Name,
			Kind:       "error",
			Name:       e.Name,
			SourceFile: e.SourceFile,
			Exported:   publicIndex.HasSymbolID(m.Path + "." + e.Name),
			Members:    []Member{},
		}
		if op, ok := diffMap[internal.ID]; ok {
			internal.Diff = op
		}
		applyLegacyDiffDetailToInternal(&internal, legacyDiffs)
		if op, ok := publicSymbolDiff(publicDiffs, internal.ID); ok {
			internal.Diff = op
		}
		applyPublicDiffDetailToInternal(&internal, publicDiffs)
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
			ID:     m.Path + ":out:" + t,
			Side:   "right",
			Kind:   "out",
			Name:   "use " + path.Base(t),
			Public: publicIndex.HasPackageDependency(m.Path, t),
		}
		comp.Ports = append(comp.Ports, port)
	}

	return comp
}

type publicDiffAnnotation struct {
	diff     string
	kind     string
	parentID string
	name     string
	before   string
	after    string
}

func buildPublicDiffMap(d *publicapi.Diff) map[string]publicDiffAnnotation {
	m := make(map[string]publicDiffAnnotation)
	if d == nil {
		return m
	}
	for _, change := range d.Changes {
		word := publicDiffWord(change.Op)
		if word == "" {
			continue
		}
		m[change.ID] = publicDiffAnnotation{
			diff:     word,
			kind:     change.Kind,
			parentID: change.ParentID,
			name:     change.Name,
			before:   change.Before,
			after:    change.After,
		}
	}
	return m
}

func publicDiffWord(op string) string {
	switch op {
	case "added", "removed", "changed":
		return op
	default:
		return ""
	}
}

func publicSymbolDiff(publicDiffs map[string]publicDiffAnnotation, id string) (string, bool) {
	ann, ok := publicDiffs[id]
	if !ok || !strings.HasPrefix(ann.kind, "symbol:") || ann.diff == "" {
		return "", false
	}
	return ann.diff, true
}

func publicMemberDiff(publicDiffs map[string]publicDiffAnnotation, id string) (string, bool) {
	ann, ok := publicDiffs[id]
	if !ok || !strings.HasPrefix(ann.kind, "member:") || ann.diff == "" {
		return "", false
	}
	return ann.diff, true
}

func applyPublicDiffDetailToInternal(internal *Internal, publicDiffs map[string]publicDiffAnnotation) {
	ann, ok := publicDiffs[internal.ID]
	if !ok || !strings.HasPrefix(ann.kind, "symbol:") {
		return
	}
	internal.DiffBefore, internal.DiffAfter = publicDiffBeforeAfter(ann)
}

func applyPublicDiffDetailToMember(member *Member, publicDiffs map[string]publicDiffAnnotation) {
	ann, ok := publicDiffs[member.ID]
	if !ok || !strings.HasPrefix(ann.kind, "member:") {
		return
	}
	member.DiffBefore, member.DiffAfter = publicDiffBeforeAfter(ann)
}

func appendPublicDiffMembers(
	members []Member,
	parentID string,
	sourceFile string,
	publicDiffs map[string]publicDiffAnnotation,
) []Member {
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		seen[member.ID] = struct{}{}
	}
	for id, ann := range publicDiffs {
		if ann.parentID != parentID || !strings.HasPrefix(ann.kind, "member:") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		members = append(members, Member{
			ID:         id,
			Kind:       publicMemberKind(ann.kind),
			Name:       publicMemberDisplayName(ann),
			SourceFile: sourceFile,
			Exported:   true,
			Diff:       ann.diff,
			DiffBefore: publicDiffBefore(ann),
			DiffAfter:  publicDiffAfter(ann),
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	return members
}

func publicMemberKind(kind string) string {
	switch strings.TrimPrefix(kind, "member:") {
	case "field":
		return "prop"
	case "method", "const":
		return strings.TrimPrefix(kind, "member:")
	default:
		return "member"
	}
}

func publicMemberDisplayName(ann publicDiffAnnotation) string {
	fingerprint := ann.after
	if fingerprint == "" {
		fingerprint = ann.before
	}
	if _, signature, ok := strings.Cut(fingerprint, "\x00"); ok && signature != "" {
		return signature
	}
	if strings.TrimPrefix(ann.kind, "member:") == "method" && !strings.Contains(ann.name, "(") {
		return ann.name + "()"
	}
	return ann.name
}

func publicDiffBeforeAfter(ann publicDiffAnnotation) (string, string) {
	return publicDiffBefore(ann), publicDiffAfter(ann)
}

func publicDiffBefore(ann publicDiffAnnotation) string {
	return publicDiffSignature(ann.before)
}

func publicDiffAfter(ann publicDiffAnnotation) string {
	return publicDiffSignature(ann.after)
}

func publicDiffSignature(fingerprint string) string {
	if fingerprint == "" {
		return ""
	}
	if _, signature, ok := strings.Cut(fingerprint, "\x00"); ok {
		return signature
	}
	return fingerprint
}

func constDisplayName(c domain.ConstDef) string {
	name := c.Name
	if c.Type.Name != "" || c.Type.Package != "" {
		name += " : " + c.Type.String()
	}
	if c.Value != "" {
		name += " = " + c.Value
	}
	return name
}

func varDisplayName(v domain.VarDef) string {
	name := v.Name
	if v.Type.Name != "" || v.Type.Package != "" {
		name += " : " + v.Type.String()
	}
	return name
}

func errorDisplayName(e domain.ErrorDef) string {
	if e.Message == "" {
		return e.Name
	}
	return e.Name + ` = "` + e.Message + `"`
}

type diffDetail struct {
	diff   string
	kind   string
	name   string
	before string
	after  string
}

type legacyDiffDetails struct {
	symbols map[string]diffDetail
	members map[string]diffDetail
}

func buildLegacyDiffDetails(d *diff.Diff) legacyDiffDetails {
	details := legacyDiffDetails{
		symbols: map[string]diffDetail{},
		members: map[string]diffDetail{},
	}
	if d == nil {
		return details
	}
	for _, c := range d.Changes {
		word := diffWord(string(c.Op))
		if word == "" {
			continue
		}
		switch c.Kind {
		case diff.KindInterface:
			if detail, ok := symbolDiffDetail(c, interfaceSignature); ok {
				detail.kind = "iface"
				details.symbols[c.Path] = detail
			}
			if word == "changed" {
				before, beforeOK := c.Before.(domain.InterfaceDef)
				after, afterOK := c.After.(domain.InterfaceDef)
				if beforeOK && afterOK {
					addMethodDiffDetails(details.members, c.Path, before.Methods, after.Methods)
				}
			}
		case diff.KindStruct:
			if detail, ok := symbolDiffDetail(c, structSignature); ok {
				detail.kind = "class"
				details.symbols[c.Path] = detail
			}
			if word == "changed" {
				before, beforeOK := c.Before.(domain.StructDef)
				after, afterOK := c.After.(domain.StructDef)
				if beforeOK && afterOK {
					addFieldDiffDetails(details.members, c.Path, before.Fields, after.Fields)
					addMethodDiffDetails(details.members, c.Path, before.Methods, after.Methods)
				}
			}
		case diff.KindFunction:
			if detail, ok := symbolDiffDetail(c, functionSignature); ok {
				detail.kind = "func"
				details.symbols[c.Path] = detail
			}
		case diff.KindTypeDef:
			if detail, ok := symbolDiffDetail(c, typeDefSignature); ok {
				detail.kind = "type"
				details.symbols[c.Path] = detail
			}
			if word == "changed" {
				before, beforeOK := c.Before.(domain.TypeDef)
				after, afterOK := c.After.(domain.TypeDef)
				if beforeOK && afterOK {
					addStringMemberDiffDetails(details.members, c.Path, "const", before.Constants, after.Constants)
				}
			}
		case diff.KindConst:
			if detail, ok := symbolDiffDetail(c, constSignature); ok {
				detail.kind = "const"
				details.symbols[c.Path] = detail
			}
		case diff.KindVar:
			if detail, ok := symbolDiffDetail(c, varSignature); ok {
				detail.kind = "var"
				details.symbols[c.Path] = detail
			}
		case diff.KindError:
			if detail, ok := symbolDiffDetail(c, errorSignature); ok {
				detail.kind = "error"
				details.symbols[c.Path] = detail
			}
		}
	}
	return details
}

func symbolDiffDetail(c diff.Change, signature func(any) string) (diffDetail, bool) {
	word := diffWord(string(c.Op))
	if word == "" {
		return diffDetail{}, false
	}
	beforeCurrent := signature(c.Before)
	afterTarget := signature(c.After)
	detail := diffDetail{diff: word}
	switch word {
	case "added":
		detail.name = beforeCurrent
		detail.after = beforeCurrent
	case "removed":
		detail.name = afterTarget
		detail.before = afterTarget
	case "changed":
		detail.name = beforeCurrent
		detail.before = afterTarget
		detail.after = beforeCurrent
	}
	if detail.before == detail.after {
		detail.before = ""
		detail.after = ""
	}
	return detail, true
}

func interfaceSignature(v any) string {
	if iface, ok := v.(domain.InterfaceDef); ok {
		return "type " + iface.Name + " interface"
	}
	return ""
}

func structSignature(v any) string {
	if s, ok := v.(domain.StructDef); ok {
		return "type " + s.Name + " struct"
	}
	return ""
}

func functionSignature(v any) string {
	if fn, ok := v.(domain.FunctionDef); ok {
		return fn.Signature()
	}
	return ""
}

func typeDefSignature(v any) string {
	if td, ok := v.(domain.TypeDef); ok {
		if td.UnderlyingType.Name == "" && td.UnderlyingType.Package == "" {
			return "type " + td.Name
		}
		return "type " + td.Name + " " + td.UnderlyingType.String()
	}
	return ""
}

func constSignature(v any) string {
	if c, ok := v.(domain.ConstDef); ok {
		return constDisplayName(c)
	}
	return ""
}

func varSignature(v any) string {
	if v, ok := v.(domain.VarDef); ok {
		return varDisplayName(v)
	}
	return ""
}

func errorSignature(v any) string {
	if e, ok := v.(domain.ErrorDef); ok {
		return errorDisplayName(e)
	}
	return ""
}

func addMethodDiffDetails(out map[string]diffDetail, parentID string, current, target []domain.MethodDef) {
	currentByName := make(map[string]domain.MethodDef, len(current))
	for _, method := range current {
		currentByName[method.Name] = method
	}
	targetByName := make(map[string]domain.MethodDef, len(target))
	for _, method := range target {
		targetByName[method.Name] = method
	}
	for _, name := range unionNames(currentByName, targetByName) {
		cur, hasCur := currentByName[name]
		tgt, hasTgt := targetByName[name]
		id := parentID + "." + name
		switch {
		case hasCur && !hasTgt:
			out[id] = diffDetail{diff: "added", kind: "method", name: cur.Signature(), after: cur.Signature()}
		case !hasCur && hasTgt:
			out[id] = diffDetail{diff: "removed", kind: "method", name: tgt.Signature(), before: tgt.Signature()}
		case hasCur && hasTgt && cur.Signature() != tgt.Signature():
			out[id] = diffDetail{
				diff:   "changed",
				kind:   "method",
				name:   cur.Signature(),
				before: tgt.Signature(),
				after:  cur.Signature(),
			}
		}
	}
}

func addFieldDiffDetails(out map[string]diffDetail, parentID string, current, target []domain.FieldDef) {
	currentByName := make(map[string]domain.FieldDef, len(current))
	for _, field := range current {
		currentByName[field.Name] = field
	}
	targetByName := make(map[string]domain.FieldDef, len(target))
	for _, field := range target {
		targetByName[field.Name] = field
	}
	for _, name := range unionNames(currentByName, targetByName) {
		cur, hasCur := currentByName[name]
		tgt, hasTgt := targetByName[name]
		id := parentID + "." + name
		switch {
		case hasCur && !hasTgt:
			out[id] = diffDetail{diff: "added", kind: "prop", name: fieldDisplayName(cur), after: fieldDisplayName(cur)}
		case !hasCur && hasTgt:
			out[id] = diffDetail{diff: "removed", kind: "prop", name: fieldDisplayName(tgt), before: fieldDisplayName(tgt)}
		case hasCur && hasTgt && fieldDisplayName(cur) != fieldDisplayName(tgt):
			out[id] = diffDetail{
				diff:   "changed",
				kind:   "prop",
				name:   fieldDisplayName(cur),
				before: fieldDisplayName(tgt),
				after:  fieldDisplayName(cur),
			}
		}
	}
}

func addStringMemberDiffDetails(out map[string]diffDetail, parentID, kind string, current, target []string) {
	currentByName := make(map[string]string, len(current))
	for _, name := range current {
		currentByName[name] = name
	}
	targetByName := make(map[string]string, len(target))
	for _, name := range target {
		targetByName[name] = name
	}
	for _, name := range unionNames(currentByName, targetByName) {
		_, hasCur := currentByName[name]
		_, hasTgt := targetByName[name]
		id := parentID + "." + name
		switch {
		case hasCur && !hasTgt:
			out[id] = diffDetail{diff: "added", kind: kind, name: name, after: name}
		case !hasCur && hasTgt:
			out[id] = diffDetail{diff: "removed", kind: kind, name: name, before: name}
		}
	}
}

func fieldDisplayName(f domain.FieldDef) string {
	return f.Name + " : " + f.Type.String()
}

func applyLegacyDiffDetailToInternal(internal *Internal, legacyDiffs legacyDiffDetails) {
	detail, ok := legacyDiffs.symbols[internal.ID]
	if !ok {
		return
	}
	if detail.diff != "" {
		internal.Diff = detail.diff
	}
	if detail.before != "" || detail.after != "" {
		internal.DiffBefore = detail.before
		internal.DiffAfter = detail.after
	}
}

func applyLegacyDiffDetailToMember(member *Member, legacyDiffs legacyDiffDetails) {
	detail, ok := legacyDiffs.members[member.ID]
	if !ok {
		return
	}
	if detail.diff != "" {
		member.Diff = detail.diff
	}
	if detail.before != "" || detail.after != "" {
		member.DiffBefore = detail.before
		member.DiffAfter = detail.after
	}
}

func appendLegacyDiffMembers(members []Member, parentID string, sourceFile string, legacyDiffs legacyDiffDetails) []Member {
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		seen[member.ID] = struct{}{}
	}
	for id, detail := range legacyDiffs.members {
		if !strings.HasPrefix(id, parentID+".") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		name := detail.name
		if name == "" {
			name = detail.after
		}
		if name == "" {
			name = detail.before
		}
		members = append(members, Member{
			ID:         id,
			Kind:       legacyMemberKind(detail.kind),
			Name:       name,
			SourceFile: sourceFile,
			Diff:       detail.diff,
			DiffBefore: detail.before,
			DiffAfter:  detail.after,
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	return members
}

func legacyMemberKind(kind string) string {
	switch kind {
	case "method", "const":
		return kind
	case "field", "prop":
		return "prop"
	default:
		return "prop"
	}
}

type dependencyDiff struct {
	From   string
	To     string
	Label  string
	Public bool
	Diff   string
}

// buildEdges creates edges from package dependencies.
// NOTE: Port-level diff annotation is a deliberate POC non-goal.
func buildEdges(
	models []domain.PackageModel,
	pkgSet map[string]bool,
	diffMap map[string]dependencyDiff,
	publicIndex publicapi.Index,
) []Edge {
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
					Public:   publicIndex.HasPackageDependency(m.Path, targetPkg),
				}
				if depDiff, ok := diffMap[edgeID]; ok {
					edge.Diff = depDiff.Diff
					edge.Public = edge.Public || depDiff.Public
				}
				edgeMap[edgeID] = edge
			} else if depDiff, ok := diffMap[edgeID]; ok {
				edge := edgeMap[edgeID]
				edge.Diff = depDiff.Diff
				edge.Public = edge.Public || depDiff.Public
				edgeMap[edgeID] = edge
			}
		}
	}

	for edgeID, depDiff := range diffMap {
		if depDiff.Diff != "removed" {
			continue
		}
		if _, exists := edgeMap[edgeID]; exists {
			continue
		}
		if !pkgSet[depDiff.From] || !pkgSet[depDiff.To] {
			continue
		}
		edgeMap[edgeID] = Edge{
			ID:       edgeID,
			From:     depDiff.From,
			To:       depDiff.To,
			FromPort: depDiff.From + ":out:" + depDiff.To,
			ToPort:   depDiff.To + ":in:...",
			Label:    depDiff.Label,
			Public:   depDiff.Public,
			Diff:     depDiff.Diff,
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

type relationDiff struct {
	relation SymbolRelation
	diff     string
	public   bool
}

type relationDiffMaps struct {
	exact    map[string]relationDiff
	fallback map[string]relationDiff
}

type symbolIndex struct {
	internals map[string]struct{}
	members   map[string]struct{}
}

func buildRelations(
	models []domain.PackageModel,
	pkgSet map[string]bool,
	diffMaps relationDiffMaps,
	publicIndex publicapi.Index,
) []SymbolRelation {
	index := buildSymbolIndex(models)
	relationMap := make(map[string]SymbolRelation)

	add := func(relation SymbolRelation) {
		if relation.FromComponentID == "" || relation.ToComponentID == "" {
			return
		}
		if !pkgSet[relation.FromComponentID] || !pkgSet[relation.ToComponentID] {
			return
		}
		if relation.FromInternalID != "" {
			if _, ok := index.internals[relation.FromInternalID]; !ok {
				return
			}
		}
		if relation.FromMemberID != "" {
			if _, ok := index.members[relation.FromMemberID]; !ok {
				return
			}
		}
		if relation.ToInternalID != "" {
			if _, ok := index.internals[relation.ToInternalID]; !ok {
				return
			}
		}
		if relation.ToMemberID != "" {
			if _, ok := index.members[relation.ToMemberID]; !ok {
				return
			}
		}

		if diff, ok := diffMaps.exact[relation.ID]; ok {
			relation.Diff = diff.diff
			relation.Public = relation.Public || diff.public
		} else if diff, ok := diffMaps.fallback[relationFallbackID(relation.Kind, relation.FromInternalID, relation.ToInternalID)]; ok {
			relation.Diff = diff.diff
			relation.Public = relation.Public || diff.public
		}
		if existing, ok := relationMap[relation.ID]; ok {
			existing.Public = existing.Public || relation.Public
			if existing.Diff == "" {
				existing.Diff = relation.Diff
			}
			relationMap[relation.ID] = existing
			return
		}
		relationMap[relation.ID] = relation
	}

	addTypeRelations := func(
		currentPkg string,
		fromInternalID string,
		fromMemberID string,
		fromLabel string,
		kind domain.DependencyKind,
		refs []domain.TypeRef,
	) {
		for _, ref := range refs {
			for _, target := range flattenTypeRefs(ref) {
				targetPkg := target.Package
				if targetPkg == "" || targetPkg == "." {
					targetPkg = currentPkg
				}
				if target.Name == "" || !pkgSet[targetPkg] {
					continue
				}
				toInternalID := targetPkg + "." + target.Name
				if _, ok := index.internals[toInternalID]; !ok {
					continue
				}
				fromID := fromInternalID
				if fromMemberID != "" {
					fromID = fromMemberID
				}
				fromPublic := publicIndex.HasSymbolID(fromInternalID)
				if fromMemberID != "" {
					fromPublic = publicIndex.HasMemberID(fromMemberID)
				}
				toPublic := publicIndex.HasSymbolID(toInternalID)
				relation := SymbolRelation{
					ID:              relationID(string(kind), fromID, toInternalID),
					Kind:            string(kind),
					FromComponentID: currentPkg,
					FromInternalID:  fromInternalID,
					FromMemberID:    fromMemberID,
					FromLabel:       fromLabel,
					ToComponentID:   targetPkg,
					ToInternalID:    toInternalID,
					ToLabel:         target.Name,
					Public:          fromPublic && toPublic,
				}
				add(relation)
			}
		}
	}

	for _, model := range models {
		for _, iface := range model.Interfaces {
			fromInternalID := model.Path + "." + iface.Name
			for _, method := range iface.Methods {
				fromMemberID := fromInternalID + "." + method.Name
				addTypeRelations(
					model.Path,
					fromInternalID,
					fromMemberID,
					method.Signature(),
					domain.DependencyUses,
					paramTypeRefs(method.Params),
				)
				addTypeRelations(
					model.Path,
					fromInternalID,
					fromMemberID,
					method.Signature(),
					domain.DependencyReturns,
					method.Returns,
				)
			}
		}

		for _, s := range model.Structs {
			fromInternalID := model.Path + "." + s.Name
			for _, field := range s.Fields {
				fromMemberID := fromInternalID + "." + field.Name
				addTypeRelations(
					model.Path,
					fromInternalID,
					fromMemberID,
					fieldDisplayName(field),
					domain.DependencyUses,
					[]domain.TypeRef{field.Type},
				)
			}
			for _, method := range s.Methods {
				fromMemberID := fromInternalID + "." + method.Name
				addTypeRelations(
					model.Path,
					fromInternalID,
					fromMemberID,
					method.Signature(),
					domain.DependencyUses,
					paramTypeRefs(method.Params),
				)
				addTypeRelations(
					model.Path,
					fromInternalID,
					fromMemberID,
					method.Signature(),
					domain.DependencyReturns,
					method.Returns,
				)
			}
		}

		for _, fn := range model.Functions {
			fromInternalID := model.Path + "." + fn.Name
			addTypeRelations(
				model.Path,
				fromInternalID,
				"",
				fn.Signature(),
				domain.DependencyUses,
				paramTypeRefs(fn.Params),
			)
			addTypeRelations(
				model.Path,
				fromInternalID,
				"",
				fn.Signature(),
				domain.DependencyReturns,
				fn.Returns,
			)
		}

		for _, td := range model.TypeDefs {
			fromInternalID := model.Path + "." + td.Name
			addTypeRelations(
				model.Path,
				fromInternalID,
				"",
				typeDefSignature(td),
				domain.DependencyUses,
				[]domain.TypeRef{td.UnderlyingType},
			)
		}

		for _, c := range model.Constants {
			fromInternalID := model.Path + "." + c.Name
			addTypeRelations(
				model.Path,
				fromInternalID,
				"",
				constDisplayName(c),
				domain.DependencyUses,
				[]domain.TypeRef{c.Type},
			)
		}

		for _, v := range model.Variables {
			fromInternalID := model.Path + "." + v.Name
			addTypeRelations(
				model.Path,
				fromInternalID,
				"",
				varDisplayName(v),
				domain.DependencyUses,
				[]domain.TypeRef{v.Type},
			)
		}

		for _, impl := range model.Implementations {
			fromInternalID, _ := relationSymbolIDs(impl.Concrete.Package, impl.Concrete.Symbol)
			toInternalID, _ := relationSymbolIDs(impl.Interface.Package, impl.Interface.Symbol)
			if fromInternalID == "" || toInternalID == "" {
				continue
			}
			relation := SymbolRelation{
				ID:              relationID(string(domain.DependencyImplements), fromInternalID, toInternalID),
				Kind:            string(domain.DependencyImplements),
				FromComponentID: impl.Concrete.Package,
				FromInternalID:  fromInternalID,
				FromLabel:       impl.Concrete.Symbol,
				ToComponentID:   impl.Interface.Package,
				ToInternalID:    toInternalID,
				ToLabel:         impl.Interface.Symbol,
				Public:          publicIndex.HasSymbolID(fromInternalID) && publicIndex.HasSymbolID(toInternalID),
			}
			add(relation)
		}
	}

	for _, diff := range diffMaps.exact {
		if diff.diff != "removed" {
			continue
		}
		relation := diff.relation
		if _, exists := relationMap[relation.ID]; exists {
			continue
		}
		if !pkgSet[relation.FromComponentID] || !pkgSet[relation.ToComponentID] {
			continue
		}
		relation.Diff = diff.diff
		relation.Public = relation.Public || diff.public
		relationMap[relation.ID] = relation
	}

	relations := make([]SymbolRelation, 0, len(relationMap))
	for _, relation := range relationMap {
		relations = append(relations, relation)
	}
	sort.Slice(relations, func(i, j int) bool { return relations[i].ID < relations[j].ID })
	return relations
}

func buildSymbolIndex(models []domain.PackageModel) symbolIndex {
	index := symbolIndex{
		internals: map[string]struct{}{},
		members:   map[string]struct{}{},
	}
	addInternal := func(id string) {
		index.internals[id] = struct{}{}
	}
	addMember := func(id string) {
		index.members[id] = struct{}{}
	}
	for _, model := range models {
		for _, iface := range model.Interfaces {
			internalID := model.Path + "." + iface.Name
			addInternal(internalID)
			for _, method := range iface.Methods {
				addMember(internalID + "." + method.Name)
			}
		}
		for _, s := range model.Structs {
			internalID := model.Path + "." + s.Name
			addInternal(internalID)
			for _, field := range s.Fields {
				addMember(internalID + "." + field.Name)
			}
			for _, method := range s.Methods {
				addMember(internalID + "." + method.Name)
			}
		}
		for _, fn := range model.Functions {
			addInternal(model.Path + "." + fn.Name)
		}
		for _, td := range model.TypeDefs {
			internalID := model.Path + "." + td.Name
			addInternal(internalID)
			for _, c := range td.Constants {
				addMember(internalID + "." + c)
			}
		}
		for _, c := range model.Constants {
			addInternal(model.Path + "." + c.Name)
		}
		for _, v := range model.Variables {
			addInternal(model.Path + "." + v.Name)
		}
		for _, e := range model.Errors {
			addInternal(model.Path + "." + e.Name)
		}
	}
	return index
}

func paramTypeRefs(params []domain.ParamDef) []domain.TypeRef {
	refs := make([]domain.TypeRef, 0, len(params))
	for _, param := range params {
		refs = append(refs, param.Type)
	}
	return refs
}

func flattenTypeRefs(ref domain.TypeRef) []domain.TypeRef {
	if ref.IsMap {
		var refs []domain.TypeRef
		if ref.KeyType != nil {
			refs = append(refs, flattenTypeRefs(*ref.KeyType)...)
		}
		if ref.ValueType != nil {
			refs = append(refs, flattenTypeRefs(*ref.ValueType)...)
		}
		return refs
	}
	if ref.Name == "" || isBasicRelationType(ref.Name) {
		return nil
	}
	return []domain.TypeRef{ref}
}

func isBasicRelationType(name string) bool {
	switch name {
	case "bool", "byte", "complex64", "complex128", "error", "float32", "float64",
		"int", "int8", "int16", "int32", "int64", "rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "any":
		return true
	default:
		return false
	}
}

func relationID(kind, fromID, toID string) string {
	return "r:" + kind + ":" + fromID + "->" + toID
}

func relationFallbackID(kind, fromInternalID, toInternalID string) string {
	return "rf:" + kind + ":" + fromInternalID + "->" + toInternalID
}

func relationSymbolIDs(pkg, symbol string) (string, string) {
	pkg = strings.TrimSpace(pkg)
	symbol = strings.TrimSpace(symbol)
	if pkg == "" || symbol == "" {
		return "", ""
	}
	parts := strings.SplitN(symbol, ".", 2)
	internalID := pkg + "." + parts[0]
	if len(parts) == 1 || parts[1] == "" {
		return internalID, ""
	}
	return internalID, internalID + "." + parts[1]
}

func buildPolicyViolations(models []domain.PackageModel, cfg *overlay.Config) []PolicyViolation {
	if cfg == nil || len(cfg.Layers) == 0 {
		return nil
	}

	pkgLayer := make(map[string]string, len(models))
	for _, model := range models {
		if model.Layer != "" {
			pkgLayer[model.Path] = model.Layer
		}
	}

	seen := make(map[string]struct{})
	var violations []PolicyViolation
	for _, model := range models {
		sourceLayer := model.Layer
		if sourceLayer == "" {
			continue
		}
		allowed, hasRule := cfg.LayerRules[sourceLayer]
		allowSet := make(map[string]struct{}, len(allowed)+1)
		for _, layer := range allowed {
			allowSet[layer] = struct{}{}
		}
		allowSet[sourceLayer] = struct{}{}

		for _, dep := range model.Dependencies {
			if dep.To.External {
				continue
			}
			targetPkg := normalizeModulePackage(cfg.Module, dep.To.Package)
			if targetPkg == "" || targetPkg == model.Path {
				continue
			}
			targetLayer, ok := pkgLayer[targetPkg]
			if !ok {
				continue
			}
			if hasRule {
				if _, ok := allowSet[targetLayer]; ok {
					continue
				}
			} else if targetLayer == sourceLayer {
				continue
			}

			id := "policy:layer-rule:" + model.Path + "->" + targetPkg
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			violations = append(violations, PolicyViolation{
				ID:                id,
				Kind:              "layer_rule",
				SourceComponentID: model.Path,
				TargetComponentID: targetPkg,
				SourceLayer:       sourceLayer,
				TargetLayer:       targetLayer,
				Message: fmt.Sprintf(
					"%s (%s) imports %s (%s), which is not allowed by layer_rules",
					model.Path,
					sourceLayer,
					targetPkg,
					targetLayer,
				),
			})
		}
	}

	sort.Slice(violations, func(i, j int) bool { return violations[i].ID < violations[j].ID })
	return violations
}

func normalizeModulePackage(module, pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	if module == "" {
		return pkg
	}
	if pkg == module {
		return "."
	}
	if strings.HasPrefix(pkg, module+"/") {
		return strings.TrimPrefix(pkg, module+"/")
	}
	return pkg
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

func buildDependencyDiffMap(d *diff.Diff) map[string]dependencyDiff {
	m := make(map[string]dependencyDiff)
	if d == nil {
		return m
	}
	for _, c := range d.Changes {
		if c.Kind != diff.KindDep {
			continue
		}
		dep, ok := changeDependency(c)
		if !ok || dep.To.Package == "" {
			continue
		}
		pkg := dependencyChangePackage(c.Path)
		if dep.From.Package != "" {
			pkg = dep.From.Package
		}
		if pkg == "" {
			continue
		}
		if word := diffWord(string(c.Op)); word != "" {
			to := dep.To.Package
			m["e:"+pkg+"->"+to] = dependencyDiff{
				From:   pkg,
				To:     to,
				Label:  string(dep.Kind),
				Public: dep.ThroughExported,
				Diff:   word,
			}
		}
	}
	return m
}

func buildRelationDiffMap(d *diff.Diff) relationDiffMaps {
	maps := relationDiffMaps{
		exact:    map[string]relationDiff{},
		fallback: map[string]relationDiff{},
	}
	if d == nil {
		return maps
	}
	for _, c := range d.Changes {
		if c.Kind != diff.KindDep {
			continue
		}
		dep, ok := changeDependency(c)
		if !ok || dep.To.Package == "" {
			continue
		}
		fromPkg := dependencyChangePackage(c.Path)
		if dep.From.Package != "" {
			fromPkg = dep.From.Package
		}
		toPkg := dep.To.Package
		if fromPkg == "" || toPkg == "" {
			continue
		}
		word := diffWord(string(c.Op))
		if word == "" {
			continue
		}
		fromInternalID, fromMemberID := relationSymbolIDs(fromPkg, dep.From.Symbol)
		toInternalID, toMemberID := relationSymbolIDs(toPkg, dep.To.Symbol)
		if fromInternalID == "" || toInternalID == "" {
			continue
		}
		fromID := fromInternalID
		if fromMemberID != "" {
			fromID = fromMemberID
		}
		toID := toInternalID
		if toMemberID != "" {
			toID = toMemberID
		}
		relation := SymbolRelation{
			ID:              relationID(string(dep.Kind), fromID, toID),
			Kind:            string(dep.Kind),
			FromComponentID: fromPkg,
			FromInternalID:  fromInternalID,
			FromMemberID:    fromMemberID,
			FromLabel:       dep.From.Symbol,
			ToComponentID:   toPkg,
			ToInternalID:    toInternalID,
			ToMemberID:      toMemberID,
			ToLabel:         dep.To.Symbol,
			Public:          dep.ThroughExported,
			Diff:            word,
		}
		diff := relationDiff{relation: relation, diff: word, public: dep.ThroughExported}
		maps.exact[relation.ID] = diff
		maps.fallback[relationFallbackID(string(dep.Kind), fromInternalID, toInternalID)] = diff
	}
	return maps
}

func changeDependency(c diff.Change) (domain.Dependency, bool) {
	if dep, ok := c.Before.(domain.Dependency); ok {
		return dep, true
	}
	if dep, ok := c.After.(domain.Dependency); ok {
		return dep, true
	}
	return domain.Dependency{}, false
}

func dependencyChangePackage(changePath string) string {
	if i := strings.IndexByte(changePath, '#'); i >= 0 {
		return changePath[:i]
	}
	return ""
}

func unionNames[V any](a, b map[string]V) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	names := make([]string, 0, len(a)+len(b))
	for name := range a {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range b {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func buildPR(d *diff.Diff) *PR {
	stats := Stats{}
	for _, c := range d.Changes {
		// Use diffWord to get the UI-perspective direction (inverted from diff.Op).
		// This ensures PR stats match the per-element diff flags.
		switch diffWord(string(c.Op)) {
		case "added":
			stats.Added++
		case "removed":
			stats.Removed++
		case "changed":
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
		if m.Aggregate != "" {
			for id, bc := range cfg.BoundedContexts {
				for _, aggName := range bc.Aggregates {
					if aggName == m.Aggregate {
						return id
					}
				}
			}
		}
		// Try to find a BC that contains this package's aggregate
		for id, bc := range cfg.BoundedContexts {
			for _, aggName := range bc.Aggregates {
				if agg, ok := cfg.Aggregates[aggName]; ok {
					// Check if the aggregate root is in this package
					relRoot := normalizeModulePackage(cfg.Module, agg.Root)
					if strings.HasPrefix(relRoot, m.Path+".") {
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

func firstLine(m domain.PackageModel) string {
	// Try interfaces first
	for _, iface := range m.Interfaces {
		if iface.Doc != "" {
			lines := strings.SplitN(iface.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	// Try structs
	for _, s := range m.Structs {
		if s.Doc != "" {
			lines := strings.SplitN(s.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	for _, fn := range m.Functions {
		if fn.Doc != "" {
			lines := strings.SplitN(fn.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	for _, td := range m.TypeDefs {
		if td.Doc != "" {
			lines := strings.SplitN(td.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	for _, c := range m.Constants {
		if c.Doc != "" {
			lines := strings.SplitN(c.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	for _, v := range m.Variables {
		if v.Doc != "" {
			lines := strings.SplitN(v.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	for _, e := range m.Errors {
		if e.Doc != "" {
			lines := strings.SplitN(e.Doc, "\n", 2)
			return strings.TrimSpace(lines[0])
		}
	}
	return ""
}
