package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
)

func TestService_GenerateCombined(t *testing.T) {
	tests := []struct {
		name                  string
		opts                  GenerateCombinedOptions
		mockPackages          []domain.PackageModel
		mockReadError         error
		mockWriteCombinedErr  error
		wantPackageCount      int
		wantError             bool
		checkResults          func(t *testing.T, result *GenerateCombinedResult, writer *mockWriter)
	}{
		{
			name: "generates combined diagram for multiple packages",
			opts: GenerateCombinedOptions{
				Paths:      []string{"./internal/..."},
				OutputPath: "architecture.d2",
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "pkg1",
					Path: "internal/pkg1",
					Interfaces: []domain.InterfaceDef{
						{Name: "Service1", IsExported: true, SourceFile: "service.go"},
					},
				},
				{
					Name: "pkg2",
					Path: "internal/pkg2",
					Structs: []domain.StructDef{
						{Name: "Entity", IsExported: true, SourceFile: "entity.go"},
					},
				},
			},
			wantPackageCount: 2,
			wantError:        false,
			checkResults: func(t *testing.T, result *GenerateCombinedResult, writer *mockWriter) {
				if result.OutputPath != "architecture.d2" {
					t.Errorf("Result.OutputPath = %q, want %q", result.OutputPath, "architecture.d2")
				}
				if result.PackageCount != 2 {
					t.Errorf("Result.PackageCount = %d, want %d", result.PackageCount, 2)
				}

				// Verify WriteCombined was called once
				if len(writer.combinedCalls) != 1 {
					t.Fatalf("WriteCombined called %d times, want 1", len(writer.combinedCalls))
				}

				call := writer.combinedCalls[0]
				if call.outputPath != "architecture.d2" {
					t.Errorf("WriteCombined outputPath = %q, want %q", call.outputPath, "architecture.d2")
				}
				if len(call.models) != 2 {
					t.Errorf("WriteCombined models count = %d, want %d", len(call.models), 2)
				}
			},
		},
		{
			name: "filters packages without exported symbols",
			opts: GenerateCombinedOptions{
				Paths:      []string{"./internal/..."},
				OutputPath: "output.d2",
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "pkg1",
					Path: "internal/pkg1",
					Interfaces: []domain.InterfaceDef{
						{Name: "Service", IsExported: true, SourceFile: "service.go"},
					},
				},
				{
					Name: "pkg2",
					Path: "internal/pkg2",
					// Only unexported symbols
					Structs: []domain.StructDef{
						{Name: "helper", IsExported: false, SourceFile: "helper.go"},
					},
				},
			},
			wantPackageCount: 1,
			wantError:        false,
			checkResults: func(t *testing.T, result *GenerateCombinedResult, writer *mockWriter) {
				if result.PackageCount != 1 {
					t.Errorf("Result.PackageCount = %d, want %d", result.PackageCount, 1)
				}

				// Verify only 1 package was passed to WriteCombined
				if len(writer.combinedCalls[0].models) != 1 {
					t.Errorf("WriteCombined models count = %d, want %d", len(writer.combinedCalls[0].models), 1)
				}
			},
		},
		{
			name: "returns zero package count when no packages have exported symbols",
			opts: GenerateCombinedOptions{
				Paths:      []string{"./internal/..."},
				OutputPath: "output.d2",
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "pkg1",
					Path: "internal/pkg1",
					// Only unexported symbols
					Structs: []domain.StructDef{
						{Name: "helper", IsExported: false, SourceFile: "helper.go"},
					},
				},
			},
			wantPackageCount: 0,
			wantError:        false,
			checkResults: func(t *testing.T, result *GenerateCombinedResult, writer *mockWriter) {
				if result.PackageCount != 0 {
					t.Errorf("Result.PackageCount = %d, want %d", result.PackageCount, 0)
				}
				if result.OutputPath != "output.d2" {
					t.Errorf("Result.OutputPath = %q, want %q", result.OutputPath, "output.d2")
				}

				// WriteCombined should not be called when no packages have exports
				if len(writer.combinedCalls) != 0 {
					t.Errorf("WriteCombined called %d times, want 0", len(writer.combinedCalls))
				}
			},
		},
		{
			name: "returns error when reader fails",
			opts: GenerateCombinedOptions{
				Paths:      []string{"./invalid/..."},
				OutputPath: "output.d2",
			},
			mockReadError: errors.New("read error"),
			wantError:     true,
		},
		{
			name: "returns error when writer fails",
			opts: GenerateCombinedOptions{
				Paths:      []string{"./internal/..."},
				OutputPath: "output.d2",
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "pkg1",
					Path: "internal/pkg1",
					Interfaces: []domain.InterfaceDef{
						{Name: "Service", IsExported: true, SourceFile: "service.go"},
					},
				},
			},
			mockWriteCombinedErr: errors.New("write error"),
			wantError:            true,
		},
		{
			name: "handles single package",
			opts: GenerateCombinedOptions{
				Paths:      []string{"./pkg"},
				OutputPath: "single.d2",
			},
			mockPackages: []domain.PackageModel{
				{
					Name: "pkg",
					Path: "pkg",
					Functions: []domain.FunctionDef{
						{Name: "NewService", IsExported: true, SourceFile: "factory.go"},
					},
				},
			},
			wantPackageCount: 1,
			wantError:        false,
			checkResults: func(t *testing.T, result *GenerateCombinedResult, writer *mockWriter) {
				if len(writer.combinedCalls) != 1 {
					t.Fatalf("WriteCombined called %d times, want 1", len(writer.combinedCalls))
				}
				if len(writer.combinedCalls[0].models) != 1 {
					t.Errorf("WriteCombined models count = %d, want %d", len(writer.combinedCalls[0].models), 1)
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
				writeCombinedFunc: func(ctx context.Context, models []domain.PackageModel, outputPath string) error {
					return tt.mockWriteCombinedErr
				},
			}

			svc := &Service{
				goReader: reader,
				d2Writer: writer,
			}

			result, err := svc.GenerateCombined(context.Background(), tt.opts)

			if (err != nil) != tt.wantError {
				t.Errorf("GenerateCombined() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError {
				if result == nil {
					t.Fatal("GenerateCombined() returned nil result")
				}
				if result.PackageCount != tt.wantPackageCount {
					t.Errorf("GenerateCombined() PackageCount = %d, want %d", result.PackageCount, tt.wantPackageCount)
				}
			}

			if tt.checkResults != nil && result != nil {
				tt.checkResults(t, result, writer)
			}
		})
	}
}

func TestService_GenerateCombined_ContextCancellation(t *testing.T) {
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

	opts := GenerateCombinedOptions{
		Paths:      []string{"./test"},
		OutputPath: "output.d2",
	}

	_, err := svc.GenerateCombined(ctx, opts)
	if err == nil {
		t.Fatal("GenerateCombined() expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("GenerateCombined() error = %v, want context.Canceled", err)
	}
}
