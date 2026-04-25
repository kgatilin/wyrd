package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kgatilin/archai/internal/plugin"
	"github.com/kgatilin/archai/internal/serve"
)

// newTestServerWithPlugins constructs a Server, attaches the given
// plugin.BootstrapResult, wires routes, and returns a running
// httptest.Server.
func newTestServerWithPlugins(t *testing.T, res plugin.BootstrapResult) *httptest.Server {
	t.Helper()
	state := serve.NewState(t.TempDir())
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithPlugins(res)
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	return httptest.NewServer(mux)
}

// TestServer_PluginAPIRoute verifies that an HTTPHandler contributed
// by a plugin is reachable at /api/plugins/<name><Path>.
func TestServer_PluginAPIRoute(t *testing.T) {
	res := plugin.BootstrapResult{
		HTTPHandlers: []plugin.NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: plugin.HTTPHandler{
				Path:    "/scores",
				Methods: []string{nethttp.MethodGet},
				Handler: nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"hello":"plugin"}`))
				}),
			},
		}},
	}
	ts := newTestServerWithPlugins(t, res)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/plugins/complexity/scores")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"hello":"plugin"`) {
		t.Errorf("body = %q", body)
	}
}

// TestServer_PluginAssetRoute verifies plugin asset bundles are served
// from the embedded fs.FS at /plugins/<name>/assets/<entry>.
func TestServer_PluginAssetRoute(t *testing.T) {
	body := []byte(`customElements.define("plugin-x", class extends HTMLElement{});`)
	res := plugin.BootstrapResult{
		UIComponents: []plugin.NamedUIComponent{{
			Plugin: "x",
			Component: plugin.UIComponent{
				Element: "plugin-x",
				Assets:  fstest.MapFS{"x.js": &fstest.MapFile{Data: body}},
				Entry:   "x.js",
				EmbedAt: []plugin.EmbedSlot{{View: plugin.ViewDashboard, Slot: plugin.SlotMain}},
			},
		}},
	}
	ts := newTestServerWithPlugins(t, res)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/plugins/x/assets/x.js")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != string(body) {
		t.Errorf("body mismatch:\n got %q\nwant %q", got, body)
	}
}

// TestServer_PackageDetailRendersPluginTab verifies the package detail
// template surfaces extra tabs from plugin UIComponents and renders
// the active plugin tab as its custom element.
func TestServer_PackageDetailRendersPluginTab(t *testing.T) {
	fix := newPackagesTestServer(t)
	// newPackagesTestServer wires its own server without plugins; spin
	// up a sibling server that does have plugins, sharing the same root.
	st := serve.NewState(fix.root)
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv, err := NewServer(st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.WithPlugins(plugin.BootstrapResult{
		HTTPHandlers: []plugin.NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: plugin.HTTPHandler{
				Path:    "/scores",
				Methods: []string{nethttp.MethodGet},
				Handler: nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
					_, _ = w.Write([]byte("{}"))
				}),
			},
		}},
		UIComponents: []plugin.NamedUIComponent{{
			Plugin: "complexity",
			Component: plugin.UIComponent{
				Element:  "plugin-complexity-heatmap",
				Assets:   fstest.MapFS{"heatmap.js": &fstest.MapFile{Data: []byte("// stub")}},
				Entry:    "heatmap.js",
				EmbedAt:  []plugin.EmbedSlot{{View: plugin.ViewPackageDetail, Slot: plugin.SlotExtraTab, Label: "Complexity"}},
				ModelURL: "/api/plugins/complexity/scores",
			},
		}},
	})
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Tabs strip lists the plugin tab.
	resp, err := ts.Client().Get(ts.URL + "/packages/internal/foo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	for _, want := range []string{
		"plugin-tab",
		`tab=plugin:complexity`,
		`Complexity</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("package detail missing %q", want)
		}
	}

	// Querying ?tab=plugin:complexity renders the custom element.
	resp2, err := ts.Client().Get(ts.URL + "/packages/internal/foo?tab=plugin:complexity")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	html2 := string(body2)
	for _, want := range []string{
		"<plugin-complexity-heatmap",
		`data-model-url="/api/plugins/complexity/scores"`,
		`data-package="internal/foo"`,
	} {
		if !strings.Contains(html2, want) {
			t.Errorf("plugin tab missing %q\n%s", want, truncate(html2, 1200))
		}
	}
}

// TestServer_DashboardRendersPluginPanel verifies the dashboard
// template surfaces a plugin's UIComponent for ViewDashboard/SlotMain.
func TestServer_DashboardRendersPluginPanel(t *testing.T) {
	res := plugin.BootstrapResult{
		HTTPHandlers: []plugin.NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: plugin.HTTPHandler{
				Path:    "/scores",
				Methods: []string{nethttp.MethodGet},
				Handler: nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
					_, _ = w.Write([]byte("{}"))
				}),
			},
		}},
		UIComponents: []plugin.NamedUIComponent{{
			Plugin: "complexity",
			Component: plugin.UIComponent{
				Element:  "plugin-complexity-heatmap",
				Assets:   fstest.MapFS{"heatmap.js": &fstest.MapFile{Data: []byte("// stub")}},
				Entry:    "heatmap.js",
				EmbedAt:  []plugin.EmbedSlot{{View: plugin.ViewDashboard, Slot: plugin.SlotMain, Label: "Complexity"}},
				ModelURL: "/api/plugins/complexity/scores",
			},
		}},
	}
	ts := newTestServerWithPlugins(t, res)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	wants := []string{
		"<plugin-complexity-heatmap",
		`data-model-url="/api/plugins/complexity/scores"`,
		`/plugins/complexity/assets/heatmap.js`,
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q\n--- body ---\n%s", want, html)
		}
	}
}

// TestServer_DashboardWidgetModelURLIsRoutable is the regression test for
// issue #74: when the dashboard widget renders a plugin's custom element,
// its data-model-url attribute must point at a route that actually
// returns 200, not 404. The previous default produced /api/plugins/<name>
// which didn't match a handler mounted at /<sub-path>.
func TestServer_DashboardWidgetModelURLIsRoutable(t *testing.T) {
	res := plugin.BootstrapResult{
		HTTPHandlers: []plugin.NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: plugin.HTTPHandler{
				Path:    "/scores",
				Methods: []string{nethttp.MethodGet},
				Handler: nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ok":true}`))
				}),
			},
		}},
		UIComponents: []plugin.NamedUIComponent{{
			Plugin: "complexity",
			Component: plugin.UIComponent{
				Element:  "plugin-complexity-heatmap",
				Assets:   fstest.MapFS{"heatmap.js": &fstest.MapFile{Data: []byte("// stub")}},
				Entry:    "heatmap.js",
				EmbedAt:  []plugin.EmbedSlot{{View: plugin.ViewDashboard, Slot: plugin.SlotMain, Label: "Complexity"}},
				ModelURL: "/api/plugins/complexity/scores",
			},
		}},
	}
	ts := newTestServerWithPlugins(t, res)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Pull the data-model-url value the browser would use.
	idx := strings.Index(html, `data-model-url="`)
	if idx < 0 {
		t.Fatalf("dashboard missing data-model-url attribute\n%s", html)
	}
	rest := html[idx+len(`data-model-url="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("malformed data-model-url attribute")
	}
	modelURL := rest[:end]
	if modelURL == "" {
		t.Fatalf("data-model-url is empty")
	}

	// Hit it. Pre-fix this returned 404.
	resp2, err := ts.Client().Get(ts.URL + modelURL)
	if err != nil {
		t.Fatalf("GET %s: %v", modelURL, err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != nethttp.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (issue #74 regression)", modelURL, resp2.StatusCode)
	}
	got, _ := io.ReadAll(resp2.Body)
	if len(got) == 0 {
		t.Errorf("GET %s body is empty", modelURL)
	}
}
