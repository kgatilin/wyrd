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
	t.Setenv("ARCHAI_REVIEW_UI_DIR", "")

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
