package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGoMod(t *testing.T, module string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	content := "module " + module + "\n\ngo 1.21\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return path
}

// validConfig returns a fully-valid Config paired with a matching go.mod
// path. Individual tests mutate fields to exercise specific errors.
func validConfig(t *testing.T) (*Config, string) {
	t.Helper()
	return &Config{
		Module: "github.com/example/app",
		Layers: map[string][]string{
			"domain":  {"internal/domain/..."},
			"service": {"internal/service/..."},
		},
		LayerRules: map[string][]string{
			"service": {"domain"},
		},
		Aggregates: map[string]Aggregate{
			"Order": {Root: "github.com/example/app/internal/domain.Order"},
		},
		Configs: []string{"github.com/example/app/internal/config.AppConfig"},
	}, writeGoMod(t, "github.com/example/app")
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg, goMod := validConfig(t)
	if err := Validate(cfg, goMod); err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
}

func TestValidate_NilConfig(t *testing.T) {
	if err := Validate(nil, ""); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestValidate_ModuleMismatch(t *testing.T) {
	cfg, _ := validConfig(t)
	goMod := writeGoMod(t, "github.com/other/app")

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected module mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "module mismatch") {
		t.Errorf("expected 'module mismatch' in error, got: %v", err)
	}
}

func TestValidate_EmptyModule(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.Module = ""

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected error for empty module")
	}
	if !strings.Contains(err.Error(), "module is required") {
		t.Errorf("expected 'module is required' in error, got: %v", err)
	}
}

func TestValidate_NoLayers(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.Layers = nil

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected error for missing layers")
	}
	if !strings.Contains(err.Error(), "at least one layer") {
		t.Errorf("expected 'at least one layer' in error, got: %v", err)
	}
}

func TestValidate_EmptyLayerGlobs(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.Layers["domain"] = nil

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected error for empty globs")
	}
	if !strings.Contains(err.Error(), "no package globs") {
		t.Errorf("expected 'no package globs' in error, got: %v", err)
	}
}

func TestValidate_BadGlobs(t *testing.T) {
	cases := map[string]string{
		"empty glob":         "",
		"whitespace":         "internal/ domain/...",
		"absolute":           "/internal/domain/...",
		"interior ellipsis":  "internal/.../domain",
		"trailing whitespace": "internal/domain/... ",
	}
	for name, glob := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, goMod := validConfig(t)
			cfg.Layers["domain"] = []string{glob}

			if err := Validate(cfg, goMod); err == nil {
				t.Fatalf("expected error for glob %q", glob)
			}
		})
	}
}

func TestValidate_LayerRuleUnknownKey(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.LayerRules["ghost"] = []string{"domain"}

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected error for unknown rule key")
	}
	if !strings.Contains(err.Error(), "unknown layer \"ghost\"") {
		t.Errorf("expected 'unknown layer \"ghost\"' in error, got: %v", err)
	}
}

func TestValidate_LayerRuleUnknownTarget(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.LayerRules["service"] = []string{"phantom"}

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected error for unknown rule target")
	}
	if !strings.Contains(err.Error(), "phantom") {
		t.Errorf("expected 'phantom' in error, got: %v", err)
	}
}

func TestValidate_BadAggregateRoot(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"no dot":          "OrderRoot",
		"trailing dot":    "github.com/example/app/internal/domain.",
		"leading dot":     ".Order",
		"slash in name":   "github.com/example/app/internal.domain/Order",
	}
	for name, root := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, goMod := validConfig(t)
			cfg.Aggregates["Bad"] = Aggregate{Root: root}

			if err := Validate(cfg, goMod); err == nil {
				t.Fatalf("expected error for root %q", root)
			}
		})
	}
}

func TestValidate_BadConfigRef(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.Configs = append(cfg.Configs, "not-qualified")

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected error for malformed config ref")
	}
	if !strings.Contains(err.Error(), "configs[1]") {
		t.Errorf("expected 'configs[1]' in error, got: %v", err)
	}
}

func TestValidate_SkipsGoModCheckWhenPathEmpty(t *testing.T) {
	cfg, _ := validConfig(t)
	// A caller that doesn't know where go.mod lives should still be able
	// to validate the rest of the overlay.
	if err := Validate(cfg, ""); err != nil {
		t.Fatalf("Validate with empty goModPath returned error: %v", err)
	}
}

func TestValidate_MissingGoMod(t *testing.T) {
	cfg, _ := validConfig(t)
	err := Validate(cfg, filepath.Join(t.TempDir(), "go.mod"))
	if err == nil {
		t.Fatal("expected error for missing go.mod")
	}
}

func TestValidate_AggregatesWithMultipleErrorsJoined(t *testing.T) {
	cfg, goMod := validConfig(t)
	cfg.Aggregates["Bad1"] = Aggregate{Root: ""}
	cfg.Aggregates["Bad2"] = Aggregate{Root: "NoDot"}

	err := Validate(cfg, goMod)
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Bad1") || !strings.Contains(msg, "Bad2") {
		t.Errorf("expected both Bad1 and Bad2 reported, got: %v", err)
	}
}
