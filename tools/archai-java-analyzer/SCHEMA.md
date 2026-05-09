# JavaFacts JSON schema

`schema: "javafacts/v1"` — emitted by `archai-java-analyzer.jar`. Consumed by
`internal/adapter/java/` on the Go side (issue #102). Bump the version on any
breaking change to this contract.

## Design principles

1. **AST-shape, not archai-shape.** The Java analyzer is intentionally
   semantically dumb: it dumps what the AST tells it. No archai stereotypes,
   no domain mapping, no hexagonal vocabulary. All semantic interpretation
   lives in the Go translator.
2. **Empty arrays explicit.** Empty collections always serialise as `[]`,
   never omitted — keeps the downstream parser simple.
3. **Deterministic ordering.** Classes sort by FQN. Imports sort by
   `(from, to_class, kind)`. Members within a class follow source order
   (already deterministic per parsed file). Packages sort alphabetically.
4. **Best-effort resolution.** Where JavaParser's symbol solver succeeds the
   `to_class` and annotation `fqn` fields carry resolved fully-qualified
   names; otherwise the textual form as written is preserved. The Go
   translator decides what to do with unresolved references.

## Top-level shape

```json
{
  "schema": "javafacts/v1",
  "src_roots": ["src/main/java"],
  "packages": ["com.example", "com.example.util"],
  "classes": [ /* JavaClass… */ ],
  "imports":  [ /* JavaImport… */ ],
  "parse_warnings": [ /* ParseWarning… */ ]
}
```

| Field            | Type            | Meaning                                                                 |
|------------------|-----------------|-------------------------------------------------------------------------|
| `schema`         | string          | Schema version. Always `javafacts/v1` for this revision.                |
| `src_roots`      | `[]string`      | Source roots passed on the CLI, in input order.                         |
| `packages`       | `[]string`      | All Java package names encountered, sorted alphabetically.              |
| `classes`        | `[]JavaClass`   | Type declarations (top-level + nested). Sorted by `fqn`.                |
| `imports`        | `[]JavaImport`  | One entry per `import` statement.                                       |
| `parse_warnings` | `[]ParseWarning`| Non-fatal parse problems. Empty array on a fully clean run.             |

## `JavaClass`

```json
{
  "fqn": "com.example.Greeter",
  "package": "com.example",
  "name": "Greeter",
  "kind": "class|interface|enum|record|annotation",
  "modifiers": ["public", "final"],
  "type_parameters": ["T extends Number"],
  "extends": "com.example.Base",
  "implements": ["com.example.Runnable"],
  "permits": [],
  "source_file": "com/example/Greeter.java",
  "doc": "javadoc text with leading * stripped",
  "annotations": [ /* JavaAnnotation… */ ],
  "fields":  [ /* JavaField… */ ],
  "methods": [ /* JavaMethod… */ ],
  "enum_constants": []
}
```

Notes:
- **Nested types** are emitted as siblings with `Outer.Inner` in `name` and
  `pkg.Outer.Inner` in `fqn`. `kind` reflects the nested declaration's own
  kind.
- **Interface super-interfaces** (Java's `interface A extends B, C`) populate
  `implements`, not `extends` — keeps a single edge collection downstream.
  `extends` on an interface is always empty. Documented choice; the Go
  translator interprets `implements` on a `kind:"interface"` as
  super-interfaces.
- **Records** populate `fields` from their components (record components are
  always private+final). The canonical and any explicit constructors land in
  `methods`.
- **Modifiers** preserve declared order (matches source).
- **`source_file`** is relative to the matching `src_roots[]` entry, with
  forward-slash separators regardless of host OS — keeps output portable
  across Linux/macOS golden tests.
- **`doc`** has the leading `*` (and one optional space) stripped from each
  javadoc body line; lines join with `\n`. Empty when no javadoc is present.

## `JavaField`

```json
{ "name": "x", "type": "int", "modifiers": ["private","final"],
  "annotations": [], "doc": "" }
```

One entry per declared name: `int x, y;` → two `JavaField` entries.

## `JavaMethod`

```json
{
  "name": "m",
  "kind": "method|constructor",
  "modifiers": ["public"],
  "type_parameters": ["T"],
  "params": [ /* JavaParam… */ ],
  "returns": "void",
  "throws": ["java.io.IOException"],
  "annotations": [],
  "doc": "",
  "calls": [ /* JavaCall… */ ]
}
```

Constructors set `kind:"constructor"` and `returns:"void"`; their `name`
matches the enclosing class's simple name. Static initialisers are not
emitted (out of scope for v1).

## `JavaParam`

```json
{ "name": "tricks", "type": "List<String>", "varargs": false,
  "modifiers": [], "annotations": [] }
```

Varargs parameters set `"varargs": true`; the `type` is the element type
(`String` for `String... args`).

## `JavaCall`

```json
{ "to_class": "Math", "to_method": "sqrt", "static": true, "external": false }
```

| Field       | Meaning                                                                              |
|-------------|--------------------------------------------------------------------------------------|
| `to_class`  | Receiver textual scope (resolved FQN if symbol solver succeeded). Empty for unqualified calls. |
| `to_method` | Method name as written.                                                              |
| `static`    | Heuristic — `true` when `to_class` looks like a Type (uppercase, no dot).            |
| `external`  | `true` when the receiver couldn't be resolved to the analyzed source set. v1 leaves this `false`; the Go translator may flip it after applying the analyzed-package set. |

## `JavaImport`

```json
{ "from": "com.example.Greeter", "to_class": "java.util.Objects", "kind": "class" }
```

`kind` ∈ {`class`, `static`, `wildcard`, `static_wildcard`}. `from` is the
FQN of the compilation unit's primary class; `to_class` is the imported
symbol's textual form (FQN for normal imports, `pkg.*` for wildcards).

## `JavaAnnotation`

```json
{ "fqn": "java.lang.Override", "args": [] }
```

`args` carry the textual form of annotation members:
- Single-member: `@Path("/users")` → `args: ["\"/users\""]`
- Normal: `@Mapping(method=GET, path="/x")` → `args: ["method=GET", "path=\"/x\""]`
- Marker: `@Override` → `args: []`

No type resolution on argument values — downstream pattern-matches on the
textual form.

## `ParseWarning`

```json
{ "file": "src/com/Broken.java", "message": "expected ';'" }
```

Non-fatal. Emitted when JavaParser cannot parse a single file but the run
has at least one successful file. If every file fails, the analyzer exits
with code 1 and prints the warnings to stderr instead.

## What's not in v1

- Static initialiser blocks
- Resolved annotation argument values (only textual)
- Cross-JAR type resolution (only same-source-set resolution attempted)
- Method-body fact other than method calls (no field reads, control flow,
  lambda targets)
- Generic type substitutions (parameter types stay as written, e.g.
  `List<String>` rather than `java.util.List<java.lang.String>`)

These are explicit follow-ups, captured as TODOs in code where relevant.
