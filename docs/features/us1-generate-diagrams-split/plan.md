# Implementation Plan: US-1 Generate Diagrams - Split Mode

## Overview

This plan covers the implementation of **US-1: Generate Diagrams - Split Mode**, which generates D2 architecture diagrams from Go source code and saves them to `.arch/` folders within each package.

---

## Research Summary

### Go Code Parsing

**Recommended Approach:** `golang.org/x/tools/go/packages` + `go/types`

**Rationale:**
- Handles multi-package analysis automatically with proper dependency resolution
- Provides full type information (interfaces, structs, methods, signatures)
- Respects `go.mod` and handles imports correctly
- Single API for loading packages with configurable modes

**Key Capabilities:**
- Extract packages, interfaces, structs, functions, and type definitions
- Distinguish public (exported) vs unexported symbols via `IsExported()`
- Get method signatures including parameter and return types
- Traverse AST for docstrings and comments

### D2 Generation

**Approach:** Direct D2 text generation (NOT d2oracle API)

**Rationale:**
- D2 syntax is simple and human-readable - designed to be written directly
- `d2oracle` API is verbose and awkward for class fields/methods
- Direct text generation gives full control over output format
- Easier to debug (just inspect the generated string)
- No library quirks with special characters in method signatures

**D2 Library Usage:**
- `oss.terrastruct.com/d2` only needed for future features (parsing existing diagrams, validation)
- For US-1, we generate D2 text strings directly using a `D2Writer` component

**Key Output Elements:**
- Legend block (top-right corner with DDD color explanations)
- File containers (group symbols by source file)
- Color coding by file role (service, model, config, etc.)
- Dependencies section (explicit connections at bottom)

---

## Project Structure

```
archai/
├── cmd/
│   └── archai/
│       └── main.go              # CLI entry point
│
├── internal/
│   ├── domain/                  # Core domain model (CONCRETE STRUCTS)
│   │   ├── module.go            # Module (root context)
│   │   ├── package.go           # PackageModel aggregate
│   │   ├── interface.go         # InterfaceDef value object
│   │   ├── struct.go            # StructDef value object
│   │   ├── function.go          # FunctionDef value object
│   │   ├── method.go            # MethodDef, ParamDef, TypeRef
│   │   ├── field.go             # FieldDef value object
│   │   ├── dependency.go        # Dependency, SymbolRef
│   │   └── stereotype.go        # Stereotype enum
│   │
│   ├── adapter/                 # Ports & Adapters (read/write model)
│   │   ├── golang/              # Go code adapter
│   │   │   ├── reader.go        # Go code → domain.PackageModel
│   │   │   ├── stereotype.go    # Stereotype detection logic
│   │   │   └── writer.go        # domain.PackageModel → Go code (future)
│   │   │
│   │   └── d2/                  # D2 diagram adapter
│   │       ├── reader.go        # D2 file → domain.PackageModel (US-3+)
│   │       ├── writer.go        # domain.PackageModel → D2 file
│   │       ├── builder.go       # D2 text builder (strings.Builder)
│   │       ├── templates.go     # Legend, reusable D2 fragments
│   │       └── styles.go        # Colors, stereotypes, file roles
│   │
│   └── service/                 # Business operations
│       ├── service.go           # Service struct definition
│       ├── factory.go           # NewService() - wires adapters
│       ├── options.go           # Shared interfaces, options, results
│       ├── generate.go          # Generate() operation (US-1, US-2)
│       ├── split.go             # Split() operation (US-3)
│       ├── compose.go           # Compose() operation (US-4)
│       └── diff.go              # Diff() operation (US-5, US-6, US-7)
│
├── go.mod
└── go.sum
```

**Design Principles:**

1. **Hexagonal Architecture (Ports & Adapters)**
   - Domain model at the center
   - Adapters handle read/write for different formats (Go code, D2 diagrams)
   - Same interfaces for all adapters → easy to swap or add new formats

2. **Unified Adapter Interfaces**
   ```go
   type ModelReader interface {
       Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
   }

   type ModelWriter interface {
       Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error
   }
   ```
   - `golang.Reader` implements `ModelReader` (reads Go source code)
   - `d2.Reader` implements `ModelReader` (reads D2 files)
   - `d2.Writer` implements `ModelWriter` (writes D2 files)
   - `golang.Writer` implements `ModelWriter` (future: generate Go code)

3. **Service Organization**
   - Factory handles dependency injection
   - One file per operation for clear separation
   - Operations compose adapters (e.g., Generate = golang.Reader → d2.Writer)

4. **Domain Models are Concrete Structs**
   - Data containers, not behavioral contracts
   - Shared across all adapters and operations

---

## Domain Model (DDD)

All domain models are **concrete structs** in `internal/domain/`. They are data containers, not behavioral contracts. Paths are relative to the module root.

### Aggregates and Value Objects

```go
// package.go - Aggregate Root
type PackageModel struct {
    Path         string          // Relative to module: "internal/service"
    Name         string          // Package name: "service"
    Interfaces   []InterfaceDef
    Structs      []StructDef
    Functions    []FunctionDef
    TypeDefs     []TypeDef
    Dependencies []Dependency
}

// stereotype.go - Stereotype enum for DDD classification
type Stereotype string

const (
    StereotypeNone       Stereotype = ""           // No specific stereotype
    StereotypeInterface  Stereotype = "interface"  // Generic interface
    StereotypeService    Stereotype = "service"    // Service interface
    StereotypeRepository Stereotype = "repository" // Repository interface
    StereotypePort       Stereotype = "port"       // Port (hexagonal)
    StereotypeFactory    Stereotype = "factory"    // Factory function
    StereotypeAggregate  Stereotype = "aggregate"  // DDD aggregate root
    StereotypeEntity     Stereotype = "entity"     // DDD entity
    StereotypeValue      Stereotype = "value"      // Value object
    StereotypeEnum       Stereotype = "enum"       // Enum type
)

// interface.go - Value Object
type InterfaceDef struct {
    Name       string
    Methods    []MethodDef
    IsExported bool
    SourceFile string      // e.g., "service.go"
    Doc        string      // Documentation comment
    Stereotype Stereotype  // Detected or explicit via annotation
}

// struct.go - Value Object
type StructDef struct {
    Name       string
    Fields     []FieldDef
    Methods    []MethodDef  // Methods with this struct as receiver
    IsExported bool
    SourceFile string
    Doc        string
    Stereotype Stereotype  // Detected or explicit via annotation
}

// function.go - Value Object
type FunctionDef struct {
    Name       string
    Params     []ParamDef
    Returns    []TypeRef
    IsExported bool
    SourceFile string
    Doc        string
    Stereotype Stereotype  // Detected or explicit via annotation
}

// method.go - Value Objects
type MethodDef struct {
    Name    string
    Params  []ParamDef
    Returns []TypeRef
}

type ParamDef struct {
    Name string
    Type TypeRef
}

type TypeRef struct {
    Name      string  // e.g., "PackageModel", "string", "error"
    Package   string  // e.g., "context", "" for local/builtin
    IsPointer bool
    IsSlice   bool
    IsMap     bool
}

// field.go - Value Object
type FieldDef struct {
    Name       string
    Type       TypeRef
    IsExported bool
    Tag        string  // Struct tag
}
```

### Module (Root Context)

The `Module` provides context for relative paths:

```go
// module.go
type Module struct {
    Path     string          // From go.mod: "github.com/kgatilin/wyrd"
    Packages []PackageModel
}
```

### Dependencies (Symbol References)

Dependencies are structured objects (not strings), keeping the domain model format-agnostic:

```go
// dependency.go

// SymbolRef is a fully qualified reference to a symbol
type SymbolRef struct {
    Package  string  // Relative to module: "internal/service"
    File     string  // Filename: "service.go"
    Symbol   string  // Type/func name: "Service"
    External bool    // true if outside module (e.g., "context")
}

// Dependency tracks a reference from one symbol to another
type Dependency struct {
    From SymbolRef
    To   SymbolRef
    Kind DependencyKind
}

type DependencyKind string

const (
    DependencyUses       DependencyKind = "uses"       // Parameter type
    DependencyReturns    DependencyKind = "returns"    // Return type
    DependencyImplements DependencyKind = "implements" // Interface impl
)
```

**Dependency Collection During Parsing:**

When parsing:
```go
// File: internal/service/deps.go
type Comparator interface {
    Compare(spec PackageModel, code PackageModel) ComparisonResult
}
```

The Go adapter records:
```go
Dependency{
    From: SymbolRef{Package: "internal/service", File: "deps.go", Symbol: "Comparator"},
    To:   SymbolRef{Package: "internal/domain", File: "model.go", Symbol: "PackageModel"},
    Kind: DependencyUses,
}
```

**D2 Adapter Responsibility:**

The D2 adapter converts `SymbolRef` to D2 container paths:
```go
// adapter/d2/writer.go
func (w *Writer) toD2Path(ref domain.SymbolRef) string {
    if ref.External {
        return ref.Package + "." + ref.Symbol  // context.Context
    }
    fileID := strings.TrimSuffix(ref.File, ".go")
    return fileID + "." + ref.Symbol  // deps.Comparator
}
```

This keeps the domain model clean and puts D2-specific logic in the adapter.

### Stereotype Detection

Stereotypes determine the color and label in D2 diagrams. Detection uses **heuristics + explicit override**.

#### Priority
1. **Explicit annotation** in doc comment → use it
2. **No annotation** → apply heuristics
3. **Heuristic fails** → default stereotype

#### Annotation Format

Use `archspec:<stereotype>` in the doc comment:

```go
// DiagramService handles diagram generation operations.
// archspec:service
type DiagramService interface {
    Generate(ctx context.Context, opts GenerateOptions) error
}

// NewService creates a new diagram service.
// archspec:factory
func NewService(reader ModelReader, writer ModelWriter) *Service { ... }

// GenerateOptions configures the generate operation.
// archspec:value
type GenerateOptions struct {
    Paths []string
}

// PackageModel represents a Go package's structure.
// archspec:aggregate
type PackageModel struct { ... }
```

#### Heuristics

**Factory (functions):**
```go
func detectFactoryStereotype(fn FunctionDef) Stereotype {
    // Explicit annotation takes priority
    if s := parseAnnotation(fn.Doc); s != "" {
        return s
    }
    // Heuristic: New* prefix, returns type, no receiver
    if strings.HasPrefix(fn.Name, "New") && len(fn.Returns) > 0 {
        return StereotypeFactory
    }
    return StereotypeNone
}
```

**Service (interfaces):**
```go
func detectInterfaceStereotype(iface InterfaceDef) Stereotype {
    if s := parseAnnotation(iface.Doc); s != "" {
        return s
    }
    // Heuristic: name suffix
    servicePatterns := []string{"Service", "Handler", "Manager", "Controller"}
    for _, p := range servicePatterns {
        if strings.HasSuffix(iface.Name, p) {
            return StereotypeService
        }
    }
    // Heuristic: Repository suffix
    if strings.HasSuffix(iface.Name, "Repository") ||
       strings.HasSuffix(iface.Name, "Store") {
        return StereotypeRepository
    }
    // Heuristic: Reader/Writer suffix (ports)
    if strings.HasSuffix(iface.Name, "Reader") ||
       strings.HasSuffix(iface.Name, "Writer") {
        return StereotypePort
    }
    return StereotypeInterface
}
```

**Value Object (structs):**
```go
func detectStructStereotype(s StructDef, pkgPath string) Stereotype {
    if st := parseAnnotation(s.Doc); st != "" {
        return st
    }
    // Heuristic: Options/Config/Result/Request/Response patterns
    valuePatterns := []string{"Options", "Config", "Result", "Request",
                              "Response", "Params", "Ref", "Info"}
    for _, p := range valuePatterns {
        if strings.HasSuffix(s.Name, p) {
            return StereotypeValue
        }
    }
    // Heuristic: located in domain/model/entity path → aggregate/entity
    if strings.Contains(pkgPath, "/domain/") ||
       strings.Contains(pkgPath, "/model/") ||
       strings.Contains(pkgPath, "/entity/") {
        if len(s.Methods) > 0 {
            return StereotypeAggregate
        }
        return StereotypeEntity
    }
    // Heuristic: no methods → value object
    if len(s.Methods) == 0 {
        return StereotypeValue
    }
    return StereotypeNone
}
```

**Enum (type aliases):**
```go
func detectTypeDefStereotype(td TypeDef) Stereotype {
    if s := parseAnnotation(td.Doc); s != "" {
        return s
    }
    // Heuristic: has constants defined
    if len(td.Constants) > 0 {
        return StereotypeEnum
    }
    return StereotypeNone
}
```

#### Annotation Parser

```go
func parseAnnotation(doc string) Stereotype {
    re := regexp.MustCompile(`archspec:(\w+)`)
    if m := re.FindStringSubmatch(doc); m != nil {
        switch m[1] {
        case "service":    return StereotypeService
        case "repository": return StereotypeRepository
        case "port":       return StereotypePort
        case "factory":    return StereotypeFactory
        case "aggregate":  return StereotypeAggregate
        case "entity":     return StereotypeEntity
        case "value":      return StereotypeValue
        case "enum":       return StereotypeEnum
        }
    }
    return StereotypeNone
}
```

#### Stereotype → D2 Color Mapping

| Stereotype | D2 Label | Color | Usage |
|------------|----------|-------|-------|
| `service` | `<<service>>` | Purple `#f0e8fc` | Service interfaces |
| `repository` | `<<repository>>` | Purple `#f0e8fc` | Repository interfaces |
| `port` | `<<port>>` | Purple `#f0e8fc` | Adapter ports (Reader/Writer) |
| `factory` | `<<factory>>` | Green `#e8fce8` | Factory functions |
| `aggregate` | `<<aggregate>>` | Blue `#e8f4fc` | DDD aggregates |
| `entity` | `<<entity>>` | Blue `#e8f4fc` | DDD entities |
| `value` | `<<value>>` | Gray `#f8f8f8` | Value objects, options |
| `enum` | `<<enum>>` | Gray `#f8f8f8` | Enum types |
| `interface` | `<<interface>>` | Purple `#f0e8fc` | Generic interfaces |
| (none) | (none) | Gray `#f8f8f8` | Unclassified |

---

## Interface Definitions (Ports & Adapters)

### Core Adapter Interfaces

Both Go and D2 adapters implement the same interfaces, enabling uniform handling:

```go
// ModelReader reads package models from a source (code or diagrams)
type ModelReader interface {
    Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
}

// ModelWriter writes package models to a destination
type ModelWriter interface {
    Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error
}

// WriteOptions configures how models are written
type WriteOptions struct {
    OutputPath   string  // Where to write (file path or directory)
    PublicOnly   bool    // Only include exported symbols
    ToStdout     bool    // Write to stdout instead of file
}
```

### Go Adapter (`internal/adapter/golang/`)

```go
// reader.go
type Reader struct{}

func NewReader() *Reader { return &Reader{} }

// Read parses Go source code and returns package models
func (r *Reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
    // Uses golang.org/x/tools/go/packages
}

// writer.go (future - generate Go code from model)
type Writer struct{}

func NewWriter() *Writer { return &Writer{} }

func (w *Writer) Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error {
    // Future: generate Go code scaffolding from diagram spec
    return errors.New("not implemented")
}
```

### D2 Adapter (`internal/adapter/d2/`)

```go
// reader.go (US-3+)
type Reader struct{}

func NewReader() *Reader { return &Reader{} }

// Read parses D2 diagram files and returns package models
func (r *Reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
    // Uses oss.terrastruct.com/d2 for parsing
}

// writer.go
type Writer struct{}

func NewWriter() *Writer { return &Writer{} }

// Write generates D2 diagram from package model
func (w *Writer) Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error {
    // Uses D2Writer for text generation
}
```

### Service (`internal/service/`)

```go
// service.go - Service struct definition
type Service struct {
    goReader  ModelReader  // golang.Reader
    d2Reader  ModelReader  // d2.Reader (US-3+)
    d2Writer  ModelWriter  // d2.Writer
}

// factory.go - Dependency injection
func NewService() *Service {
    return &Service{
        goReader: golang.NewReader(),
        d2Writer: d2.NewWriter(),
        // d2Reader added in US-3
    }
}

// generate.go - Generate operation
type GenerateOptions struct {
    Paths        []string  // Package paths (e.g., "./internal/...")
    OutputFile   string    // If set, combined mode; if empty, split mode
    PublicOnly   bool      // Generate only pub.d2 (--pub flag)
    InternalOnly bool      // Generate only internal.d2 (--internal flag)
}

type GenerateResult struct {
    PackagePath  string
    PubFile      string  // Path to generated pub.d2 (if created)
    InternalFile string  // Path to generated internal.d2 (if created)
    Error        error   // Per-package error (nil if success)
}

func (s *Service) Generate(ctx context.Context, opts GenerateOptions) ([]GenerateResult, error) {
    // 1. Read from Go code
    models, err := s.goReader.Read(ctx, opts.Paths)
    // 2. Write to D2 for each model
    for _, model := range models {
        s.d2Writer.Write(ctx, model, WriteOptions{...})
    }
}
```

---

## Component Interactions

```
┌──────────────┐
│     CLI      │
│  (cmd/wyrd)│
└──────┬───────┘
       │ creates via factory
       ▼
┌───────────────────────────────────────────────────────────────┐
│                    internal/service/                           │
│                                                                │
│  Service (factory.go)                                          │
│    - goReader:  ModelReader  (golang.Reader)                   │
│    - d2Reader:  ModelReader  (d2.Reader) [US-3+]               │
│    - d2Writer:  ModelWriter  (d2.Writer)                       │
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │ Generate (generate.go)                                   │  │
│  │   goReader.Read(paths) ──► []PackageModel               │  │
│  │   d2Writer.Write(model) ──► .arch/pub.d2, internal.d2   │  │
│  └─────────────────────────────────────────────────────────┘  │
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │ Split (split.go) [US-3]                                  │  │
│  │   d2Reader.Read(file) ──► PackageModel                  │  │
│  │   d2Writer.Write(model) ──► per-package specs           │  │
│  └─────────────────────────────────────────────────────────┘  │
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │ Diff (diff.go) [US-5/6]                                  │  │
│  │   goReader.Read() vs d2Reader.Read() ──► CompareResult  │  │
│  └─────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────┘
       │                              │
       │ uses                         │ uses
       ▼                              ▼
┌─────────────────────┐     ┌─────────────────────┐
│  adapter/golang/    │     │    adapter/d2/      │
│                     │     │                     │
│  Reader (read)      │     │  Reader (read)      │
│  Writer (future)    │     │  Writer (write)     │
└─────────────────────┘     └─────────────────────┘
       │                              │
       ▼                              ▼
   Go source code              D2 diagram files
```

**Data Flow for US-1 (Generate):**
```
Go Code ──► golang.Reader.Read() ──► []PackageModel ──► d2.Writer.Write() ──► .arch/*.d2
```

---

## Implementation Tasks

### Iteration 1: Project Foundation

#### Task 1.1: Initialize Project Structure
- Create directory structure as outlined above
- Set up `go.mod` with dependencies
- Add initial `.gitignore`

**Dependencies to add:**
```
golang.org/x/tools v0.28.0    // Go code parsing
github.com/spf13/cobra v1.8.1 // CLI framework
```

#### Task 1.2: Define Domain Models
- Create `internal/domain/` package
- Implement all structs: `PackageModel`, `InterfaceDef`, `StructDef`, `FunctionDef`, `MethodDef`, `ParamDef`, `TypeRef`, `FieldDef`, `Dependency`
- Add helper methods (e.g., `TypeRef.String()`, `MethodDef.Signature()`)

### Iteration 2: Go Adapter (Reader)

#### Task 2.1: Create Adapter Package Structure
- Create `internal/adapter/golang/` package
- Define `ModelReader` interface in a shared location or inline

#### Task 2.2: Implement Go Reader
- Create `internal/adapter/golang/reader.go`
- Use `golang.org/x/tools/go/packages` for loading
- Configure package loading mode:
  ```go
  cfg := &packages.Config{
      Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
            packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
  }
  ```

#### Task 2.3: Implement Symbol Extraction
- Extract interfaces with methods
- Extract structs with fields and methods
- Extract functions (identify factories by naming convention `New*` + return type)
- Handle exported vs unexported filtering
- Capture source file for each symbol

#### Task 2.4: Implement Dependency Collection
- Track type references in method parameters → `DependencyUses`
- Track type references in return types → `DependencyReturns`
- Build dependency list for each package (intra-package references)

#### Task 2.5: Implement Stereotype Detection
- Create `internal/adapter/golang/stereotype.go`
- Parse doc comments for `archspec:<stereotype>` annotations
- Implement heuristics for automatic detection:
  - Factory: `New*` prefix, returns type, no receiver
  - Service: `*Service`, `*Handler`, `*Manager` suffix
  - Repository: `*Repository`, `*Store` suffix
  - Port: `*Reader`, `*Writer` suffix
  - Value: `*Options`, `*Config`, `*Result` suffix, or no methods
  - Aggregate/Entity: in `domain/`, `model/`, `entity/` paths
  - Enum: type with constants
- Priority: explicit annotation > heuristic > default

### Iteration 3: D2 Adapter (Writer)

#### Task 3.1: Create D2 Adapter Package Structure
- Create `internal/adapter/d2/` package
- Placeholder for `reader.go` (US-3+)

#### Task 3.2: Implement D2 Writer
- Create `internal/adapter/d2/writer.go`
- Implement `ModelWriter` interface
- Use internal `D2TextBuilder` for text generation

```go
// writer.go
type Writer struct{}

func NewWriter() *Writer { return &Writer{} }

func (w *Writer) Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error {
    builder := newD2TextBuilder()
    content := builder.Build(model, opts)

    if opts.ToStdout {
        fmt.Print(content)
        return nil
    }

    // Write to .arch/ directory
    archDir := filepath.Join(findPackageDir(model.Path), ".arch")
    if err := os.MkdirAll(archDir, 0755); err != nil {
        return err
    }

    filename := "internal.d2"
    if opts.PublicOnly {
        filename = "pub.d2"
    }

    return os.WriteFile(filepath.Join(archDir, filename), []byte(content), 0644)
}
```

#### Task 3.3: Implement D2 Text Builder
- Create `internal/adapter/d2/builder.go`
- Build D2 text directly using `strings.Builder`
- Handle indentation management
- Escape special characters in method signatures

```go
type d2TextBuilder struct {
    buf    strings.Builder
    indent int
}

func newD2TextBuilder() *d2TextBuilder {
    return &d2TextBuilder{}
}

func (b *d2TextBuilder) Build(pkg domain.PackageModel, opts WriteOptions) string {
    b.buf.Reset()
    b.indent = 0

    // 1. Write header comment
    b.writeComment(fmt.Sprintf("%s package", pkg.Name))
    b.writeLine("")

    // 2. Write legend
    b.writeLegend()
    b.writeLine("")

    // 3. Group symbols by file and write containers
    b.writeComment("Files")
    fileGroups := b.groupByFile(pkg, opts.PublicOnly)
    for _, fg := range fileGroups {
        b.writeFileContainer(fg)
        b.writeLine("")
    }

    // 4. Write dependencies
    b.writeComment("Dependencies")
    deps := b.filterDependencies(pkg.Dependencies, opts.PublicOnly)
    for _, dep := range deps {
        b.writeDependency(dep)
    }

    return b.buf.String()
}
```

#### Task 3.4: Implement Templates
- Create `internal/adapter/d2/templates.go`
- Define legend template (DDD color explanations)
- Legend positioned at top-right with `near: top-right`

```go
func (b *d2TextBuilder) writeLegend() {
    b.writeLine("legend: {")
    b.indent++
    b.writeLine(`label: "Color Legend (DDD)"`)
    b.writeLine(`style.stroke: "#999"`)
    b.writeLine(`style.fill: "#fafafa"`)
    b.writeLine(`near: top-right`)
    b.writeLine("")
    b.writeLegendItem("aggregate", "Domain Model", "#e8f4fc")
    b.writeLegendItem("service", "Service", "#f0e8fc")
    b.writeLegendItem("factory", "Factory", "#e8fce8")
    b.writeLegendItem("options", "Value Object", "#f8f8f8")
    b.indent--
    b.writeLine("}")
}
```

#### Task 3.5: Define Styles and Conventions
- Create `internal/adapter/d2/styles.go`
- Map stereotypes to D2 colors and labels
- File container color based on dominant stereotype of contents

```go
// Stereotype → D2 styling
func stereotypeColor(s domain.Stereotype) string {
    switch s {
    case domain.StereotypeService,
         domain.StereotypeRepository,
         domain.StereotypePort,
         domain.StereotypeInterface:
        return "#f0e8fc" // Purple
    case domain.StereotypeFactory:
        return "#e8fce8" // Green
    case domain.StereotypeAggregate,
         domain.StereotypeEntity:
        return "#e8f4fc" // Blue
    default:
        return "#f8f8f8" // Gray (value, enum, none)
    }
}

func stereotypeLabel(s domain.Stereotype) string {
    if s == domain.StereotypeNone {
        return ""
    }
    return fmt.Sprintf("<<%s>>", s)
}

// File container color: use dominant stereotype of its contents
func fileContainerColor(symbols []domain.Symbol) string {
    // Count stereotypes
    counts := make(map[domain.Stereotype]int)
    for _, sym := range symbols {
        counts[sym.Stereotype()]++
    }
    // Find dominant (or default to gray)
    var dominant domain.Stereotype
    maxCount := 0
    for s, c := range counts {
        if c > maxCount {
            dominant = s
            maxCount = c
        }
    }
    return stereotypeColor(dominant)
}
```

**D2 Output Structure (matching example format):**
```d2
# archspec package

# Legend
legend: {
  label: "Color Legend (DDD)"
  style.stroke: "#999"
  style.fill: "#fafafa"
  near: top-right

  aggregate: {
    label: "Domain Model"
    shape: class
    style.fill: "#e8f4fc"
    style.font-color: "#000"
  }
  service: {
    label: "Service"
    shape: class
    style.fill: "#f0e8fc"
    style.font-color: "#000"
  }
  factory: {
    label: "Factory"
    shape: class
    style.fill: "#e8fce8"
    style.font-color: "#000"
  }
  options: {
    label: "Value Object"
    shape: class
    style.fill: "#f8f8f8"
    style.font-color: "#000"
  }
}

# Files
model: {
  label: "model.go"
  style.fill: "#e8f4fc"

  PackageModel: {
    shape: class
    stereotype: "<<interface>>"

    "Name()": "string"
    "Path()": "string"
    "Interfaces()": "[]InterfaceDef"
    "Structs()": "[]StructDef"
    "Dependencies()": "[]Dependency"
  }

  InterfaceDef: {
    shape: class
    stereotype: "<<interface>>"

    "Name()": "string"
    "Methods()": "[]MethodDef"
    "IsExported()": "bool"
    "SourceFile()": "string"
  }
}

service: {
  label: "service.go"
  style.fill: "#f0e8fc"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "Generate(paths: []string, opts: GenerateOptions)": "([]GenerateResult, error)"
    "Validate(paths: []string, opts: ValidationOptions)": "(ValidationResult, error)"
  }
}

deps: {
  label: "deps.go"
  style.fill: "#f0e8fc"

  Generator: {
    shape: class
    stereotype: "<<interface>>"

    "Generate(paths: []string, opts: GenerateOptions)": "([]GenerateResult, error)"
  }

  Parser: {
    shape: class
    stereotype: "<<interface>>"

    "ParsePackages(ctx: context.Context, paths: []string)": "([]PackageModel, error)"
  }
}

# Dependencies
service.Service -> model.PackageModel: "returns"
service.Service -> config.GenerateOptions: "uses"
service.Service -> result.GenerateResult: "returns"
deps.Generator -> config.GenerateOptions: "uses"
deps.Generator -> result.GenerateResult: "returns"
deps.Parser -> model.PackageModel: "returns"
model.PackageModel -> model.InterfaceDef: "returns"
model.PackageModel -> model.StructDef: "returns"
model.PackageModel -> model.Dependency: "returns"
```

### Iteration 4: Service Layer

#### Task 4.1: Create Service Structure
- Create `internal/service/service.go` - Service struct definition
- Create `internal/service/factory.go` - NewService() with DI wiring
- Create `internal/service/options.go` - Shared options and result types

```go
// service.go
type Service struct {
    goReader  ModelReader   // adapter/golang.Reader
    d2Reader  ModelReader   // adapter/d2.Reader (nil until US-3)
    d2Writer  ModelWriter   // adapter/d2.Writer
}

// factory.go
func NewService() *Service {
    return &Service{
        goReader: golang.NewReader(),
        d2Writer: d2.NewWriter(),
    }
}

// options.go
type ModelReader interface {
    Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
}

type ModelWriter interface {
    Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error
}

type WriteOptions struct {
    OutputPath string
    PublicOnly bool
    ToStdout   bool
}
```

#### Task 4.2: Implement Generate Operation
- Create `internal/service/generate.go`
- Implement `Generate()` method:
  1. Read packages using goReader
  2. For each package: write diagrams using d2Writer
  3. Collect and return results

```go
// generate.go
type GenerateOptions struct {
    Paths        []string  // Package paths
    OutputFile   string    // If set, combined mode; if empty, split mode
    PublicOnly   bool      // Generate only pub.d2
    InternalOnly bool      // Generate only internal.d2
}

type GenerateResult struct {
    PackagePath  string
    PubFile      string
    InternalFile string
    Error        error
}

func (s *Service) Generate(ctx context.Context, opts GenerateOptions) ([]GenerateResult, error) {
    // 1. Read all packages from Go code
    packages, err := s.goReader.Read(ctx, opts.Paths)
    if err != nil {
        return nil, fmt.Errorf("reading packages: %w", err)
    }

    var results []GenerateResult
    for _, pkg := range packages {
        result := GenerateResult{PackagePath: pkg.Path}

        // 2. Write pub.d2 (unless --internal only)
        if !opts.InternalOnly {
            writeOpts := WriteOptions{
                OutputPath: filepath.Join(findPackageDir(pkg.Path), ".arch", "pub.d2"),
                PublicOnly: true,
            }
            if err := s.d2Writer.Write(ctx, pkg, writeOpts); err != nil {
                result.Error = err
            } else {
                result.PubFile = writeOpts.OutputPath
            }
        }

        // 3. Write internal.d2 (unless --pub only)
        if !opts.PublicOnly && result.Error == nil {
            writeOpts := WriteOptions{
                OutputPath: filepath.Join(findPackageDir(pkg.Path), ".arch", "internal.d2"),
                PublicOnly: false,
            }
            if err := s.d2Writer.Write(ctx, pkg, writeOpts); err != nil {
                result.Error = err
            } else {
                result.InternalFile = writeOpts.OutputPath
            }
        }

        results = append(results, result)
    }

    return results, nil
}
```

### Iteration 5: CLI

#### Task 5.1: Set Up Cobra CLI
- Create `cmd/wyrd/main.go`
- Set up root command
- Configure `diagram` subcommand group

#### Task 5.2: Implement Generate Command
- Add `diagram generate` command
- Parse arguments: package paths
- Parse flags: `--pub`, `--internal`, `--output`
- Create service via factory and call Generate()

```go
// cmd/wyrd/main.go
func main() {
    rootCmd := &cobra.Command{Use: "archai"}

    diagramCmd := &cobra.Command{Use: "diagram"}
    rootCmd.AddCommand(diagramCmd)

    generateCmd := &cobra.Command{
        Use:   "generate [packages...]",
        Short: "Generate D2 diagrams from Go packages",
        RunE:  runGenerate,
    }
    generateCmd.Flags().Bool("pub", false, "Generate only pub.d2")
    generateCmd.Flags().Bool("internal", false, "Generate only internal.d2")
    generateCmd.Flags().StringP("output", "o", "", "Output to single file (combined mode)")
    diagramCmd.AddCommand(generateCmd)

    rootCmd.Execute()
}

func runGenerate(cmd *cobra.Command, args []string) error {
    // Create service via factory
    svc := service.NewService()

    // Build options from flags
    pubOnly, _ := cmd.Flags().GetBool("pub")
    internalOnly, _ := cmd.Flags().GetBool("internal")
    output, _ := cmd.Flags().GetString("output")

    opts := service.GenerateOptions{
        Paths:        args,
        PublicOnly:   pubOnly,
        InternalOnly: internalOnly,
        OutputFile:   output,
    }

    // Execute
    results, err := svc.Generate(cmd.Context(), opts)
    if err != nil {
        return err
    }

    // Display results
    for _, r := range results {
        if r.Error != nil {
            fmt.Fprintf(os.Stderr, "ERROR %s: %v\n", r.PackagePath, r.Error)
        } else {
            if r.PubFile != "" {
                fmt.Printf("Created %s\n", r.PubFile)
            }
            if r.InternalFile != "" {
                fmt.Printf("Created %s\n", r.InternalFile)
            }
        }
    }
    return nil
}
```

**CLI Interface:**
```bash
# Split mode (default for US-1)
archai diagram generate ./internal/...

# With flags
archai diagram generate ./internal/... --pub
archai diagram generate ./internal/... --internal
```

#### Task 5.3: Add Progress and Error Reporting
- Show progress for each package being processed
- Report errors per-package (continue processing other packages)
- Summary at the end (X packages processed, Y errors)

### Iteration 6: Testing

#### Task 7.1: Unit Tests
- Test domain models
- Test parser with sample Go files
- Test generator with sample PackageModels
- Test writer with mock filesystem

#### Task 7.2: Integration Tests
- Test full flow: Go code → D2 diagrams
- Verify output structure and content

#### Task 7.3: End-to-End Tests
- Test CLI with real packages
- Verify `.arch/` folder creation
- Verify diagram content accuracy

---

## Key Design Decisions

### 1. Split vs Combined Mode Detection
```go
if opts.OutputFile != "" {
    // Combined mode (US-2) - single output file
} else {
    // Split mode (US-1) - per-package .arch/ folders
}
```

### 2. Symbol Visibility Filtering
```go
type VisibilityFilter int

const (
    PublicOnly  VisibilityFilter = iota  // For pub.d2
    AllSymbols                            // For internal.d2
)
```

### 3. Factory Function Detection
Identify factories by naming convention and return type:
```go
func isFactory(fn *ast.FuncDecl) bool {
    // Starts with "New"
    // Returns a concrete type or interface
    // Has no receiver (package-level function)
}
```

### 4. File-Based Grouping
Group symbols by source file to improve diagram readability:
```d2
package: {
  file1.go: {
    Interface1: { ... }
    Struct1: { ... }
  }
  file2.go: {
    Interface2: { ... }
  }
}
```

### 5. Cross-Package Connections
Show dependencies when packages are analyzed together:
```d2
pkg.linter.Linter -> pkg.archspec.Service: uses
```

---

## D2 Style Guide

### Stereotypes
| Stereotype | Usage | Color |
|------------|-------|-------|
| `<<interface>>` | Go interfaces | Purple (#f0e8fc) |
| `<<struct>>` | General structs | Blue (#e8f4fc) |
| `<<service>>` | Service implementations | Purple (#f0e8fc) |
| `<<aggregate>>` | DDD aggregate roots | Blue (#e8f4fc) |
| `<<value>>` | Value objects, options | Gray (#f8f8f8) |
| `<<factory>>` | Factory functions | Green (#e8fce8) |
| `<<repository>>` | Repository interfaces | Purple (#f0e8fc) |

### Method Signature Format
```
"+MethodName(param1 Type1, param2 Type2)": "(ReturnType, error)"
```

- `+` prefix for exported
- `-` prefix for unexported (internal.d2 only)
- Parentheses around multiple return values

---

## Acceptance Criteria Mapping

| Criteria | Implementation |
|----------|----------------|
| Accept one or multiple package paths | `GenerateOptions.Paths []string` |
| Create `.arch/` folder in each package | `DiagramWriter.EnsureArchDir()` |
| Generate `pub.d2` (public interfaces only) | `Generator.GeneratePublic()` |
| Generate `internal.d2` (all symbols) | `Generator.GenerateInternal()` |
| Default: generate both | Default behavior when no flags |
| Options: `--pub`, `--internal`, or both | `GenerateOptions.PublicOnly/InternalOnly` |
| Show cross-package connections | Dependency tracking in parser |
| Overwrites existing files | File writer overwrites by default |

---

## Dependencies

```go
require (
    golang.org/x/tools v0.28.0    // Go code parsing
    github.com/spf13/cobra v1.8.1 // CLI framework
)
```

**Note:** `oss.terrastruct.com/d2` is NOT required for US-1. We generate D2 text directly. The D2 library will be added later for parsing existing diagrams (US-3, US-4) and validation.

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Large codebases slow parsing | Add progress indicators, consider caching |
| Complex type signatures | Escape special characters, use quotes around method signatures |
| Circular dependencies | Detect and warn, don't fail |
| D2 syntax changes | Minimal risk - D2 syntax is stable and we generate simple constructs |
| File role detection inaccurate | Allow manual role hints via comments or future config |

---

## Next Steps After US-1

1. **US-2**: Add `--output` flag for combined mode
2. **US-3**: Implement `diagram split` command
3. **US-4**: Implement `diagram compose` command
4. **US-5/6/7**: Implement diff functionality

---

## File Checklist

### To Create

**Domain Models (`internal/domain/`):**
- [ ] `module.go` - Module (root context with module path)
- [ ] `package.go` - PackageModel aggregate
- [ ] `interface.go` - InterfaceDef value object
- [ ] `struct.go` - StructDef value object
- [ ] `function.go` - FunctionDef value object
- [ ] `method.go` - MethodDef, ParamDef, TypeRef
- [ ] `field.go` - FieldDef value object
- [ ] `dependency.go` - Dependency, SymbolRef, DependencyKind
- [ ] `stereotype.go` - Stereotype enum and constants

**Go Adapter (`internal/adapter/golang/`):**
- [ ] `reader.go` - Go code → domain.PackageModel
- [ ] `stereotype.go` - Stereotype detection (heuristics + annotations)
- [ ] `writer.go` - (placeholder for future: domain.PackageModel → Go code)

**D2 Adapter (`internal/adapter/d2/`):**
- [ ] `reader.go` - (placeholder for US-3+: D2 → domain.PackageModel)
- [ ] `writer.go` - domain.PackageModel → D2 file
- [ ] `builder.go` - D2 text builder (strings.Builder wrapper)
- [ ] `templates.go` - Legend, reusable D2 fragments
- [ ] `styles.go` - Colors, stereotypes, file roles

**Service (`internal/service/`):**
- [ ] `service.go` - Service struct definition
- [ ] `factory.go` - NewService() with DI wiring
- [ ] `options.go` - Shared interfaces, options, result types
- [ ] `generate.go` - Generate() operation

**CLI (`cmd/wyrd/`):**
- [ ] `main.go` - CLI entry point with Cobra
