package plugin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// stubHost is a minimal Host for bootstrap tests. Plugins under test
// don't actually call back into it; we just need a non-nil value.
type stubHost struct{}

func (stubHost) CurrentModel() *Model                            { return nil }
func (stubHost) Targets() []TargetMeta                           { return nil }
func (stubHost) Target(string) (*TargetSnapshot, error)          { return nil, nil }
func (stubHost) ActiveTarget() *TargetSnapshot                   { return nil }
func (stubHost) Diff(string, string) (*Diff, error)              { return nil, nil }
func (stubHost) Validate(string) (*ValidationReport, error)      { return nil, nil }
func (stubHost) Subscribe(func(ModelEvent)) Unsubscribe          { return func() {} }
func (stubHost) Logger() *slog.Logger                            { return slog.Default() }

func TestBootstrap_RunsInitAndCollectsCapabilities(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	cli := &cobra.Command{Use: "demo-cli"}
	mcpHandlerCalled := false
	httpHandlerCalled := false

	p := &fakePlugin{
		name: "demo",
		cli:  []CLICommand{{Cmd: cli}},
		mcp: []MCPTool{{
			Name: "demo.tool",
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				mcpHandlerCalled = true
				return "ok", nil
			},
		}},
		http: []HTTPHandler{{
			Path: "/api/demo/ping",
			Handler: http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				httpHandlerCalled = true
			}),
		}},
		ui: []UIComponent{{Slot: EmbedSlotDashboard, Title: "Demo", AssetPath: "/demo.js"}},
	}
	RegisterPlugin(p)

	res, err := Bootstrap(context.Background(), stubHost{}, nil)
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if p.initCalls != 1 {
		t.Errorf("Init calls = %d, want 1", p.initCalls)
	}
	if p.host == nil {
		t.Errorf("plugin did not receive a Host")
	}

	if got := len(res.CLICommands); got != 1 {
		t.Errorf("CLICommands len = %d, want 1", got)
	} else if res.CLICommands[0].Plugin != "demo" {
		t.Errorf("CLICommands[0].Plugin = %q, want %q", res.CLICommands[0].Plugin, "demo")
	}
	if got := len(res.MCPTools); got != 1 {
		t.Errorf("MCPTools len = %d, want 1", got)
	}
	if got := len(res.HTTPHandlers); got != 1 {
		t.Errorf("HTTPHandlers len = %d, want 1", got)
	}
	if got := len(res.UIComponents); got != 1 {
		t.Errorf("UIComponents len = %d, want 1", got)
	}

	// Smoke test: handlers wired through the bootstrap descriptors
	// can still be invoked.
	if _, err := res.MCPTools[0].Tool.Handler(context.Background(), nil); err != nil {
		t.Errorf("MCP handler error: %v", err)
	}
	if !mcpHandlerCalled {
		t.Errorf("MCP handler was not invoked")
	}

	mux := http.NewServeMux()
	MountHTTPHandlers(mux, res.HTTPHandlers)
	req, _ := http.NewRequest(http.MethodGet, "/api/demo/ping", nil)
	rw := &recordingResponseWriter{}
	mux.ServeHTTP(rw, req)
	if !httpHandlerCalled {
		t.Errorf("HTTP handler was not invoked")
	}
}

func TestBootstrap_AggregatesInitErrors(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	good := &fakePlugin{name: "good"}
	bad := &fakePlugin{name: "bad", initErr: errors.New("boom")}
	RegisterPlugin(good)
	RegisterPlugin(bad)

	_, err := Bootstrap(context.Background(), stubHost{}, nil)
	if err == nil {
		t.Fatalf("Bootstrap did not surface init error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error = %v, want it to mention bad plugin", err)
	}
	if good.initCalls != 1 {
		t.Errorf("good plugin Init calls = %d, want 1", good.initCalls)
	}
}

func TestBootstrap_ConfigPathFunc(t *testing.T) {
	resetRegistryForTest()
	t.Cleanup(resetRegistryForTest)

	p := &fakePlugin{name: "needs-config"}
	RegisterPlugin(p)

	wantPath := "/etc/archai/needs-config.yaml"
	gotName := ""
	gotPath := ""

	cfg := func(name string) string {
		gotName = name
		return wantPath
	}
	// Plug in a probe that captures configPath via Init's third arg.
	p.initErr = nil
	wrapped := &capturingPlugin{fakePlugin: p, capturedPath: &gotPath}
	resetRegistryForTest()
	RegisterPlugin(wrapped)

	if _, err := Bootstrap(context.Background(), stubHost{}, cfg); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	if gotName != "needs-config" {
		t.Errorf("ConfigPathFunc called with name = %q, want %q", gotName, "needs-config")
	}
	if gotPath != wantPath {
		t.Errorf("plugin saw configPath = %q, want %q", gotPath, wantPath)
	}
}

func TestAddCLICommandsToRoot_AttachesEveryCommand(t *testing.T) {
	root := &cobra.Command{Use: "archai"}
	cmds := []NamedCLICommand{
		{Plugin: "a", Command: CLICommand{Cmd: &cobra.Command{Use: "alpha"}}},
		{Plugin: "b", Command: CLICommand{Cmd: &cobra.Command{Use: "beta"}}},
		{Plugin: "c", Command: CLICommand{Cmd: nil}}, // should be skipped
	}
	AddCLICommandsToRoot(root, cmds)

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Use] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("missing commands in root: %v", names)
	}
}

// capturingPlugin records the configPath received by Init.
type capturingPlugin struct {
	*fakePlugin
	capturedPath *string
}

func (p *capturingPlugin) Init(ctx context.Context, h Host, configPath string) error {
	*p.capturedPath = configPath
	return p.fakePlugin.Init(ctx, h, configPath)
}

// recordingResponseWriter is a minimal http.ResponseWriter that
// satisfies the interface for our bootstrap smoke test.
type recordingResponseWriter struct {
	header http.Header
	status int
	body   []byte
}

func (r *recordingResponseWriter) Header() http.Header {
	if r.header == nil {
		r.header = http.Header{}
	}
	return r.header
}
func (r *recordingResponseWriter) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}
func (r *recordingResponseWriter) WriteHeader(status int) { r.status = status }
