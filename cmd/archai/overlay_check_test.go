package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/overlay"
)

func TestPrintOverlayViolations_FormatsHumanReadableLines(t *testing.T) {
	violations := []overlay.Violation{
		{
			Package: "internal/service",
			Layer:   "service",
			Imports: []string{"internal/adapter/yaml"},
		},
		{
			Package: "internal/domain",
			Layer:   "domain",
			Imports: []string{"internal/adapter/yaml", "internal/overlay"},
		},
	}
	pkgLayer := map[string]string{
		"internal/adapter/yaml": "adapter",
		"internal/overlay":      "overlay",
	}

	var buf bytes.Buffer
	printOverlayViolations(&buf, violations, pkgLayer)

	out := buf.String()

	wantHeader := "Found 3 layer-rule violation(s):"
	if !strings.Contains(out, wantHeader) {
		t.Errorf("output missing header %q:\n%s", wantHeader, out)
	}

	wantLines := []string{
		"VIOLATION: package internal/service (layer service) imports package internal/adapter/yaml (layer adapter) — not allowed",
		"VIOLATION: package internal/domain (layer domain) imports package internal/adapter/yaml (layer adapter) — not allowed",
		"VIOLATION: package internal/domain (layer domain) imports package internal/overlay (layer overlay) — not allowed",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("output missing line %q:\n%s", want, out)
		}
	}
}

func TestPrintOverlayViolations_UnknownTargetLayerRendersQuestionMark(t *testing.T) {
	violations := []overlay.Violation{
		{
			Package: "internal/service",
			Layer:   "service",
			Imports: []string{"internal/adapter/yaml"},
		},
	}
	pkgLayer := map[string]string{} // empty — target layer unknown

	var buf bytes.Buffer
	printOverlayViolations(&buf, violations, pkgLayer)

	want := "imports package internal/adapter/yaml (layer ?)"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output missing %q:\n%s", want, buf.String())
	}
}

func TestViolationCount_SumsImportsAcrossViolations(t *testing.T) {
	v := []overlay.Violation{
		{Imports: []string{"a", "b"}},
		{Imports: []string{"c"}},
		{Imports: nil},
	}
	if got, want := violationCount(v), 3; got != want {
		t.Errorf("violationCount = %d, want %d", got, want)
	}
}

func TestTrimModulePrefix(t *testing.T) {
	cases := []struct {
		module, pkg, want string
	}{
		{"github.com/kgatilin/archai", "github.com/kgatilin/archai/internal/service", "internal/service"},
		{"github.com/kgatilin/archai", "github.com/kgatilin/archai", ""},
		{"github.com/kgatilin/archai", "github.com/other/mod/foo", "github.com/other/mod/foo"},
		{"github.com/kgatilin/archai", "internal/service", "internal/service"},
	}
	for _, c := range cases {
		if got := trimModulePrefix(c.module, c.pkg); got != c.want {
			t.Errorf("trimModulePrefix(%q, %q) = %q, want %q", c.module, c.pkg, got, c.want)
		}
	}
}
