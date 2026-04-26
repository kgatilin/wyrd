package main

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests exercise loadServeHTTPAddrFromOverlay directly. The
// runServe wrapper consults this helper only when the user did not
// pass --http on the command line, so verifying the helper plus its
// "skip when flag was changed" gate gives us full coverage of the
// precedence rules without spinning up a real serve loop.

func writeArchaiYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "archai.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write archai.yaml: %v", err)
	}
	return dir
}

const overlayWithServe = `module: github.com/example/app

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

const overlayWithoutServe = `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []
`

func TestLoadServeHTTPAddr_OverlayPresent(t *testing.T) {
	root := writeArchaiYAML(t, overlayWithServe)
	got, err := loadServeHTTPAddrFromOverlay(root)
	if err != nil {
		t.Fatalf("loadServeHTTPAddrFromOverlay: %v", err)
	}
	if want := "0.0.0.0:47823"; got != want {
		t.Errorf("addr = %q, want %q", got, want)
	}
}

func TestLoadServeHTTPAddr_OverlayWithoutServe(t *testing.T) {
	root := writeArchaiYAML(t, overlayWithoutServe)
	got, err := loadServeHTTPAddrFromOverlay(root)
	if err != nil {
		t.Fatalf("loadServeHTTPAddrFromOverlay: %v", err)
	}
	if got != "" {
		t.Errorf("addr = %q, want empty (caller falls back to flag default)", got)
	}
}

func TestLoadServeHTTPAddr_OverlayMissing(t *testing.T) {
	// No archai.yaml in the directory at all: helper must return
	// ("", nil) so the flag default takes over silently.
	root := t.TempDir()
	got, err := loadServeHTTPAddrFromOverlay(root)
	if err != nil {
		t.Fatalf("loadServeHTTPAddrFromOverlay: %v", err)
	}
	if got != "" {
		t.Errorf("addr = %q, want empty for missing overlay", got)
	}
}

func TestLoadServeHTTPAddr_OverlayMalformed(t *testing.T) {
	// A malformed serve.http_addr must surface as a hard error so the
	// daemon does not silently fall back to the flag default and bind
	// somewhere unexpected.
	root := writeArchaiYAML(t, `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []

serve:
  http_addr: "127.0.0.1:abc"
`)
	_, err := loadServeHTTPAddrFromOverlay(root)
	if err == nil {
		t.Fatal("expected error for malformed serve.http_addr, got nil")
	}
}

func TestLoadServeHTTPAddr_RootDefaultsToCWD(t *testing.T) {
	// Empty root means "current working directory". chdir into a temp
	// directory with no archai.yaml so the helper returns ("", nil)
	// rather than reading the repo's real archai.yaml.
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	got, err := loadServeHTTPAddrFromOverlay("")
	if err != nil {
		t.Fatalf("loadServeHTTPAddrFromOverlay(\"\"): %v", err)
	}
	if got != "" {
		t.Errorf("addr = %q, want empty when cwd has no archai.yaml", got)
	}
}
