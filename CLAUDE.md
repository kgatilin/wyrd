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

## Architectural Analysis (MCP graph tools)

Beyond diagram generation, archai runs as an MCP server (`archai serve
--mcp-stdio`, wired in `.mcp.json`) exposing a typed **code graph** of the
project and a set of analysis lenses over it. The graph models:

```
package ─contains→ file ─contains→ type/function
type ─contains→ method/field
fn/method ─calls/usesType/returns→ type/fn      (behavioral flow edges)
struct ─implements→ interface
```

Node id scheme (see `internal/adapter/archmotif/exporter.go`):
`pkg:<path>`, `file:<path>/<base>`, `type:<path>.<Name>`, `fn:<path>.<Name>`,
`method:<path>.<Recv>.<Name>`, `field:<path>.<Struct>.<Name>`.

### Analysis lenses (MCP tools)

All take `{package, include_subpackages}` and run on the package subgraph.

- **`components`** — connected components over *all* edges. Finds shattered
  graphs / isolated symbols (missing edges). Singletons = something unlinked.
- **`file_hotspots`** — top-level declarations per file; flags structural
  overload (god-files) at `>= max(3× median, 20)`. Backed by `filestats`.
- **`trophic_layers`** — emergent layers from dependency *direction* (no policy
  needed). Solves a graph-Laplacian for a trophic height per node; reports
  `F0` incoherence ∈ [0,1] (~0 layered, >0.4 tangled), integer layers
  (0 = foundation, top = entry points), backward edges (inversions), and cycles.
- **`spectral_cluster`** — natural module clusters via the eigengap heuristic.

### Key concepts (hard-won)

- **Flow vs structural nodes.** `calls/usesType/returns/implements` are
  behavioral flow edges; `contains` is structural. `trophic_layers` runs on the
  flow projection and **excludes `field`/`file`/`package`** kinds (structural
  leaves with no flow edges — a field's type-coupling is recorded on its struct,
  not the field). `components` keeps them (it uses all edges incl. `contains`).
- **Trophic level ≠ DDD layer.** Trophic height is pure dependency-flow depth:
  sinks (depend on nothing) at the bottom, sources (entry points) on top. An
  aggregate like `domain.PackageModel` sits *above* its leaf value types because
  it depends on them — so "domain" is not a single bottom layer. The macro
  ordering (domain < serve < http < mcp) is still correct.
- **Layers are integer-rounded trophic levels**, not gap-cuts — gap-cutting does
  not scale to dense graphs (everything collapses into one band).
- **Model cache** (`internal/serve/model_cache.go`) is keyed on the binary's
  build version + executable stamp. After `make install`, restart the daemon and
  `refresh`, or a parser-logic change is masked by a stale cache.

### Where the code lives

- Graph build: `internal/adapter/golang/reader.go` (parse) →
  `internal/adapter/archmotif/exporter.go` (→ archmotif graph).
- Analysis: `github.com/kgatilin/archmotif/pkg/{components,filestats,trophic,spectralcluster}`.
- MCP handlers: `internal/adapter/mcp/tools.go` (`handle*`, registered in
  `builtinToolDefinitions` + `Dispatch`).

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
