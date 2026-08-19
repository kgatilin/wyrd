package overlay

import (
	"os"
	"path/filepath"
	"testing"
)

const serveAddrOverlay = `module: github.com/example/app

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

const serveAddrOverlayNoServe = `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []
`

func writeOverlay(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", FileName, err)
	}
	return dir
}

func TestServeHTTPAddr_Pinned(t *testing.T) {
	got, err := ServeHTTPAddr(writeOverlay(t, serveAddrOverlay))
	if err != nil {
		t.Fatalf("ServeHTTPAddr: %v", err)
	}
	if want := "0.0.0.0:47823"; got != want {
		t.Errorf("addr = %q, want %q", got, want)
	}
}

func TestServeHTTPAddr_NoServeBlock(t *testing.T) {
	got, err := ServeHTTPAddr(writeOverlay(t, serveAddrOverlayNoServe))
	if err != nil {
		t.Fatalf("ServeHTTPAddr: %v", err)
	}
	if got != "" {
		t.Errorf("addr = %q, want empty so the caller keeps its own default", got)
	}
}

func TestServeHTTPAddr_OverlayMissing(t *testing.T) {
	got, err := ServeHTTPAddr(t.TempDir())
	if err != nil {
		t.Fatalf("ServeHTTPAddr: %v", err)
	}
	if got != "" {
		t.Errorf("addr = %q, want empty for a project with no overlay", got)
	}
}

func TestServeHTTPAddr_Malformed(t *testing.T) {
	// A bad address must be an error: binding a random port instead of
	// the configured one is exactly what pinning exists to prevent.
	root := writeOverlay(t, `module: github.com/example/app

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
	if _, err := ServeHTTPAddr(root); err == nil {
		t.Fatal("expected an error for a malformed serve.http_addr")
	}
}

func TestServeHTTPAddr_RootDefaultsToCWD(t *testing.T) {
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	got, err := ServeHTTPAddr("")
	if err != nil {
		t.Fatalf("ServeHTTPAddr(\"\"): %v", err)
	}
	if got != "" {
		t.Errorf("addr = %q, want empty when cwd has no overlay", got)
	}
}
