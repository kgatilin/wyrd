package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

// setupSmallProj writes a two-package Go module into a temp dir and
// returns its root path. Each test then runs `extract` against it.
func setupSmallProj(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test.example/smallproj\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	// Package 1: greet
	if err := os.MkdirAll(filepath.Join(root, "greet"), 0o755); err != nil {
		t.Fatal(err)
	}
	greet := `package greet

// Hello returns a greeting.
func Hello(name string) string { return "hello " + name }
`
	if err := os.WriteFile(filepath.Join(root, "greet", "greet.go"), []byte(greet), 0o644); err != nil {
		t.Fatal(err)
	}
	// Package 2: math
	if err := os.MkdirAll(filepath.Join(root, "mathx"), 0o755); err != nil {
		t.Fatal(err)
	}
	math := `package mathx

// Add adds two ints.
func Add(a, b int) int { return a + b }
`
	if err := os.WriteFile(filepath.Join(root, "mathx", "math.go"), []byte(math), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestExtract_StreamYAML verifies the default path: per-package YAML
// documents streamed to stdout separated by `---`.
func TestExtract_StreamYAML(t *testing.T) {
	root := setupSmallProj(t)

	cmd := newExtractCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{root})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "package: greet") {
		t.Errorf("missing greet package in output:\n%s", got)
	}
	if !strings.Contains(got, "package: mathx") {
		t.Errorf("missing mathx package in output:\n%s", got)
	}
	if !strings.Contains(got, "---\n") {
		t.Errorf("expected `---` separator between packages:\n%s", got)
	}

	// Every document must round-trip via yaml unmarshal.
	docs := strings.Split(got, "\n---\n")
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 yaml docs, got %d", len(docs))
	}
	for i, doc := range docs {
		var m map[string]any
		if err := yamlv3.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatalf("doc %d failed to parse as yaml: %v\n%s", i, err, doc)
		}
		if _, ok := m["package"]; !ok {
			t.Errorf("doc %d missing 'package' key", i)
		}
	}
}

// TestExtract_StreamJSON verifies JSON format output is a single valid
// JSON array of package documents.
func TestExtract_StreamJSON(t *testing.T) {
	root := setupSmallProj(t)

	cmd := newExtractCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{root, "--format", "json"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var docs []map[string]any
	if err := json.Unmarshal(out.Bytes(), &docs); err != nil {
		t.Fatalf("not valid json: %v\n%s", err, out.String())
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 package docs, got %d: %v", len(docs), docs)
	}
	paths := map[string]bool{}
	for _, d := range docs {
		if p, ok := d["package"].(string); ok {
			paths[p] = true
		}
	}
	if !paths["greet"] || !paths["mathx"] {
		t.Errorf("missing package paths, got: %v", paths)
	}
}

// TestExtract_WriteOutDir verifies per-package files are written under
// the requested --out directory.
func TestExtract_WriteOutDir(t *testing.T) {
	root := setupSmallProj(t)
	outDir := filepath.Join(root, ".arch", "packages")

	cmd := newExtractCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{root, "--out", outDir})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !strings.Contains(out.String(), "Extracted 2 package(s)") {
		t.Errorf("expected summary line, got: %s", out.String())
	}

	// Both package YAML files must exist and round-trip.
	for _, rel := range []string{"greet/internal.yaml", "mathx/internal.yaml"} {
		full := filepath.Join(outDir, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("reading %s: %v", full, err)
		}
		var m map[string]any
		if err := yamlv3.Unmarshal(data, &m); err != nil {
			t.Fatalf("parsing %s: %v", full, err)
		}
		if _, ok := m["package"]; !ok {
			t.Errorf("%s missing 'package' key", full)
		}
	}
}

// TestExtract_UnsupportedFormat rejects unknown formats.
func TestExtract_UnsupportedFormat(t *testing.T) {
	cmd := newExtractCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{".", "--format", "xml"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("unexpected error: %v", err)
	}
}
