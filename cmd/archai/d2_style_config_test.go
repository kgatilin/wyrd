package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/d2"
)

func TestLoadD2StyleConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archai.yaml")
	content := `module: github.com/example/app

layers:
  app:
    - "..."

layer_rules:
  app: []

aggregates: {}
configs: []

diagrams:
  d2:
    styles:
      factory:
        container_fill: "#dcfce7"
        container_font_color: "#052e16"
        class_fill: "#14532d"
        class_font_color: "#f0fdf4"
      legend:
        fill: "#ffffff"
        stroke: "#d1d5db"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write archai.yaml: %v", err)
	}

	got, err := loadD2StyleConfig(path)
	if err != nil {
		t.Fatalf("loadD2StyleConfig() error = %v", err)
	}

	if got.Factory.ContainerFill != "#dcfce7" {
		t.Errorf("Factory.ContainerFill = %q, want #dcfce7", got.Factory.ContainerFill)
	}
	if got.Factory.ClassFill != "#14532d" {
		t.Errorf("Factory.ClassFill = %q, want #14532d", got.Factory.ClassFill)
	}
	if got.Legend.Stroke != "#d1d5db" {
		t.Errorf("Legend.Stroke = %q, want #d1d5db", got.Legend.Stroke)
	}

	writer := d2.NewWriterWithStyle(got)
	if writer == nil {
		t.Fatal("NewWriterWithStyle() returned nil")
	}
}
