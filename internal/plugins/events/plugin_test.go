package events

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/eventmodel"
	"github.com/kgatilin/archai/internal/plugin"
)

// testdataRoot returns the absolute path to the testdata directory.
func testdataRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file location")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

// hostStub wires a fixed repo root into the plugin.
type hostStub struct {
	root string
}

func (h *hostStub) RepoRoot() string                                     { return h.root }
func (h *hostStub) CurrentModel() *plugin.Model                          { return nil }
func (h *hostStub) Targets() []plugin.TargetMeta                         { return nil }
func (h *hostStub) Target(string) (*plugin.TargetSnapshot, error)        { return nil, nil }
func (h *hostStub) ActiveTarget() *plugin.TargetSnapshot                 { return nil }
func (h *hostStub) Diff(string, string) (*plugin.Diff, error)            { return nil, nil }
func (h *hostStub) Validate(string) (*plugin.ValidationReport, error)    { return nil, nil }
func (h *hostStub) Subscribe(func(plugin.ModelEvent)) plugin.Unsubscribe { return func() {} }
func (h *hostStub) Logger() *slog.Logger                                 { return slog.Default() }

func TestPlugin_Manifest(t *testing.T) {
	p := &Plugin{}
	mf := p.Manifest()
	if mf.Name != "events" {
		t.Errorf("Manifest.Name = %q, want %q", mf.Name, "events")
	}
}

func TestPlugin_ValidateCmd_Clean(t *testing.T) {
	// Use contracts fixture - it's vocabulary-only and has no cross-component dependencies.
	root := filepath.Join(testdataRoot(t), "contracts")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cmd := p.validateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err != nil {
		t.Errorf("validate should pass for clean fixtures: %v", err)
	}
	if !strings.Contains(out.String(), "OK") {
		t.Errorf("output should contain OK: %s", out.String())
	}
}

func TestPlugin_ValidateCmd_WithErrors(t *testing.T) {
	// Use violations fixture which has deliberate rule violations.
	root := filepath.Join(testdataRoot(t), "violations")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cmd := p.validateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Error("validate should fail when violations are present")
	}
	output := out.String()
	if !strings.Contains(output, "ERROR") {
		t.Errorf("output should contain ERROR: %s", output)
	}
	for _, want := range []string{"exclusive-unhandled", "partition-mismatch", "malformed-slot", "unresolved-ref", "kind-role-conflict", "self-receive-conflict"} {
		if !strings.Contains(output, want) {
			t.Errorf("output should mention %s: %s", want, output)
		}
	}
	// The fixture deliberately emits a fact into, and observes an action from, a
	// namespace it does not own. Under event-sourced semantics that is legal.
	if strings.Contains(output, "ownership-violation") {
		t.Errorf("ownership must not restrict production or observation: %s", output)
	}
}

func TestPlugin_GraphCmd_Mermaid(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cmd := p.graphCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("graph: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "flowchart") {
		t.Errorf("mermaid output should contain flowchart: %s", output)
	}
}

func TestPlugin_GraphCmd_GraphML(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cmd := p.graphCmd()
	cmd.SetArgs([]string{"--format", "graphml"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("graph: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "graphml") {
		t.Errorf("graphml output should contain graphml: %s", output)
	}
}

func TestPlugin_MCPEventModel(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tools := p.MCPTools()
	var modelTool *plugin.MCPTool
	for i := range tools {
		if tools[i].Name == "event_model" {
			modelTool = &tools[i]
			break
		}
	}
	if modelTool == nil {
		t.Fatal("event_model tool not found")
	}

	out, err := modelTool.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(text, "COMPONENT: billing") {
		t.Errorf("output should contain billing component: %s", text)
	}
}

func TestPlugin_MCPEventKind(t *testing.T) {
	// Use billing fixture for kind lookup.
	root := filepath.Join(testdataRoot(t), "billing")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tools := p.MCPTools()
	var kindTool *plugin.MCPTool
	for i := range tools {
		if tools[i].Name == "event_kind" {
			kindTool = &tools[i]
			break
		}
	}
	if kindTool == nil {
		t.Fatal("event_kind tool not found")
	}

	out, err := kindTool.Handler(context.Background(), map[string]any{
		"kind": "billing.invoice.issued",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(text, "KIND: billing.invoice.issued") {
		t.Errorf("output should contain kind header: %s", text)
	}
	if !strings.Contains(text, "Producers:") {
		t.Errorf("output should contain Producers: %s", text)
	}
	if !strings.Contains(text, "Consumers:") {
		t.Errorf("output should contain Consumers: %s", text)
	}
}

// TestPlugin_MCPEventKind_Delivery checks that the delivery policy and the
// schema-owner framing reach the agent-facing output. Broadcast is the default
// and must be stated explicitly, so an agent never has to assume.
func TestPlugin_MCPEventKind_Delivery(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "ledger")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tools := p.MCPTools()
	var kindTool *plugin.MCPTool
	for i := range tools {
		if tools[i].Name == "event_kind" {
			kindTool = &tools[i]
			break
		}
	}
	if kindTool == nil {
		t.Fatal("event_kind tool not found")
	}

	cases := []struct {
		kind string
		want string
	}{
		{"ledger.entry.post", "Delivery: exclusive"},
		{"ledger.entry.posted", "Delivery: broadcast"},
	}
	for _, tc := range cases {
		out, err := kindTool.Handler(context.Background(), map[string]any{"kind": tc.kind})
		if err != nil {
			t.Fatalf("handler(%s): %v", tc.kind, err)
		}
		text := out.(string)
		if !strings.Contains(text, tc.want) {
			t.Errorf("%s: output should contain %q: %s", tc.kind, tc.want, text)
		}
		if !strings.Contains(text, "Schema owner: ledger") {
			t.Errorf("%s: output should name the schema owner: %s", tc.kind, text)
		}
	}
}

func TestPlugin_MCPEventValidate(t *testing.T) {
	// Use violations fixture which has deliberate rule violations.
	root := filepath.Join(testdataRoot(t), "violations")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tools := p.MCPTools()
	var validateTool *plugin.MCPTool
	for i := range tools {
		if tools[i].Name == "event_validate" {
			validateTool = &tools[i]
			break
		}
	}
	if validateTool == nil {
		t.Fatal("event_validate tool not found")
	}

	out, err := validateTool.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	text, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	// Should have errors from violations fixture.
	if !strings.Contains(text, "ERROR") {
		t.Errorf("output should contain ERROR: %s", text)
	}
}

func TestPlugin_HTTPHandler(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	handlers := p.HTTPHandlers()
	if len(handlers) != 1 {
		t.Fatalf("HTTPHandlers len = %d, want 1", len(handlers))
	}

	srv := httptest.NewServer(handlers[0].Handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Components map[string]*eventmodel.Component `json:"components"`
		Graph      *eventmodel.Graph                `json:"graph"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Components == nil {
		t.Error("components is nil")
	}
	if result.Graph == nil {
		t.Error("graph is nil")
	}
	if _, ok := result.Components["billing"]; !ok {
		t.Error("billing component not found")
	}
}

func TestPlugin_UIComponents_Empty(t *testing.T) {
	p := &Plugin{}
	comps := p.UIComponents()
	if len(comps) != 0 {
		t.Errorf("UIComponents should be empty for v1, got %d", len(comps))
	}
}

// TestPlugin_ValidateCmd_RootFlag exercises the --root flag to scan a specific
// directory, bypassing the testdata skip in the reader.
func TestPlugin_ValidateCmd_RootFlag(t *testing.T) {
	// Initialize with an empty host root so we must use --root.
	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: ""}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	t.Run("valid_set", func(t *testing.T) {
		cmd := p.validateCmd()
		cmd.SetArgs([]string{"--root", filepath.Join(testdataRoot(t), "contracts")})
		var out bytes.Buffer
		cmd.SetOut(&out)

		err := cmd.Execute()
		if err != nil {
			t.Errorf("validate with --root should pass for clean fixture: %v", err)
		}
		if !strings.Contains(out.String(), "OK") {
			t.Errorf("output should contain OK: %s", out.String())
		}
	})

	t.Run("violation_set", func(t *testing.T) {
		cmd := p.validateCmd()
		cmd.SetArgs([]string{"--root", filepath.Join(testdataRoot(t), "violations")})
		var out bytes.Buffer
		cmd.SetOut(&out)

		err := cmd.Execute()
		if err == nil {
			t.Error("validate with --root should fail for violations fixture")
		}
		output := out.String()
		if !strings.Contains(output, "ERROR") {
			t.Errorf("output should contain ERROR: %s", output)
		}
	})
}

// TestPlugin_GraphCmd_RootFlag exercises the --root flag for graph generation.
func TestPlugin_GraphCmd_RootFlag(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: ""}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cmd := p.graphCmd()
	cmd.SetArgs([]string{"--root", filepath.Join(testdataRoot(t), "billing")})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("graph with --root: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "flowchart") {
		t.Errorf("mermaid output should contain flowchart: %s", output)
	}
	if !strings.Contains(output, "billing") {
		t.Errorf("mermaid output should contain billing: %s", output)
	}
}
