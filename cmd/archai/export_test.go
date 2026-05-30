package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/uigraph"
)

// projectRoot returns the absolute path to the archai project root.
// Tests need this because they run from cmd/archai/ but need to reference
// paths relative to the project root.
func projectRoot(t *testing.T) string {
	t.Helper()
	// Get the path of this test file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get test file path")
	}
	// cmd/archai/export_test.go -> project root is ../../
	return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
}

func TestExportUI(t *testing.T) {
	root := projectRoot(t)

	// Create temp output file
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "archgraph.json")

	cmd := newExportCmd()
	// Use paths that don't include archmotif (which has external dep issues)
	// Test with domain, diff, overlay packages
	cmd.SetArgs([]string{
		"ui",
		filepath.Join(root, "internal", "domain"),
		filepath.Join(root, "internal", "diff"),
		filepath.Join(root, "internal", "overlay"),
		"-o", outFile,
	})

	// Capture output
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	// Run command
	ctx := context.Background()
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("export ui failed: %v\noutput: %s", err, stdout.String())
	}

	// Read output file
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	// Verify it's valid JSON
	var g uigraph.UIGraph
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}

	// Verify schema
	if g.Schema != uigraph.Schema {
		t.Errorf("schema = %q, want %q", g.Schema, uigraph.Schema)
	}

	// Verify we have components (archai has many internal packages)
	if len(g.Components) == 0 {
		t.Error("expected at least one component, got none")
	}

	// Log summary for visibility
	t.Logf("exported %d bounded contexts, %d components, %d edges",
		len(g.BoundedContexts), len(g.Components), len(g.Edges))
}

func TestExportUIToStdout(t *testing.T) {
	root := projectRoot(t)

	cmd := newExportCmd()
	cmd.SetArgs([]string{"ui", filepath.Join(root, "internal", "adapter", "uigraph")})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	ctx := context.Background()
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("export ui failed: %v", err)
	}

	// Verify stdout contains valid JSON
	var g uigraph.UIGraph
	if err := json.Unmarshal(stdout.Bytes(), &g); err != nil {
		t.Fatalf("parsing stdout JSON: %v\nraw: %s", err, stdout.String())
	}

	if g.Schema != uigraph.Schema {
		t.Errorf("schema = %q, want %q", g.Schema, uigraph.Schema)
	}
}
