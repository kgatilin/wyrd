package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGlobalRegistry_Isolation(t *testing.T) {
	// Use a temp dir for ARCHAI_HOME so we don't pollute ~/.arch.
	tmpHome := t.TempDir()
	t.Setenv("ARCHAI_HOME", tmpHome)

	// Create a fake project root.
	projectRoot := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Initially no record.
	rec, err := ReadGlobalRecord(projectRoot)
	if err != nil {
		t.Fatalf("ReadGlobalRecord: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record, got %+v", rec)
	}

	// Write a record.
	testRec := DaemonRecord{
		RepoRoot:  projectRoot,
		HTTPAddr:  "127.0.0.1:12345",
		PID:       os.Getpid(), // Use our own PID so PIDAlive returns true.
		Caps:      []string{"mcp", "multi", "ui"},
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Worktrees: []string{"main", "feature"},
	}
	if err := WriteGlobalRecord(testRec); err != nil {
		t.Fatalf("WriteGlobalRecord: %v", err)
	}

	// Read it back.
	rec, err = ReadGlobalRecord(projectRoot)
	if err != nil {
		t.Fatalf("ReadGlobalRecord: %v", err)
	}
	if rec == nil {
		t.Fatal("expected record, got nil")
	}
	if rec.RepoRoot != projectRoot {
		t.Errorf("RepoRoot = %q, want %q", rec.RepoRoot, projectRoot)
	}
	if rec.HTTPAddr != "127.0.0.1:12345" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:12345", rec.HTTPAddr)
	}
	if !rec.HasCap("multi") {
		t.Errorf("expected 'multi' cap")
	}

	// Verify it's in the list.
	daemons, err := ListGlobalDaemons()
	if err != nil {
		t.Fatalf("ListGlobalDaemons: %v", err)
	}
	if len(daemons) != 1 {
		t.Fatalf("expected 1 daemon, got %d", len(daemons))
	}
	if daemons[0].Record.RepoRoot != projectRoot {
		t.Errorf("listed RepoRoot = %q, want %q", daemons[0].Record.RepoRoot, projectRoot)
	}

	// Remove the record.
	if err := RemoveGlobalRecord(projectRoot); err != nil {
		t.Fatalf("RemoveGlobalRecord: %v", err)
	}

	// Should be gone.
	rec, err = ReadGlobalRecord(projectRoot)
	if err != nil {
		t.Fatalf("ReadGlobalRecord after remove: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil after remove, got %+v", rec)
	}
}

func TestGlobalRegistry_StaleCleanup(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("ARCHAI_HOME", tmpHome)

	projectRoot := filepath.Join(t.TempDir(), "staleproject")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a record with PID 0 (never alive).
	testRec := DaemonRecord{
		RepoRoot:  projectRoot,
		HTTPAddr:  "127.0.0.1:1",
		PID:       0, // PIDAlive will return false.
		Caps:      []string{"mcp"},
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := WriteGlobalRecord(testRec); err != nil {
		t.Fatalf("WriteGlobalRecord: %v", err)
	}

	// ReadGlobalRecord should detect stale PID and remove the file.
	rec, err := ReadGlobalRecord(projectRoot)
	if err != nil {
		t.Fatalf("ReadGlobalRecord: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil for stale record, got %+v", rec)
	}

	// Verify the file was removed by checking the registry dir.
	dir, err := registryDir()
	if err != nil {
		t.Fatalf("registryDir: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			t.Errorf("stale record file not cleaned up: %s", e.Name())
		}
	}
}

func TestGlobalRegistry_MultipleRepos(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("ARCHAI_HOME", tmpHome)

	// Create two fake project roots.
	project1 := filepath.Join(t.TempDir(), "project1")
	project2 := filepath.Join(t.TempDir(), "project2")
	for _, p := range []string{project1, project2} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	pid := os.Getpid()

	// Write records for both.
	rec1 := DaemonRecord{
		RepoRoot:  project1,
		HTTPAddr:  "127.0.0.1:11111",
		PID:       pid,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	rec2 := DaemonRecord{
		RepoRoot:  project2,
		HTTPAddr:  "127.0.0.1:22222",
		PID:       pid,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := WriteGlobalRecord(rec1); err != nil {
		t.Fatalf("WriteGlobalRecord 1: %v", err)
	}
	if err := WriteGlobalRecord(rec2); err != nil {
		t.Fatalf("WriteGlobalRecord 2: %v", err)
	}

	// List should show both.
	daemons, err := ListGlobalDaemons()
	if err != nil {
		t.Fatalf("ListGlobalDaemons: %v", err)
	}
	if len(daemons) != 2 {
		t.Fatalf("expected 2 daemons, got %d", len(daemons))
	}

	// Each should be independently readable.
	r1, _ := ReadGlobalRecord(project1)
	r2, _ := ReadGlobalRecord(project2)
	if r1 == nil || r1.HTTPAddr != "127.0.0.1:11111" {
		t.Errorf("project1 record mismatch: %+v", r1)
	}
	if r2 == nil || r2.HTTPAddr != "127.0.0.1:22222" {
		t.Errorf("project2 record mismatch: %+v", r2)
	}
}
