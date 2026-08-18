package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildCheckBinary compiles archai-check once per test run.
func buildCheckBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "archai-check")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building archai-check: %v\n%s", err, out)
	}
	return bin
}

// writeLayeredModule lays out a two-package module whose overlay forbids
// domain -> adapter. When invert is true the domain package imports the
// adapter, which is the violation.
func writeLayeredModule(t *testing.T, invert bool) string {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, content string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("go.mod", "module test.example/layered\n\ngo 1.21\n")
	write("archai.yaml", `module: test.example/layered
layers:
  domain:
    - internal/domain/...
  adapter:
    - internal/adapter/...
layer_rules:
  adapter:
    - domain
  domain: []
policy:
  allow:
    - "internal/adapter/... -> internal/domain/..."
`)

	if invert {
		write("internal/domain/model.go", `package domain

import "test.example/layered/internal/adapter"

// Model reaches down into the adapter layer, which the rules forbid.
type Model struct {
	Reader adapter.Reader
}
`)
		write("internal/adapter/reader.go", `package adapter

// Reader is an adapter type.
type Reader struct{}
`)
	} else {
		write("internal/domain/model.go", `package domain

// Model is a domain type.
type Model struct {
	Name string
}
`)
		write("internal/adapter/reader.go", `package adapter

import "test.example/layered/internal/domain"

// Reader reads models, which the rules allow.
type Reader struct{}

// Read returns a model.
func (r Reader) Read() domain.Model { return domain.Model{} }
`)
	}
	return dir
}

func TestE2E_Overlay_CleanModulePasses(t *testing.T) {
	bin := buildCheckBinary(t)
	dir := writeLayeredModule(t, false)

	cmd := exec.Command(bin, "-C", dir, "overlay")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("archai-check overlay failed on a clean module: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "no layer-rule violations found") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestE2E_Overlay_InversionFailsWithNonZeroExit(t *testing.T) {
	bin := buildCheckBinary(t)
	dir := writeLayeredModule(t, true)

	cmd := exec.Command(bin, "-C", dir, "overlay")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("archai-check overlay exited 0 on a layering inversion:\n%s", out)
	}
	want := "VIOLATION: package internal/domain (layer domain) imports package internal/adapter (layer adapter)"
	if !strings.Contains(string(out), want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
}

func TestE2E_All_RunsBothGates(t *testing.T) {
	bin := buildCheckBinary(t)
	dir := writeLayeredModule(t, false)

	cmd := exec.Command(bin, "-C", dir, "all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("archai-check all failed on a clean module: %v\n%s", err, out)
	}
	for _, want := range []string{"no layer-rule violations found", "no dependency-policy violations"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestE2E_NoOverlayIsAClearError(t *testing.T) {
	bin := buildCheckBinary(t)

	cmd := exec.Command(bin, "-C", t.TempDir(), "overlay")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("archai-check overlay exited 0 without an overlay:\n%s", out)
	}
	if !strings.Contains(string(out), "no overlay found") {
		t.Errorf("output missing the 'no overlay found' hint:\n%s", out)
	}
}
