# Implementation Plan: US-4 Compose Diagram from Saved Specs

## Overview

**User Story:** As a developer, I want to compose a single combined diagram from all saved `.arch/` specification files, so that I can see the full target architecture (not current code) across multiple packages.

**Source:** Reads saved `.arch/*-spec.d2` or `.arch/*.d2` files (not Go code)
**Output:** Single combined D2 diagram

## Scope

### In Scope
- New CLI command: `diagram compose <packages> --output=<file>`
- Mode flags: `--spec` (prefer `-spec.d2`), `--code` (prefer `.d2`), auto-detect (default)
- Service method to orchestrate the compose operation
- File discovery logic to find `.arch/` spec files across packages
- Graceful handling of missing `.arch/` folders (warn and skip)

### Out of Scope
- D2 reader enhancements (already complete)
- Combined builder changes (already supports multi-package)
- Dependency reconstruction improvements (deferred to US-5/US-6)

## Architecture

### Component Overview

```
CLI (main.go)
    │
    ▼
Service.Compose(ctx, ComposeOptions)
    │
    ├── findSpecFiles(paths, mode) → []string (internal helper)
    │
    ├── d2Reader.Read(ctx, specFiles) → []PackageModel
    │
    └── d2Writer.WriteCombined(ctx, models, outputPath) → combined D2
```

### Key Design Decisions

1. **Reuse Existing Components:** The D2 reader and combined writer are already fully implemented. Compose is orchestration only.

2. **File Discovery Abstraction:** A `SpecFinder` utility handles locating `.arch/` files with mode-aware selection logic.

3. **Mode Selection Logic:**
   - Auto (default): Use `pub.d2` files (code-generated diagrams)
   - `--spec`: Use `*-spec.d2` files only (target specifications)

4. **Path Expansion:** Support glob patterns (`./pkg/...`) like other commands.

## Implementation Tasks

### Task 1: Add ComposeOptions and ComposeResult Types

**File:** `internal/service/options.go`

Add new types for the compose operation:

```go
// ComposeMode specifies which diagram files to compose from
type ComposeMode int

const (
    ComposeModeAuto ComposeMode = iota // Default: use pub.d2 files (code-generated)
    ComposeModeSpec                     // Only use *-spec.d2 files (target specs)
)

// ComposeOptions configures the compose operation
type ComposeOptions struct {
    Paths      []string    // Package paths to search for .arch/ folders
    OutputPath string      // Required: path to output combined diagram
    Mode       ComposeMode // Which files to compose from
}

// ComposeResult contains the result of a compose operation
type ComposeResult struct {
    OutputPath   string   // Path to generated combined diagram
    PackageCount int      // Number of packages included
    SkippedPaths []string // Packages skipped due to missing .arch/
}
```

**Acceptance Criteria:**
- [ ] Types compile without errors
- [ ] ComposeMode constants defined with iota
- [ ] SkippedPaths field for transparency on missing .arch/ folders

---

### Task 2: Implement Spec File Discovery (Internal Helper)

**File:** `internal/service/compose.go` (same file as Compose method)

Add unexported helper functions for spec file discovery:

```go
// findSpecFiles locates .arch/ spec files in package directories
func findSpecFiles(paths []string, mode ComposeMode) (files []string, skipped []string, err error)

// expandGlobPatterns converts Go-style patterns to concrete paths
func expandGlobPatterns(patterns []string) ([]string, error)
```

**Implementation Logic:**

1. Expand glob patterns (e.g., `./pkg/...`) to concrete package paths
2. For each package path:
   - Check if `.arch/` directory exists
   - If not, add to `skipped` and continue
   - Based on mode, select files:
     - **Auto:** Use `pub.d2` (code-generated)
     - **Spec:** Use `pub-spec.d2` (target specification)
3. Return collected file paths and skipped paths

**Key Details:**
- Use `filepath.WalkDir` for recursive pattern expansion
- Handle both absolute and relative paths
- Support multiple patterns in a single call
- Skip packages gracefully without failing entire operation
- Keep as unexported helper functions (not part of public API)

**Acceptance Criteria:**
- [ ] Expands `./pkg/...` patterns correctly
- [ ] Finds `pub.d2` files in Auto mode (default)
- [ ] Finds `pub-spec.d2` files in Spec mode (`--spec` flag)
- [ ] Returns skipped paths for packages without `.arch/`
- [ ] Works with both absolute and relative paths

---

### Task 3: Implement Service.Compose Method

**File:** `internal/service/compose.go` (new)

Add the compose operation to the service:

```go
// Compose combines saved .arch/ spec files into a single diagram
func (s *Service) Compose(ctx context.Context, opts ComposeOptions) (*ComposeResult, error) {
    // 1. Validate options
    if opts.OutputPath == "" {
        return nil, errors.New("output path is required")
    }
    if len(opts.Paths) == 0 {
        return nil, errors.New("at least one package path is required")
    }

    // 2. Find spec files (using internal helper)
    files, skipped, err := findSpecFiles(opts.Paths, opts.Mode)
    if err != nil {
        return nil, fmt.Errorf("finding spec files: %w", err)
    }

    if len(files) == 0 {
        return nil, errors.New("no spec files found in specified packages")
    }

    // 3. Read spec files into models
    models, err := s.d2Reader.Read(ctx, files)
    if err != nil {
        return nil, fmt.Errorf("reading spec files: %w", err)
    }

    // 4. Write combined diagram
    if err := s.d2Writer.WriteCombined(ctx, models, opts.OutputPath); err != nil {
        return nil, fmt.Errorf("writing combined diagram: %w", err)
    }

    return &ComposeResult{
        OutputPath:   opts.OutputPath,
        PackageCount: len(models),
        SkippedPaths: skipped,
    }, nil
}
```

**Acceptance Criteria:**
- [ ] Validates required OutputPath
- [ ] Validates at least one path provided
- [ ] Uses SpecFinder to locate files
- [ ] Returns error if no spec files found
- [ ] Reads all found files via d2Reader
- [ ] Writes combined diagram via d2Writer
- [ ] Returns result with package count and skipped paths
- [ ] Properly propagates context for cancellation

---

### Task 4: Add CLI Compose Command

**File:** `cmd/archai/main.go`

Add the `compose` subcommand to the CLI:

```go
// In diagram command setup
composeCmd := &cobra.Command{
    Use:   "compose [packages...]",
    Short: "Compose a single diagram from saved .arch/ spec files",
    Long: `Compose reads saved .arch/ specification files from multiple packages
and combines them into a single diagram file.

Examples:
  # Compose from saved specs (target state)
  archai diagram compose pkg/archspec pkg/linter --output=docs/target-architecture.d2

  # Compose from code-generated diagrams
  archai diagram compose pkg/... --code --output=docs/current-architecture.d2

  # Auto-detect (prefer -spec.d2 if exists, fallback to .d2)
  archai diagram compose pkg/... --output=docs/architecture.d2`,
    Args: cobra.MinimumNArgs(1),
    RunE: runCompose,
}

composeCmd.Flags().StringP("output", "o", "", "Output file path (required)")
composeCmd.Flags().Bool("spec", false, "Use *-spec.d2 files instead of pub.d2")
composeCmd.MarkFlagRequired("output")

diagramCmd.AddCommand(composeCmd)
```

**runCompose Implementation:**

```go
func runCompose(cmd *cobra.Command, args []string) error {
    // 1. Wire dependencies
    d2Reader := d2.NewReader()
    d2Writer := d2.NewWriter()
    goReader := golang.NewReader() // Required by factory, though not used

    svc := service.NewService(goReader, d2Reader, d2Writer)

    // 2. Parse flags
    outputPath, _ := cmd.Flags().GetString("output")
    specOnly, _ := cmd.Flags().GetBool("spec")

    // 3. Determine mode
    mode := service.ComposeModeAuto
    if specOnly {
        mode = service.ComposeModeSpec
    }

    // 5. Execute compose
    result, err := svc.Compose(cmd.Context(), service.ComposeOptions{
        Paths:      args,
        OutputPath: outputPath,
        Mode:       mode,
    })
    if err != nil {
        return err
    }

    // 6. Display result
    fmt.Printf("Composed %d packages into %s\n", result.PackageCount, result.OutputPath)
    if len(result.SkippedPaths) > 0 {
        fmt.Printf("Skipped %d packages (no .arch/ folder):\n", len(result.SkippedPaths))
        for _, path := range result.SkippedPaths {
            fmt.Printf("  - %s\n", path)
        }
    }

    return nil
}
```

**Acceptance Criteria:**
- [ ] Command registered under `diagram` parent
- [ ] `--output` flag is required
- [ ] `--spec` and `--code` flags are mutually exclusive
- [ ] Displays package count and output path on success
- [ ] Shows skipped packages with reason
- [ ] Proper error messages for invalid inputs

---

### Task 5: Handle Glob Pattern Expansion

**File:** `internal/service/compose.go`

The helper needs to expand Go-style glob patterns like `./pkg/...`:

```go
// expandGlobPatterns converts Go-style patterns to concrete paths
func expandGlobPatterns(patterns []string) ([]string, error) {
    var paths []string
    for _, pattern := range patterns {
        if strings.HasSuffix(pattern, "/...") {
            // Recursive pattern - find all subdirectories
            base := strings.TrimSuffix(pattern, "/...")
            err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
                if err != nil {
                    return err
                }
                if d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
                    // Check if this looks like a Go package (has .go files)
                    if hasGoFiles(path) {
                        paths = append(paths, path)
                    }
                }
                return nil
            })
            if err != nil {
                return nil, fmt.Errorf("expanding pattern %s: %w", pattern, err)
            }
        } else {
            // Concrete path
            paths = append(paths, pattern)
        }
    }
    return paths, nil
}

func hasGoFiles(dir string) bool {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return false
    }
    for _, e := range entries {
        if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
            return true
        }
    }
    return false
}
```

**Acceptance Criteria:**
- [ ] Expands `./pkg/...` to all subdirectories with Go files
- [ ] Handles `./internal/...` pattern
- [ ] Passes through concrete paths unchanged
- [ ] Skips hidden directories (starting with `.`)
- [ ] Returns error for non-existent base paths

---

### Task 6: Write Unit Tests

**File:** `internal/service/compose_test.go` (new)

#### File Discovery Tests

```go
func Test_findSpecFiles(t *testing.T) {
    tests := []struct {
        name        string
        setup       func(t *testing.T) string // returns temp dir
        paths       []string
        mode        ComposeMode
        wantFiles   int
        wantSkipped int
        wantErr     bool
    }{
        {
            name: "auto mode finds pub.d2 files",
            // setup creates pkg1/.arch/pub.d2 and pkg2/.arch/pub.d2
        },
        {
            name: "spec mode finds spec files",
            // setup creates pkg1/.arch/pub-spec.d2
        },
        {
            name: "missing arch folder is skipped",
            // setup creates pkg without .arch/
        },
        {
            name: "glob pattern expands correctly",
            // setup creates pkg/a/.arch, pkg/b/.arch
        },
    }
    // ... implementation
}
```

#### Compose Service Tests

```go
func TestService_Compose(t *testing.T) {
    tests := []struct {
        name        string
        setup       func(t *testing.T) string
        opts        ComposeOptions
        wantCount   int
        wantSkipped int
        wantErr     string
    }{
        {
            name: "composes multiple packages",
        },
        {
            name: "error when no output path",
            opts: ComposeOptions{Paths: []string{"pkg"}},
            wantErr: "output path is required",
        },
        {
            name: "error when no paths",
            opts: ComposeOptions{OutputPath: "out.d2"},
            wantErr: "at least one package path",
        },
        {
            name: "error when no spec files found",
        },
    }
    // ... implementation
}
```

**Acceptance Criteria:**
- [ ] File discovery tests cover both modes (Auto, Spec)
- [ ] File discovery tests cover glob expansion
- [ ] File discovery tests cover missing `.arch/` handling
- [ ] Compose tests cover happy path
- [ ] Compose tests cover validation errors
- [ ] Compose tests cover no-files-found error
- [ ] Tests use temp directories for isolation

---

### Task 7: Write Integration Test

**File:** `tests/integration/compose_test.go` (new)

End-to-end test using real file system:

```go
func TestComposeIntegration(t *testing.T) {
    // 1. Create test fixture with multiple packages
    tmpDir := t.TempDir()

    // pkg/a/.arch/pub-spec.d2
    createSpecFile(t, tmpDir, "pkg/a", "pub-spec.d2", `
pkg.a: {
    label: "pkg/a"
    Service: {
        shape: class
        stereotype: "<<interface>>"
    }
}`)

    // pkg/b/.arch/pub-spec.d2
    createSpecFile(t, tmpDir, "pkg/b", "pub-spec.d2", `
pkg.b: {
    label: "pkg/b"
    Repository: {
        shape: class
        stereotype: "<<interface>>"
    }
}`)

    // 2. Run compose
    outputPath := filepath.Join(tmpDir, "combined.d2")
    // ... execute compose via CLI or service

    // 3. Verify output
    content, err := os.ReadFile(outputPath)
    require.NoError(t, err)

    // Should contain both packages
    assert.Contains(t, string(content), "pkg.a")
    assert.Contains(t, string(content), "pkg.b")
    assert.Contains(t, string(content), "Service")
    assert.Contains(t, string(content), "Repository")
}
```

**Acceptance Criteria:**
- [ ] Creates realistic test fixture with multiple packages
- [ ] Tests full compose flow end-to-end
- [ ] Verifies output contains all expected packages
- [ ] Tests glob pattern expansion with real directories
- [ ] Tests mixed spec/code file scenarios

---

## Implementation Order

1. **Task 1:** Add types (ComposeOptions, ComposeResult, ComposeMode) - foundation
2. **Task 5:** Implement glob pattern expansion - needed by Task 2
3. **Task 2:** Implement file discovery helpers - internal logic
4. **Task 3:** Implement Service.Compose - orchestration
5. **Task 4:** Add CLI command - user interface
6. **Task 6:** Write unit tests - verify components
7. **Task 7:** Write integration test - verify end-to-end

## Testing Strategy

### Unit Tests
- SpecFinder with mocked filesystem
- Service.Compose with mocked reader/writer
- CLI flag parsing

### Integration Tests
- Full compose flow with temp directories
- Glob pattern expansion with real filesystem
- Mixed spec/code file scenarios

### Manual Testing
```bash
# Test with actual packages
archai diagram compose ./pkg/... --output=combined.d2

# Test spec-only mode
archai diagram compose ./pkg/... --spec --output=specs.d2

# Test code-only mode
archai diagram compose ./pkg/... --code --output=code.d2

# Test missing .arch/ warning
archai diagram compose ./nonexistent --output=out.d2
```

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| D2 reader doesn't handle all spec formats | Low | Medium | Reader already tested with split output |
| Glob expansion edge cases | Medium | Low | Comprehensive unit tests for patterns |
| Performance with many packages | Low | Low | Stream processing if needed |

## Dependencies

- **Existing Components (no changes needed):**
  - `d2.Reader` - parses D2 files into PackageModel
  - `d2.Writer.WriteCombined()` - writes combined diagram
  - `combinedBuilder` - renders multi-package D2

- **External Dependencies:**
  - `oss.terrastruct.com/d2/d2parser` (already used by d2.Reader)

## Acceptance Criteria Summary

1. [ ] `archai diagram compose pkg/a pkg/b --output=out.d2` creates combined diagram
2. [ ] Default mode uses `pub.d2` files (code-generated)
3. [ ] `--spec` flag uses `*-spec.d2` files (target specifications)
4. [ ] Glob patterns (`./pkg/...`) expand correctly
5. [ ] Missing `.arch/` folders are warned but don't fail operation
6. [ ] Output shows package count and skipped paths
7. [ ] `--output` flag is required
8. [ ] Error when no diagram files found in any package
