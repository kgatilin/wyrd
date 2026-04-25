package http

import (
	"html/template"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// packageDetailTab is the currently-selected tab in the package detail
// page. The zero value ("overview") is the default landing tab.
type packageDetailTab string

const (
	tabOverview     packageDetailTab = "overview"
	tabPublicAPI    packageDetailTab = "public"
	tabInternal     packageDetailTab = "internal"
	tabDependencies packageDetailTab = "dependencies"
	tabConfigs      packageDetailTab = "configs"
)

// validTabs is the canonical ordered list of tabs shown in the detail
// page. Iteration order matters — it defines the nav order.
var validTabs = []packageDetailTab{
	tabOverview, tabPublicAPI, tabInternal, tabDependencies, tabConfigs,
}

// parseTab coerces a string (from a query param) into a known tab,
// falling back to Overview.
func parseTab(s string) packageDetailTab {
	switch packageDetailTab(s) {
	case tabPublicAPI:
		return tabPublicAPI
	case tabInternal:
		return tabInternal
	case tabDependencies:
		return tabDependencies
	case tabConfigs:
		return tabConfigs
	default:
		return tabOverview
	}
}

// tabLabel returns the human-readable label for a tab used in the UI.
func tabLabel(t packageDetailTab) string {
	switch t {
	case tabOverview:
		return "Overview"
	case tabPublicAPI:
		return "Public API"
	case tabInternal:
		return "Internal"
	case tabDependencies:
		return "Dependencies"
	case tabConfigs:
		return "Configs"
	}
	return string(t)
}

// packageTab is a simple nav entry used by the tabs strip.
type packageTab struct {
	ID     string
	Label  string
	Active bool
}

// pluginPanel is one plugin-contributed extra tab: the custom element
// to render and the URL its data-model-url attribute should point at.
//
// OpenTag/CloseTag are pre-rendered template.HTML values because Go's
// html/template escapes "<" / ">" around dynamic interpolations
// ("<{{.Element}}>" becomes "&lt;...&gt;"). The host validates Element
// against a strict allow-list (lowercase letters, digits, hyphen) and
// pre-builds the open/close tags here so the template can emit them
// verbatim. Element is retained for tests and filtering.
type pluginPanel struct {
	TabID    string // e.g. "plugin:complexity"
	Label    string
	Element  string        // custom-element tag (validated)
	ModelURL string        // data-model-url attribute value
	Active   bool
	OpenTag  template.HTML // e.g. <plugin-x data-model-url="/api/plugins/x">
	CloseTag template.HTML // e.g. </plugin-x>
}

// pluginScript is one <script defer> tag the page must inject so the
// browser registers a plugin's custom element.
type pluginScript struct {
	URL string
}

// validCustomElementName reports whether s is a safe custom-element
// tag name (HTML spec requires lowercase letters / digits / hyphens
// and at least one hyphen). The allow-list also rejects characters
// that would let a malicious plugin name break out of the tag.
func validCustomElementName(s string) bool {
	if s == "" || !strings.ContainsRune(s, '-') {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
		if i == 0 && (r < 'a' || r > 'z') {
			// Custom element names must start with a lowercase letter.
			return false
		}
	}
	return true
}

// buildPluginPanel constructs a pluginPanel and pre-renders its open
// and close tags as template.HTML. Returns ok=false when the element
// name fails validCustomElementName — the host then drops the entry
// rather than emit unsafe markup.
func buildPluginPanel(tabID, label, element, modelURL string, active bool, extraAttrs string) (pluginPanel, bool) {
	if !validCustomElementName(element) {
		return pluginPanel{}, false
	}
	// modelURL is composed from PluginAPIPrefix + plugin name (validated
	// upstream: pluginNames pass through registry.RegisterPlugin which
	// already restricts to lowercase identifiers). We still HTML-escape
	// it defensively in case future plugins ship custom paths.
	open := "<" + element + ` data-model-url="` + template.HTMLEscapeString(modelURL) + `"`
	if extraAttrs != "" {
		open += " " + extraAttrs
	}
	open += ">"
	close := "</" + element + ">"
	return pluginPanel{
		TabID:    tabID,
		Label:    label,
		Element:  element,
		ModelURL: modelURL,
		Active:   active,
		OpenTag:  template.HTML(open),
		CloseTag: template.HTML(close),
	}, true
}

// buildTabs builds the tab list with Active set on the currently
// selected tab.
func buildTabs(active packageDetailTab) []packageTab {
	out := make([]packageTab, 0, len(validTabs))
	for _, t := range validTabs {
		out = append(out, packageTab{
			ID:     string(t),
			Label:  tabLabel(t),
			Active: t == active,
		})
	}
	return out
}

// depLink is one hyperlinked entry in the dependencies tab.
// InternalPath is set when the dependency points at another package
// within the module; External contains the raw name otherwise (e.g.
// "context.Context") and is rendered as plain text.
type depLink struct {
	Package      string
	Symbol       string
	InternalPath string
	External     bool
}

// outboundDep groups all inter-package dependencies originating in a
// single source package; the View is the list of destination symbols,
// grouped by destination package.
type outboundDep struct {
	TargetPkg    string
	InternalPath string
	External     bool
	Symbols      []string
}

// inboundDep groups dependencies that point into this package, keyed by
// the source package.
type inboundDep struct {
	SourcePkg    string
	InternalPath string
	Symbols      []string
}

// configTypeView is a view-model for a config type displayed on the
// Configs tab. It reuses the struct's existing Fields/Doc and adds a
// display name.
type configTypeView struct {
	Name   string
	Doc    string
	Fields []domain.FieldDef
}

// packageDetailData is the view-model for package_detail.html. It
// carries the base pageData plus per-tab state. Heavy fields (like the
// SVG) are only populated when needed for the active tab to keep
// response sizes small.
type packageDetailData struct {
	pageData
	Pkg    domain.PackageModel
	Tabs   []packageTab
	Active packageDetailTab

	// PluginExtraTabs are M13-injected tabs contributed by plugins
	// declaring an EmbedAt of (package_detail, extra_tab). The host
	// renders one custom-element panel per entry.
	PluginExtraTabs []pluginPanel
	// PluginScripts is the de-duplicated list of <script defer> tags
	// to inject so the browser registers each plugin's custom
	// element. Empty when no plugins target package_detail.
	PluginScripts []pluginScript
	// PluginActive is true when the user clicked into a plugin tab;
	// the value is the plugin tab id (e.g. "plugin:complexity").
	PluginActive string

	// Overview
	SVG         template.HTML
	SVGError    string
	Stereotypes []string
	LayerBadge  string

	// Public API / Internal
	Interfaces []domain.InterfaceDef
	Structs    []domain.StructDef
	Functions  []domain.FunctionDef
	TypeDefs   []domain.TypeDef
	Constants  []domain.ConstDef
	Variables  []domain.VarDef
	Errors     []domain.ErrorDef

	// Dependencies
	Outbound []outboundDep
	Inbound  []inboundDep

	// Configs
	ConfigTypes []configTypeView

	// Partial marks that we're rendering only the tab fragment (for
	// HTMX swaps into #pkg-tab-content).
	Partial bool
}

// buildPackageDetail constructs a view-model for the given package.
// svgSource is the raw D2 diagram for the Overview tab; if rendering
// failed the handler passes "" and an error string.
func buildPackageDetail(
	active packageDetailTab,
	pkg domain.PackageModel,
	allPkgs []domain.PackageModel,
	cfg *overlay.Config,
	modulePath string,
) *packageDetailData {
	data := &packageDetailData{
		Pkg:         pkg,
		Active:      active,
		Tabs:        buildTabs(active),
		Stereotypes: collectStereotypes(pkg),
		LayerBadge:  pkg.Layer,
	}

	switch active {
	case tabPublicAPI:
		data.Interfaces = pkg.ExportedInterfaces()
		data.Structs = pkg.ExportedStructs()
		data.Functions = pkg.ExportedFunctions()
		data.TypeDefs = pkg.ExportedTypeDefs()
		data.Constants = pkg.ExportedConstants()
		data.Variables = pkg.ExportedVariables()
		data.Errors = pkg.ExportedErrors()
	case tabInternal:
		data.Interfaces = unexportedInterfaces(pkg.Interfaces)
		data.Structs = unexportedStructs(pkg.Structs)
		data.Functions = unexportedFunctions(pkg.Functions)
		data.TypeDefs = unexportedTypeDefs(pkg.TypeDefs)
		data.Constants = unexportedConstants(pkg.Constants)
		data.Variables = unexportedVariables(pkg.Variables)
		data.Errors = unexportedErrors(pkg.Errors)
	case tabDependencies:
		data.Outbound = buildOutbound(pkg, allPkgs)
		data.Inbound = buildInbound(pkg, allPkgs)
	case tabConfigs:
		data.ConfigTypes = buildConfigTypes(pkg, cfg, modulePath)
	}
	return data
}

// unexported* helpers: symmetric with the Exported* methods on
// PackageModel; kept here (not on domain) to avoid growing the domain
// API for a single consumer.

func unexportedInterfaces(in []domain.InterfaceDef) []domain.InterfaceDef {
	var out []domain.InterfaceDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

func unexportedStructs(in []domain.StructDef) []domain.StructDef {
	var out []domain.StructDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

func unexportedFunctions(in []domain.FunctionDef) []domain.FunctionDef {
	var out []domain.FunctionDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

func unexportedTypeDefs(in []domain.TypeDef) []domain.TypeDef {
	var out []domain.TypeDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

func unexportedConstants(in []domain.ConstDef) []domain.ConstDef {
	var out []domain.ConstDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

func unexportedVariables(in []domain.VarDef) []domain.VarDef {
	var out []domain.VarDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

func unexportedErrors(in []domain.ErrorDef) []domain.ErrorDef {
	var out []domain.ErrorDef
	for _, x := range in {
		if !x.IsExported {
			out = append(out, x)
		}
	}
	return out
}

// buildOutbound groups dependencies leaving this package by target
// package. Internal targets (other packages in the module) are keyed by
// their relative path and hyperlinked; external targets (stdlib /
// 3rd-party) render as plain text.
func buildOutbound(pkg domain.PackageModel, allPkgs []domain.PackageModel) []outboundDep {
	known := knownPackagePaths(allPkgs)
	groups := make(map[string]*outboundDep)
	for _, d := range pkg.Dependencies {
		if d.To.Package == pkg.Path {
			// Same-package deps aren't interesting on this tab; they
			// show up in Public API / Internal already.
			continue
		}
		if d.To.Package == "" {
			continue
		}
		key := d.To.Package
		g, ok := groups[key]
		if !ok {
			g = &outboundDep{
				TargetPkg: d.To.Package,
				External:  d.To.External,
			}
			if !d.To.External {
				if _, isKnown := known[d.To.Package]; isKnown {
					g.InternalPath = d.To.Package
				}
			}
			groups[key] = g
		}
		if !containsString(g.Symbols, d.To.Symbol) {
			g.Symbols = append(g.Symbols, d.To.Symbol)
		}
	}
	out := make([]outboundDep, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.Symbols)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		// Internal packages first, then alphabetical.
		if out[i].External != out[j].External {
			return !out[i].External
		}
		return out[i].TargetPkg < out[j].TargetPkg
	})
	return out
}

// buildInbound walks every other package in the snapshot and collects
// dependencies that point back at pkg.
func buildInbound(pkg domain.PackageModel, allPkgs []domain.PackageModel) []inboundDep {
	groups := make(map[string]*inboundDep)
	for _, src := range allPkgs {
		if src.Path == pkg.Path {
			continue
		}
		for _, d := range src.Dependencies {
			if d.To.Package != pkg.Path {
				continue
			}
			g, ok := groups[src.Path]
			if !ok {
				g = &inboundDep{
					SourcePkg:    src.Path,
					InternalPath: src.Path,
				}
				groups[src.Path] = g
			}
			if !containsString(g.Symbols, d.To.Symbol) {
				g.Symbols = append(g.Symbols, d.To.Symbol)
			}
		}
	}
	out := make([]inboundDep, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.Symbols)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourcePkg < out[j].SourcePkg })
	return out
}

// knownPackagePaths returns a set of all package paths in the snapshot
// for efficient membership checks.
func knownPackagePaths(pkgs []domain.PackageModel) map[string]struct{} {
	out := make(map[string]struct{}, len(pkgs))
	for _, p := range pkgs {
		out[p.Path] = struct{}{}
	}
	return out
}

// containsString reports whether s appears in xs.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// buildConfigTypes walks the overlay config list and picks out entries
// whose fully-qualified name resolves to a struct in this package.
// modulePath is the Go module path from go.mod — used to convert
// "github.com/foo/bar/internal/x.Type" into the relative path
// "internal/x" that PackageModel.Path uses.
func buildConfigTypes(pkg domain.PackageModel, cfg *overlay.Config, modulePath string) []configTypeView {
	if cfg == nil {
		return nil
	}
	var out []configTypeView
	for _, fq := range cfg.Configs {
		pkgPath, typeName := splitFQTypeName(fq)
		if pkgPath == "" || typeName == "" {
			continue
		}
		relPath := relToModule(pkgPath, modulePath)
		if relPath != pkg.Path {
			continue
		}
		for _, s := range pkg.Structs {
			if s.Name == typeName {
				out = append(out, configTypeView{
					Name:   s.Name,
					Doc:    s.Doc,
					Fields: s.Fields,
				})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// splitFQTypeName splits a fully-qualified type name of the form
// "some/pkg/path.TypeName" at the last dot. Returns ("", "") on a
// malformed input (no dot at all).
func splitFQTypeName(fq string) (string, string) {
	i := strings.LastIndex(fq, ".")
	if i <= 0 || i == len(fq)-1 {
		return "", ""
	}
	return fq[:i], fq[i+1:]
}

// relToModule converts an absolute Go import path to a
// module-relative path. Returns pkgPath unchanged if modulePath is
// empty or pkgPath does not belong to the module.
func relToModule(pkgPath, modulePath string) string {
	if modulePath == "" {
		return pkgPath
	}
	if pkgPath == modulePath {
		return "."
	}
	if strings.HasPrefix(pkgPath, modulePath+"/") {
		return strings.TrimPrefix(pkgPath, modulePath+"/")
	}
	return pkgPath
}
