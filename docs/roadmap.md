# Architecture as Data — Roadmap

## Vision

Make a Go project's architecture a first-class artifact: extract it from code,
store it as YAML, intentionally evolve it via small diffs against a locked
target, and explore it through a polished web browser.

Two outcomes:

1. **Validation tool** — generate, lock, diff, and validate architecture state
   from CLI and MCP.
2. **Architecture browser** — opinionated web UI to navigate the project's
   structure, configs, dependencies, and call graph.

## Concepts

### Logical model

Three sources merge into a single in-memory model:

1. **Extracted** — from Go source: interfaces, structs, functions, type defs,
   methods, fields, constants, exported vars, sentinel errors, dependencies,
   and *which struct implements which interface*.
2. **Overlay** — from a hand-edited `archai.yaml` at the project root: layers,
   layer rules, named aggregates, and pointers to types that should be treated
   as *configs*.
3. **Annotations** — stereotypes and tags inferred from doc comments.

### Files on disk

| Path                          | Purpose                                              | Source     |
| ----------------------------- | ---------------------------------------------------- | ---------- |
| `archai.yaml`                 | Root overlay: layers, rules, named aggregates, configs | Manual   |
| `.arch/pub.yaml`              | Per-package public-API model                         | Generated  |
| `.arch/internal.yaml`         | Per-package full model                               | Generated  |
| `.arch/targets/<id>/`         | Locked target snapshot                               | Generated  |
| `.arch/targets/CURRENT`       | Active target id                                     | Generated  |

YAML is the canonical interchange format. D2 stays as a presentation format.

### Targets and diffs

- `archai target lock <id>` — snapshot the current extracted+overlay model as
  target `<id>`.
- `archai target list | show <id> | use <id> | delete <id>` — manage targets.
- `archai diff` — structured YAML diff of current model vs active target.
- `archai diff apply <patch.yaml>` — apply a small diff onto the active target
  (the workflow for "we agreed to add these public methods").
- `archai validate` — exit non-zero if current ≠ active target.

Multiple targets exist so that parallel branches don't conflict on a single
shared target file.

### Server

- `archai serve` — long-running daemon.
- Watches the project directory via `fsnotify`, incrementally re-parses
  changed packages.
- Exposes:
  - **MCP** tools (`extract`, `lock_target`, `diff`, `validate`,
    `apply_diff`, `list_packages`, `get_package`, `get_sequence`, ...)
  - **HTTP** for the architecture browser.

CLI commands and MCP tools are 1:1 — same operations, two surfaces.

## Milestones

| #   | Title                                              | Outcome                                                         | Tracking |
| --- | -------------------------------------------------- | --------------------------------------------------------------- | -------- |
| M1  | YAML I/O                                           | Per-package YAML adapter (read+write), roundtrip-safe           | [#1](https://github.com/kgatilin/archai/issues/1) |
| M2  | Extended domain model                              | Constants, vars, errors, interface implementations              | [#2](https://github.com/kgatilin/archai/issues/2) |
| M3  | Project overlay (`archai.yaml`)                    | Layers, layer rules, named aggregates, config markers           | [#3](https://github.com/kgatilin/archai/issues/3) |
| M4  | Targets, diff, validate (CLI)                      | Lock / list / use targets, structured diffs, validation command | [#4](https://github.com/kgatilin/archai/issues/4) |
| M5  | Server + MCP                                       | `archai serve`, fsnotify watcher, MCP tool surface              | [#5](https://github.com/kgatilin/archai/issues/5) |
| M6  | Call graph + sequence                              | Static method-to-method extraction, per-method sequence views   | [#6](https://github.com/kgatilin/archai/issues/6) |
| M7  | Architecture browser                               | Polished HTML site served from `archai serve`                   | [#7](https://github.com/kgatilin/archai/issues/7) |

End state: a `validate`-able architecture model + a usable web browser for it.

## Browser sketch (M7)

Top nav: **Dashboard** / **Layers** / **Packages** / **Configs** / **Targets**
/ **Diff** / **Search**.

### Dashboard

- Module name, locked-target id, drift status badge (matches / drifted →
  link to Diff view).
- Layer map (compact).
- Counts: packages, interfaces, structs, configs.
- Recent activity: last extract, last lock, last validate.

### Layers

- One box per declared layer with packages listed inside.
- Inter-layer edges, color-coded by allowed/violation per layer rules.
- Click a layer → filtered package list.

### Packages

- Tree by directory, filter by layer / stereotype.
- Symbol search.

### Package detail

Tabs:

- **Overview** — D2-rendered SVG, package doc, layer badge, stereotype tags.
- **Public API** — interfaces, structs, functions, constants, errors.
- **Internal** — full implementation.
- **Dependencies** — out (what this package uses) and in (who uses it),
  hyperlinked across packages.
- **Configs** — any types in this package marked as configs in the overlay.

### Type detail

- Fields, methods, doc.
- For interfaces: list of implementations (hyperlinked).
- For structs: list of interfaces this struct implements.
- "Used by" panel.
- Sequence: starting from each public method, expandable call tree.

### Config catalog

- All types tagged as configs across the whole project.
- Per-config: field table (name, type, default if derivable, doc) + an
  example YAML/JSON instance synthesized from zero values.

### Diff view

- Side-by-side current vs active target.
- Per-package and per-symbol delta.
- Color-coded: added / removed / changed.
- Filter by kind (interface / struct / method / field / dep).

### Targets view

- List of saved targets with metadata.
- Switch active.
- Diff between any two targets.

## Tech choices (browser)

- Go-only stack: `html/template` + `embed` for assets, no node/build step.
- HTMX for partial updates (no JS framework).
- D2 → SVG via `oss.terrastruct.com/d2` (already a dependency).
- CSS: minimal, hand-rolled or a small framework like Pico.

## Out of scope (for now)

- Cross-repo / SDK boundary tracking across modules.
- Build tags / GOOS-specific API surfaces.
- `// Deprecated:` marker tracking and breaking-change classification.
- Generic type-parameter detail in `TypeRef`.
- Dynamic-dispatch resolution in sequence diagrams (follow real impl chains).

## Working with this roadmap

Each milestone has a tracking issue (epic) on GitHub. Per-feature plans land
in `docs/features/<slug>/plan.md` following the existing convention. The
roadmap is the single source of truth for milestone scope; epics link back
here.
