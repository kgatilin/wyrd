# archai · Architecture Review UI (POC)

A standalone web app that renders a repository's **architecture** and a
**target-vs-current architectural diff** in the hi-fi "V4" review surface from the
Claude Design handoff. It is a front-end on top of the existing `archai` engine — see
[`../docs/poc/`](../docs/poc/) for the concept, repo assessment, design, and plan.

> Proof-of-concept. The goal is "does this work end-to-end?", not production polish.

## Run it

```bash
cd web
npm install
npm run dev        # Vite; port 5173, auto-falls-back if busy (e.g. 5174)
```

Open the URL Vite prints (e.g. http://localhost:5174/).

## Where the data comes from

The app loads a `UIGraph` JSON via `src/data/load.ts` in this order:

1. `public/archgraph.json` — a live export (gitignored; you generate it, see below)
2. `public/archgraph.sample.json` — a committed real export of archai's own packages
3. `src/data/fixture.ts` — the designed scenario from the mockup (rich diff), used as a
   guaranteed fallback so the diff UI is always demonstrable

`showDiff` is on when the graph has a `pr` block: the left panel defaults to **CHANGES**
and diff coloring shows. With no `pr` (the sample) it defaults to **CONTEXTS**.

## Generate a live UIGraph from a repo

The engine side is the `archai export ui` command (package `internal/adapter/uigraph`):

```bash
# from the repo root
go build -o bin/archai ./cmd/archai

# current architecture, no diff (CONTEXTS view)
./bin/archai export ui ./internal/... -o web/public/archgraph.json

# target-vs-current diff (CHANGES view): lock a baseline, change code, re-export
./bin/archai diagram generate ./internal/... --format yaml
./bin/archai target lock baseline
#   …make an architectural change in the working tree…
./bin/archai export ui ./internal/... --target baseline -o web/public/archgraph.json
```

Diff direction: `export ui` runs `diff.Compute(current, target)`, then the projection
inverts it to the reviewer's perspective — an element present in **current** (the
agent's after-state) but not in **target** (the baseline) renders as **added**.

## The contract

`src/types.ts` is the TypeScript mirror of the Go `uigraph.UIGraph`. archai emits the
**semantic** fields (bounded contexts → components → internals → members + ports;
edges; per-element `diff`; PR meta). The app's `src/layout/layout.ts` adds **geometry**
(`x/y/w/h/wx/hx`) deterministically — except when geometry is already present (the
fixture), which it preserves.

## What works (verified in Chrome)

- Real archai-on-archai data renders (sample): CONTEXTS tree, dot-grid canvas,
  components with real doc descriptions, expandable internals → members.
- Diff review (fixture): AGENT-PR header (+/−/~ stats), CHANGES list, green/red/amber
  diff fills on components/internals/members/ports/edges, NEW/MOD/DEL badges, animated
  edge flow, numbered comment pins, comments rail.
- Interactions: expand/collapse, focus mode (click → dim unrelated), change-row →
  focus + center, CHANGES↔CONTEXTS switch, collapsible panels, inline click-to-comment
  with pinned numbered markers, dark/light theme toggle.

## Known POC limitations (next steps)

- **Auto-layout** of real (non-pre-positioned) graphs is a simple deterministic grid;
  large/expanded components can overlap. Next: ELK/dagre (already vendored by archai's
  own UI) for collision-free layout.
- **Bounded-context resolution** on real data can fall back to an "all" group when
  overlay BCs don't map cleanly to packages; the projection's `resolveBC` should be
  tightened.
- **Edge/port-level diff flags** are a deliberate POC non-goal (component / internal /
  member diffs are wired and tested).
- Comments are **local state only** (no persistence) and "Submit review" is a stub.
- The wider `archai` module currently fails `go build ./...` because the private
  `github.com/kgatilin/archmotif` dependency is unreachable in this environment; this
  is pre-existing and unrelated to the POC. `go build ./cmd/archai` (what the POC needs)
  is unaffected.
