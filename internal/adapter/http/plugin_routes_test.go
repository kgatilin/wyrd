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
