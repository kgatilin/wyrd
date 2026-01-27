# US-3: Split Combined Diagram into Per-Package Specs

## Overview

Implement the `diagram split` command that takes a combined D2 diagram and splits it into per-package specification files.

**Input:** Combined D2 diagram file (e.g., `docs/master-plan.d2`)
**Output:** Individual `.arch/pub-spec.d2` files in each package directory

## User Story Requirements

From `docs/features/init/user-stories.md`:

- Input: Combined D2 diagram file
- Output: Individual `.arch/pub-spec.d2` files
- Files use `-spec.d2` suffix (target specification vs actual state)
- Create `.arch/` folder if missing
- Overwrite existing `-spec.d2` files
- Support `--dry-run` flag

**CLI:**
```bash
archai diagram split docs/master-plan.d2
archai diagram split docs/master-plan.d2 --dry-run
```

## Architecture

Following existing patterns in the codebase:

```
CLI (main.go)
  └── runSplit() - wires d2.Reader + d2.Writer, calls service.Split()

Service (internal/service/)
  └── Split(ctx, SplitOptions) → SplitResult
      1. d2Reader.Read(diagramPath) → []PackageModel
      2. For each package: d2Writer.Write(..., pub-spec.d2)

Adapter (internal/adapter/d2/)
  └── reader.go - Parse D2 → []PackageModel (main work)
```

## Implementation Tasks

### Task 1: D2 Parser/Reader (`internal/adapter/d2/reader.go`)

Implement D2 parsing to reconstruct `[]domain.PackageModel` from combined diagram.

**Approach:** Use `oss.terrastruct.com/d2` library for proper AST parsing

**Add dependency:**
```bash
go get oss.terrastruct.com/d2
```

**Key d2-go types:**
- `d2parser.Parse()` → `*d2ast.Map` (root AST)
- `d2ast.Map` contains `Nodes []MapNodeBox` (children)
- `d2ast.Key` represents identifiers and edges
- `d2ast.Value` for scalar values (strings, etc.)

**Parsing with d2-go:**
```go
import (
    "oss.terrastruct.com/d2/d2parser"
    "oss.terrastruct.com/d2/d2ast"
)

func (r *reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
    content, err := os.ReadFile(paths[0])
    if err != nil {
        return nil, err
    }

    // Parse D2 to AST
    ast, err := d2parser.Parse(paths[0], bytes.NewReader(content), nil)
    if err != nil {
        return nil, fmt.Errorf("d2 parse error: %w", err)
    }

    // Walk AST to extract packages
    return r.extractPackages(ast)
}

func (r *reader) extractPackages(ast *d2ast.Map) ([]domain.PackageModel, error) {
    var packages []domain.PackageModel

    for _, node := range ast.Nodes {
        // Skip legend and comments
        if isLegendOrComment(node) {
            continue
        }

        // Each top-level container is a package
        if mapNode, ok := node.(*d2ast.Map); ok {
            pkg := r.parsePackageContainer(mapNode)
            packages = append(packages, pkg)
        }
    }

    return packages, nil
}
```

**D2 AST structure for our output:**
```
Map (root)
├── Key "legend" → Map (skip)
├── Key "internal.service" → Map
│   ├── Key "label" → Value "internal/service"
│   ├── Key "style.fill" → Value "#..."
│   ├── Key "Service" → Map (symbol)
│   │   ├── Key "shape" → Value "class"
│   │   ├── Key "stereotype" → Value "<<struct>>"
│   │   └── Key "+Method()" → Value "return"
│   └── Edge "Service -> Options" (intra-package dep)
└── Edge "internal.service.Service -> internal.domain.Model" (cross-package dep)
```

**Validation (part of Read):**
- Validate diagram structure before extracting packages
- Return clear errors: `ErrNoPackages`, `ErrInvalidLabel`, `ErrEmptyDiagram`
- Skip known non-package containers (e.g., `legend`)

**Files:**
- `internal/adapter/d2/reader.go` - Main implementation using d2-go
- `internal/adapter/d2/reader_test.go` - Tests including validation edge cases

### Task 2: Service Split Operation (`internal/service/split.go`)

**New types:**
```go
type SplitOptions struct {
    DiagramPath string
    DryRun      bool
}

type SplitResult struct {
    Files    []SplitFileResult
    DryRun   bool
}

type SplitFileResult struct {
    PackagePath string
    FilePath    string
    Error       error
}
```

**Service method:**
```go
func (s *Service) Split(ctx context.Context, opts SplitOptions) (*SplitResult, error) {
    // 1. Read diagram file
    models, err := s.d2Reader.Read(ctx, []string{opts.DiagramPath})

    // 2. For each package, write spec file
    for _, pkg := range models {
        // Create package directory if it doesn't exist (supports "plan first" workflow)
        pkgDir := pkg.Path  // e.g., "pkg/newfeature"
        archDir := filepath.Join(pkgDir, ".arch")

        if !opts.DryRun {
            os.MkdirAll(archDir, 0755)  // Creates both pkg/newfeature/ and pkg/newfeature/.arch/
        }

        outputPath := filepath.Join(archDir, "pub-spec.d2")
        if !opts.DryRun {
            s.d2Writer.Write(ctx, pkg, WriteOptions{OutputPath: outputPath, PublicOnly: true})
        }
        // Collect result
    }

    return result, nil
}
```

**Directory creation behavior:**
- Creates package directory if it doesn't exist (e.g., `pkg/newfeature/`)
- Creates `.arch/` subdirectory
- Supports "plan first, implement later" workflow
- Dry-run shows what *would* be created without creating directories

**Files:**
- `internal/service/split.go` - Split operation
- `internal/service/split_test.go` - Unit tests
- `internal/service/service.go` - Add d2Reader field

### Task 3: Update Service Constructor

Add `d2Reader` to Service struct:

```go
// service.go
type Service struct {
    goReader ModelReader
    d2Reader ModelReader  // NEW
    d2Writer ModelWriter
}

// factory.go - update constructor
func NewService(goReader, d2Reader ModelReader, d2Writer ModelWriter) *Service
```

**Files:**
- `internal/service/service.go`
- `internal/service/factory.go`

### Task 4: CLI Split Command (`cmd/archai/main.go`)

Add `split` subcommand under `diagram`:

```go
splitCmd := &cobra.Command{
    Use:   "split <diagram-file>",
    Short: "Split combined diagram into per-package specs",
    Args:  cobra.ExactArgs(1),
    RunE:  runSplit,
}
splitCmd.Flags().Bool("dry-run", false, "Show what would be created without writing files or directories")
diagramCmd.AddCommand(splitCmd)

func runSplit(cmd *cobra.Command, args []string) error {
    d2Reader := d2.NewReader()
    d2Writer := d2.NewWriter()
    goReader := golang.NewReader()
    svc := service.NewService(goReader, d2Reader, d2Writer)

    opts := service.SplitOptions{
        DiagramPath: args[0],
        DryRun:      dryRun,
    }

    result, err := svc.Split(ctx, opts)
    // Print results
}
```

**Files:**
- `cmd/archai/main.go`

## Critical Files

| File | Action | Purpose |
|------|--------|---------|
| `internal/adapter/d2/reader.go` | Implement | D2 parsing → PackageModel |
| `internal/service/split.go` | Create | Split operation |
| `internal/service/service.go` | Modify | Add d2Reader field |
| `internal/service/factory.go` | Modify | Update constructor |
| `cmd/archai/main.go` | Modify | Add split command |

## D2 Parsing Strategy

Using `oss.terrastruct.com/d2` library for proper AST-based parsing.

**Why d2-go over regex:**
- Handles edge cases (escaping, nested structures)
- Robust against D2 syntax variations
- Maintainable and future-proof
- Provides proper error messages with line numbers

**Key d2-go concepts:**
```go
// Parse D2 content to AST
ast, _ := d2parser.Parse(filename, reader, nil)

// AST is a tree of nodes
// - d2ast.Map: container with children (packages, symbols)
// - d2ast.Key: identifier (package name, symbol name, property)
// - d2ast.Value: scalar value (strings)
// - d2ast.Edge: connections (dependencies)

// Walking the AST
for _, node := range ast.Nodes {
    switch n := node.MapNodeBox.(type) {
    case *d2ast.Key:
        // Key can have a Value (scalar) or a Map (container)
    case *d2ast.Edge:
        // Edge represents dependency arrows
    }
}
```

**AST mapping to our structure:**
| D2 AST | Domain Model |
|--------|--------------|
| Top-level Map with `label:` | PackageModel (path from label) |
| Nested Map with `shape: class` | Interface/Struct/Function |
| `stereotype: "<<X>>"` | Stereotype enum |
| Key-value pairs in symbol | Fields/Methods |
| Edge within package | Intra-package Dependency |
| Edge at root level | Cross-package Dependency |

## Output Format

Split produces `pub-spec.d2` files that look like per-package diagrams (similar to `pub.d2` from `diagram generate`), but:

1. No file-level grouping (combined diagrams don't have file info)
2. Uses `-spec.d2` suffix to indicate specification (target state)
3. Preserves all symbol metadata from the combined diagram

## Verification

After implementation:

1. **Generate test combined diagram:**
   ```bash
   archai diagram generate ./internal/... -o /tmp/test-combined.d2
   ```

2. **Run split with dry-run:**
   ```bash
   archai diagram split /tmp/test-combined.d2 --dry-run
   ```
   Expected output:
   ```
   Would create: internal/adapter/d2/.arch/pub-spec.d2
   Would create: internal/adapter/golang/.arch/pub-spec.d2
   Would create: internal/domain/.arch/pub-spec.d2
   Would create: internal/service/.arch/pub-spec.d2

   4 spec file(s) would be created
   ```

3. **Run split:**
   ```bash
   archai diagram split /tmp/test-combined.d2
   ```
   Expected output:
   ```
   Created: internal/adapter/d2/.arch/pub-spec.d2
   Created: internal/adapter/golang/.arch/pub-spec.d2
   Created: internal/domain/.arch/pub-spec.d2
   Created: internal/service/.arch/pub-spec.d2

   Split complete: 4 spec file(s) created
   ```

4. **Verify output:**
   ```bash
   cat internal/service/.arch/pub-spec.d2
   ```

5. **Test with non-existing package:**
   Create a diagram referencing `pkg/newfeature`, run split, verify directory is created.

6. **Run tests:**
   ```bash
   go test ./internal/adapter/d2/...
   go test ./internal/service/...
   ```

## Input Validation

Before processing, validate the diagram has the expected structure for splitting:

**Required structure:**
1. At least one package container (top-level map with `label:` containing a path)
2. Package containers must have valid Go package paths in `label:` (e.g., `"internal/service"`)
3. At least one symbol (class shape) in at least one package

**Validation errors:**
```go
var (
    ErrNoPackages     = errors.New("no package containers found in diagram")
    ErrInvalidLabel   = errors.New("package container missing valid label")
    ErrEmptyDiagram   = errors.New("diagram contains no symbols")
)
```

**Detection heuristics:**
- Package container: top-level key with nested map containing `label:` that looks like a path (contains `/` or is a simple identifier)
- Skip `legend` container (known non-package)
- Symbol: nested map with `shape: class`

**Example validation:**
```go
func (r *reader) validate(ast *d2ast.Map) error {
    packages := r.findPackageContainers(ast)
    if len(packages) == 0 {
        return ErrNoPackages
    }

    totalSymbols := 0
    for _, pkg := range packages {
        if pkg.Label == "" || pkg.Label == "legend" {
            continue
        }
        totalSymbols += len(pkg.Symbols)
    }

    if totalSymbols == 0 {
        return ErrEmptyDiagram
    }

    return nil
}
```

## Output Format Note

Split produces **flat** `pub-spec.d2` files (no file-level grouping) because:
- Combined diagrams don't contain source file information
- Specs represent architectural intent ("what"), not implementation details ("where")
- The `-spec.d2` suffix distinguishes from code-generated `pub.d2` (which has file grouping)

This is intentional - when comparing spec vs code with `diagram diff`, symbols are compared by name/signature, not file location.

## Notes

- Combined diagrams only contain public API (exported symbols), so split produces only `pub-spec.d2` (no `internal-spec.d2`)
- Using d2-go library provides robust, maintainable parsing with proper error handling
- Package paths are recovered from the `label:` field, not the container ID
- New dependency: `oss.terrastruct.com/d2` (official D2 library from Terrastruct)
