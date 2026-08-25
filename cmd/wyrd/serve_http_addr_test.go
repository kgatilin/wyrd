package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The --http precedence rules live in overlay.ServeHTTPAddr (tested
// there); what remains here is the wiring runServe puts around it.

func TestResolveServeRootMode_RepoEnablesMulti(t *testing.T) {
	root, multi, err := resolveServeRootMode(".", false, "/tmp/repo", false)
	if err != nil {
		t.Fatalf("resolveServeRootMode: %v", err)
	}
	if root != "/tmp/repo" {
		t.Fatalf("root = %q, want /tmp/repo", root)
	}
	if !multi {
		t.Fatalf("multi = false, want true")
	}
}

func TestResolveServeRootMode_RootRepoConflict(t *testing.T) {
	_, _, err := resolveServeRootMode("/tmp/other", true, "/tmp/repo", false)
	if err == nil {
		t.Fatalf("expected conflict error")
	}
}

func TestReviewUIDistExists(t *testing.T) {
	dir := t.TempDir()
	if reviewUIDistExists(dir) {
		t.Fatalf("empty dir should not be a review UI dist")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if !reviewUIDistExists(dir) {
		t.Fatalf("expected valid review UI dist")
	}
}

func TestResolveReviewUIFS_EmbeddedDefault(t *testing.T) {
	t.Setenv("WYRD_REVIEW_UI_DIR", "")

	files, err := resolveReviewUIFS()
	if err != nil {
		t.Fatalf("resolveReviewUIFS: %v", err)
	}
	data, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `<div id="root">`) || !strings.Contains(text, `Reading architecture graph`) {
		t.Fatalf("embedded index.html does not look like the review UI")
	}
}

// The token itself is read by overlay.ServeAuthToken (tested there); what is
// wired here is which source wins. The environment has to, or a secret can
// only be supplied by writing it into a file that ships with the project.
func TestResolveServeAuthToken_EnvironmentWinsOverTheOverlay(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "wyrd.yaml"), []byte(`module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []

serve:
  auth_token: "from-the-file"
`), 0o644); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	t.Setenv("WYRD_HTTP_TOKEN", "")
	got, err := resolveServeAuthToken(root)
	if err != nil {
		t.Fatalf("resolveServeAuthToken: %v", err)
	}
	if want := "from-the-file"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}

	t.Setenv("WYRD_HTTP_TOKEN", "  from-the-env  ")
	got, err = resolveServeAuthToken(root)
	if err != nil {
		t.Fatalf("resolveServeAuthToken: %v", err)
	}
	if want := "from-the-env"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}
}

// No token anywhere leaves the endpoint open — the loopback default.
func TestResolveServeAuthToken_NothingConfiguredIsOpen(t *testing.T) {
	t.Setenv("WYRD_HTTP_TOKEN", "")
	got, err := resolveServeAuthToken(t.TempDir())
	if err != nil {
		t.Fatalf("resolveServeAuthToken: %v", err)
	}
	if got != "" {
		t.Errorf("token = %q, want it empty", got)
	}
}
