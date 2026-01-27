# Archai

Architecture diagram generator for Go projects. Analyzes Go source code and generates D2 diagrams showing interfaces, structs, functions, and their relationships.

## Project Structure

```
archai/
├── cmd/archai/           # CLI entry point (Cobra)
│   └── main.go           # Wires dependencies, defines commands
│
├── internal/
│   ├── domain/           # Core domain models (data containers)
│   │   ├── package.go    # PackageModel - aggregate root
│   │   ├── interface.go  # InterfaceDef
│   │   ├── struct.go     # StructDef
│   │   ├── function.go   # FunctionDef
│   │   ├── method.go     # MethodDef, ParamDef, TypeRef
│   │   ├── field.go      # FieldDef
│   │   ├── dependency.go # Dependency, SymbolRef
│   │   ├── typedef.go    # TypeDef (for enums)
│   │   ├── stereotype.go # Stereotype enum
│   │   └── module.go     # Module (root context)
│   │
│   ├── adapter/
│   │   ├── golang/       # Go code adapter
│   │   │   ├── reader.go     # Parses Go code → domain models
│   │   │   ├── stereotype.go # Stereotype detection heuristics
│   │   │   └── writer.go     # Placeholder for future
│   │   │
│   │   └── d2/           # D2 diagram adapter
│   │       ├── writer.go     # domain models → D2 files
│   │       ├── builder.go    # D2 text generation
│   │       ├── templates.go  # Legend template
│   │       └── styles.go     # Color mappings
│   │
│   └── service/          # Business operations
│       ├── service.go    # Service struct
│       ├── factory.go    # NewService(reader, writer)
│       ├── options.go    # ModelReader/ModelWriter interfaces
│       └── generate.go   # Generate operation
│
├── tests/
│   └── integration/      # Integration tests
│
└── docs/
    └── features/         # Feature specifications and plans
```

## Architecture

**Hexagonal Architecture (Ports & Adapters)**:
- Domain models at the center (no dependencies)
- Adapters handle read/write for different formats
- Service layer orchestrates operations
- CLI wires dependencies (proper DI)

**Key Interfaces**:
```go
type ModelReader interface {
    Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)
}

type ModelWriter interface {
    Write(ctx context.Context, model domain.PackageModel, opts WriteOptions) error
}
```

## Usage

```bash
# Generate diagrams for packages
archai diagram generate ./internal/...

# Public API only
archai diagram generate ./internal/... --pub

# Internal implementation only
archai diagram generate ./internal/... --internal
```

Output goes to `.arch/` folder in each package:
- `pub.d2` - exported symbols only
- `internal.d2` - all symbols

## What's Implemented (US-1)

- [x] Go code parsing with `golang.org/x/tools/go/packages`
- [x] Symbol extraction (interfaces, structs, functions, type definitions)
- [x] Stereotype detection (factory functions via `New*` prefix)
- [x] D2 diagram generation with legend
- [x] File-based grouping of symbols
- [x] Dependency tracking and visualization
- [x] CLI with Cobra

## D2 Output Format

Stereotypes:
- `<<interface>>` - Go interfaces
- `<<struct>>` - Go structs
- `<<factory>>` - Factory functions (New* prefix)
- `<<function>>` - Regular functions
- `<<enum>>` - Type definitions with constants

Each function is its own class shape with parameters as fields and a `return` field.

## Development Rules

1. **No test-only production code** - Don't add functions/parameters, types, or exported wrappers solely for testing. If you need to expose internals for testing, the architecture is wrong. Solutions:
   - Test through the public interface (e.g., test `Writer.Write()` output, not internal builder methods)
   - Use internal tests (`package foo`) instead of external tests (`package foo_test`) when testing implementation details
   - Refactor to make the code naturally testable through its public API
2. **No unnecessary exports** - Don't export functions, types, or constants that are only used within the package. Exported symbols are public API and should have a reason to be public. If tests need access to internals, use internal tests (`package foo`) not external tests.
3. **Proper DI** - Services receive dependencies via constructor, not create them
4. **Domain models are data containers** - No behavior, no external dependencies
5. **Adapters depend on domain** - Never the reverse
6. **CLI does the wiring** - Assembles adapters and passes to service

## Running Tests

```bash
go test ./...
```

## Building

```bash
go build -o archai ./cmd/archai
```
