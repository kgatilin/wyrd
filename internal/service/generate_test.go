package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

// mockReader implements ModelReader for testing.
type mockReader struct {
	packages []domain.PackageModel
	err      error
}

func (m *mockReader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.packages, nil
}

// mockWriter implements ModelWriter for testing.
type mockWriter struct {
	writeFunc         func(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error
	writeCombinedFunc func(ctx context.Context, models []domain.PackageModel, outputPath string) error
	calls             []writeCall
	combinedCalls     []writeCombinedCall
}

type writeCall struct {
	model domain.PackageModel
	opts  domain.WriteOptions
}

type writeCombinedCall struct {
	models     []domain.PackageModel
	outputPath string
}

func (m *mockWriter) Write(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error {
	m.calls = append(m.calls, writeCall{model: model, opts: opts})
	if m.writeFunc != nil {
		return m.writeFunc(ctx, model, opts)
	}
	return nil
}

func (m *mockWriter) WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error {
	m.combinedCalls = append(m.combinedCalls, writeCombinedCall{models: models, outputPath: outputPath})
	if m.writeCombinedFunc != nil {
		return m.writeCombinedFunc(ctx, models, outputPath)
	}
	return nil
}

func TestService_Generate(t *testing.T) {
	tests := []struct {
		name           string
		opts           GenerateOptions
		mockPackages   []domain.PackageModel
		mockReadError  error
		mockWriteError error
		wantResults    int
		wantError      bool
		checkResults   func(t *testing.T, results []GenerateResult, writer *mockWriter)
	}{
		{
			name: "generates both pub and internal diagrams by default",
			opts: GenerateOptions{
				Paths: []string{"./test"},
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "testpkg",
					Path: "internal/testpkg",
				},
			},
			wantResults: 1,
			wantError:   false,
			checkResults: func(t *testing.T, results []GenerateResult, writer *mockWriter) {
				if len(results) != 1 {
					t.Fatalf("Expected 1 result, got %d", len(results))
				}

				result := results[0]
				if result.PackagePath != "internal/testpkg" {
					t.Errorf("Result.PackagePath = %q, want %q", result.PackagePath, "internal/testpkg")
				}
				if result.Error != nil {
					t.Errorf("Result.Error = %v, want nil", result.Error)
				}
				if result.PubFile == "" {
					t.Errorf("Result.PubFile should not be empty")
				}
				if result.InternalFile == "" {
					t.Errorf("Result.InternalFile should not be empty")
				}

				// Verify writer was called twice (pub and internal)
				if len(writer.calls) != 2 {
					t.Errorf("Writer called %d times, want 2", len(writer.calls))
				}

				// First call should be pub.d2
				if !writer.calls[0].opts.PublicOnly {
					t.Errorf("First write should be PublicOnly=true")
				}
				if writer.calls[0].opts.OutputPath != "internal/testpkg/.arch/pub.d2" {
					t.Errorf("First write OutputPath = %q, want %q", writer.calls[0].opts.OutputPath, "internal/testpkg/.arch/pub.d2")
				}

				// Second call should be internal.d2
				if writer.calls[1].opts.PublicOnly {
					t.Errorf("Second write should be PublicOnly=false")
				}
				if writer.calls[1].opts.OutputPath != "internal/testpkg/.arch/internal.d2" {
					t.Errorf("Second write OutputPath = %q, want %q", writer.calls[1].opts.OutputPath, "internal/testpkg/.arch/internal.d2")
				}
			},
		},
		{
			name: "generates only pub diagram when PublicOnly is true",
			opts: GenerateOptions{
				Paths:      []string{"./test"},
				PublicOnly: true,
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "testpkg",
					Path: "internal/testpkg",
				},
			},
			wantResults: 1,
			wantError:   false,
			checkResults: func(t *testing.T, results []GenerateResult, writer *mockWriter) {
				result := results[0]
				if result.PubFile == "" {
					t.Errorf("Result.PubFile should not be empty")
				}
				if result.InternalFile != "" {
					t.Errorf("Result.InternalFile should be empty, got %q", result.InternalFile)
				}

				// Verify writer was called once for pub.d2
				if len(writer.calls) != 1 {
					t.Errorf("Writer called %d times, want 1", len(writer.calls))
				}
				if !writer.calls[0].opts.PublicOnly {
					t.Errorf("Write should be PublicOnly=true")
				}
			},
		},
		{
			name: "generates only internal diagram when InternalOnly is true",
			opts: GenerateOptions{
				Paths:        []string{"./test"},
				InternalOnly: true,
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "testpkg",
					Path: "internal/testpkg",
				},
			},
			wantResults: 1,
			wantError:   false,
			checkResults: func(t *testing.T, results []GenerateResult, writer *mockWriter) {
				result := results[0]
				if result.PubFile != "" {
					t.Errorf("Result.PubFile should be empty, got %q", result.PubFile)
				}
				if result.InternalFile == "" {
					t.Errorf("Result.InternalFile should not be empty")
				}

				// Verify writer was called once for internal.d2
				if len(writer.calls) != 1 {
					t.Errorf("Writer called %d times, want 1", len(writer.calls))
				}
				if writer.calls[0].opts.PublicOnly {
					t.Errorf("Write should be PublicOnly=false")
				}
			},
		},
		{
			name: "handles multiple packages",
			opts: GenerateOptions{
				Paths: []string{"./internal/..."},
			},
			mockPackages: []domain.PackageModel{
				{Name: "pkg1", Path: "internal/pkg1"},
				{Name: "pkg2", Path: "internal/pkg2"},
				{Name: "pkg3", Path: "internal/pkg3"},
			},
			wantResults: 3,
			wantError:   false,
			checkResults: func(t *testing.T, results []GenerateResult, writer *mockWriter) {
				if len(results) != 3 {
					t.Fatalf("Expected 3 results, got %d", len(results))
				}

				// Each package should have both pub and internal files
				for i, result := range results {
					if result.Error != nil {
						t.Errorf("Result[%d].Error = %v, want nil", i, result.Error)
					}
					if result.PubFile == "" {
						t.Errorf("Result[%d].PubFile should not be empty", i)
					}
					if result.InternalFile == "" {
						t.Errorf("Result[%d].InternalFile should not be empty", i)
					}
				}

				// Writer should be called 6 times (2 per package)
				if len(writer.calls) != 6 {
					t.Errorf("Writer called %d times, want 6", len(writer.calls))
				}
			},
		},
		{
			name: "returns error when reader fails",
			opts: GenerateOptions{
				Paths: []string{"./test"},
			},
			mockReadError: errors.New("read error"),
			wantResults:   0,
			wantError:     true,
		},
		{
			name: "continues processing other packages when write fails",
			opts: GenerateOptions{
				Paths: []string{"./test"},
			},
			mockPackages: []domain.PackageModel{
				{Name: "pkg1", Path: "internal/pkg1"},
				{Name: "pkg2", Path: "internal/pkg2"},
			},
			mockWriteError: errors.New("write error"),
			wantResults:    2,
			wantError:      false,
			checkResults: func(t *testing.T, results []GenerateResult, writer *mockWriter) {
				if len(results) != 2 {
					t.Fatalf("Expected 2 results, got %d", len(results))
				}

				// Both results should have errors
				for i, result := range results {
					if result.Error == nil {
						t.Errorf("Result[%d].Error should not be nil", i)
					}
				}
			},
		},
		{
			name: "handles root package",
			opts: GenerateOptions{
				Paths: []string{"."},
			},
			mockPackages: []domain.PackageModel{
				{Name: "main", Path: ""},
			},
			wantResults: 1,
			wantError:   false,
			checkResults: func(t *testing.T, results []GenerateResult, writer *mockWriter) {
				// Root package should create .arch directory at root
				if writer.calls[0].opts.OutputPath != ".arch/pub.d2" {
					t.Errorf("Root package OutputPath = %q, want %q", writer.calls[0].opts.OutputPath, ".arch/pub.d2")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReader{
				packages: tt.mockPackages,
				err:      tt.mockReadError,
			}

			writer := &mockWriter{
				writeFunc: func(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error {
					return tt.mockWriteError
				},
			}

			svc := &Service{
				goReader: reader,
				d2Writer: writer,
			}

			results, err := svc.Generate(context.Background(), tt.opts)

			if (err != nil) != tt.wantError {
				t.Errorf("Generate() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if len(results) != tt.wantResults {
				t.Errorf("Generate() returned %d results, want %d", len(results), tt.wantResults)
			}

			if tt.checkResults != nil {
				tt.checkResults(t, results, writer)
			}
		})
	}
}

func TestService_Generate_ContextCancellation(t *testing.T) {
	reader := &mockReader{
		packages: []domain.PackageModel{
			{Name: "pkg1", Path: "internal/pkg1"},
		},
	}

	writer := &mockWriter{}

	svc := &Service{
		goReader: reader,
		d2Writer: writer,
	}

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := GenerateOptions{
		Paths: []string{"./test"},
	}

	_, err := svc.Generate(ctx, opts)
	if err == nil {
		t.Fatal("Generate() expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("Generate() error = %v, want context.Canceled", err)
	}
}

func TestService_resolveArchDir(t *testing.T) {
	svc := &Service{}

	tests := []struct {
		name    string
		pkgPath string
		want    string
	}{
		{
			name:    "regular package",
			pkgPath: "internal/service",
			want:    "internal/service/.arch",
		},
		{
			name:    "nested package",
			pkgPath: "internal/adapter/golang",
			want:    "internal/adapter/golang/.arch",
		},
		{
			name:    "root package empty string",
			pkgPath: "",
			want:    ".arch",
		},
		{
			name:    "root package dot",
			pkgPath: ".",
			want:    ".arch",
		},
		{
			name:    "cmd package",
			pkgPath: "cmd/archai",
			want:    "cmd/archai/.arch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.resolveArchDir(tt.pkgPath)
			if got != tt.want {
				t.Errorf("resolveArchDir(%q) = %q, want %q", tt.pkgPath, got, tt.want)
			}
		})
	}
}
