# US-2: Generate Combined Diagram from Code - Implementation Plan

## Overview

**User Story:** As a developer, I want to generate a single combined diagram from multiple packages' actual code, so that I can visualize the current implementation's cross-package architecture in one file.

**Key Differentiator:** When `--output=<file.d2>` is specified, generate ONE combined diagram instead of split mode (per-package `.arch/` files).

## Current State Analysis

US-1 (split mode) is fully implemented:
- CLI accepts `--pub` and `--internal` flags
- Service generates per-package diagrams to `.arch/pub.d2` and `.arch/internal.d2`
- `--output` flag exists but returns an error (stubbed for US-2)

Key components:
- `cmd/archai/main.go:100-104` - Currently rejects `--output` flag
- `internal/service/generate.go` - Iterates packages, writes each independently
- `internal/adapter/d2/writer.go` - Writes single package to single file
- `internal/adapter/d2/builder.go` - Builds D2 content for single package

## Design Decisions

### 1. Separate Service Method (Not Branching)

Instead of adding branching logic to `Generate()`, create a completely separate `GenerateCombined()` method:

**Why:**
- Combined mode has different semantics (always public, single output)
- Different options needed (no `PublicOnly`, `InternalOnly` flags)
- Different return type (single result vs per-package results)
- No invalid state combinations possible
- No validation logic needed

```go
// Split mode (existing, unchanged)
func (s *Service) Generate(ctx, opts GenerateOptions) ([]GenerateResult, error)

// Combined mode (new, separate)
func (s *Service) GenerateCombined(ctx, opts GenerateCombinedOptions) (*GenerateCombinedResult, error)
```

### 2. Dedicated Options and Result Types

```go
// Combined mode options - only what's needed
type GenerateCombinedOptions struct {
    Paths      []string  // Package patterns
    OutputPath string    // Required output file
}

// Combined mode result - single file, not per-package
type GenerateCombinedResult struct {
    OutputPath   string  // Path to generated file
    PackageCount int     // Number of packages included
}
```

**Benefits:**
- `OutputPath` is required (not optional like in split mode)
- No `PublicOnly`/`InternalOnly` flags (always public)
- Result reflects single-file output, not per-package

### 3. Simplified Writer Interface

```go
type CombinedModelWriter interface {
    WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error
}
```

**Why not `WriteOptions`:**
- `PublicOnly` not needed (always true for combined)
- `ToStdout` not needed (combined always writes to file)
- Just pass `outputPath` directly

### 4. Package-Level Containers (Not File-Level)

Split mode groups by source file within a package. Combined mode groups by package:

```d2
# Combined mode structure
pkg.archspec: {
  label: "pkg/archspec"
  Service: { ... }
  PackageModel: { ... }
}

pkg.linter: {
  label: "pkg/linter"
  Linter: { ... }
}

# Cross-package dependencies
pkg.linter.Linter -> pkg.archspec.Service: "uses"
```

### 5. Cross-Package Dependencies

The key value of combined diagrams is showing inter-package relationships:
- Reader already tracks dependencies with full package paths (`SymbolRef.Package`)
- Builder needs to render cross-package arrows
- Dependencies within same package still shown

## Implementation Tasks

### Task 1: Add Combined Options and Result Types

**File:** `internal/service/generate_combined.go` (new file)

```go
package service

import "context"

// GenerateCombinedOptions configures combined diagram generation.
// Combined mode always generates public API only into a single file.
type GenerateCombinedOptions struct {
    Paths      []string // Package patterns to include
    OutputPath string   // Output file path (required)
}

// GenerateCombinedResult contains the result of combined diagram generation.
type GenerateCombinedResult struct {
    OutputPath   string // Path to generated diagram
    PackageCount int    // Number of packages included
}

// GenerateCombined creates a single diagram from multiple packages.
// Unlike Generate (split mode), this always produces public API only.
func (s *Service) GenerateCombined(ctx context.Context, opts GenerateCombinedOptions) (*GenerateCombinedResult, error) {
    if ctx.Err() != nil {
        return nil, ctx.Err()
    }

    // Read all packages
    packages, err := s.goReader.Read(ctx, opts.Paths)
    if err != nil {
        return nil, fmt.Errorf("failed to read packages: %w", err)
    }

    // Filter to packages with exported symbols
    var filtered []domain.PackageModel
    for _, pkg := range packages {
        if pkg.HasExportedSymbols() {
            filtered = append(filtered, pkg)
        }
    }

    if len(filtered) == 0 {
        return &GenerateCombinedResult{
            OutputPath:   opts.OutputPath,
            PackageCount: 0,
        }, nil
    }

    // Write combined diagram
    if err := s.d2Writer.WriteCombined(ctx, filtered, opts.OutputPath); err != nil {
        return nil, fmt.Errorf("failed to write combined diagram: %w", err)
    }

    return &GenerateCombinedResult{
        OutputPath:   opts.OutputPath,
        PackageCount: len(filtered),
    }, nil
}
```

### Task 2: Extend ModelWriter Interface

**File:** `internal/service/options.go`

```go
type ModelWriter interface {
    Write(ctx context.Context, model domain.PackageModel, opts domain.WriteOptions) error
    WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error
}
```

### Task 3: Implement Combined Builder

**File:** `internal/adapter/d2/combined_builder.go` (new file)

```go
package d2

type combinedBuilder struct {
    buf    strings.Builder
    indent int
}

func (b *combinedBuilder) Build(packages []domain.PackageModel) string {
    // 1. Write header comment
    b.writeLine("# Combined Architecture Diagram")
    b.writeLine("")

    // 2. Write legend
    b.writeLegend()

    // 3. For each package, create container with symbols
    for _, pkg := range packages {
        b.writePackageContainer(pkg)
    }

    // 4. Write cross-package dependencies
    b.writeCrossPackageDependencies(packages)

    return b.buf.String()
}

func (b *combinedBuilder) writePackageContainer(pkg domain.PackageModel) {
    pkgID := sanitizePackageID(pkg.Path)

    b.writeLine("%s: {", pkgID)
    b.indent++
    b.writeLine("label: %q", pkg.Path)
    b.writeLine("")

    // Write exported symbols only (combined is always public)
    for _, iface := range pkg.ExportedInterfaces() {
        b.writeInterface(iface)
    }
    for _, strct := range pkg.ExportedStructs() {
        b.writeStruct(strct)
    }
    for _, fn := range pkg.ExportedFunctions() {
        b.writeFunction(fn)
    }
    for _, td := range pkg.ExportedTypeDefs() {
        b.writeTypeDef(td)
    }

    b.indent--
    b.writeLine("}")
    b.writeLine("")
}

func (b *combinedBuilder) writeCrossPackageDependencies(packages []domain.PackageModel) {
    deps := b.collectCrossPackageDeps(packages)
    if len(deps) == 0 {
        return
    }

    b.writeLine("# Cross-package dependencies")
    for _, dep := range deps {
        fromID := fmt.Sprintf("%s.%s", sanitizePackageID(dep.fromPkg), dep.fromSymbol)
        toID := fmt.Sprintf("%s.%s", sanitizePackageID(dep.toPkg), dep.toSymbol)
        b.writeLine("%s -> %s: %q", fromID, toID, dep.kind)
    }
}

func sanitizePackageID(path string) string {
    return strings.ReplaceAll(path, "/", ".")
}
```

### Task 4: Implement WriteCombined in D2 Writer

**File:** `internal/adapter/d2/writer.go`

```go
func (w *Writer) WriteCombined(ctx context.Context, models []domain.PackageModel, outputPath string) error {
    if ctx.Err() != nil {
        return ctx.Err()
    }

    builder := &combinedBuilder{}
    content := builder.Build(models)

    // Ensure parent directory exists
    if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
        return fmt.Errorf("failed to create output directory: %w", err)
    }

    return os.WriteFile(outputPath, []byte(content), 0644)
}
```

### Task 5: Update CLI Command

**File:** `cmd/archai/main.go`

CLI decides which service method to call based on `--output` flag:

```go
func runGenerate(cmd *cobra.Command, args []string) error {
    // ... setup code ...

    outputFile, _ := cmd.Flags().GetString("output")
    pubOnly, _ := cmd.Flags().GetBool("pub")
    internalOnly, _ := cmd.Flags().GetBool("internal")

    // Combined mode: --output flag present
    if outputFile != "" {
        // --pub and --internal don't apply to combined mode
        if pubOnly || internalOnly {
            fmt.Fprintln(os.Stderr, "Note: --pub and --internal flags are ignored in combined mode (always public)")
        }

        opts := service.GenerateCombinedOptions{
            Paths:      args,
            OutputPath: outputFile,
        }

        result, err := svc.GenerateCombined(ctx, opts)
        if err != nil {
            return err
        }

        fmt.Printf("Combined diagram generated: %s\n", result.OutputPath)
        fmt.Printf("Packages included: %d\n", result.PackageCount)
        return nil
    }

    // Split mode: existing logic (unchanged)
    opts := service.GenerateOptions{
        Paths:        args,
        PublicOnly:   pubOnly,
        InternalOnly: internalOnly,
    }

    results, err := svc.Generate(ctx, opts)
    // ... existing split mode output logic ...
}
```

### Task 6: Cross-Package Dependency Collection

**File:** `internal/adapter/d2/combined_builder.go`

```go
type crossPkgDep struct {
    fromPkg    string
    fromSymbol string
    toPkg      string
    toSymbol   string
    kind       domain.DependencyKind
}

func (b *combinedBuilder) collectCrossPackageDeps(packages []domain.PackageModel) []crossPkgDep {
    // Build index of visible symbols per package
    symbolIndex := make(map[string]map[string]bool)
    for _, pkg := range packages {
        symbols := make(map[string]bool)
        for _, iface := range pkg.ExportedInterfaces() {
            symbols[iface.Name] = true
        }
        for _, strct := range pkg.ExportedStructs() {
            symbols[strct.Name] = true
        }
        for _, fn := range pkg.ExportedFunctions() {
            symbols[fn.Name] = true
        }
        for _, td := range pkg.ExportedTypeDefs() {
            symbols[td.Name] = true
        }
        symbolIndex[pkg.Path] = symbols
    }

    // Collect cross-package dependencies
    var deps []crossPkgDep
    seen := make(map[string]bool)

    for _, pkg := range packages {
        for _, dep := range pkg.Dependencies {
            // Skip intra-package
            if dep.From.Package == dep.To.Package {
                continue
            }
            // Skip if target package not in diagram
            if _, ok := symbolIndex[dep.To.Package]; !ok {
                continue
            }
            // Skip if source or target symbol not visible
            if !symbolIndex[dep.From.Package][dep.From.Symbol] {
                continue
            }
            if !symbolIndex[dep.To.Package][dep.To.Symbol] {
                continue
            }

            // Deduplicate
            key := fmt.Sprintf("%s.%s->%s.%s", dep.From.Package, dep.From.Symbol, dep.To.Package, dep.To.Symbol)
            if seen[key] {
                continue
            }
            seen[key] = true

            deps = append(deps, crossPkgDep{
                fromPkg:    dep.From.Package,
                fromSymbol: dep.From.Symbol,
                toPkg:      dep.To.Package,
                toSymbol:   dep.To.Symbol,
                kind:       dep.Kind,
            })
        }
    }

    return deps
}
```

## Test Plan

### Unit Tests

**`internal/adapter/d2/combined_builder_test.go`:**
- Single package renders with container
- Multiple packages each get containers
- Cross-package dependencies render as arrows
- Packages without exported symbols skipped
- Package ID sanitization (`pkg/foo` → `pkg.foo`)

**`internal/adapter/d2/writer_test.go`:**
- `WriteCombined` writes to file
- `WriteCombined` creates parent directories

**`internal/service/generate_combined_test.go`:**
- Returns correct `PackageCount`
- Filters empty packages
- Context cancellation
- Reader error handling
- Writer error handling

### E2E Tests

**`cmd/archai/e2e_test.go`:**
- `TestE2E_CLI_GenerateCombined`: Basic `--output` flag
- `TestE2E_CLI_GenerateCombined_MultiplePackages`: Multiple packages in one file
- `TestE2E_CLI_GenerateCombined_CrossPackageDeps`: Verify cross-package arrows in output
- `TestE2E_CLI_GenerateCombined_IgnoresPubInternalFlags`: Warning printed but works

## Acceptance Criteria Checklist

- [ ] Accept multiple package paths as input
- [ ] When `--output=<file.d2>` is specified, generate ONE combined diagram
- [ ] Public interfaces only (combined diagrams are for high-level overview)
- [ ] Show all packages in one diagram with inter-package connections
- [ ] Package relationships visible (which packages depend on which)

## File Changes Summary

| File | Change Type | Description |
|------|-------------|-------------|
| `internal/service/options.go` | Modify | Add `WriteCombined` to `ModelWriter` interface |
| `internal/service/generate_combined.go` | **New** | `GenerateCombined`, options, result types |
| `internal/adapter/d2/combined_builder.go` | **New** | Multi-package D2 builder |
| `internal/adapter/d2/writer.go` | Modify | Add `WriteCombined` method |
| `cmd/archai/main.go` | Modify | Remove stub, branch to `GenerateCombined` |
| `internal/service/generate_combined_test.go` | **New** | Unit tests |
| `internal/adapter/d2/combined_builder_test.go` | **New** | Unit tests |
| `cmd/archai/e2e_test.go` | Modify | Add E2E tests |

## Implementation Order

1. **Task 2**: Add `WriteCombined` to interface (enables compilation)
2. **Task 3**: Implement combined builder (core D2 generation logic)
3. **Task 4**: Implement `WriteCombined` in writer (file output)
4. **Task 1**: Add service method and types (orchestration)
5. **Task 5**: Update CLI (user-facing)
6. **Task 6**: Cross-package deps (part of Task 3)

## Out of Scope

- `--internal` flag support for combined mode (user story specifies public-only)
- Intra-package file grouping in combined mode (package-level containers only)
- Custom package ordering or layout
- `--stdout` support for combined mode (always writes to file)
