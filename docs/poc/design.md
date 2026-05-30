# Design — POC: Architecture Review UI on top of archai

> Status: **proposed design, awaiting approval.** Date: 2026-05-30.
> Scope: proof-of-concept. Goal is "does this work end-to-end?", not production.
> Prerequisites: [`concept.md`](./concept.md), [`repo-assessment.md`](./repo-assessment.md).

## 1. Goal & success criteria

Demonstrate the concept end-to-end: **a real repository's architecture, plus a
target-vs-current diff, rendered in the hi-fi review UI** from the design handoff.

The POC succeeds when:

1. **Real data.** `archai` projects a real Go repo (archai itself) into a `UIGraph`
   JSON — bounded contexts, components, internals→members, ports, edges.
2. **Diff.** Against a locked target with at least one genuine change, the JSON marks
   the right elements `added / removed / changed`, and they appear in the CHANGES list.
3. **The UI renders it.** The standalone web app loads the JSON and shows the V4
   layout: dark IDE chrome, dot-grid canvas, BC groups, components with diff badges,
   expandable internals (auto-expanding members), ports, edges with labels.
4. **Core interactions work.** Expand/collapse, focus mode (click a component → dim
   unrelated), CHANGES↔CONTEXTS switch, collapsible side panels, theme toggle, and
   inline click-to-comment that pins a numbered marker.
5. **Verifiable in Chrome** on a local dev server.

Explicitly **out of scope** for the POC: comment/review persistence, auth, writing
review results back through MCP, multi-language demo (Go is enough), production
build/deploy, pixel-perfect auto-layout, making the L1/L2/L3 switch change the data
(cosmetic for now).

## 2. Chosen direction

A **separate web application** that consumes a new **`UIGraph` JSON projection**
emitted by archai. archai's engine stays essentially untouched; we add one thin Go
projection + a CLI command, and one new front-end app.

```
┌─────────────────────────── archai (Go, existing engine) ───────────────────────────┐
│  golang.Reader ─┐                                                                    │
│  java.Reader  ──┤→ []domain.PackageModel ─┬─ overlay.Config (BCs, layers)            │
│                 │                          └─ target snapshot ─→ diff.Compute()       │
│                 │                                                      │              │
│                 └──────────────────  NEW: uigraph projection  ◀───────┘              │
│                                              │                                       │
│                                  NEW CLI: `archai export ui` → archgraph.json         │
└──────────────────────────────────────────────│─────────────────────────────────────┘
                                                 │ (static JSON file; stretch: HTTP endpoint)
                                                 ▼
┌─────────────────────── web/ (NEW standalone app: Vite + React + TS) ─────────────────┐
│  fetch archgraph.json → layout(semantic) → V4 components (ported from hi-fi mockup)    │
│  dark IDE · dot-grid canvas · diff coloring · focus mode · click-to-comment (local)   │
│  dev server on :5173 (fallback :5174)                                                 │
└───────────────────────────────────────────────────────────────────────────────────┘
```

Why a static JSON file rather than a live daemon connection for the POC: zero CORS /
proxy friction, fully reproducible, and it makes the data contract the single thing
both sides agree on. Wiring the same projection into a `serve` HTTP endpoint is a
trivial follow-on once the contract is proven.

## 3. The data contract: `UIGraph` JSON

This is the seam. archai produces the **semantic** fields; the web app computes
**layout** fields. The semantic shape mirrors the mockup's `SCENARIO`
(`docs/poc/handoff/project/shared.jsx`) minus geometry.

```jsonc
{
  "schema": "archai.uigraph/v0",
  "pr": {                          // synthesized from git + diff summary; omit if no diff
    "title": "string",
    "branch": "string",
    "agent": "string",             // best-effort; may be "" 
    "summary": "string",
    "stats": { "added": 0, "removed": 0, "changed": 0, "comments": 0 }
  },
  "boundedContexts": [
    { "id": "ordering", "name": "Ordering" }
  ],
  "components": [
    {
      "id": "internal/service",          // stable id (package path)
      "name": "OrderService",            // short display name
      "tech": "Go",                      // language / tech tag
      "desc": "one-line doc",
      "bc": "ordering",                  // bounded-context id (or "" )
      "diff": "changed",                 // added|removed|changed|absent
      "internals": [
        {
          "id": "internal/service.Service",
          "kind": "class",               // class | iface
          "name": "Service",
          "diff": "changed",
          "members": [
            { "id": "internal/service.Service.Handle", "kind": "method", "name": "Handle(ctx)", "diff": "added" },
            { "id": "internal/service.Service.cfg",    "kind": "prop",   "name": "cfg : Config" }
          ]
        }
      ],
      "ports": [
        { "id": "internal/service:in:Reader", "side": "left",  "kind": "in",  "name": "Reader",   "diff": "absent" },
        { "id": "internal/service:out:domain","side": "right", "kind": "out", "name": "use domain","diff": "absent" }
      ]
    }
  ],
  "edges": [
    { "id": "e:service->domain", "from": "internal/service", "to": "internal/domain",
      "fromPort": "internal/service:out:domain", "toPort": "internal/domain:in:...",
      "label": "uses", "diff": "absent" }
  ],
  "comments": []                    // UI-local for the POC; archai emits []
}
```

Rules:
- `diff` ∈ `{added, removed, changed}` or omitted/`absent`. Derived by mapping
  `diff.Op` → `OpAdd:added, OpRemove:removed, OpChange:changed` onto the element named
  by `diff.Change.Path`.
- All ids are stable and derivable from archai paths (reuse `archgraph` id scheme
  where possible). Layout fields (`x/y/w/h/wx/hx`, port `y`) are **absent** in
  archai's output and filled in by the web app.

## 4. archai side (Go)

### 4.1 New projection — `internal/adapter/uigraph`
- `Project(models []domain.PackageModel, cfg *overlay.Config, d *diff.Diff) (UIGraph, error)`
- Maps packages→components, interfaces/structs→internals, methods/fields→members,
  exported interfaces→`in` ports, outbound dependencies→`out` ports, dependencies→edges,
  overlay bounded contexts→`boundedContexts` (fallback: layers, else a single default).
- A small **diff-path resolver**: parse `diff.Change.Path`
  (`pkg`, `pkg.Type`, `pkg.Type.Member`) and stamp the `diff` flag on the matching
  node. This is the one piece with real logic → covered by unit tests (TDD).
- JSON tags only; no behavior on the model. Pure function, no I/O.

### 4.2 New CLI — `archai export ui`
```
archai export ui [paths...] [--target <id>] [-o out.json] [--overlay archai.yaml]
```
- Reads current model (YAML specs if present, else Go reader) — reuse existing
  `loadCurrentModel` plumbing.
- If `--target` (or active CURRENT) resolves, compute `diff.Compute(current, target)`
  and synthesize the `pr` block (git branch/commit via existing helpers; stats from
  the diff). Otherwise emit a no-diff graph.
- Project → marshal → write to `-o` (default stdout).
- Wiring lives in `cmd/archai/`, consistent with `newExtractCmd()`.

No other archai code changes. (Stretch, not in POC: a `/api/ui-graph` serve handler
reusing `uigraph.Project`.)

## 5. Web app side (`web/`)

### 5.1 Stack
- **Vite + React + TypeScript.** Standard, fast HMR, easy `:5173` dev server.
- Reuse `docs/poc/handoff/project/hifi-tokens.css` almost verbatim (the design tokens
  + component CSS are the bulk of the visual fidelity).
- Fonts via the same Google Fonts import (Inter + JetBrains Mono).

### 5.2 Structure
```
web/
  index.html
  package.json
  vite.config.ts            # server.port 5173 (strictPort:false → 5174 fallback)
  src/
    main.tsx
    App.tsx                 # ports HFV4: appbar, PR header, 3-pane stage
    types.ts                # UIGraph TS types (mirror of the contract)
    data/
      load.ts               # fetch /archgraph.json (+ fixture fallback)
      fixture.ts            # the mockup SCENARIO as a guaranteed-rich fallback
    layout/
      layout.ts             # semantic UIGraph → positioned graph (deterministic)
    components/             # ported 1:1 from hifi-shared.jsx primitives
      AppBar.tsx  PrHeader.tsx  Tree.tsx  BCGroups.tsx
      EdgeLayer.tsx  Component.tsx  Legend.tsx  CanvasToolbar.tsx
      InlinePopover.tsx  PinnedMarker.tsx  ChangesPanel.tsx
    state/                  # useExpansion, useFocus, comments (local) — from hifi-v4.jsx
    styles/hifi-tokens.css
  public/
    archgraph.json          # written by `archai export ui` (gitignored; sample committed)
```

### 5.3 Layout (the one novel front-end piece)
The mockup hand-placed coordinates. The POC computes them **deterministically**
(no ELK yet — keep it tractable and predictable):
- Bounded contexts laid out left→right (wrap into rows); each BC sized to fit its
  components in an inner grid.
- Components placed in a grid within their BC; `w/h` from collapsed defaults, `wx/hx`
  grown to fit internals when expanded.
- Internals placed in a grid inside an expanded component; member height adds to `hx`.
- Ports stacked on the left wall (`in`) / right wall (`out`), `y` spaced evenly.
ELK/dagre is a **stretch** only if the deterministic layout reads poorly.

### 5.4 Behavior parity with V4 (from `hifi-v4.jsx`)
Expand/collapse components & internals (members auto-expand on component expand);
focus mode (click component → highlight it + neighbors, dim the rest, fade unrelated
edges); CHANGES↔CONTEXTS left tabs (default CHANGES when a diff exists, else CONTEXTS);
collapsible left/right panels; theme toggle (dark default); inline click-to-comment
popover that pins a numbered marker and lists in the right rail; animated edge-flow
dots; legend + toolbar. Comments are **local React state** — no backend.

## 6. Data flow (the demo)
```
1. (engine)  archai diagram generate ./internal/... --format yaml      # current specs
2. (engine)  archai target lock baseline                               # freeze target
3. (change)  introduce one real architectural change in the working tree
4. (engine)  archai export ui ./internal/... --target baseline -o web/public/archgraph.json
5. (ui)      cd web && npm run dev   → open http://localhost:5173
6. (verify)  Chrome: architecture renders; the change shows added/removed/changed,
             listed in CHANGES; click-to-comment pins a marker.
```
A committed **sample `archgraph.json`** (and the in-app fixture) guarantees the UI is
demonstrable even before step 1–4 are run.

## 7. Testing
- **Go (production code → TDD per repo rules):** unit-test `uigraph.Project` mapping
  and the diff-path resolver (golden UIGraph from a small fixture model; verify
  `added/removed/changed` land on the right nodes). Wire-level test for `export ui`.
- **Web (POC → light):** a couple of `vitest` tests for `layout.ts` (no overlaps /
  containment) and the contract types; otherwise manual verification in Chrome.

## 8. Risks & mitigations
| Risk | Mitigation |
|---|---|
| Auto-layout looks messy | Deterministic grid first; ELK only if needed; components collapsed by default. |
| Diff-path → node resolver is fiddly | Isolate + unit-test it; it's the only non-trivial Go logic. |
| Porting 995 lines of CSS + JSX is large but mechanical | Parallelize across subagents; reuse `hifi-tokens.css` verbatim. |
| Port semantics (in/out) unclear on real code | Start from exported interfaces (in) + outbound deps (out); refine after seeing archai-on-archai. |
| Real diff is empty/boring | Demo script makes one deliberate change; fixture guarantees a rich diff regardless. |

## 9. Build phasing (feeds the plan)
- **Phase A — contract & engine:** TS types + Go `uigraph` projection + `export ui` CLI + tests; produce a real `archgraph.json`.
- **Phase B — app shell:** Vite+React+TS scaffold, tokens CSS, data load + fixture, layout, app frame (appbar/PR header/3-pane).
- **Phase C — canvas:** BC groups, Component (collapsed/expanded/internals/members/ports), EdgeLayer, legend/toolbar, diff coloring.
- **Phase D — interactions:** focus mode, CHANGES/CONTEXTS, collapsible panels, click-to-comment + pins, theme toggle.
- **Phase E — integrate & verify:** point the app at the real archai-exported JSON; verify in Chrome; capture a screenshot.

Phases A and B/C can start in parallel (the contract decouples them).
