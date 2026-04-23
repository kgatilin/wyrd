package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

// setupProject creates a temporary project directory with a handful of
// packages that each already contain a .arch/ folder with pub.yaml and
// internal.yaml files. It mirrors what `archai diagram generate --format
// yaml` would produce — Lock then just has to freeze these files.
func setupProject(t *testing.T, withOverlay bool) string {
	t.Helper()
	root := t.TempDir()

	pkgs := []string{
		"internal/domain",
		"internal/service",
		"cmd/archai",
	}
	for _, p := range pkgs {
		archDir := filepath.Join(root, p, ".arch")
		if err := os.MkdirAll(archDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", archDir, err)
		}
		if err := os.WriteFile(filepath.Join(archDir, "pub.yaml"), []byte("package: "+p+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(archDir, "internal.yaml"), []byte("package: "+p+"\ninternal: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withOverlay {
		if err := os.WriteFile(filepath.Join(root, "archai.yaml"), []byte("module: example.com/foo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLock_CreatesLayout(t *testing.T) {
	root := setupProject(t, true)

	if err := Lock(root, "v1", LockOptions{Description: "first snapshot"}); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	targetDir := filepath.Join(root, ".arch", "targets", "v1")

	// meta.yaml should exist and parse.
	metaPath := filepath.Join(targetDir, "meta.yaml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta.yaml: %v", err)
	}
	var meta TargetMeta
	if err := yamlv3.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta.ID != "v1" {
		t.Errorf("meta.ID = %q, want v1", meta.ID)
	}
	if meta.Description != "first snapshot" {
		t.Errorf("meta.Description = %q, want %q", meta.Description, "first snapshot")
	}
	if meta.CreatedAt == "" {
		t.Error("meta.CreatedAt is empty")
	}

	// overlay.yaml should be copied.
	overlayData, err := os.ReadFile(filepath.Join(targetDir, "overlay.yaml"))
	if err != nil {
		t.Fatalf("read overlay.yaml: %v", err)
	}
	if !strings.Contains(string(overlayData), "module: example.com/foo") {
		t.Errorf("overlay.yaml content unexpected: %q", string(overlayData))
	}

	// Per-package model files should be copied.
	for _, pkg := range []string{"internal/domain", "internal/service", "cmd/archai"} {
		for _, fn := range []string{"pub.yaml", "internal.yaml"} {
			p := filepath.Join(targetDir, "model", pkg, fn)
			if _, err := os.Stat(p); err != nil {
				t.Errorf("expected %s to exist: %v", p, err)
			}
		}
	}
}

func TestLock_NoOverlay(t *testing.T) {
	root := setupProject(t, false)
	if err := Lock(root, "v1", LockOptions{}); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	overlayPath := filepath.Join(root, ".arch", "targets", "v1", "overlay.yaml")
	if _, err := os.Stat(overlayPath); !os.IsNotExist(err) {
		t.Errorf("expected overlay.yaml absent when archai.yaml missing, got err=%v", err)
	}
}

func TestLock_DuplicateFails(t *testing.T) {
	root := setupProject(t, false)
	if err := Lock(root, "v1", LockOptions{}); err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if err := Lock(root, "v1", LockOptions{}); err == nil {
		t.Fatal("expected error locking existing target, got nil")
	}
}

func TestLock_NoPackagesFails(t *testing.T) {
	root := t.TempDir()
	if err := Lock(root, "v1", LockOptions{}); err == nil {
		t.Fatal("expected error when no packages present, got nil")
	}
}

func TestLock_InvalidID(t *testing.T) {
	root := setupProject(t, false)
	cases := []string{"", "foo/bar", "a\\b"}
	for _, id := range cases {
		if err := Lock(root, id, LockOptions{}); err == nil {
			t.Errorf("Lock(%q): expected error, got nil", id)
		}
	}
}

func TestList_ReturnsAllTargets(t *testing.T) {
	root := setupProject(t, false)

	for _, id := range []string{"v1", "v3", "v2"} {
		if err := Lock(root, id, LockOptions{Description: "desc-" + id}); err != nil {
			t.Fatalf("Lock %s: %v", id, err)
		}
	}

	metas, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("got %d metas, want 3", len(metas))
	}
	// List must be sorted by id.
	for i, want := range []string{"v1", "v2", "v3"} {
		if metas[i].ID != want {
			t.Errorf("metas[%d].ID = %q, want %q", i, metas[i].ID, want)
		}
	}
}

func TestList_NoTargetsDir(t *testing.T) {
	root := t.TempDir()
	metas, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("expected empty list, got %d", len(metas))
	}
}

func TestShow_ReadsMetaAndPackages(t *testing.T) {
	root := setupProject(t, true)
	if err := Lock(root, "v1", LockOptions{Description: "desc"}); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	meta, pkgs, err := Show(root, "v1")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if meta.ID != "v1" || meta.Description != "desc" {
		t.Errorf("unexpected meta: %+v", meta)
	}
	wantPkgs := map[string]bool{
		"internal/domain":  false,
		"internal/service": false,
		"cmd/archai":       false,
	}
	for _, p := range pkgs {
		// Normalize to forward slashes for cross-platform comparison.
		normalized := filepath.ToSlash(p)
		if _, ok := wantPkgs[normalized]; !ok {
			t.Errorf("unexpected package in show: %q", normalized)
			continue
		}
		wantPkgs[normalized] = true
	}
	for p, seen := range wantPkgs {
		if !seen {
			t.Errorf("missing package in show: %q", p)
		}
	}
}

func TestShow_NotFound(t *testing.T) {
	root := setupProject(t, false)
	if _, _, err := Show(root, "missing"); err == nil {
		t.Fatal("expected error for missing target, got nil")
	}
}

func TestUse_WritesCURRENT(t *testing.T) {
	root := setupProject(t, false)
	if err := Lock(root, "v1", LockOptions{}); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if err := Use(root, "v1"); err != nil {
		t.Fatalf("Use: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".arch", "targets", "CURRENT"))
	if err != nil {
		t.Fatalf("read CURRENT: %v", err)
	}
	if strings.TrimSpace(string(data)) != "v1" {
		t.Errorf("CURRENT = %q, want v1", string(data))
	}

	cur, err := Current(root)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur != "v1" {
		t.Errorf("Current() = %q, want v1", cur)
	}
}

func TestUse_NonExistentTarget(t *testing.T) {
	root := setupProject(t, false)
	if err := Use(root, "ghost"); err == nil {
		t.Fatal("expected error using missing target, got nil")
	}
}

func TestCurrent_MissingReturnsEmpty(t *testing.T) {
	root := setupProject(t, false)
	cur, err := Current(root)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur != "" {
		t.Errorf("Current() = %q, want empty", cur)
	}
}

func TestDelete_RemovesTarget(t *testing.T) {
	root := setupProject(t, false)
	if err := Lock(root, "v1", LockOptions{}); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := Delete(root, "v1", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".arch", "targets", "v1")); !os.IsNotExist(err) {
		t.Errorf("expected target dir removed, got err=%v", err)
	}
}

func TestDelete_CurrentRequiresForce(t *testing.T) {
	root := setupProject(t, false)
	if err := Lock(root, "v1", LockOptions{}); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := Use(root, "v1"); err != nil {
		t.Fatalf("Use: %v", err)
	}

	if err := Delete(root, "v1", false); err == nil {
		t.Fatal("expected error deleting current target without force")
	}

	if err := Delete(root, "v1", true); err != nil {
		t.Fatalf("Delete with force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".arch", "targets", "v1")); !os.IsNotExist(err) {
		t.Errorf("expected target dir removed, got err=%v", err)
	}
	// CURRENT file should also be cleared.
	if _, err := os.Stat(filepath.Join(root, ".arch", "targets", "CURRENT")); !os.IsNotExist(err) {
		t.Errorf("expected CURRENT cleared, got err=%v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	root := setupProject(t, false)
	if err := Delete(root, "ghost", false); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLock_SkipsTargetsTree(t *testing.T) {
	// After a Lock, a second Lock should not pick up copies nested inside
	// .arch/targets/<prev>/model/<pkg>/.arch — we don't actually create
	// a nested .arch under model/, but we guard against it anyway.
	root := setupProject(t, false)
	if err := Lock(root, "v1", LockOptions{}); err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	// Lock a second target; should still work and only reference the
	// real packages, not anything inside .arch/targets.
	if err := Lock(root, "v2", LockOptions{}); err != nil {
		t.Fatalf("second Lock: %v", err)
	}
	_, pkgs, err := Show(root, "v2")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	for _, p := range pkgs {
		if strings.Contains(filepath.ToSlash(p), ".arch/targets") {
			t.Errorf("target leaked into model/ packages: %q", p)
		}
	}
}
