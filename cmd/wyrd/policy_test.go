package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCallOnlyModule lays out a two-package module under a deny-by-default
// policy whose single allow rule does not cover a -> b. Package a uses b only
// through a call in a function body, so no type of b appears in a's
// declarations.
func writeCallOnlyModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module test.example/callonly\n\ngo 1.21\n")
	write("wyrd.yaml", `module: test.example/callonly
layers:
  all:
    - ...
policy:
  deny_by_default: true
  allow:
    - "c -> b"
`)
	write("b/b.go", `package b

// Name returns a name.
func Name() string { return "b" }
`)
	write("a/a.go", `package a

import "test.example/callonly/b"

// F reaches b only through a call.
func F() string { return b.Name() }
`)
	return root
}

// TestPolicyCheck_ReportsCallOnlyImport runs `wyrd policy check` on a module
// whose only a -> b link is a call: the import is the dependency, so the
// check must report it as an unlisted edge and fail.
func TestPolicyCheck_ReportsCallOnlyImport(t *testing.T) {
	root := writeCallOnlyModule(t)
	t.Chdir(root)

	cmd := newPolicyCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"check"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("policy check succeeded; want a violation for a -> b:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "1 dependency-policy violation(s) found") {
		t.Errorf("error = %v, want one violation", err)
	}
	if !strings.Contains(out.String(), "[unlisted-edge] a -> b") {
		t.Errorf("output missing the a -> b edge:\n%s", out.String())
	}
}
