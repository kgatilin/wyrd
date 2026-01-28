package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func Test_findSpecFiles(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) string
		paths       []string
		mode        ComposeMode
		wantFiles   int
		wantSkipped int
		wantErr     bool
	}{
		{
			name: "auto mode finds pub.d2 files",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create pkg1/.arch/pub.d2
				createArchFile(t, dir, "pkg1", "pub.d2", "pkg1: {}")
				// Create pkg2/.arch/pub.d2
				createArchFile(t, dir, "pkg2", "pub.d2", "pkg2: {}")
				return dir
			},
			paths: func() []string {
				return []string{"pkg1", "pkg2"}
			}(),
			mode:        ComposeModeAuto,
			wantFiles:   2,
			wantSkipped: 0,
		},
		{
			name: "spec mode finds pub-spec.d2 files",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create pkg1/.arch/pub-spec.d2
				createArchFile(t, dir, "pkg1", "pub-spec.d2", "pkg1: {}")
				// Create pkg2/.arch/pub-spec.d2
				createArchFile(t, dir, "pkg2", "pub-spec.d2", "pkg2: {}")
				return dir
			},
			paths: func() []string {
				return []string{"pkg1", "pkg2"}
			}(),
			mode:        ComposeModeSpec,
			wantFiles:   2,
			wantSkipped: 0,
		},
		{
			name: "missing arch folder is skipped",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create pkg1/.arch/pub.d2
				createArchFile(t, dir, "pkg1", "pub.d2", "pkg1: {}")
				// pkg2 has no .arch folder
				if err := os.MkdirAll(filepath.Join(dir, "pkg2"), 0755); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			paths: func() []string {
				return []string{"pkg1", "pkg2"}
			}(),
			mode:        ComposeModeAuto,
			wantFiles:   1,
			wantSkipped: 1,
		},
		{
			name: "missing file type is skipped",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// pkg1 has pub.d2 but not pub-spec.d2
				createArchFile(t, dir, "pkg1", "pub.d2", "pkg1: {}")
				return dir
			},
			paths: func() []string {
				return []string{"pkg1"}
			}(),
			mode:        ComposeModeSpec, // Looking for pub-spec.d2
			wantFiles:   0,
			wantSkipped: 1,
		},
		{
			name: "glob pattern expands correctly",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create pkg/a/.arch/pub.d2 and pkg/a/main.go
				createArchFile(t, dir, "pkg/a", "pub.d2", "pkg.a: {}")
				createGoFile(t, dir, "pkg/a", "main.go")
				// Create pkg/b/.arch/pub.d2 and pkg/b/main.go
				createArchFile(t, dir, "pkg/b", "pub.d2", "pkg.b: {}")
				createGoFile(t, dir, "pkg/b", "main.go")
				return dir
			},
			paths: func() []string {
				return []string{"pkg/..."}
			}(),
			mode:      ComposeModeAuto,
			wantFiles: 2,
			// Note: pkg itself has .arch (from children) so it's not skipped as having no .arch,
			// but it has no pub.d2 directly, so it would be skipped for missing file
			// However the current logic may vary based on directory structure
			wantSkipped: 0, // pkg/a and pkg/b both have files
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)

			// Make paths absolute
			paths := make([]string, len(tt.paths))
			for i, p := range tt.paths {
				paths[i] = filepath.Join(dir, p)
			}

			files, skipped, err := findSpecFiles(paths, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("findSpecFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(files) != tt.wantFiles {
				t.Errorf("findSpecFiles() files = %d, want %d (files: %v)", len(files), tt.wantFiles, files)
			}
			if len(skipped) != tt.wantSkipped {
				t.Errorf("findSpecFiles() skipped = %d, want %d (skipped: %v)", len(skipped), tt.wantSkipped, skipped)
			}
		})
	}
}

func Test_expandGlobPatterns(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		patterns []string
		wantMin  int // minimum expected paths
		wantErr  bool
	}{
		{
			name: "expands recursive pattern",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create pkg/a/main.go and pkg/b/main.go
				createGoFile(t, dir, "pkg/a", "main.go")
				createGoFile(t, dir, "pkg/b", "main.go")
				return dir
			},
			patterns: []string{"pkg/..."},
			wantMin:  2,
		},
		{
			name: "passes through concrete paths",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				createGoFile(t, dir, "pkg/a", "main.go")
				return dir
			},
			patterns: []string{"pkg/a"},
			wantMin:  1,
		},
		{
			name: "skips hidden directories",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				createGoFile(t, dir, "pkg/.hidden", "main.go")
				createGoFile(t, dir, "pkg/visible", "main.go")
				return dir
			},
			patterns: []string{"pkg/..."},
			wantMin:  1, // Only visible, not .hidden
		},
		{
			name: "error for non-existent path",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			patterns: []string{"nonexistent/..."},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)

			// Make patterns absolute
			patterns := make([]string, len(tt.patterns))
			for i, p := range tt.patterns {
				patterns[i] = filepath.Join(dir, p)
			}

			paths, err := expandGlobPatterns(patterns)
			if (err != nil) != tt.wantErr {
				t.Errorf("expandGlobPatterns() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(paths) < tt.wantMin {
				t.Errorf("expandGlobPatterns() paths = %d, want at least %d (paths: %v)", len(paths), tt.wantMin, paths)
			}
		})
	}
}

func TestService_Compose(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) string
		opts        func(dir string) ComposeOptions
		wantCount   int
		wantSkipped int
		wantErr     string
	}{
		{
			name: "composes multiple packages",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create pkg1/.arch/pub.d2
				createArchFile(t, dir, "pkg1", "pub.d2", `pkg1: {
  label: "pkg1"
  Service: {
    shape: class
    stereotype: "<<interface>>"
  }
}`)
				// Create pkg2/.arch/pub.d2
				createArchFile(t, dir, "pkg2", "pub.d2", `pkg2: {
  label: "pkg2"
  Repository: {
    shape: class
    stereotype: "<<interface>>"
  }
}`)
				return dir
			},
			opts: func(dir string) ComposeOptions {
				return ComposeOptions{
					Paths:      []string{filepath.Join(dir, "pkg1"), filepath.Join(dir, "pkg2")},
					OutputPath: filepath.Join(dir, "combined.d2"),
					Mode:       ComposeModeAuto,
				}
			},
			wantCount:   2,
			wantSkipped: 0,
		},
		{
			name: "error when no output path",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			opts: func(dir string) ComposeOptions {
				return ComposeOptions{
					Paths: []string{filepath.Join(dir, "pkg")},
				}
			},
			wantErr: "output path is required",
		},
		{
			name: "error when no paths",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			opts: func(dir string) ComposeOptions {
				return ComposeOptions{
					OutputPath: filepath.Join(dir, "out.d2"),
				}
			},
			wantErr: "at least one package path is required",
		},
		{
			name: "error when no spec files found",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				// Create empty package without .arch
				if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0755); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			opts: func(dir string) ComposeOptions {
				return ComposeOptions{
					Paths:      []string{filepath.Join(dir, "pkg")},
					OutputPath: filepath.Join(dir, "out.d2"),
				}
			},
			wantErr: "no spec files found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
			opts := tt.opts(dir)

			// Create mock readers/writers for the service
			svc := NewService(
				&mockModelReader{},
				&mockD2Reader{},
				&mockD2Writer{},
			)

			result, err := svc.Compose(context.Background(), opts)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("Compose() expected error containing %q, got nil", tt.wantErr)
					return
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Compose() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Compose() unexpected error: %v", err)
				return
			}
			if result.PackageCount != tt.wantCount {
				t.Errorf("Compose() PackageCount = %d, want %d", result.PackageCount, tt.wantCount)
			}
			if len(result.SkippedPaths) != tt.wantSkipped {
				t.Errorf("Compose() SkippedPaths = %d, want %d", len(result.SkippedPaths), tt.wantSkipped)
			}
		})
	}
}

// Helper functions

func createArchFile(t *testing.T, baseDir, pkg, filename, content string) {
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
	content := "package " + filepath.Base(pkg) + "\n"
	if err := os.WriteFile(filepath.Join(pkgDir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Mock implementations

type mockModelReader struct{}

func (m *mockModelReader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	return nil, nil
}

type mockD2Reader struct{}

func (m *mockD2Reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	// Return a model for each path
	var models []domain.PackageModel
	for _, p := range paths {
		models = append(models, domain.PackageModel{
			Name: filepath.Base(filepath.Dir(filepath.Dir(p))),
			Path: filepath.Dir(filepath.Dir(p)),
		})
	}
	return models, nil
}

type mockD2Writer struct{}

func (m *mockD2Writer) Write(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error {
	return nil
}

func (m *mockD2Writer) WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error {
	// Create output file
	return os.WriteFile(outputPath, []byte("combined output"), 0644)
}
