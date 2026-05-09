# archai-java-analyzer

JavaParser-based source analyzer that emits `JavaFacts` JSON for the archai
Go-side Java adapter. Part of [issue #100][epic] / [#101][ticket].

[epic]: https://github.com/kgatilin/archai/issues/100
[ticket]: https://github.com/kgatilin/archai/issues/101

## What it does

Walks one or more Java source roots and prints a JSON document describing
every package / class / interface / enum / record / annotation found:
modifiers, fields, methods (with parameters, return type, throws, calls),
imports, and annotations. The shape is intentionally Java-native — see
[SCHEMA.md](./SCHEMA.md). All archai-domain interpretation (stereotypes,
hexagonal mapping) lives in the Go translator (`internal/adapter/java/`,
issue #102), not here.

## Requirements

- JVM 17 or newer (Linux / macOS supported).
- Maven 3.9+ to build from source.

## Build

```sh
mvn -f tools/archai-java-analyzer/pom.xml package
# → tools/archai-java-analyzer/target/archai-java-analyzer.jar
```

From the archai repo root, the convenience targets are:

```sh
make java-analyzer        # build + run JAR tests (issue #101 acceptance)
make java-analyzer-build  # build only (skip tests; faster)
make build-all            # Go binary + JAR copied to bin/archai-java-analyzer.jar
```

The default `make build` stays Go-only (preserves «no JVM needed for Go-only
users»). `make build-all` deposits the JAR next to `bin/archai` so the
sibling-binary resolver in #102's exec wrapper picks it up without extra
configuration.

## Run

```sh
java -jar tools/archai-java-analyzer/target/archai-java-analyzer.jar \
    path/to/src/main/java > facts.json
```

Multiple source roots are accepted. JSON is printed to stdout; progress and
errors go to stderr; exit code is 0 on success, 1 on hard failure.

### Flags

| Flag                     | Effect                                                  |
|--------------------------|---------------------------------------------------------|
| `--pretty`               | Pretty-print JSON (default: minified, single line).     |
| `--include-private`      | Include private members (default).                       |
| `--no-include-private`   | Skip private members.                                    |
| `--version`              | Print schema version and exit.                           |
| `-h`, `--help`           | Show usage.                                              |

### Hard failures vs warnings

- Missing or non-existent CLI argument → exit 1, message on stderr.
- Empty source root → exit 0, valid JavaFacts with empty `classes`.
- Unparseable single file in an otherwise valid tree → recorded under
  `parse_warnings` in the JSON, run continues.
- All files unparseable → exit 1, warnings printed to stderr.

## JavaFacts schema (v1)

See [SCHEMA.md](./SCHEMA.md) for the full contract. At a glance:

```json
{
  "schema": "javafacts/v1",
  "src_roots": ["..."],
  "packages": ["com.example"],
  "classes": [
    {
      "fqn": "com.example.Greeter",
      "kind": "class",
      "modifiers": ["public"],
      "fields": [...], "methods": [...]
    }
  ],
  "imports": [{"from": "...", "to_class": "...", "kind": "class"}],
  "parse_warnings": []
}
```

Output is deterministic: classes sorted by `fqn`, imports sorted, packages
sorted, members in source order, no timestamps.

## Tests

```sh
mvn -f tools/archai-java-analyzer/pom.xml test
```

Golden fixtures live under `src/test/resources/golden/<name>/` and pair a
small Java source tree with an `expected.json`. The `AnalyzerTest`
JUnit test factory runs every fixture and compares byte-for-byte.

To regenerate golden files after an intentional schema change:

```sh
mvn -f tools/archai-java-analyzer/pom.xml -Darchai.update-golden=true test
```

Re-running without the flag confirms determinism.

Current fixtures:

| Fixture                      | What it covers                                                  |
|------------------------------|-----------------------------------------------------------------|
| `simple-class`               | Single class, fields, methods, calls, imports                   |
| `interface-with-default`     | Interface modifiers + default method bodies                     |
| `record`                     | Record kind, components-as-fields                               |
| `sealed-and-enums`           | `sealed`, `permits`, enum constants                             |
| `inheritance`                | `extends`, `implements`, multi-file package                     |
| `call-resolution`            | `JavaCall.target_fqn` resolution + lambda/anonymous-class boundary |

## Layout

```
tools/archai-java-analyzer/
├── pom.xml                                     # Maven build (shade fat-jar)
├── README.md                                   # this file
├── SCHEMA.md                                   # JavaFacts schema
└── src/
    ├── main/java/io/archai/javaanalyzer/
    │   ├── Main.java                           # CLI entry
    │   ├── Analyzer.java                       # JavaParser walk → facts
    │   ├── facts/                              # JavaFacts data classes
    │   └── json/Writer.java                    # Jackson serializer
    └── test/
        ├── java/io/archai/javaanalyzer/        # JUnit 5 tests
        └── resources/golden/                   # input + expected.json pairs
```

## Notes for downstream consumers (issue #102)

- The JAR exits 1 only on hard failure. Empty `classes` + non-empty
  `parse_warnings` always means «every file failed»; the Go side does not
  need to second-guess this.
- `JavaCall.target_fqn` is the canonical edge target. It is non-empty only
  when JavaParser's symbol solver bound the receiver to a class declared in
  the analyzed source set; build `domain.CallEdge`s from those calls. Calls
  with `external: true` carry the textual receiver/method under
  `unresolved` for diagnostics — drop them from the graph.
- `to_class` on calls is the raw textual receiver scope as written; kept
  for backward compatibility and human inspection. Use `target_fqn` /
  `unresolved` instead for routing decisions.
- Default JAR distribution location: `bin/archai-java-analyzer.jar` next to
  the Go binary (deposited by `make build-all`). #102's exec wrapper looks
  there first.
- Field/method/parameter ordering inside a class is source-order — stable
  because JavaParser parses deterministically. Cross-class order is
  alphabetical FQN. Parse warnings are sorted by `(file, message)`.
