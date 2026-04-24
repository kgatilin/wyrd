package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestName_FallbackToProjectRootBasename(t *testing.T) {
	root := t.TempDir()
	// No git repo: Name must fall back to filepath.Base.
	got := Name(root)
	if got != filepath.Base(root) {
		t.Errorf("Name(%q) = %q, want %q", root, got, filepath.Base(root))
	}
}

func TestName_UsesGitTopLevelBasename(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	wt := filepath.Join(root, "myproj")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if out, err := exec.Command("git", "-C", wt, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	// Inside the repo, Name should return "myproj".
	if got := Name(wt); got != "myproj" {
		t.Errorf("Name inside repo = %q, want %q", got, "myproj")
	}
	// From a sub-directory, Name should still return the top-level basename.
	sub := filepath.Join(wt, "internal")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if got := Name(sub); got != "myproj" {
		t.Errorf("Name in sub = %q, want %q", got, "myproj")
	}
}

func TestCurrent_RoundTrip(t *testing.T) {
	root := t.TempDir()
	name := "wt1"

	// Missing file -> empty, not-from-legacy, no error.
	got, fromLegacy, err := ReadCurrent(root, name)
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got != "" || fromLegacy {
		t.Fatalf("missing file: got (%q, %v), want (\"\", false)", got, fromLegacy)
	}

	if err := WriteCurrent(root, name, "v1"); err != nil {
		t.Fatalf("WriteCurrent: %v", err)
	}
	got, fromLegacy, err = ReadCurrent(root, name)
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got != "v1" || fromLegacy {
		t.Fatalf("after write: got (%q, %v), want (\"v1\", false)", got, fromLegacy)
	}

	if err := RemoveCurrent(root, name); err != nil {
		t.Fatalf("RemoveCurrent: %v", err)
	}
	got, _, err = ReadCurrent(root, name)
	if err != nil {
		t.Fatalf("ReadCurrent post-remove: %v", err)
	}
	if got != "" {
		t.Fatalf("after remove: got %q, want empty", got)
	}
}

func TestCurrent_LegacyFallback(t *testing.T) {
	root := t.TempDir()
	// Seed the pre-M9 location.
	legacyDir := filepath.Join(root, ".arch", "targets")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "CURRENT"), []byte("legacy-v1\n"), 0o644); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	got, fromLegacy, err := ReadCurrent(root, "wt1")
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got != "legacy-v1" {
		t.Fatalf("id = %q, want %q", got, "legacy-v1")
	}
	if !fromLegacy {
		t.Fatalf("fromLegacy = false, want true")
	}

	// After WriteCurrent, the new location takes precedence and the
	// legacy flag is no longer reported.
	if err := WriteCurrent(root, "wt1", "new-v2"); err != nil {
		t.Fatalf("WriteCurrent: %v", err)
	}
	got, fromLegacy, err = ReadCurrent(root, "wt1")
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got != "new-v2" || fromLegacy {
		t.Fatalf("after new write: (%q, %v), want (\"new-v2\", false)", got, fromLegacy)
	}
}

func TestServe_RoundTrip(t *testing.T) {
	root := t.TempDir()
	name := "wt1"

	rec := ServeRecord{
		PID:       os.Getpid(),
		HTTPAddr:  "127.0.0.1:54321",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := WriteServe(root, name, rec); err != nil {
		t.Fatalf("WriteServe: %v", err)
	}

	got, err := ReadServe(root, name)
	if err != nil {
		t.Fatalf("ReadServe: %v", err)
	}
	if got == nil {
		t.Fatal("ReadServe returned nil")
	}
	if got.PID != rec.PID || got.HTTPAddr != rec.HTTPAddr || got.StartedAt != rec.StartedAt {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", *got, rec)
	}

	if err := RemoveServe(root, name); err != nil {
		t.Fatalf("RemoveServe: %v", err)
	}
	got, err = ReadServe(root, name)
	if err != nil {
		t.Fatalf("ReadServe post-remove: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after remove, got %+v", got)
	}
}

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("PIDAlive(self) = false, want true")
	}
	if PIDAlive(0) {
		t.Error("PIDAlive(0) = true, want false")
	}
	if PIDAlive(-1) {
		t.Error("PIDAlive(-1) = true, want false")
	}
	// An extremely high pid is almost certainly free.
	if PIDAlive(0x7fff_fffe) {
		t.Error("PIDAlive(huge) = true, want false")
	}
}

func TestListDaemons_FiltersStale(t *testing.T) {
	root := t.TempDir()

	live := ServeRecord{PID: os.Getpid(), HTTPAddr: "127.0.0.1:1111", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := WriteServe(root, "alive", live); err != nil {
		t.Fatalf("WriteServe alive: %v", err)
	}

	// Write a stale record with a very high pid to ensure it doesn't
	// refer to a live process on the test host.
	stale := ServeRecord{PID: 0x7fff_fffe, HTTPAddr: "127.0.0.1:2222", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := WriteServe(root, "stale", stale); err != nil {
		t.Fatalf("WriteServe stale: %v", err)
	}

	// A directory without serve.json should be silently skipped.
	if err := os.MkdirAll(filepath.Join(root, ".arch", ".worktree", "empty"), 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}

	daemons, err := ListDaemons(root)
	if err != nil {
		t.Fatalf("ListDaemons: %v", err)
	}
	if len(daemons) != 1 {
		t.Fatalf("got %d live daemons, want 1: %+v", len(daemons), daemons)
	}
	if daemons[0].Worktree != "alive" {
		t.Errorf("Worktree = %q, want %q", daemons[0].Worktree, "alive")
	}
	if daemons[0].Record.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", daemons[0].Record.PID, os.Getpid())
	}
}

func TestListDaemons_MissingRoot(t *testing.T) {
	root := t.TempDir()
	daemons, err := ListDaemons(root)
	if err != nil {
		t.Fatalf("ListDaemons: %v", err)
	}
	if len(daemons) != 0 {
		t.Fatalf("got %d daemons, want 0", len(daemons))
	}
}
