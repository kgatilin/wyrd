package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/service"
)

func TestComposeIntegration(t *testing.T) {
	// 1. Create test fixture with multiple packages
	tmpDir := t.TempDir()

	// pkg/a/.arch/pub.d2 (simulating code-generated diagram)
	createSpecFile(t, tmpDir, "pkg/a", "pub.d2", `pkg.a: {
  label: "pkg/a"
  Service: {
    shape: class
    stereotype: "<<interface>>"
    Run(): void
  }
}`)

	// pkg/b/.arch/pub.d2 (simulating code-generated diagram)
	createSpecFile(t, tmpDir, "pkg/b", "pub.d2", `pkg.b: {
  label: "pkg/b"
  Repository: {
    shape: class
    stereotype: "<<interface>>"
    Save(data): error
  }
}`)

	// pkg/c/.arch/pub-spec.d2 (simulating target specification)
	createSpecFile(t, tmpDir, "pkg/c", "pub-spec.d2", `pkg.c: {
  label: "pkg/c"
  Handler: {
    shape: class
    stereotype: "<<struct>>"
  }
}`)

	// 2. Run compose in auto mode (pub.d2 files)
	outputPath := filepath.Join(tmpDir, "combined-auto.d2")

	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	goReader := golang.NewReader()
	svc := service.NewService(goReader, d2Reader, d2Writer)

	result, err := svc.Compose(context.Background(), service.ComposeOptions{
		Paths:      []string{filepath.Join(tmpDir, "pkg/a"), filepath.Join(tmpDir, "pkg/b")},
		OutputPath: outputPath,
		Mode:       service.ComposeModeAuto,
	})
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}

	// Verify result
	if result.PackageCount != 2 {
		t.Errorf("PackageCount = %d, want 2", result.PackageCount)
	}

	// 3. Verify output file exists and contains expected content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	contentStr := string(content)

	// Should contain both packages
	if !strings.Contains(contentStr, "pkg.a") {
		t.Error("Output should contain pkg.a")
	}
	if !strings.Contains(contentStr, "pkg.b") {
		t.Error("Output should contain pkg.b")
	}
	if !strings.Contains(contentStr, "Service") {
		t.Error("Output should contain Service interface")
	}
	if !strings.Contains(contentStr, "Repository") {
		t.Error("Output should contain Repository interface")
	}

	// Should not contain pkg.c (it only has pub-spec.d2, not pub.d2)
	if strings.Contains(contentStr, "pkg.c") {
		t.Error("Output should not contain pkg.c (only has spec file, not pub.d2)")
	}
}

func TestComposeIntegration_SpecMode(t *testing.T) {
	tmpDir := t.TempDir()

	// Create spec files
	createSpecFile(t, tmpDir, "pkg/a", "pub-spec.d2", `pkg.a: {
  label: "pkg/a"
  TargetService: {
    shape: class
    stereotype: "<<interface>>"
  }
}`)

	createSpecFile(t, tmpDir, "pkg/b", "pub-spec.d2", `pkg.b: {
  label: "pkg/b"
  TargetRepository: {
    shape: class
    stereotype: "<<interface>>"
  }
}`)

	// Also create pub.d2 files to verify they're not picked up in spec mode
	createSpecFile(t, tmpDir, "pkg/a", "pub.d2", `pkg.a: {
  label: "pkg/a"
  CurrentService: {
    shape: class
  }
}`)

	outputPath := filepath.Join(tmpDir, "combined-spec.d2")

	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	goReader := golang.NewReader()
	svc := service.NewService(goReader, d2Reader, d2Writer)

	result, err := svc.Compose(context.Background(), service.ComposeOptions{
		Paths:      []string{filepath.Join(tmpDir, "pkg/a"), filepath.Join(tmpDir, "pkg/b")},
		OutputPath: outputPath,
		Mode:       service.ComposeModeSpec,
	})
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}

	if result.PackageCount != 2 {
		t.Errorf("PackageCount = %d, want 2", result.PackageCount)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	contentStr := string(content)

	// Should contain target specs
	if !strings.Contains(contentStr, "TargetService") {
		t.Error("Output should contain TargetService from spec files")
	}
	if !strings.Contains(contentStr, "TargetRepository") {
		t.Error("Output should contain TargetRepository from spec files")
	}

	// Should not contain current state (from pub.d2)
	if strings.Contains(contentStr, "CurrentService") {
		t.Error("Output should not contain CurrentService from pub.d2")
	}
}

func TestComposeIntegration_GlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple packages under a common parent
	// Note: D2 reader expects shape: class AND stereotype to be a valid symbol
	createSpecFile(t, tmpDir, "internal/adapter/http", "pub.d2", `internal.adapter.http: {
  label: "internal/adapter/http"
  Handler: {
    shape: class
    stereotype: "<<struct>>"
  }
}`)
	createGoFile(t, tmpDir, "internal/adapter/http", "handler.go")

	createSpecFile(t, tmpDir, "internal/adapter/grpc", "pub.d2", `internal.adapter.grpc: {
  label: "internal/adapter/grpc"
  Server: {
    shape: class
    stereotype: "<<struct>>"
  }
}`)
	createGoFile(t, tmpDir, "internal/adapter/grpc", "server.go")

	createSpecFile(t, tmpDir, "internal/service", "pub.d2", `internal.service: {
  label: "internal/service"
  Service: {
    shape: class
    stereotype: "<<struct>>"
  }
}`)
	createGoFile(t, tmpDir, "internal/service", "service.go")

	outputPath := filepath.Join(tmpDir, "combined-glob.d2")

	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	goReader := golang.NewReader()
	svc := service.NewService(goReader, d2Reader, d2Writer)

	result, err := svc.Compose(context.Background(), service.ComposeOptions{
		Paths:      []string{filepath.Join(tmpDir, "internal/...")},
		OutputPath: outputPath,
		Mode:       service.ComposeModeAuto,
	})
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}

	// Should have found all 3 packages
	if result.PackageCount != 3 {
		t.Errorf("PackageCount = %d, want 3", result.PackageCount)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	contentStr := string(content)

	if !strings.Contains(contentStr, "Handler") {
		t.Error("Output should contain Handler from http package")
	}
	if !strings.Contains(contentStr, "Server") {
		t.Error("Output should contain Server from grpc package")
	}
	if !strings.Contains(contentStr, "Service") {
		t.Error("Output should contain Service from service package")
	}
}

func TestComposeIntegration_SkippedPackages(t *testing.T) {
	tmpDir := t.TempDir()

	// pkg/a has .arch/pub.d2
	createSpecFile(t, tmpDir, "pkg/a", "pub.d2", `pkg.a: {
  label: "pkg/a"
}`)

	// pkg/b has no .arch folder at all
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg/b"), 0755); err != nil {
		t.Fatal(err)
	}
	createGoFile(t, tmpDir, "pkg/b", "main.go")

	// pkg/c has .arch but no pub.d2 (only internal.d2)
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg/c/.arch"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pkg/c/.arch/internal.d2"), []byte("pkg.c: {}"), 0644); err != nil {
		t.Fatal(err)
	}
	createGoFile(t, tmpDir, "pkg/c", "main.go")

	outputPath := filepath.Join(tmpDir, "combined.d2")

	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	goReader := golang.NewReader()
	svc := service.NewService(goReader, d2Reader, d2Writer)

	result, err := svc.Compose(context.Background(), service.ComposeOptions{
		Paths: []string{
			filepath.Join(tmpDir, "pkg/a"),
			filepath.Join(tmpDir, "pkg/b"),
			filepath.Join(tmpDir, "pkg/c"),
		},
		OutputPath: outputPath,
		Mode:       service.ComposeModeAuto,
	})
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}

	// Only pkg/a should be included
	if result.PackageCount != 1 {
		t.Errorf("PackageCount = %d, want 1", result.PackageCount)
	}

	// pkg/b and pkg/c should be skipped
	if len(result.SkippedPaths) != 2 {
		t.Errorf("SkippedPaths = %d, want 2 (got: %v)", len(result.SkippedPaths), result.SkippedPaths)
	}
}

func TestComposeIntegration_NoFilesFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create packages without any .arch folders
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg/a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg/b"), 0755); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(tmpDir, "combined.d2")

	d2Reader := d2.NewReader()
	d2Writer := d2.NewWriter()
	goReader := golang.NewReader()
	svc := service.NewService(goReader, d2Reader, d2Writer)

	_, err := svc.Compose(context.Background(), service.ComposeOptions{
		Paths: []string{
			filepath.Join(tmpDir, "pkg/a"),
			filepath.Join(tmpDir, "pkg/b"),
		},
		OutputPath: outputPath,
		Mode:       service.ComposeModeAuto,
	})

	if err == nil {
		t.Fatal("Expected error when no spec files found")
	}
	if !strings.Contains(err.Error(), "no spec files found") {
		t.Errorf("Expected error about no spec files, got: %v", err)
	}
}

// Helper functions

func createSpecFile(t *testing.T, baseDir, pkg, filename, content string) {
	t.Helper()
	archDir := filepath.Join(baseDir, pkg, ".arch")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func createGoFile(t *testing.T, baseDir, pkg, filename string) {
	t.Helper()
	pkgDir := filepath.Join(baseDir, pkg)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	pkgName := filepath.Base(pkg)
	content := "package " + pkgName + "\n"
	if err := os.WriteFile(filepath.Join(pkgDir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
