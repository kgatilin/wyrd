# Archai User Guide

Archai is an architecture tool for Go projects. It extracts a structured
model of your packages (interfaces, structs, functions, methods,
dependencies, calls) and lets you:

- generate D2 diagrams of the current code,
- declare architectural layers and rules in `archai.yaml` and enforce
  them,
- freeze the current architecture as a named **target**, diff the live
  code against it, and validate in CI,
- browse the model through a local web UI,
- expose the same model to Claude Code / Codex / other MCP clients as
  structured tools.

This guide covers what is on `main` today. For the roadmap of remaining
work see [`docs/roadmap.md`](roadmap.md).

---

## 1. Installation

### From source (go install)

```bash
go install github.com/kgatilin/archai/cmd/archai@latest
```

This installs the `archai` binary into `$(go env GOBIN)` (or
`$(go env GOPATH)/bin`). Make sure that directory is on your `PATH`.

### From a local clone

```bash
git clone https://github.com/kgatilin/archai.git
cd archai
go build -o archai ./cmd/archai
./archai --help
```

### Prebuilt binaries

Prebuilt binaries are not published yet. Once they are attached to a
GitHub release, download the archive for your OS from
<https://github.com/kgatilin/archai/releases>, extract `archai`, and put
it on your `PATH`.

### Verifying the install

Verify the install by running:

```bash
archai version
archai --help
```

`archai version` prints the stamped build version. The top-level help
should show the `diagram`, `target`, `diff`, `validate`, `overlay`,
`serve`, `extract`, `where`, `list-daemons`, and `sequence` command
groups.

Go 1.25 or newer is required (see `go.mod`).

---

## 2. Quick start

Run these from the root of your Go module.

### 2.1 Extract the model and generate diagrams

```bash
# Generate pub.d2 + internal.d2 under each package's .arch/ directory.
archai diagram generate ./...

# Or restrict to a sub-tree.
archai diagram generate ./internal/...
```

Each package gets a `.arch/` folder with:

- `pub.d2` — exported API only,
- `internal.d2` — full implementation.

Pass `--pub` or `--internal` to produce only one, or `-o FILE` to write
a single combined diagram to one file. `--format yaml` emits the
structured YAML model used by targets instead of D2.

### 2.2 Check overlay (layer rules)

Declare layers and allowed cross-layer imports in `archai.yaml` (see
[§3](#3-project-setup)), then:

```bash
archai overlay check
```

Exits `0` when the overlay is valid and no layer-rule violations exist.
Exits `1` otherwise. This is the command to wire into CI for
architecture enforcement.

### 2.3 Lock a target, diff, validate

```bash
# Freeze the current architecture as target v1.
archai target lock v1 --description "baseline at 2026-04"

# Make v1 the active target (written to .arch/targets/CURRENT).
archai target use v1

# See drift as code evolves.
archai diff

# CI-friendly exit code.
archai validate
```

`archai target lock` regenerates the per-package YAML specs under
`.arch/` (equivalent to `archai diagram generate --format yaml`) and
copies them into `.arch/targets/<id>/model/`. Pass `--skip-generate` to
reuse existing specs, or `-p ./internal/...` to limit which packages are
refreshed.

### 2.4 Browse the model

```bash
archai serve --http :8080
```

Open <http://localhost:8080>. See [§4](#4-architecture-browser).

### 2.5 Inspect a call sequence

```bash
archai sequence internal/service.Service.Generate
archai sequence internal/service.Service.Generate --depth 3
archai sequence internal/service.Service.Generate --format d2 -o gen.d2
```

Target format is `<pkg/path>.<FuncName>` or
`<pkg/path>.<TypeName>.<MethodName>`. The current model is loaded from
per-package `.arch/*.yaml` specs when present, otherwise the Go reader
parses `./...` directly.

---

## 3. Project setup

### 3.1 Minimal `archai.yaml`

Put this next to `go.mod`. It declares module-wide layers and the
allowed dependencies between them. Aggregates and configs can live here,
but for larger projects prefer package-local fragments so each package
owns its own architecture metadata.

```yaml
# archai.yaml

module: github.com/example/app

layers:
  cli:
    - cmd/...
  service:
    - internal/service/...
  adapter:
    - internal/adapter/...
  domain:
    - internal/domain/...

# For each layer, list the layers it is allowed to depend on.
layer_rules:
  cli:     [adapter, domain, service]
  service: [domain]
  adapter: [domain, service]
  domain:  []
```

Patterns under `layers` are module-relative Go import patterns
(`pkg/...` matches the package and all sub-packages). `layer_rules`
entries are strict allow-lists: any dependency outside the list is a
violation.

### 3.2 Package-local overlay fragments

Any package can add architecture metadata in `.arch/overlay.yaml` next
to its generated model files. These fragments are composed with the
root `archai.yaml` by `diagram generate`, `overlay check`, `serve`, and
`target lock`.

Example:

```yaml
# internal/service/.arch/overlay.yaml

aggregates:
  generation_service:
    root: Service

configs:
  - GenerateOptions
  - ComposeOptions
```

Plain type names are package-local. In the example above, `Service`
resolves to `github.com/example/app/internal/service.Service`.
Module-relative type refs are also accepted:

```yaml
configs:
  - internal/domain.WriteOptions
```

Fragments may also add `layers` or `layer_rules`, but the recommended
shape is to keep global dependency policy in root `archai.yaml` and put
package-owned domain/config metadata in package fragments. `aggregates`
attach a domain root to a package for browser grouping. `configs`
declare configuration bundles surfaced in the browser's *Configs* view.

For a real example, see [`archai.yaml`](../archai.yaml) at the root of
this repo. Archai is self-hosted: this repository keeps generated
per-package `.arch/` files and a locked `self-hosted` target produced by
the local binary.

Useful maintenance commands for this repository:

```bash
make archai-generate   # refresh per-package D2/YAML and combined docs
make archai-baseline   # refresh artifacts and lock target self-hosted
make archai-check      # overlay check + diff + validate
make archai-smoke      # CLI smoke on the archai repo itself
```

### 3.3 `.arch/` and targets on disk

Archai writes all generated artifacts under per-package `.arch/`
directories and under `.arch/targets/` at the project root:

```
.arch/targets/
├── CURRENT                      # plain text file containing the active target id
├── v1/
│   ├── meta.yaml                # id, description, created_at, ...
│   ├── overlay.yaml             # copy of archai.yaml at lock time
│   ├── overlays/                # package-local overlay fragments
│   │   └── internal/service/overlay.yaml
│   └── model/
│       ├── internal/service/pub.yaml
│       ├── internal/service/internal.yaml
│       └── ...
└── v2/
    └── ...
```

Per-package `.arch/` folders contain the `pub.d2`, `internal.d2`, and
(when generated with `--format yaml`) `pub.yaml` / `internal.yaml`
files. A package-owned `.arch/overlay.yaml` is source metadata, not a
generated model file.

### 3.4 `.gitignore` guidance

Decide per repo whether the current-model D2 files are artifacts or
source of truth. A typical pattern:

```gitignore
# Regenerated on every `archai diagram generate` — ignore.
**/.arch/pub.d2
**/.arch/internal.d2
**/.arch/pub.yaml
**/.arch/internal.yaml
```

Keep `.arch/targets/` **checked in** — that is your locked
architectural baseline and what `archai diff` / `archai validate`
compare against. Keep `archai.yaml` checked in. If you use
package-local overlays, keep `**/.arch/overlay.yaml` checked in too.

### 3.5 Bounded contexts

Bounded contexts are an optional DDD-style overlay on top of layers and
aggregates. They let you group aggregates into named domain areas and
declare context-map relationships between them.

Add a `bounded_contexts:` block to `archai.yaml`:

```yaml
# archai.yaml

bounded_contexts:
  model_core:
    description: "Core domain model and operations"
    aggregates:
      - domain
      - model_ops

  io_adapters:
    description: "I/O adapters for Go source, YAML, and D2"
    aggregates:
      - source_adapter
    downstream:
      - model_core

  serving:
    description: "HTTP server and MCP adapter"
    aggregates:
      - runtime
      - transport
    downstream:
      - model_core
      - io_adapters

  cli_entry:
    description: "CLI entry point and command wiring"
    aggregates:
      - cli
    downstream:
      - serving
      - io_adapters
```

**Schema reference**

| Field          | Type             | Required | Description |
|----------------|------------------|----------|-------------|
| `description`  | string           | no       | Human-readable purpose of the context. |
| `aggregates`   | list of strings  | no       | Aggregate names (declared in `aggregates:`) that belong to this context. |
| `upstream`     | list of strings  | no       | Contexts this context depends on (consumes). |
| `downstream`   | list of strings  | no       | Contexts that depend on this one (consumers). You may declare the relationship from either side — archai reads both. |
| `relationship` | string           | no       | Optional context-map pattern label. Common values: `shared-kernel`, `customer-supplier`, `conformist`, `acl`, `open-host`. |

Aggregates and bounded contexts are both optional — you can use layers
alone, aggregates alone, or the full three-tier model.

### 3.6 CI integration

The minimum useful gate is `archai overlay check` (layer rules) and
`archai validate` (drift from the active target). Example GitHub
Actions step:

```yaml
- name: Install archai
  run: go install github.com/kgatilin/archai/cmd/archai@latest

- name: Layer rules
  run: archai overlay check

- name: Architecture drift
  run: archai validate
```

Both commands exit non-zero on failure, so CI will fail the job. Use
`archai validate --format json` when you want structured output for
downstream tools.

---

## 4. Architecture browser

`archai serve --http :PORT` runs a long-running daemon that keeps an
in-memory model of the project, watches the filesystem with fsnotify,
and serves the browser UI on the given address.

```bash
archai serve --http :8080
# open http://localhost:8080
```

Other flags:

- `--root PATH` — project root (defaults to `.`).
- `--mcp-stdio` — also expose the model via MCP over stdio (see
  [§6](#6-agent-integration-mcp)).
- `--debug` — verbose per-event logging.

### 4.1 Views

| Route                 | View             | What it shows |
|-----------------------|------------------|---------------|
| `/`                   | Dashboard        | Project summary — module, layer counts, package/type counts, active target, drift status. |
| `/layers`             | Layers           | The layer map from `archai.yaml` with package counts per layer and an allowed-dependencies grid. Red cells are layer-rule violations in the current code. |
| `/packages`           | Packages         | Flat list of all packages with layer tag, counts, and import-path search. |
| `/packages/{path}`    | Package detail   | Interfaces, structs, functions, methods, and dependencies for one package. Links to types and bounded context. |
| `/types/{pkg}.{type}` | Type detail      | Fields/methods of a struct or interface, implementers/implementations, inbound references. |
| `/configs`            | Configs          | Config bundles declared in `archai.yaml` (empty when no `configs:` entries). |
| `/bc`                 | Domain           | Bounded context catalog with an interactive context-map graph (visible only when `bounded_contexts:` is declared). |
| `/bc/{name}`          | BC detail        | Aggregates, upstream/downstream peer contexts, and member packages for one bounded context. |
| `/targets`            | Targets          | All locked targets, which one is CURRENT, created_at, description. |
| `/diff`               | Diff             | Structured diff between current code and the active target — color-coded: green = added, red = removed, amber = modified. |
| `/search`             | Global search    | Packages, types, and functions by name substring. |

Screenshots live under [`docs/screenshots/`](screenshots/) — see the
README there for the expected filenames.

### 4.2 Reading the diff colors

The diff view groups changes by kind and operation:

- **Added** (green) — symbol exists in current code but not in the
  target.
- **Removed** (red) — symbol exists in the target but not in current
  code.
- **Modified** (amber) — signature, fields, or methods changed.

The same structure is returned by `archai diff --format yaml|json` and
by the MCP `diff` tool, so UI, CLI, and agents all see the same
changes.

---

## 5. Editor integration

### 5.1 VS Code — `tasks.json`

Add to `.vscode/tasks.json`:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "archai: generate diagrams",
      "type": "shell",
      "command": "archai diagram generate ./...",
      "problemMatcher": []
    },
    {
      "label": "archai: overlay check",
      "type": "shell",
      "command": "archai overlay check",
      "problemMatcher": []
    },
    {
      "label": "archai: validate",
      "type": "shell",
      "command": "archai validate",
      "problemMatcher": []
    }
  ]
}
```

Bind `archai: validate` to a keystroke via
**File → Preferences → Keyboard Shortcuts** (`workbench.action.tasks.runTask`)
and run it before you commit. The D2 preview is best handled by the
official D2 VS Code extension.

### 5.2 GoLand — External Tools

**Settings → Tools → External Tools → +**:

| Field              | Value                                               |
|--------------------|-----------------------------------------------------|
| Name               | `archai generate`                                   |
| Program            | `archai`                                            |
| Arguments          | `diagram generate ./...`                            |
| Working directory  | `$ProjectFileDir$`                                  |

Repeat for `overlay check`, `diff`, `validate`, and `sequence`. Assign
keymaps under **Settings → Keymap → External Tools**.

---

## 6. Agent integration (MCP)

Archai exposes its model to MCP clients via `archai serve --mcp-stdio`.
The same daemon serves the browser UI, so you can run both together:

```bash
# Long-running HTTP daemon (UI + /api/mcp/* JSON API).
archai serve --http :8080

# In another terminal — or as the MCP client's command — run the
# stdio thin client. It will discover or auto-start the HTTP daemon
# above and proxy every tools/call to it.
archai serve --mcp-stdio
```

For the full MCP / server reference (operational modes, HTTP API,
tool examples, agent workflows, UI vs CLI vs MCP boundaries), see
[`mcp-server-guide.md`](mcp-server-guide.md).

### 6.1 Claude Code — `.mcp.json`

Place at the repo root:

```json
{
  "mcpServers": {
    "archai": {
      "command": "archai",
      "args": ["serve", "--mcp-stdio", "--root", "."]
    }
  }
}
```

### 6.2 Codex CLI — `config.toml`

```toml
[mcp_servers.archai]
command = "archai"
args    = ["serve", "--mcp-stdio", "--root", "."]
```

### 6.3 MCP tools

The daemon advertises eleven tools (defined in
`internal/adapter/mcp/tools.go`):

| Tool                      | Purpose                                                                 |
|---------------------------|-------------------------------------------------------------------------|
| `extract`                 | Return the full extracted Go model. Optional `paths` filter.            |
| `list_packages`           | Minimal per-package summary (path, name, layer, counts).                |
| `get_package`             | Full `PackageModel` for one package (`path` required).                  |
| `lock_target`             | Freeze the current in-memory model as `.arch/targets/<id>/`.            |
| `list_targets`            | List locked targets.                                                    |
| `set_current_target`      | Write `.arch/targets/CURRENT`.                                          |
| `diff`                    | Structured diff of current model vs a target (`target` defaults to CURRENT). |
| `apply_diff`              | Apply a YAML patch onto a target snapshot (`patch_yaml` required).      |
| `validate`                | `{ok, violations: [...]}` — same drift as `archai validate`.            |
| `list_bounded_contexts`   | List all bounded contexts from `archai.yaml` with aggregates and relationships. |
| `get_bounded_context`     | Full detail for one BC by `name`: aggregates, upstream/downstream peers, member packages. |

### 6.4 Example agent prompts

- *"Use archai `list_packages` to find every package in the `adapter`
  layer, then `get_package` on each to summarise its responsibilities."*
- *"Call archai `diff` and explain the drift in plain English, grouped
  by package."*
- *"Run archai `validate` before I push — if `ok: false`, paste the
  violations and suggest the smallest fix."*
- *"Propose a refactor of `internal/service`: call `get_package`, draft
  the new shape, then call `lock_target` with id `refactor-service` so
  I can review the snapshot."*

---

## 7. Typical workflows

### 7.1 Onboarding to an unfamiliar codebase

1. `archai diagram generate ./...` — emit per-package D2.
2. `archai serve --http :8080` — open the dashboard, skim *Layers* to
   see the overall shape, drill into *Packages* for entry points.
3. `archai sequence <pkg>.<Type>.<Method>` on the main request entry
   point to understand the call flow.

### 7.2 Refactor against a locked target

1. Decide the target shape and write it into `archai.yaml` / the
   existing model.
2. `archai target lock v-next --description "post-refactor shape"`.
3. `archai target use v-next`.
4. Keep editing. Run `archai diff` (or the *Diff* view) to see what is
   still missing or wrong.
5. When `archai validate` exits `0`, you are done.

### 7.3 Enforcing architecture in CI

1. Commit `archai.yaml` with `layers` and `layer_rules`.
2. Commit `.arch/targets/<id>/` for your baseline and an
   `.arch/targets/CURRENT` pointer.
3. Add `archai overlay check` and `archai validate` to the pipeline
   (see [§3.5](#35-ci-integration)).

### 7.4 Exploring code with an agent

1. Run `archai serve --mcp-stdio` (add `--http :8080` if you also want
   the UI).
2. Register it in `.mcp.json` / `config.toml` (see [§6](#6-agent-integration-mcp)).
3. Ask the agent questions grounded in the real model —
   `list_packages`, `get_package`, `diff`, `validate`.

---

## Coming soon

The browser views listed in [§4.1](#41-views) are all wired to the
server. Future milestones will polish the UI and add richer interaction
— see the tracking issues in
[`docs/roadmap.md`](roadmap.md) and the open
[milestone issues](https://github.com/kgatilin/archai/issues).

---

## References

- [`archai.yaml`](../archai.yaml) — real overlay used by archai itself.
- [`docs/roadmap.md`](roadmap.md) — milestone plan.
- [`docs/mcp-server-guide.md`](mcp-server-guide.md) — full MCP and
  `archai serve` reference for agents.
- [`docs/d2guide.md`](d2guide.md) — D2 diagram notation reference.
- [`docs/architecture.d2`](architecture.d2) / [`docs/arch-composed.d2`](arch-composed.d2)
  — generated diagrams of archai itself.
