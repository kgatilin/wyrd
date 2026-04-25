package complexity

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/plugin"
)

// realHostStub wires a fixed Model into the plugin so tests don't
// need to load a real project from disk.
type realHostStub struct{ model *plugin.Model }

func (h *realHostStub) CurrentModel() *plugin.Model                       { return h.model }
func (h *realHostStub) Targets() []plugin.TargetMeta                      { return nil }
func (h *realHostStub) Target(string) (*plugin.TargetSnapshot, error)     { return nil, nil }
func (h *realHostStub) ActiveTarget() *plugin.TargetSnapshot              { return nil }
func (h *realHostStub) Diff(string, string) (*plugin.Diff, error)         { return nil, nil }
func (h *realHostStub) Validate(string) (*plugin.ValidationReport, error) { return nil, nil }
func (h *realHostStub) Subscribe(func(plugin.ModelEvent)) plugin.Unsubscribe {
	return func() {}
}
func (h *realHostStub) Logger() *slog.Logger { return slog.Default() }

func TestComplexity_Manifest(t *testing.T) {
	p := &Plugin{}
	mf := p.Manifest()
	if mf.Name != "complexity" {
		t.Errorf("Manifest.Name = %q, want %q", mf.Name, "complexity")
	}
}

func TestComplexity_Scores(t *testing.T) {
	model := &plugin.Model{
		Module: "acme.io/x",
		Packages: []*domain.PackageModel{
			{
				Path:       "internal/heavy",
				Name:       "heavy",
				Layer:      "service",
				Interfaces: []domain.InterfaceDef{{Name: "I1"}, {Name: "I2"}},
				Structs: []domain.StructDef{
					{Name: "S1", Methods: []domain.MethodDef{{Name: "M1"}, {Name: "M2"}}},
				},
				Functions: []domain.FunctionDef{{Name: "F1"}, {Name: "F2"}, {Name: "F3"}},
			},
			{
				Path:       "internal/light",
				Name:       "light",
				Layer:      "domain",
				Interfaces: []domain.InterfaceDef{{Name: "Tiny"}},
			},
		},
	}
	host := &realHostStub{model: model}
	p := &Plugin{}
	if err := p.Init(context.Background(), host, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}

	scores := p.scores()
	if len(scores) != 2 {
		t.Fatalf("scores len = %d, want 2", len(scores))
	}
	// Heavy first (sorted by score desc).
	if scores[0].Package != "internal/heavy" {
		t.Errorf("scores[0].Package = %q, want internal/heavy", scores[0].Package)
	}
	if scores[0].Score < scores[1].Score {
		t.Errorf("scores not sorted desc: %v", scores)
	}
}

func TestComplexity_HTTPHandler(t *testing.T) {
	model := &plugin.Model{
		Packages: []*domain.PackageModel{{Path: "internal/x", Functions: []domain.FunctionDef{{Name: "F"}}}},
	}
	p := &Plugin{}
	if err := p.Init(context.Background(), &realHostStub{model: model}, ""); err != nil {
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
	var got []PackageScore
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Package != "internal/x" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestComplexity_MCPTool(t *testing.T) {
	model := &plugin.Model{
		Packages: []*domain.PackageModel{{Path: "internal/x", Functions: []domain.FunctionDef{{Name: "F"}}}},
	}
	p := &Plugin{}
	if err := p.Init(context.Background(), &realHostStub{model: model}, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	tools := p.MCPTools()
	if len(tools) != 1 {
		t.Fatalf("MCPTools len = %d, want 1", len(tools))
	}
	out, err := tools[0].Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("MCP handler: %v", err)
	}
	scores, ok := out.([]PackageScore)
	if !ok {
		t.Fatalf("MCP handler returned %T, want []PackageScore", out)
	}
	if len(scores) != 1 {
		t.Errorf("scores len = %d, want 1", len(scores))
	}
}
