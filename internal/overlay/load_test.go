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
