package events

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kgatilin/wyrd/internal/eventmodel"
	"github.com/kgatilin/wyrd/internal/plugin"
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
	for _, want := range []string{"exclusive-unhandled", "partition-mismatch", "malformed-slot", "unresolved-ref", "kind-pattern-conflict", "self-input-conflict"} {
		if !strings.Contains(output, want) {
			t.Errorf("output should mention %s: %s", want, output)
		}
	}
	// The fixture deliberately appends into, and is triggered by, a namespace it
	// does not own. Under event-sourced semantics that is legal.
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
	if !strings.Contains(text, "Subject: svc.*.billing.{account}.invoice.issued") {
		t.Errorf("output should contain the kind's address: %s", text)
	}
	if !strings.Contains(text, "Outputs (producers): billing") {
		t.Errorf("output should name the producers: %s", text)
	}
	if !strings.Contains(text, "Inputs (triggered):") {
		t.Errorf("output should report what the kind triggers: %s", text)
	}
	if !strings.Contains(text, "State events (folded by): billing") {
		t.Errorf("output should report who folds the kind: %s", text)
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

	var result modelView
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var billing *componentView
	for i := range result.Components {
		if result.Components[i].ID == "billing" {
			billing = &result.Components[i]
		}
	}
	if billing == nil {
		t.Fatalf("billing component not found in %d component(s)", len(result.Components))
	}
	if len(billing.Outputs) == 0 {
		t.Error("billing declares outputs; none survived the projection")
	}
	if len(result.Kinds) == 0 {
		t.Error("kinds is empty")
	}

	// The response answers in the canvas's units: an edge is one component
	// reaching another, not a hop through a kind node. This fixture is one
	// component folding its own output, which draws nothing — the idiom is so
	// common that the self-loop would sit on nearly every node and say nothing.
	if len(result.Flows) != 0 {
		t.Errorf("flows = %+v, want none for a lone component folding its own output", result.Flows)
	}

	// The kind it appends into a namespace it does not own reaches nobody, and
	// the projection carries that verdict rather than leaving it to be noticed.
	var post *kindView
	for i := range result.Kinds {
		if result.Kinds[i].Name == "ledger.entry.post" {
			post = &result.Kinds[i]
		}
	}
	if post == nil {
		t.Fatal("no kind view for ledger.entry.post")
	}
	if post.Health != string(eventmodel.HealthOrphan) {
		t.Errorf("health = %q, want orphan", post.Health)
	}
	if !reflect.DeepEqual(post.Producers, []string{"billing"}) {
		t.Errorf("producers = %v, want [billing]", post.Producers)
	}
}

// A kind carries an example beside its schema, with $refs already followed:
// the canvas has no way to resolve "#/types/Hold" against the component that
// declared it, so the projection resolves it here.
func TestPlugin_HTTPHandler_KindExample(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "shop")

	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	srv := httptest.NewServer(p.HTTPHandlers()[0].Handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	var result modelView
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byName := make(map[string]kindView, len(result.Kinds))
	for _, kind := range result.Kinds {
		byName[kind.Name] = kind
	}

	// inventory.event.reserved is declared as a bare $ref to a local type.
	reserved, ok := byName["inventory.event.reserved"]
	if !ok {
		t.Fatal("no kind view for inventory.event.reserved")
	}
	object, ok := reserved.Example.(map[string]any)
	if !ok {
		t.Fatalf("example = %#v, want an object", reserved.Example)
	}
	if object["OrderID"] != "string" {
		t.Errorf("example[OrderID] = %#v, want \"string\"", object["OrderID"])
	}
	lines, ok := object["Lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("example[Lines] = %#v, want one element", object["Lines"])
	}
	line, ok := lines[0].(map[string]any)
	if !ok || line["Sku"] != "string" {
		t.Errorf("example line = %#v, want the Line type expanded", lines[0])
	}

	// An imported declaration reads the same way; its schema is inline, so
	// the example is built from the payload the AsyncAPI document states.
	authorize, ok := byName["payments.command.authorize"]
	if !ok {
		t.Fatal("no kind view for payments.command.authorize")
	}
	command, ok := authorize.Example.(map[string]any)
	if !ok {
		t.Fatalf("example = %#v, want an object", authorize.Example)
	}
	// `currency` declares a default, and a stated value beats a placeholder.
	if command["currency"] != "EUR" {
		t.Errorf("example[currency] = %#v, want \"EUR\"", command["currency"])
	}
}

// An empty repo answers with empty lists rather than nulls, so the canvas
// iterates the response without a guard on every field.
func TestPlugin_HTTPHandler_EmptyRepo(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: t.TempDir()}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	srv := httptest.NewServer(p.HTTPHandlers()[0].Handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got, want := strings.TrimSpace(string(body)), `{"components":[],"flows":[],"kinds":[]}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
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

// --- gen ---------------------------------------------------------------------

// writeTemplate drops a template into a temp dir and returns the dir.
func writeTemplate(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func genPlugin(t *testing.T, root string) *Plugin {
	t.Helper()
	p := &Plugin{}
	if err := p.Init(context.Background(), &hostStub{root: root}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
}

func TestPlugin_GenCmd_RendersPerComponent(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl",
		"package {{ .Component }}\n{{ range .Kinds }}// {{ goIdent . }} = {{ quote . }}\n{{ end }}")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out.String())
	}

	got, err := os.ReadFile(filepath.Join(outDir, "billing", "contract_gen.go"))
	if err != nil {
		t.Fatalf("generated file: %v", err)
	}
	for _, want := range []string{"package billing", `BillingInvoiceIssued = "billing.invoice.issued"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("generated file should contain %q:\n%s", want, got)
		}
	}
}

// --out redirects everything, so generation can be exercised without touching
// the source tree.
func TestPlugin_GenCmd_DryRunWritesNothing(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package {{ .Component }}\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir, "--dry-run"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v", err)
	}
	if !strings.Contains(out.String(), "would write") {
		t.Errorf("dry run should report intent: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "billing", "contract_gen.go")); !os.IsNotExist(err) {
		t.Error("dry run must not write anything")
	}
}

// Generating from a declaration set with errors produces code that is wrong in
// ways the compiler cannot catch, so it is refused by default.
func TestPlugin_GenCmd_RefusesInvalidModel(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "violations")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package {{ .Component }}\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir})
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("gen should refuse an invalid model")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should point at the escape hatch: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "violations", "contract_gen.go")); !os.IsNotExist(statErr) {
		t.Error("nothing should be written when validation fails")
	}
}

func TestPlugin_GenCmd_ForceOverridesValidation(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "violations")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package {{ .Component }}\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir, "--force"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen --force: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "violations", "contract_gen.go")); err != nil {
		t.Errorf("--force should generate anyway: %v", err)
	}
}

// The generator owns *_gen.* and nothing else: a template that would write a
// plausibly-handwritten file is rejected before anything is generated.
func TestPlugin_GenCmd_RejectsNonGeneratedOutputName(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract.go.tmpl", "package {{ .Component }}\n")

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", t.TempDir()})
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("want an error for a template that writes a handwritten-looking file")
	}
	if !strings.Contains(err.Error(), "_gen.") {
		t.Errorf("error should name the required marker: %v", err)
	}
}

func TestPlugin_GenCmd_NoTemplates(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", t.TempDir(), "--out", t.TempDir()})
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("want an error when no templates exist")
	}
	if !strings.Contains(err.Error(), "template-driven") {
		t.Errorf("error should explain that projects supply templates: %v", err)
	}
}

func TestPlugin_GenCmd_ComponentFilter(t *testing.T) {
	root := testdataRoot(t)
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package {{ .Component }}\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir, "--component", "ledger", "--force"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "ledger", "contract_gen.go")); err != nil {
		t.Errorf("selected component should be generated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "billing", "contract_gen.go")); !os.IsNotExist(err) {
		t.Error("unselected components must be skipped")
	}
}

func TestPlugin_GenCmd_UnknownComponent(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package {{ .Component }}\n")

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", t.TempDir(), "--component", "nope"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("want an error for an unknown component")
	}
}

// Without --out, generated files land next to the component's .arch directory.
func TestPlugin_GenCmd_WritesNextToComponent(t *testing.T) {
	root := t.TempDir()
	archDir := filepath.Join(root, "billing", ".arch")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decl := `version: 2
component: billing
owns: billing
outputs:
  - kind: billing.invoice.issued
inputs:
  - kind: ledger.entry.posted
`
	if err := os.WriteFile(filepath.Join(archDir, "events.yaml"), []byte(decl), 0o644); err != nil {
		t.Fatal(err)
	}
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package {{ .Component }}\n")

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "billing", "contract_gen.go")); err != nil {
		t.Errorf("output should sit beside the component's .arch dir: %v", err)
	}
}

// Generated files are committed, so .go output is canonicalized: an unformatted
// template must not produce a noisy diff.
func TestPlugin_GenCmd_FormatsGoOutput(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl",
		"package  billing\nconst (\nA = 1\nBB = 2\n)\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(filepath.Join(outDir, "billing", "contract_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\tA  = 1\n\tBB = 2\n") {
		t.Errorf("output should be gofmt-canonical:\n%q", got)
	}
}

// A template emitting invalid Go should fail here, not at compile time.
func TestPlugin_GenCmd_InvalidGoIsAnError(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package billing\nfunc (\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir})
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("want an error for a template producing invalid Go")
	}
	if !strings.Contains(err.Error(), "invalid Go") {
		t.Errorf("error should say what is wrong: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "billing", "contract_gen.go")); !os.IsNotExist(statErr) {
		t.Error("nothing should be written when the template produces invalid Go")
	}
}

// Formatting is Go-specific; other extensions pass through untouched.
func TestPlugin_GenCmd_NonGoOutputIsNotFormatted(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.json.tmpl", "{  \"component\": {{ quote .Component }}  }")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(filepath.Join(outDir, "billing", "contract_gen.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{  "component": "billing"  }` {
		t.Errorf("non-Go output must pass through verbatim: %q", got)
	}
}

func TestPlugin_GenCmd_NoFormatFlag(t *testing.T) {
	root := filepath.Join(testdataRoot(t), "billing")
	tmplDir := writeTemplate(t, "contract_gen.go.tmpl", "package  billing\nconst A=1\n")
	outDir := t.TempDir()

	cmd := genPlugin(t, root).genCmd()
	cmd.SetArgs([]string{"--templates", tmplDir, "--out", outDir, "--no-format"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "billing", "contract_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package  billing\nconst A=1\n" {
		t.Errorf("--no-format should write the template output verbatim: %q", got)
	}
}
