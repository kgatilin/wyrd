package http

import (
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kgatilin/wyrd/internal/plugin"
	"github.com/kgatilin/wyrd/internal/serve"
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

// rootReportingPlugin answers with the repo root of whatever Host the request
// carries — which is the whole question a repo-level daemon has to get right.
func rootReportingPlugin(bootstrapRoot string) plugin.BootstrapResult {
	return plugin.BootstrapResult{
		HTTPHandlers: []plugin.NamedHTTPHandler{{
			Plugin: "probe",
			Handler: plugin.HTTPHandler{
				Path:    "/root",
				Methods: []string{nethttp.MethodGet},
				Handler: nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
					root := bootstrapRoot
					if h := plugin.HostFromContext(r.Context()); h != nil {
						root = h.RepoRoot()
					}
					_, _ = w.Write([]byte(root))
				}),
			},
		}},
		UIComponents: []plugin.NamedUIComponent{{
			Plugin: "probe",
			Component: plugin.UIComponent{
				Element: "plugin-probe",
				Assets:  fstest.MapFS{"probe.js": &fstest.MapFile{Data: []byte("//probe")}},
				Entry:   "probe.js",
			},
		}},
	}
}

// TestMultiServer_PluginAPIIsScopedToTheWorktree is the regression for a
// repo-level daemon serving one worktree's plugin answer on every worktree's
// URL — or, before that, serving none at all.
func TestMultiServer_PluginAPIIsScopedToTheWorktree(t *testing.T) {
	srv, multi, _ := buildMultiServer(t)
	srv.WithPlugins(rootReportingPlugin("bootstrap-host"))
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	for _, name := range []string{"alpha", "beta"} {
		resp, err := ts.Client().Get(ts.URL + "/w/" + name + "/api/plugins/probe/root")
		if err != nil {
			t.Fatalf("GET /w/%s: %v", name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != nethttp.StatusOK {
			t.Fatalf("/w/%s status = %d, want 200", name, resp.StatusCode)
		}
		state, err := multi.Get(t.Context(), name)
		if err != nil {
			t.Fatalf("state for %s: %v", name, err)
		}
		if got, want := string(body), state.Root(); got != want {
			t.Errorf("/w/%s root = %q, want that worktree's root %q", name, got, want)
		}
	}
}

// TestMultiServer_WorktreelessPluginURLRedirects keeps a plugin URL written
// without a worktree working the way /api/uigraph does.
func TestMultiServer_WorktreelessPluginURLRedirects(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	srv.WithPlugins(rootReportingPlugin("bootstrap-host"))
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := &nethttp.Client{
		CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error { return nethttp.ErrUseLastResponse },
	}
	resp, err := client.Get(ts.URL + "/api/plugins/probe/root")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/alpha/api/plugins/probe/root" {
		t.Errorf("Location = %q", loc)
	}
}

// TestMultiServer_PluginAssetsStayAtTheTopLevel — the bundle is the same
// bytes for every worktree, so it keeps one URL and one browser cache entry.
func TestMultiServer_PluginAssetsStayAtTheTopLevel(t *testing.T) {
	srv, _, _ := buildMultiServer(t)
	srv.WithPlugins(rootReportingPlugin("bootstrap-host"))
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/plugins/probe/assets/probe.js")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
