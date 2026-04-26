package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleArchaiYAML = `module: github.com/example/app

layers:
  domain:
    - internal/domain/...
  service:
    - internal/service/...

layer_rules:
  service:
    - domain

aggregates:
  Order:
    root: github.com/example/app/internal/domain.Order

configs:
  - github.com/example/app/internal/config.AppConfig
`

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLoad_ValidYAML(t *testing.T) {
	path := writeTempFile(t, "archai.yaml", sampleArchaiYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}

	if cfg.Module != "github.com/example/app" {
		t.Errorf("Module = %q, want github.com/example/app", cfg.Module)
	}
	if got := len(cfg.Layers); got != 2 {
		t.Errorf("len(Layers) = %d, want 2", got)
	}
	if globs := cfg.Layers["domain"]; len(globs) != 1 || globs[0] != "internal/domain/..." {
		t.Errorf("Layers[domain] = %v, want [internal/domain/...]", globs)
	}
	if rules := cfg.LayerRules["service"]; len(rules) != 1 || rules[0] != "domain" {
		t.Errorf("LayerRules[service] = %v, want [domain]", rules)
	}
	if agg, ok := cfg.Aggregates["Order"]; !ok ||
		agg.Root != "github.com/example/app/internal/domain.Order" {
		t.Errorf("Aggregates[Order] = %+v, want root=...domain.Order", agg)
	}
	if len(cfg.Configs) != 1 ||
		cfg.Configs[0] != "github.com/example/app/internal/config.AppConfig" {
		t.Errorf("Configs = %v, want one AppConfig entry", cfg.Configs)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist.yaml") {
		t.Errorf("error should mention path, got: %v", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTempFile(t, "bad.yaml", "module: [unterminated\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "bad.yaml") {
		t.Errorf("error should mention path, got: %v", err)
	}
}

func TestLoad_UnknownField(t *testing.T) {
	// KnownFields(true) should reject unknown top-level keys so typos
	// in archai.yaml fail loudly instead of silently being ignored.
	path := writeTempFile(t, "archai.yaml",
		"module: github.com/example/app\nlayer:\n  domain: [internal/domain/...]\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestLoad_ServeHTTPAddr_Present(t *testing.T) {
	yaml := `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []

serve:
  http_addr: "0.0.0.0:47823"
`
	path := writeTempFile(t, "archai.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg.Serve.HTTPAddr != "0.0.0.0:47823" {
		t.Errorf("Serve.HTTPAddr = %q, want 0.0.0.0:47823", cfg.Serve.HTTPAddr)
	}
}

func TestLoad_ServeHTTPAddr_Absent(t *testing.T) {
	// When the serve block is omitted entirely, the zero-value
	// ServeConfig{} should be returned so callers can fall through
	// to flag defaults without ambiguity.
	path := writeTempFile(t, "archai.yaml", sampleArchaiYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if cfg.Serve.HTTPAddr != "" {
		t.Errorf("Serve.HTTPAddr = %q, want empty string", cfg.Serve.HTTPAddr)
	}
}

func TestLoad_SingleBCOverlay(t *testing.T) {
	// A single-bounded-context overlay with a human-readable name and an
	// adapters block must round-trip cleanly through the loader.
	yaml := `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates:
  domain:
    root: github.com/example/app/internal/domain.Model

bounded_contexts:
  model:
    name: "Model"
    description: "The package model"
    aggregates:
      - domain

adapters:
  go_extractor:
    name: "Go Extractor"
    direction: inbound
    packages:
      - internal/adapter/golang/...
  d2_emitter:
    direction: outbound
  http_server:
    direction: bidirectional
`
	path := writeTempFile(t, "archai.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if got := len(cfg.BoundedContexts); got != 1 {
		t.Fatalf("len(BoundedContexts) = %d, want 1", got)
	}
	bc, ok := cfg.BoundedContexts["model"]
	if !ok {
		t.Fatalf("missing 'model' bounded context: %+v", cfg.BoundedContexts)
	}
	if bc.Name != "Model" {
		t.Errorf("BoundedContexts[model].Name = %q, want Model", bc.Name)
	}
	if got := len(cfg.Adapters); got != 3 {
		t.Fatalf("len(Adapters) = %d, want 3", got)
	}
}

func TestLoad_AdapterDirection(t *testing.T) {
	cases := []string{"inbound", "outbound", "bidirectional"}
	for _, dir := range cases {
		t.Run(dir, func(t *testing.T) {
			yaml := `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []

adapters:
  example:
    direction: ` + dir + `
`
			path := writeTempFile(t, "archai.yaml", yaml)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load returned unexpected error: %v", err)
			}
			ad, ok := cfg.Adapters["example"]
			if !ok {
				t.Fatalf("missing 'example' adapter: %+v", cfg.Adapters)
			}
			if ad.Direction != dir {
				t.Errorf("Adapters[example].Direction = %q, want %q", ad.Direction, dir)
			}
		})
	}
}

func TestLoad_InvalidAdapterDirection(t *testing.T) {
	// Loader-level: KnownFields(true) accepts any string for `direction`,
	// but Validate must reject unknown values.
	goMod := writeGoMod(t, "github.com/example/app")
	yaml := `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []

adapters:
  bogus:
    direction: sideways
`
	path := writeTempFile(t, "archai.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if err := Validate(cfg, goMod); err == nil {
		t.Fatal("expected validation error for unknown adapter direction, got nil")
	} else if !strings.Contains(err.Error(), "sideways") {
		t.Errorf("expected error to mention bad direction, got: %v", err)
	}
}

func TestLoadComposed_PackageFragments(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  service:
    - internal/service/...

layer_rules:
  service: []

aggregates: {}
configs: []
`)
	writeFile(t, filepath.Join(root, "internal/service/.arch/overlay.yaml"), `aggregates:
  generation_service:
    root: Service

configs:
  - GenerateOptions
  - internal/domain.WriteOptions
  - github.com/other/mod/pkg.ExternalOptions
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}

	agg, ok := cfg.Aggregates["generation_service"]
	if !ok {
		t.Fatalf("missing generation_service aggregate: %+v", cfg.Aggregates)
	}
	if want := "github.com/example/app/internal/service.Service"; agg.Root != want {
		t.Fatalf("aggregate root = %q, want %q", agg.Root, want)
	}

	wantConfigs := []string{
		"github.com/example/app/internal/service.GenerateOptions",
		"github.com/example/app/internal/domain.WriteOptions",
		"github.com/other/mod/pkg.ExternalOptions",
	}
	if len(cfg.Configs) != len(wantConfigs) {
		t.Fatalf("configs = %v, want %v", cfg.Configs, wantConfigs)
	}
	for i, want := range wantConfigs {
		if cfg.Configs[i] != want {
			t.Fatalf("configs[%d] = %q, want %q", i, cfg.Configs[i], want)
		}
	}
}

func TestLoadComposed_SkipsTargetOverlayCopies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "archai.yaml"), `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []
`)
	writeFile(t, filepath.Join(root, ".arch/targets/v1/overlay.yaml"), `configs:
  - ShouldNotAppear
`)

	cfg, err := LoadComposed(filepath.Join(root, "archai.yaml"))
	if err != nil {
		t.Fatalf("LoadComposed: %v", err)
	}
	if len(cfg.Configs) != 0 {
		t.Fatalf("configs = %v, want none", cfg.Configs)
	}
}
