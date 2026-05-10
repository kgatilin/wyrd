# Java support

Status: **experimental**, lands in archai 0.x via [#100](https://github.com/kgatilin/archai/issues/100) (children: #101, #102, #103).

## Goal

Generate the same `.arch/{pub,internal}.{d2,yaml}` outputs from Java source code as we already do from Go, with no archai-side schema changes and no extra ceremony for Go-only users.

## Architecture

```
                       ┌────────────────────────────┐
   archai (Go binary)  │  cmd/archai                │
                       │  service.Service           │
                       │   ├── adapter/golang       │
                       │   ├── adapter/java ────────┼──── exec ─────► archai-java-analyzer.jar
                       │   ├── adapter/d2           │                   (JavaParser + symbol-solver)
                       │   └── adapter/yaml         │                   emits JavaFacts/v1 JSON on stdout
                       └────────────────────────────┘
```

Two halves, talking through a versioned JSON contract:

1. **`tools/archai-java-analyzer/`** (#101): a small Maven-built fat-jar. JavaParser walks Java source under the configured `src/` roots and emits a `JavaFacts/v1` document on stdout. The analyzer is intentionally **AST-shape, not archai-shape** — it emits what the AST says, no DDD vocabulary, no archai stereotypes. Fully documented in [`tools/archai-java-analyzer/SCHEMA.md`](../../tools/archai-java-analyzer/SCHEMA.md).
2. **`internal/adapter/java/`** (#102): the Go side. Resolves the JAR (constructor arg → `ARCHAI_JAVA_JAR` → sibling-of-binary), shells out via `java -jar`, decodes the JSON, and runs a pure translator that produces `[]domain.PackageModel`.
3. **`service.Service`** (#103): now accepts language-scoped readers via `WithJavaReader` / `WithLanguageReader`. The Go reader passed to `NewService` always runs first; additional readers are dispatched only on input paths whose subtree contains the language's source files. Polyglot directories work — both readers run, results are concatenated.

## Why the schema lives in Go

Archai's domain model evolves as we learn (new stereotype, new edge kind, richer `TypeRef`). Putting that semantics in Java would couple every domain change to a Maven release; instead the JAR speaks a fixed, dumb shape and the Go translator owns all interpretation. The JAR's schema string (`javafacts/v1`) is checked on every run — a future bump fails loudly instead of silently mis-mapping.

## CLI surface

```
archai diagram generate <paths...> [--java-jar <path>]
```

- `--java-jar` is optional. Resolution order: `--java-jar` flag → `ARCHAI_JAVA_JAR` env → sibling of `bin/archai` → "skip Java" (with a one-line stderr note for the user).
- Pure-Go users: when neither the flag, env, nor sibling JAR is present, archai silently registers no Java reader and the dispatch path is a no-op.
- Polyglot dispatch: each input path is checked for `*.java` files in its subtree; the Java reader runs only on matching paths, the Go reader runs as before.

## What's not in v1

- **Cross-language edges** (Go ↔ Java in the same diagram). Each language renders its own packages.
- **Build-system integration.** The JAR takes raw source-root paths; it doesn't read `pom.xml` or `build.gradle`.
- **Annotation argument resolution.** Annotation values stay textual.
- **Resolved `extends` / `implements` FQN.** Inheritance edges to types outside the analyzed source set stay textual; only same-source class names are resolved (for in-source classes the Go translator finds them by simple name as well).

These are tracked as explicit follow-ups; anything that lands on the schema bumps `javafacts/v2`.

## Testing

- Unit tests for the JAR live in `tools/archai-java-analyzer/src/test/java/` (golden-JSON comparison).
- Unit tests for the Go side live in `internal/adapter/java/` (translator + exec resolution + reader).
- The end-to-end path is exercised by `tests/integration/java_e2e_test.go`, gated behind both the `e2e` build tag and the `RUN_E2E=1` env var:

  ```
  RUN_E2E=1 go test -tags e2e -run TestJavaEndToEnd ./tests/integration/...
  ```

  The fixture under `tests/integration/fixtures/java/` is a 3-package Java mini-project; the test asserts the analyzer + translator + service pipeline produces models for all three packages and round-trips through `adapter/yaml`.
