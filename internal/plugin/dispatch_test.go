package plugin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
)

// TestMountPluginAPIHandlers_PrefixesUnderPluginName verifies the
// /api/plugins/<name><Path> mount convention and that the plugin's
// handler observes a path stripped of that prefix.
func TestMountPluginAPIHandlers_PrefixesUnderPluginName(t *testing.T) {
	var sawPath string
	handlers := []NamedHTTPHandler{{
		Plugin: "complexity",
		Handler: HTTPHandler{
			Path:    "/scores",
			Methods: []string{http.MethodGet},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}),
		},
	}}

	mux := http.NewServeMux()
	MountPluginAPIHandlers(mux, handlers)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/plugins/complexity/scores")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if sawPath != "/scores" {
		t.Errorf("plugin saw %q, want %q (StripPrefix should remove /api/plugins/<name>)", sawPath, "/scores")
	}

	// Method enforcement still applies through the wrapper.
	post, err := http.Post(srv.URL+"/api/plugins/complexity/scores", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", post.StatusCode)
	}
}

// TestMountPluginAssetHandlers_ServesEmbeddedFS verifies that a
// plugin's Assets fs.FS is reachable at /plugins/<name>/assets/<entry>.
func TestMountPluginAssetHandlers_ServesEmbeddedFS(t *testing.T) {
	body := []byte(`console.log("heatmap");`)
	components := []NamedUIComponent{{
		Plugin: "complexity",
		Component: UIComponent{
			Element: "plugin-complexity-heatmap",
			Assets:  fstest.MapFS{"heatmap.js": &fstest.MapFile{Data: body}},
			Entry:   "heatmap.js",
			EmbedAt: []EmbedSlot{{View: ViewDashboard, Slot: SlotMain}},
		},
	}}

	mux := http.NewServeMux()
	MountPluginAssetHandlers(mux, components)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/plugins/complexity/assets/heatmap.js")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := readAll(resp.Body)
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}

	// Missing file under a real plugin returns 404.
	miss, _ := http.Get(srv.URL + "/plugins/complexity/assets/nope.js")
	if miss.StatusCode != http.StatusNotFound {
		t.Errorf("missing-asset status = %d, want 404", miss.StatusCode)
	}
	miss.Body.Close()
}

// TestBuildPluginCommand_ListPrintsCapabilities runs `archai plugin
// list` and asserts that every capability kind shows up under the
// plugin row.
func TestBuildPluginCommand_ListPrintsCapabilities(t *testing.T) {
	res := BootstrapResult{
		CLICommands: []NamedCLICommand{{
			Plugin:  "complexity",
			Command: CLICommand{Cmd: &cobra.Command{Use: "report"}},
		}},
		MCPTools: []NamedMCPTool{{
			Plugin: "complexity",
			Tool:   MCPTool{Name: "scores"},
		}},
		HTTPHandlers: []NamedHTTPHandler{{
			Plugin: "complexity",
			Handler: HTTPHandler{
				Path:    "/scores",
				Methods: []string{http.MethodGet},
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			},
		}},
		UIComponents: []NamedUIComponent{{
			Plugin: "complexity",
			Component: UIComponent{
				Element: "plugin-complexity-heatmap",
				Entry:   "heatmap.js",
				EmbedAt: []EmbedSlot{{View: ViewDashboard, Slot: SlotMain, Label: "Complexity"}},
			},
		}},
	}

	root := BuildPluginCommand(res)
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	wants := []string{
		"complexity",
		"CLI : archai plugin complexity report",
		"MCP : plugin.complexity.scores",
		"HTTP: GET /api/plugins/complexity/scores",
		"UI  : <plugin-complexity-heatmap> on dashboard/main",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("plugin list output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// TestBuildPluginCommand_GroupsByPlugin verifies CLI commands are
// grouped under `archai plugin <name>`.
func TestBuildPluginCommand_GroupsByPlugin(t *testing.T) {
	res := BootstrapResult{
		CLICommands: []NamedCLICommand{
			{Plugin: "alpha", Command: CLICommand{Cmd: &cobra.Command{Use: "do"}}},
			{Plugin: "beta", Command: CLICommand{Cmd: &cobra.Command{Use: "go"}}},
		},
	}
	root := BuildPluginCommand(res)
	groups := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		groups[c.Use] = c
	}
	if groups["alpha"] == nil || groups["beta"] == nil {
		t.Fatalf("expected per-plugin groups, got %v", groups)
	}
	if groups["list"] == nil {
		t.Errorf("expected built-in `list` subcommand")
	}
	// Each group exposes the plugin's command verbatim.
	subs := map[string]bool{}
	for _, c := range groups["alpha"].Commands() {
		subs[c.Use] = true
	}
	if !subs["do"] {
		t.Errorf("alpha group missing `do`, got %v", subs)
	}
}

// TestPrefixedMCPName_NoCollisionAcrossPlugins asserts the prefix
// scheme makes identical tool names from different plugins distinct.
func TestPrefixedMCPName_NoCollisionAcrossPlugins(t *testing.T) {
	a := PrefixedMCPName("alpha", "scores")
	b := PrefixedMCPName("beta", "scores")
	if a == b {
		t.Fatalf("prefixed names should differ across plugins, got %q == %q", a, b)
	}
	if !strings.HasPrefix(a, "plugin.alpha.") {
		t.Errorf("alpha tool name = %q, want plugin.alpha. prefix", a)
	}
	if !strings.HasPrefix(b, "plugin.beta.") {
		t.Errorf("beta tool name = %q, want plugin.beta. prefix", b)
	}
}

// TestMountPluginAssetHandlers_DeduplicatesPerPlugin verifies that a
// plugin contributing multiple UIComponents (sharing the same Assets)
// only mounts the asset prefix once. Mounting twice on a ServeMux
// panics, so success here is the absence of a panic.
func TestMountPluginAssetHandlers_DeduplicatesPerPlugin(t *testing.T) {
	assets := fstest.MapFS{"a.js": &fstest.MapFile{Data: []byte("//a")}}
	components := []NamedUIComponent{
		{Plugin: "x", Component: UIComponent{Element: "plugin-x-a", Assets: assets, Entry: "a.js", EmbedAt: []EmbedSlot{{View: ViewDashboard, Slot: SlotMain}}}},
		{Plugin: "x", Component: UIComponent{Element: "plugin-x-b", Assets: assets, Entry: "a.js", EmbedAt: []EmbedSlot{{View: ViewDiff, Slot: SlotMain}}}},
	}
	mux := http.NewServeMux()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MountPluginAssetHandlers panicked on duplicate plugin: %v", r)
		}
	}()
	MountPluginAssetHandlers(mux, components)
}

// readAll is a tiny helper kept local so the test file has no extra
// dependencies on io/ioutil-style imports.
func readAll(r interface{ Read(p []byte) (int, error) }) ([]byte, error) {
	var out []byte
	buf := make([]byte, 512)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}
