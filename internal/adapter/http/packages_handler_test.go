package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/serve"
)

// writeTestFile is a small helper mirroring the serve package's
// fixture writer. Copied rather than exported to keep the domain-to-
// test coupling unidirectional.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// newPackagesTestServer builds a minimal Go module in a temp dir and
// returns an httptest.Server wrapping a fully-loaded serve.State. The
// fixture exercises every feature the packages UI needs:
//   - multiple packages to build a tree
//   - nested directories (internal/deep/here)
//   - an overlay with layers and configs to populate filters
type loadedFixture struct {
	ts   *httptest.Server
	root string
}

func newPackagesTestServer(t *testing.T) *loadedFixture {
	t.Helper()
	root := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.21\n")

	writeTestFile(t, filepath.Join(root, "internal", "foo", "foo.go"), `package foo

// Thing is an exported struct in foo.
type Thing struct {
	Name string
}

// New returns a Thing.
func New() *Thing { return &Thing{} }

// helper is unexported.
func helper() string { return "" }
`)

	writeTestFile(t, filepath.Join(root, "internal", "bar", "bar.go"), `package bar

import "example.com/fixture/internal/foo"

// Bar uses foo.Thing.
type Bar struct {
	T *foo.Thing
}
`)

	writeTestFile(t, filepath.Join(root, "internal", "deep", "here", "here.go"), `package here

// Deep is an exported struct nested deep in the tree.
type Deep struct{}
`)

	writeTestFile(t, filepath.Join(root, "cfg", "cfg.go"), `package cfg

// AppConfig is marked as a config in archai.yaml.
type AppConfig struct {
	Host string `+"`yaml:\"host\"`"+`
	Port int    `+"`yaml:\"port\"`"+`
}
`)

	writeTestFile(t, filepath.Join(root, "archai.yaml"), `module: example.com/fixture
layers:
  domain:
    - "internal/foo/..."
    - "internal/bar/..."
    - "internal/deep/..."
  adapter:
    - "cfg/..."
layer_rules:
  domain: []
  adapter:
    - domain
configs:
  - example.com/fixture/cfg.AppConfig
`)

	st := serve.NewState(root)
	if err := st.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	srv, err := NewServer(st)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &loadedFixture{ts: ts, root: root}
}

func getBody(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp.StatusCode, string(body)
}

func getHTMX(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	req, err := nethttp.NewRequest(nethttp.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("HTMX GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestPackagesList_ShowsTreeAndFilters(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d, body=%s", code, body)
	}

	// Filter controls render.
	for _, want := range []string{`name="layer"`, `name="stereotype"`, `name="q"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing filter control %q", want)
		}
	}

	// Layer options are populated from the snapshot.
	for _, want := range []string{`>domain<`, `>adapter<`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing layer option %q", want)
		}
	}

	// Packages appear as links to their detail pages.
	for _, want := range []string{
		`href="/packages/internal/foo"`,
		`href="/packages/internal/bar"`,
		`href="/packages/internal/deep/here"`,
		`href="/packages/cfg"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing package link %q", want)
		}
	}

	// Layer badge shows on a package known to be in "domain".
	if !strings.Contains(body, "badge-layer") {
		t.Error("expected layer badges to render")
	}
}

func TestPackagesList_LayerFilter(t *testing.T) {
	fx := newPackagesTestServer(t)

	u := "/packages?" + url.Values{"layer": {"adapter"}}.Encode()
	code, body := getBody(t, fx.ts, u)
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, `href="/packages/cfg"`) {
		t.Errorf("adapter filter should keep cfg pkg")
	}
	if strings.Contains(body, `href="/packages/internal/foo"`) {
		t.Errorf("adapter filter should hide internal/foo")
	}
}

func TestPackagesList_SearchFilter(t *testing.T) {
	fx := newPackagesTestServer(t)

	u := "/packages?" + url.Values{"q": {"Thing"}}.Encode()
	code, body := getBody(t, fx.ts, u)
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// foo defines Thing; bar uses it but the substring matches both
	// paths/symbols. Importantly `cfg` has no "Thing" and must be gone.
	if strings.Contains(body, `href="/packages/cfg"`) {
		t.Errorf("search=Thing should hide cfg pkg")
	}
	if !strings.Contains(body, `href="/packages/internal/foo"`) {
		t.Errorf("search=Thing should keep internal/foo")
	}
}

func TestPackagesList_HTMXReturnsFragment(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getHTMX(t, fx.ts, "/packages?q=foo")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// Fragment must contain the tree wrapper but not the base layout.
	if !strings.Contains(body, `id="pkg-tree-wrap"`) {
		t.Errorf("fragment missing tree wrapper")
	}
	if strings.Contains(body, `<!doctype html>`) {
		t.Errorf("fragment must not include full HTML document")
	}
	if strings.Contains(body, `<header class="site-nav">`) {
		t.Errorf("fragment must not include nav header")
	}
}

func TestPackagesList_EmptyFilterResult(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages?q=zzzzzzzzz-no-match")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "No packages match") {
		t.Errorf("expected empty state message, got: %s", body)
	}
}

func TestPackageDetail_OverviewTab(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages/internal/foo")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d, body=%s", code, body)
	}

	// Crumbs + title.
	if !strings.Contains(body, "internal/foo") {
		t.Error("body missing package path")
	}
	// Tab strip.
	for _, want := range []string{
		"Overview", "Public API", "Internal", "Dependencies", "Configs",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tab missing: %q", want)
		}
	}
	// M8 (#46): Overview is rendered client-side. The handler emits a
	// .cy-graph div pointing at /api/packages/<path>/graph rather than
	// an inline SVG.
	wantAPI := `data-api="/api/packages/internal/foo/graph"`
	if !strings.Contains(body, wantAPI) {
		t.Errorf("overview: expected %s, got: %s", wantAPI, body)
	}
	if !strings.Contains(body, `class="cy-graph package-overview"`) {
		t.Errorf("overview: expected package-overview class on cy-graph, got: %s", body)
	}
	if !strings.Contains(body, "Package only") || !strings.Contains(body, "Package deps") {
		t.Errorf("overview: expected scope switcher, got: %s", body)
	}
	if !strings.Contains(body, `data-cy-action="fullscreen"`) {
		t.Errorf("overview: expected fullscreen control, got: %s", body)
	}
}

func TestPackageDetail_OverviewDepsScope(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages/internal/foo?scope=deps")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d, body=%s", code, body)
	}

	wantAPI := `data-api="/api/packages/internal/foo/deps/graph?mode=both"`
	if !strings.Contains(body, wantAPI) {
		t.Errorf("overview deps: expected %s, got: %s", wantAPI, body)
	}
	if !strings.Contains(body, `class="cy-graph package-deps"`) {
		t.Errorf("overview deps: expected package-deps class on cy-graph, got: %s", body)
	}
	if strings.Contains(body, `data-api="/api/packages/internal/foo/graph"`) {
		t.Errorf("overview deps: should not render package-overview graph source")
	}
	if !strings.Contains(body, `aria-pressed="true">Package deps</a>`) {
		t.Errorf("overview deps: expected Package deps toggle active, got: %s", body)
	}
}

func TestPackageDetail_PublicTab(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages/internal/foo?tab=public")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// Public symbols should appear; unexported `helper` must not.
	if !strings.Contains(body, "Thing") {
		t.Error("public tab missing Thing")
	}
	if strings.Contains(body, ">helper<") {
		t.Error("public tab must not show unexported helper")
	}
}

func TestPackageDetail_InternalTab(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages/internal/foo?tab=internal")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "helper") {
		t.Error("internal tab should show unexported helper")
	}
}

func TestPackageDetail_DependenciesTab(t *testing.T) {
	fx := newPackagesTestServer(t)

	// bar imports foo → outbound from bar should link to foo.
	code, body := getBody(t, fx.ts, "/packages/internal/bar?tab=dependencies")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, `href="/packages/internal/foo"`) {
		t.Errorf("outbound section missing hyperlink to internal/foo, body=%s", body)
	}

	// foo is imported by bar → inbound on foo page should link to bar.
	code, body = getBody(t, fx.ts, "/packages/internal/foo?tab=dependencies")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, `href="/packages/internal/bar"`) {
		t.Errorf("inbound section missing hyperlink to internal/bar, body=%s", body)
	}
}

func TestPackageDetail_ConfigsTab(t *testing.T) {
	fx := newPackagesTestServer(t)

	// cfg pkg has AppConfig registered as a config in archai.yaml.
	code, body := getBody(t, fx.ts, "/packages/cfg?tab=configs")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "AppConfig") {
		t.Error("configs tab missing AppConfig")
	}
	if !strings.Contains(body, "Host") || !strings.Contains(body, "Port") {
		t.Error("configs tab missing fields table")
	}

	// A pkg without any config types should render the empty state.
	code, body = getBody(t, fx.ts, "/packages/internal/foo?tab=configs")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(body, "No config types") {
		t.Errorf("expected empty state on non-config pkg, got: %s", body)
	}
}

func TestPackageDetail_HTMXReturnsTabFragment(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getHTMX(t, fx.ts, "/packages/internal/foo?tab=public")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if !strings.Contains(body, `id="pkg-tab-content"`) {
		t.Error("fragment missing tab panel wrapper")
	}
	if strings.Contains(body, `<!doctype html>`) {
		t.Error("fragment must not include full HTML document")
	}
	if strings.Contains(body, `<header class="site-nav">`) {
		t.Error("fragment must not include nav header")
	}
}

func TestPackageDetail_UnknownReturns404(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, _ := getBody(t, fx.ts, "/packages/does/not/exist")
	if code != nethttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestPackageDetail_UnknownTabFallsBackToOverview(t *testing.T) {
	fx := newPackagesTestServer(t)

	code, body := getBody(t, fx.ts, "/packages/internal/foo?tab=bogus")
	if code != nethttp.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// Overview panel renders a cy-graph div; unknown tab falls back to
	// overview.
	if !strings.Contains(body, `class="cy-graph package-overview"`) {
		t.Errorf("bogus tab: expected fallback to overview with cy-graph div")
	}
}
