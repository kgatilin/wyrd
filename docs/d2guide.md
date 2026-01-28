# D2 Diagram Style Guide

This guide documents the conventions for manually creating architecture diagrams in D2 format for this project.

## Quick Start

Copy this template to start a new diagram:

```d2
# Style classes
classes: {
  domain: {
    style.fill: "#e8f4fc"
    style.font-color: "#000"
  }
  service: {
    style.fill: "#f0e8fc"
    style.font-color: "#000"
  }
  factory: {
    style.fill: "#e8fce8"
    style.font-color: "#000"
  }
  value: {
    style.fill: "#f8f8f8"
    style.font-color: "#000"
  }
}

# Legend
legend: {
  label: "Color Legend (DDD)"
  style.stroke: "#999"
  style.fill: "#fafafa"
  near: top-right

  aggregate: {
    label: "Domain Model"
    shape: class
    class: domain
  }
  service: {
    label: "Service"
    shape: class
    class: service
  }
  factory: {
    label: "Factory"
    shape: class
    class: factory
  }
  options: {
    label: "Value Object"
    shape: class
    class: value
  }
}

# Packages
your.package: {
  label: "your/package"
  class: value

  # Add symbols here...
}
```

## Color Scheme (DDD-Inspired)

| Class | Color | Hex | Use For |
|-------|-------|-----|---------|
| `domain` | Blue | `#e8f4fc` | Aggregates, entities, core domain models |
| `service` | Purple | `#f0e8fc` | Interfaces, services, repositories, ports |
| `factory` | Green | `#e8fce8` | Factory functions (`New*` prefix) |
| `value` | Gray | `#f8f8f8` | Value objects, options, results, enums |

### Highlighting New Elements

When showing additions to existing architecture, add a `new` class. Use font color and stroke only (no fill) for a clean look that works well with class shape internals:

```d2
classes: {
  # ... standard classes ...

  new: {
    style.font-color: "#d4edda"
    style.stroke: "#d4edda"
  }
  new-arrow: {
    style.stroke: "#28a745"
  }
}
```

This approach highlights new elements with green text and border while keeping the background consistent with the parent container.

## Package Containers

Packages are top-level containers. Use dot notation for nested paths:

```d2
internal.service: {
  label: "internal/service"
  class: value

  # symbols go here
}

internal.domain: {
  label: "internal/domain"
  class: domain

  # symbols go here
}
```

**Choosing container class:**
- `domain` - packages with core domain models
- `factory` - adapter packages (e.g., `internal/adapter/d2`)
- `value` - service packages, utility packages

## Symbols

### Interfaces

```d2
ModelReader: {
  shape: class
  stereotype: "<<interface>>"

  "+Read(ctx context.Context, paths []string)": "([]PackageModel, error)"
  "+Write(ctx context.Context, model PackageModel)": "error"
}
```

### Structs

```d2
PackageModel: {
  shape: class
  stereotype: "<<struct>>"

  "+Path string": ""
  "+Name string": ""
  "+Interfaces []InterfaceDef": ""

  "+SourceFiles()": "[]string"
  "+HasExportedSymbols()": "bool"
}
```

### Factory Functions

```d2
NewService: {
  shape: class
  stereotype: "<<factory>>"

  "goReader": "ModelReader"
  "d2Reader": "ModelReader"
  "d2Writer": "ModelWriter"
  "return": "*Service"
}
```

### Enums / Type Definitions

```d2
ComposeMode: {
  shape: class
  stereotype: "<<enum>>"

  "type": "int"
  "ComposeModeAuto": "const"
  "ComposeModeSpec": "const"
  "ComposeModeCode": "const"
}
```

## Stereotypes Reference

| Stereotype | Use For |
|------------|---------|
| `<<interface>>` | Go interfaces |
| `<<struct>>` | Go structs |
| `<<factory>>` | Factory functions (typically `New*`) |
| `<<function>>` | Regular package-level functions |
| `<<enum>>` | Type definitions with constants |
| `<<service>>` | Service implementations (use sparingly) |

## Method/Field Syntax

### Visibility Prefixes

- `+` - Exported (public)
- `-` - Unexported (private)

### Methods

```d2
"+MethodName(param1 Type1, param2 Type2)": "ReturnType"
"+MultiReturn(input string)": "(Result, error)"
"-privateMethod()": ""
```

### Fields

```d2
"+ExportedField string": ""
"-unexportedField int": ""
"+Nested []OtherType": ""
```

### Factory Parameters

For factory functions, show dependencies as fields and return type:

```d2
NewService: {
  shape: class
  stereotype: "<<factory>>"

  "reader": "ModelReader"      # dependency
  "writer": "ModelWriter"      # dependency
  "return": "*Service"         # what it creates
}
```

## Dependencies

### Within a Package

```d2
internal.service: {
  # ... symbols ...

  # Dependencies (inside the package block)
  NewService -> ModelReader: "uses"
  NewService -> Service: "returns"
  Service -> GenerateOptions: "uses"
}
```

### Cross-Package

```d2
# After all package blocks
internal.adapter.d2.NewReader -> internal.service.ModelReader: "returns"
internal.service.ModelReader -> internal.domain.PackageModel: "returns"
```

### Dependency Labels

| Label | Meaning |
|-------|---------|
| `"uses"` | Uses/depends on |
| `"returns"` | Returns/creates |
| `"implements"` | Implements interface |

### Highlighting New Dependencies

```d2
Service -> ComposeOptions: "uses" { class: new-arrow }
Service -> ComposeResult: "returns" { class: new-arrow }
```

## Highlighting Individual Members

For individual rows within class shapes, use `class: new` which applies font color and stroke:

```d2
Service: {
  shape: class
  stereotype: "<<struct>>"

  "+Generate(ctx, opts)": "([]Result, error)"
  "+Split(ctx, opts)": "(*SplitResult, error)"
  "+Compose(ctx, opts)": "(*ComposeResult, error)" {
    class: new
  }
}
```

The `new` class uses font color and stroke (no fill), which renders cleanly for both containers and individual class members.

## Complete Example

```d2
classes: {
  domain: { style.fill: "#e8f4fc"; style.font-color: "#000" }
  service: { style.fill: "#f0e8fc"; style.font-color: "#000" }
  factory: { style.fill: "#e8fce8"; style.font-color: "#000" }
  value: { style.fill: "#f8f8f8"; style.font-color: "#000" }
  new: { style.font-color: "#d4edda"; style.stroke: "#d4edda" }
  new-arrow: { style.stroke: "#28a745" }
}

legend: {
  label: "Color Legend"
  style.stroke: "#999"
  style.fill: "#fafafa"
  near: top-right

  domain: { label: "Domain Model"; shape: class; class: domain }
  service: { label: "Service"; shape: class; class: service }
  factory: { label: "Factory"; shape: class; class: factory }
  value: { label: "Value Object"; shape: class; class: value }
  new: { label: "NEW"; shape: class; class: new }
}

pkg.core: {
  label: "pkg/core"
  class: domain

  Entity: {
    shape: class
    stereotype: "<<struct>>"
    "+ID string": ""
    "+Name string": ""
  }

  Repository: {
    shape: class
    stereotype: "<<interface>>"
    "+Get(id string)": "(*Entity, error)"
    "+Save(entity *Entity)": "error"
  }
}

pkg.service: {
  label: "pkg/service"
  class: value

  Service: {
    shape: class
    stereotype: "<<struct>>"
    "+Process(id string)": "error"
  }

  NewService: {
    shape: class
    stereotype: "<<factory>>"
    "repo": "Repository"
    "return": "*Service"
  }

  NewService -> Service: "returns"
}

# Cross-package
pkg.service.NewService -> pkg.core.Repository: "uses"
pkg.service.Service -> pkg.core.Entity: "uses"
```

## Tips

1. **Keep labels short** - Use abbreviated signatures in the diagram
2. **Group related symbols** - Put related types near each other
3. **Show key dependencies only** - Don't clutter with every possible arrow
4. **Use comments** - Add `# Section name` to organize large diagrams
5. **Consistent ordering** - Interfaces, then structs, then functions, then type definitions
