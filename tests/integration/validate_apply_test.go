package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/golang"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/apply"
	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/target"
)

// TestIntegration_ValidateAndApply exercises the full M4c workflow: lock a
// target from Go code, drift the code, confirm diff.Compute picks up the
// drift, run apply.Apply to update the target snapshot, and confirm the
// recomputed diff is empty.
func TestIntegration_ValidateAndApply(t *testing.T) {
	tmpDir := t.TempDir()

	// Seed a minimal Go module with one package that has a single function.
	writeFile(t, filepath.Join(tmpDir, "go.mod"), `module test.example/m4c

go 1.21
`)
	pkgDir := filepath.Join(tmpDir, "svc")
	mustMkdir(t, pkgDir)
	writeFile(t, filepath.Join(pkgDir, "svc.go"), `package svc

// Service is a service.
type Service interface {
	DoA() error
}

// NewService constructs a Service.
func NewService() Service { return nil }
`)

	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	ctx := context.Background()

	// Step 1 — generate YAML specs and lock a target.
	generateYAMLSpecs(t, ctx, tmpDir, pkgDir)
	if err := target.Lock(tmpDir, "v1", target.LockOptions{Description: "initial"}); err != nil {
		t.Fatalf("target.Lock: %v", err)
	}
	if err := target.Use(tmpDir, "v1"); err != nil {
		t.Fatalf("target.Use: %v", err)
	}

	// Step 2 — no drift yet, diff must be empty.
	emptyDiff := computeDiff(t, ctx, tmpDir, "v1")
	if !emptyDiff.IsEmpty() {
		t.Fatalf("expected empty diff directly after lock, got %+v", emptyDiff.Changes)
	}

	// Step 3 — modify the code: add a new exported function.
	writeFile(t, filepath.Join(pkgDir, "svc.go"), `package svc

// Service is a service.
type Service interface {
	DoA() error
}

// NewService constructs a Service.
func NewService() Service { return nil }

// Extra is a new function that creates drift.
func Extra() {}
`)

	// Step 4 — Regenerate current-code .arch/*.yaml so the "current" side
	// reflects the updated code (the CLI would do this via loadCurrentModel;
	// here we emulate it).
	os.RemoveAll(filepath.Join(pkgDir, ".arch"))
	generateYAMLSpecs(t, ctx, tmpDir, pkgDir)

	// Compute diff — must now show drift (OpRemove, since current has Extra
	// that target lacks; per diff.Compute semantics that's OpRemove).
	driftDiff := computeDiff(t, ctx, tmpDir, "v1")
	if driftDiff.IsEmpty() {
		t.Fatalf("expected drift diff after adding Extra, got empty")
	}
	foundExtra := false
	for _, c := range driftDiff.Changes {
		if c.Kind == diff.KindFunction && strings.HasSuffix(c.Path, ".Extra") {
			foundExtra = true
			if c.Op != diff.OpRemove {
				t.Errorf("expected OpRemove for Extra, got %s", c.Op)
			}
		}
	}
	if !foundExtra {
		t.Fatalf("drift diff does not reference Extra: %+v", driftDiff.Changes)
	}

	// Step 5 — apply the diff onto the target model.
	current := readArchYAML(t, ctx, tmpDir)
	targetModel := readTargetYAML(t, ctx, tmpDir, "v1")
	updated, err := apply.Apply(driftDiff, current, targetModel)
	if err != nil {
		t.Fatalf("apply.Apply: %v", err)
	}

	// Persist the updated target snapshot as internal.yaml files so the next
	// diff.Compute sees them.
	rewriteTargetModels(t, ctx, tmpDir, "v1", updated)

	// Step 6 — recomputed diff must be empty.
	afterDiff := computeDiff(t, ctx, tmpDir, "v1")
	if !afterDiff.IsEmpty() {
		t.Fatalf("expected empty diff after apply, got %+v", afterDiff.Changes)
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// generateYAMLSpecs shells out to `go build` indirectly by parsing with the
// golang reader and writing per-package YAML via the yaml writer, matching
// what `archai diagram generate --format yaml` does.
func generateYAMLSpecs(t *testing.T, ctx context.Context, projectRoot, pkgDir string) {
	t.Helper()
	// Ensure go modules can resolve inside the temp dir (no deps here).
	_ = exec.Command("go", "mod", "tidy").Run()

	reader := golang.NewReader()
	models, err := reader.Read(ctx, []string{"./..."})
	if err != nil {
		t.Fatalf("golang reader: %v", err)
	}
	writer := yamlAdapter.NewWriter()
	for _, m := range models {
		rel := strings.TrimPrefix(m.Path, "test.example/m4c/")
		if rel == "test.example/m4c" {
			rel = ""
		}
		out := filepath.Join(projectRoot, rel, ".arch", "internal.yaml")
		if err := writer.Write(ctx, m, domain.WriteOptions{OutputPath: out}); err != nil {
			t.Fatalf("yaml write: %v", err)
		}
	}
}

// readArchYAML reads .arch/*.yaml specs under projectRoot and returns them.
func readArchYAML(t *testing.T, ctx context.Context, projectRoot string) []domain.PackageModel {
	t.Helper()
	var files []string
	filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.Contains(path, ".arch"+string(os.PathSeparator)+"targets") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == ".arch" && strings.HasSuffix(path, ".yaml") {
			files = append(files, path)
		}
		return nil
	})
	if len(files) == 0 {
		t.Fatalf("no .arch yaml files under %s", projectRoot)
	}
	models, err := yamlAdapter.NewReader().Read(ctx, files)
	if err != nil {
		t.Fatalf("yaml reader: %v", err)
	}
	return models
}

// readTargetYAML reads the target snapshot model for id.
func readTargetYAML(t *testing.T, ctx context.Context, projectRoot, id string) []domain.PackageModel {
	t.Helper()
	modelDir := filepath.Join(projectRoot, ".arch", "targets", id, "model")
	var files []string
	filepath.WalkDir(modelDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yaml") {
			files = append(files, path)
		}
		return nil
	})
	if len(files) == 0 {
		t.Fatalf("no target yaml files under %s", modelDir)
	}
	models, err := yamlAdapter.NewReader().Read(ctx, files)
	if err != nil {
		t.Fatalf("yaml reader: %v", err)
	}
	return models
}

// computeDiff rebuilds the current+target models and returns Compute's output.
func computeDiff(t *testing.T, ctx context.Context, projectRoot, id string) *diff.Diff {
	t.Helper()
	cur := readArchYAML(t, ctx, projectRoot)
	tgt := readTargetYAML(t, ctx, projectRoot, id)
	return diff.Compute(cur, tgt)
}

// rewriteTargetModels clears the target's model/ tree and writes the given
// models as internal.yaml under their relative Path.
func rewriteTargetModels(t *testing.T, ctx context.Context, projectRoot, id string, models []domain.PackageModel) {
	t.Helper()
	modelDir := filepath.Join(projectRoot, ".arch", "targets", id, "model")
	if err := os.RemoveAll(modelDir); err != nil {
		t.Fatalf("remove %s: %v", modelDir, err)
	}
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", modelDir, err)
	}
	writer := yamlAdapter.NewWriter()
	for _, m := range models {
		out := filepath.Join(modelDir, m.Path, "internal.yaml")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(out), err)
		}
		if err := writer.Write(ctx, m, domain.WriteOptions{OutputPath: out}); err != nil {
			t.Fatalf("yaml write: %v", err)
		}
	}
}
