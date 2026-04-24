# Archai

Architecture diagram generator for Go projects. Analyzes Go source code and generates D2 diagrams showing interfaces, structs, functions, and their relationships.

> **Documentation:** the full user guide — installation, quick start,
> project setup, browser UI, editor and MCP agent integration — lives in
> [`docs/user-guide.md`](docs/user-guide.md).

## Installation

```bash
go install github.com/kgatilin/archai/cmd/archai@latest
```

Or build from source:

```bash
git clone https://github.com/kgatilin/archai.git
cd archai
go build -o archai ./cmd/archai
```

## Usage

### Generate Diagrams

Generate D2 architecture diagrams from Go source code:

```bash
# Generate diagrams for all packages in internal/
archai diagram generate ./internal/...

# Generate only public API diagrams
archai diagram generate ./internal/... --pub

# Generate only internal implementation diagrams
archai diagram generate ./internal/... --internal

# Generate a single combined diagram
archai diagram generate ./internal/... --output architecture.d2
```

Output goes to `.arch/` folder in each package:
- `pub.d2` - exported symbols only (public API)
- `internal.d2` - all symbols (full implementation)

### Split Combined Diagrams

Split a combined diagram into per-package specification files:

```bash
# Split into per-package specs
archai diagram split docs/architecture.d2

# Preview what would be created
archai diagram split docs/architecture.d2 --dry-run
```

Creates `pub-spec.d2` files in each package's `.arch/` directory.

### Compose Diagrams

Compose a single diagram from saved `.arch/` specification files:

```bash
# Compose from code-generated diagrams (default: pub.d2 files)
archai diagram compose ./internal/... --output docs/current-architecture.d2

# Compose from target specifications (pub-spec.d2 files)
archai diagram compose ./internal/... --spec --output docs/target-architecture.d2
```

## D2 Output Format

The generated diagrams use D2's class shape with stereotypes to represent Go constructs:

| Stereotype | Description |
|------------|-------------|
| `<<interface>>` | Go interfaces |
| `<<struct>>` | Go structs |
| `<<factory>>` | Factory functions (`New*` prefix) |
| `<<function>>` | Regular exported functions |
| `<<enum>>` | Type definitions with constants |

### Example Output

```d2
internal.service: {
  label: "internal/service"

  Service: {
    shape: class
    stereotype: "<<interface>>"

    "+Generate(ctx context.Context, opts GenerateOptions)": "([]GenerateResult, error)"
  }

  NewService: {
    shape: class
    stereotype: "<<factory>>"

    "reader": "ModelReader"
    "writer": "ModelWriter"
    "return": "*Service"
  }
}
```

## Viewing Diagrams

D2 diagrams can be rendered using the [D2 CLI](https://d2lang.com/):

```bash
# Install D2
curl -fsSL https://d2lang.com/install.sh | sh -s --

# Render to SVG
d2 architecture.d2 architecture.svg

# Live preview with hot reload
d2 --watch architecture.d2
```

## Project Structure

```
archai/
├── cmd/archai/           # CLI entry point (Cobra)
├── internal/
│   ├── domain/           # Core domain models
│   ├── adapter/
│   │   ├── golang/       # Go code reader
│   │   └── d2/           # D2 diagram reader/writer
│   └── service/          # Business operations
├── tests/
│   └── integration/      # Integration tests
└── docs/
    └── features/         # Feature specifications
```

## Architecture

Archai follows hexagonal architecture (ports & adapters):

- **Domain models** at the center (no external dependencies)
- **Adapters** handle reading/writing different formats (Go code, D2 diagrams)
- **Service layer** orchestrates operations
- **CLI** wires dependencies via proper dependency injection

## Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o archai ./cmd/archai
```

## License

MIT
