package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/buildinfo"
)

// TestVersionCommand verifies `archai version` prints `archai <Version>`.
func TestVersionCommand(t *testing.T) {
	orig := Version
	origBI := buildinfo.Version
	t.Cleanup(func() {
		Version = orig
		buildinfo.Version = origBI
	})

	Version = "v1.2.3-test"
	cmd := newVersionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := buf.String()
	want := "archai v1.2.3-test\n"
	if got != want {
		t.Fatalf("version output mismatch\n got: %q\nwant: %q", got, want)
	}
}

// TestVersionCommand_DevFallback checks that when Version is "dev"
// resolveVersion either returns a module-info version (inside `go test`
// with module info available) or falls back to "dev". The command must
// always start with "archai " and end with a newline.
func TestVersionCommand_DevFallback(t *testing.T) {
	orig := Version
	origBI := buildinfo.Version
	t.Cleanup(func() {
		Version = orig
		buildinfo.Version = origBI
	})

	Version = "dev"
	cmd := newVersionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "archai ") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("unexpected dev output: %q", got)
	}
	// Must not be empty after the space.
	trimmed := strings.TrimSpace(strings.TrimPrefix(got, "archai "))
	if trimmed == "" {
		t.Fatalf("version value is empty in %q", got)
	}
}

// TestResolveVersion_NonDev returns the linker-injected value verbatim
// when Version has been overridden from "dev".
func TestResolveVersion_NonDev(t *testing.T) {
	orig := Version
	origBI := buildinfo.Version
	t.Cleanup(func() {
		Version = orig
		buildinfo.Version = origBI
	})

	Version = "v0.5.0"
	if got := resolveVersion(); got != "v0.5.0" {
		t.Fatalf("resolveVersion = %q, want v0.5.0", got)
	}
}

// TestResolveVersion_MatchesBuildinfo verifies the CLI and the
// /api/version endpoint will report the same version string by going
// through the same buildinfo.Resolve() path.
func TestResolveVersion_MatchesBuildinfo(t *testing.T) {
	orig := Version
	origBI := buildinfo.Version
	t.Cleanup(func() {
		Version = orig
		buildinfo.Version = origBI
	})

	Version = "v7.7.7"
	cli := resolveVersion()
	api := buildinfo.Resolve().Version
	if cli != api {
		t.Fatalf("CLI version %q != API version %q", cli, api)
	}
}
