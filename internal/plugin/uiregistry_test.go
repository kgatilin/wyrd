package plugin

import (
	"net/http"
	"reflect"
	"testing"
	"testing/fstest"
)

// fakeUIPlugin returns a UIComponent suitable for registry tests.
func fakeUIComponent(elem, entry string, slots ...EmbedSlot) UIComponent {
	return UIComponent{
		Element: elem,
		Assets:  fstest.MapFS{entry: &fstest.MapFile{Data: []byte("// stub")}},
		Entry:   entry,
		EmbedAt: slots,
	}
}

func TestBuildUIRegistry_LookupByViewSlot(t *testing.T) {
	res := BootstrapResult{
		HTTPHandlers: []NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: HTTPHandler{
				Path:    "/scores",
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			},
		}},
		UIComponents: []NamedUIComponent{{
			Plugin: "complexity",
			Component: fakeUIComponent("plugin-complexity-heatmap", "heatmap.js",
				EmbedSlot{View: ViewDashboard, Slot: SlotMain, Label: "Complexity"},
				EmbedSlot{View: ViewPackageDetail, Slot: SlotExtraTab, Label: "Complexity"},
			),
		}},
	}

	reg := BuildUIRegistry(res)

	dash := reg.Lookup(ViewDashboard, SlotMain)
	if len(dash) != 1 {
		t.Fatalf("dashboard/main entries = %d, want 1", len(dash))
	}
	if dash[0].Element != "plugin-complexity-heatmap" {
		t.Errorf("element = %q, want %q", dash[0].Element, "plugin-complexity-heatmap")
	}
	if dash[0].ModelURL != PluginAPIPrefix+"complexity" {
		t.Errorf("ModelURL = %q, want %q", dash[0].ModelURL, PluginAPIPrefix+"complexity")
	}

	pkg := reg.Lookup(ViewPackageDetail, SlotExtraTab)
	if len(pkg) != 1 || pkg[0].Label != "Complexity" {
		t.Errorf("package_detail/extra_tab entries = %+v", pkg)
	}

	// Unknown (view, slot) returns nil.
	if reg.Lookup("nope", SlotMain) != nil {
		t.Errorf("unknown view should return nil")
	}
}

func TestBuildUIRegistry_DropsUnknownViewsAndSlots(t *testing.T) {
	res := BootstrapResult{
		UIComponents: []NamedUIComponent{{
			Plugin: "p",
			Component: fakeUIComponent("plugin-p-x", "x.js",
				EmbedSlot{View: "nope-view", Slot: SlotMain},
				EmbedSlot{View: ViewDashboard, Slot: "nope-slot"},
				EmbedSlot{View: ViewDashboard, Slot: SlotMain},
			),
		}},
	}
	reg := BuildUIRegistry(res)
	if got := reg.Lookup(ViewDashboard, SlotMain); len(got) != 1 {
		t.Errorf("valid slot count = %d, want 1", len(got))
	}
	if got := reg.Lookup("nope-view", SlotMain); len(got) != 0 {
		t.Errorf("invalid view should be dropped, got %d entries", len(got))
	}
}

func TestBuildUIRegistry_ScriptsDeduplicatedPerPlugin(t *testing.T) {
	res := BootstrapResult{
		UIComponents: []NamedUIComponent{
			{
				Plugin: "x",
				Component: fakeUIComponent("plugin-x-a", "x.js",
					EmbedSlot{View: ViewDashboard, Slot: SlotMain}),
			},
			{
				Plugin: "x",
				Component: fakeUIComponent("plugin-x-b", "x.js",
					EmbedSlot{View: ViewDiff, Slot: SlotMain}),
			},
		},
	}
	reg := BuildUIRegistry(res)
	scripts := reg.Scripts()
	if len(scripts) != 1 {
		t.Errorf("Scripts len = %d, want 1 (de-duplicated)", len(scripts))
	}
	if scripts[0].URL != PluginAssetPrefix+"x/assets/x.js" {
		t.Errorf("script URL = %q", scripts[0].URL)
	}
}

func TestBuildUIRegistry_ScriptsForFilters(t *testing.T) {
	res := BootstrapResult{
		UIComponents: []NamedUIComponent{
			{
				Plugin: "a",
				Component: fakeUIComponent("plugin-a", "a.js",
					EmbedSlot{View: ViewDashboard, Slot: SlotMain}),
			},
			{
				Plugin: "b",
				Component: fakeUIComponent("plugin-b", "b.js",
					EmbedSlot{View: ViewDiff, Slot: SlotMain}),
			},
		},
	}
	reg := BuildUIRegistry(res)

	dashScripts := reg.ScriptsFor(ViewDashboard)
	if len(dashScripts) != 1 || dashScripts[0].Plugin != "a" {
		t.Errorf("ScriptsFor(dashboard) = %+v, want only plugin a", dashScripts)
	}
	diffScripts := reg.ScriptsFor(ViewDiff)
	if len(diffScripts) != 1 || diffScripts[0].Plugin != "b" {
		t.Errorf("ScriptsFor(diff) = %+v, want only plugin b", diffScripts)
	}
	// View with no contributors → nil.
	if got := reg.ScriptsFor(ViewLayers); got != nil {
		t.Errorf("ScriptsFor(layers) = %+v, want nil", got)
	}
}

func TestPrefixedMCPName(t *testing.T) {
	got := PrefixedMCPName("complexity", "scores")
	want := "plugin.complexity.scores"
	if got != want {
		t.Errorf("PrefixedMCPName = %q, want %q", got, want)
	}
}

// TestBuildUIRegistry_ExplicitModelURLRespected verifies that when a
// UIComponent sets ModelURL the registry passes it through verbatim
// rather than falling back to the per-plugin default. This is the
// hook plugin authors use when their HTTP handler lives at a non-root
// Path (issue #74).
func TestBuildUIRegistry_ExplicitModelURLRespected(t *testing.T) {
	res := BootstrapResult{
		HTTPHandlers: []NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: HTTPHandler{
				Path:    "/scores",
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			},
		}},
		UIComponents: []NamedUIComponent{{
			Plugin: "complexity",
			Component: UIComponent{
				Element:  "plugin-complexity-heatmap",
				Assets:   fstest.MapFS{"heatmap.js": &fstest.MapFile{Data: []byte("// stub")}},
				Entry:    "heatmap.js",
				EmbedAt:  []EmbedSlot{{View: ViewDashboard, Slot: SlotMain}},
				ModelURL: "/api/plugins/complexity/scores",
			},
		}},
	}
	reg := BuildUIRegistry(res)
	dash := reg.Lookup(ViewDashboard, SlotMain)
	if len(dash) != 1 {
		t.Fatalf("entries = %d, want 1", len(dash))
	}
	if got, want := dash[0].ModelURL, "/api/plugins/complexity/scores"; got != want {
		t.Errorf("ModelURL = %q, want %q (explicit value should be respected)", got, want)
	}
}

// TestBuildUIRegistry_EmptyModelURLFallsBack verifies that an empty
// UIComponent.ModelURL falls back to PluginAPIPrefix + plugin name
// when the plugin contributes any HTTP handler. Preserves backward
// compatibility for plugins that don't set the new field.
func TestBuildUIRegistry_EmptyModelURLFallsBack(t *testing.T) {
	res := BootstrapResult{
		HTTPHandlers: []NamedHTTPHandler{{
			Plugin: "p",
			Handler: HTTPHandler{
				Path:    "",
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			},
		}},
		UIComponents: []NamedUIComponent{{
			Plugin: "p",
			Component: fakeUIComponent("plugin-p", "p.js",
				EmbedSlot{View: ViewDashboard, Slot: SlotMain}),
		}},
	}
	reg := BuildUIRegistry(res)
	dash := reg.Lookup(ViewDashboard, SlotMain)
	if len(dash) != 1 {
		t.Fatalf("entries = %d, want 1", len(dash))
	}
	if got, want := dash[0].ModelURL, PluginAPIPrefix+"p"; got != want {
		t.Errorf("ModelURL = %q, want %q (default fallback)", got, want)
	}
}

// TestBuildUIRegistry_NoHTTPHandlerAndEmptyModelURL verifies that a
// plugin without any HTTP handler and without an explicit ModelURL
// produces an empty ModelURL — there's nothing sensible to default to.
func TestBuildUIRegistry_NoHTTPHandlerAndEmptyModelURL(t *testing.T) {
	res := BootstrapResult{
		UIComponents: []NamedUIComponent{{
			Plugin: "p",
			Component: fakeUIComponent("plugin-p", "p.js",
				EmbedSlot{View: ViewDashboard, Slot: SlotMain}),
		}},
	}
	reg := BuildUIRegistry(res)
	dash := reg.Lookup(ViewDashboard, SlotMain)
	if len(dash) != 1 {
		t.Fatalf("entries = %d, want 1", len(dash))
	}
	if dash[0].ModelURL != "" {
		t.Errorf("ModelURL = %q, want empty", dash[0].ModelURL)
	}
}

func TestUIRegistry_Views(t *testing.T) {
	res := BootstrapResult{
		UIComponents: []NamedUIComponent{
			{
				Plugin: "a",
				Component: fakeUIComponent("plugin-a", "a.js",
					EmbedSlot{View: ViewDashboard, Slot: SlotMain},
					EmbedSlot{View: ViewPackages, Slot: SlotSidePanel}),
			},
		},
	}
	reg := BuildUIRegistry(res)
	got := reg.Views()
	want := []string{ViewDashboard, ViewPackages}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Views = %v, want %v", got, want)
	}
}
