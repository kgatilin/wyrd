# Java e2e fixture

Three-package Java mini-project used by the gated end-to-end test in
`java_e2e_test.go`. Kept tiny on purpose so the test stays fast and
self-contained:

| Package | Class | Role |
|---|---|---|
| `com.example.app` | `App` | Wires the other two together |
| `com.example.service` | `Greeter` | Pure formatter |
| `com.example.repo` | `Storage` | In-memory key-value map |

The test runs the analyzer JAR over `src/`, asks the CLI to emit per-package
`.arch/pub.d2` + `pub.yaml`, and round-trips the YAML back through
`adapter/yaml`. It is gated behind both the `e2e` build tag and the
`RUN_E2E=1` env var so default `go test ./...` doesn't require a JVM.
