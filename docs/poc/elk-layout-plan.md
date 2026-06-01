# Plan: ELK Layout for the Architecture Review UI

> Interface-first plan. The implementer writes the implementation via TDD. This
> file lists contracts + acceptance criteria only — no implementation code.

## Context

archai POC web app (`web/`, Vite + React 18 + TypeScript) renders a `UIGraph`
(bounded contexts → components → internals/ports, edges, diff flags) as an
architecture-review surface. Layout (geometry) is computed client-side; archai
emits only semantics. We are replacing the hand-rolled grid (`layout.ts`) and
the straight bezier edges (`EdgeLayer.tsx`) with **ELK** (layered algorithm +
orthogonal edge routing) to fix two bugs: (1) component boxes overlap, (2) edges
cut straight through boxes.

## Design (locked)

- **Engine:** `elkjs` (`elk.bundled.js`, main thread), client-side. archai's
  semantic contract is unchanged.
- **Algorithm:** ELK `layered` + `elk.edgeRouting: ORTHOGONAL`, options mirrored
  from the proven preset in `internal/adapter/http/assets/graph.js`
  (`spacing.nodeNode: 42`, `crossingMinimization: LAYER_SWEEP`,
  `nodePlacement: BRANDES_KOEPF`, `hierarchyHandling: INCLUDE_CHILDREN`).
- **Mapping:** BC → compound ELK node; component → child node (size = current
  collapsed *or* expanded size); ports → ELK ports (`in`→WEST/left,
  `out`→EAST/right); edges → ELK edges between ports. Internals are **not** in
  ELK — they are stacked inside the component box by the existing renderer.
- **Expand:** re-layout (async) whenever the expanded set changes.
- **Geometry for all:** the "preserve pre-set geometry" escape hatch is removed;
  fixture and real data both go through ELK.
- **Types change (minimal, additive):** one optional geometry field
  `Edge.points?: { x: number; y: number }[]`, filled by layout, never emitted by
  archai — mirrors the existing optional node `x/y/w/h`. No semantic change.

## Tasks

### Task: ElkLayoutEngine

Files to touch: `web/package.json`, `web/src/types.ts`, `web/src/layout/layout.ts`, `web/src/layout/layout.test.ts`

Interface:
```typescript
// types.ts — additive, optional, geometry-only (layout-filled; archai never emits it)
export interface Edge {
  // ...existing fields unchanged...
  points?: { x: number; y: number }[]; // ELK-routed polyline: start + bends + end
}

// layout.ts
export interface LayoutOptions {
  expanded: Set<string>;          // component ids currently expanded
  internalExpanded: Set<string>;  // internal ids currently expanded (affects expanded height)
}

// Async: elk.layout() returns a Promise. Returns a NEW UIGraph (input not mutated).
export function layout(graph: UIGraph, opts?: LayoutOptions): Promise<UIGraph>;
```

Observable behavior:
- Every bounded context has numeric `x, y, w, h` (absolute canvas coords).
- Every component has numeric `x, y, w, h` (absolute canvas coords).
- Every port has a numeric `y` **relative to its component** (the existing
  `Component`/`EdgeLayer` consume `port.y` as a component-relative offset — keep
  that convention so those files need no change).
- Every edge has `points` with ≥ 2 entries, routed orthogonally (start at source
  port, bends, end at target port).
- Component size handed to ELK = collapsed `(w, h)` unless the component id is in
  `opts.expanded`, in which case expanded size
  (`computeExpandedHeight(cmp, opts.internalExpanded)` for height — import from
  `../state/hooks`).
- Each component's box lies fully inside its bounded context's box.
- No two components overlap; no two bounded contexts overlap.
- ELK child coordinates (relative to parent) are flattened to absolute canvas
  coordinates on the returned graph.
- Deterministic: same input → same geometry.

Acceptance criteria:
- `npm test` green, with `layout.test.ts` rewritten to assert invariants (not
  fixed pixels): async/Promise return; all-numeric BC + component geometry;
  every component inside its BC box; no component–component overlap; no BC–BC
  overlap; every edge has ≥ 2 points; determinism (two runs equal).
- The old "preserves pre-set geometry (fixture data)" test is removed/replaced
  (escape hatch is gone).
- `npm run build` (tsc) passes; `elkjs` is in `dependencies`.
- This task does **not** edit `EdgeLayer.tsx` or `App.tsx`.

---

### Task: OrthogonalEdgeRendering

Files to touch: `web/src/components/EdgeLayer.tsx`

Interface: `EdgeLayer` props are unchanged. Internal `computeEdgePath` consumes
`edge.points` instead of computing a bezier.

Observable behavior:
- When `edge.points` has ≥ 2 entries: render an SVG path through those points as
  an orthogonal polyline (rounded corners acceptable); arrowhead at the end.
- All existing edge behaviors retained: per-diff arrow markers
  (`added`/`removed`/`changed`), flow-dot animation along the path
  (`offset-path` uses the generated path string), edge label near a mid point,
  invisible hit-path for click-to-comment, comment marker, focus dim/highlight.
- Fallback: if `edge.points` is absent/empty, keep the current bezier behavior so
  nothing breaks before layout has run.

Acceptance criteria:
- Edges render as orthogonal polylines that follow `edge.points`.
- Visual check (real sample export): edges no longer cut straight through
  component boxes.
- `npm run build` (tsc) passes; `npm test` green.
- This task does **not** edit `layout.ts` or `App.tsx`.

---

### Task: AsyncLayoutIntegration

Files to touch: `web/src/App.tsx`, `web/src/styles/hifi-tokens.css`

Observable behavior:
- `App` loads the **raw** graph (no layout at load time) and passes it to
  `AppContent`.
- `AppContent` computes the laid-out graph by calling
  `await layout(graph, { expanded, internalExpanded })` inside an effect keyed on
  `[graph, expanded, internalExpanded]`, holds the result in state, and renders
  all **geometry** consumers from the laid-out graph: `BCGroups`, `EdgeLayer`,
  `Component`, `seedMarkers` positioning, `canvasDimensions`, and the
  `goToChange` scroll math.
- **Semantic** consumers (derived changes, `Tree`, `showDiff`, comment seeding by
  id) may continue to read the raw graph.
- While the first layout resolves, show the existing "Loading…" state. On
  re-layout, keep the previous laid-out graph visible (no flash to empty).
- A CSS transition on component position (`.hf-cmp` `left`/`top`) makes the
  reflow on expand/collapse smooth rather than an instant jump.

Acceptance criteria:
- App renders the graph using ELK geometry (no overlapping boxes on the real
  sample export).
- Expanding/collapsing a component triggers re-layout; neighbors move so nothing
  ends up overlapping the now-taller box.
- No console errors; comment markers and change-scroll still target correct
  on-canvas positions.
- `npm run build` (tsc) passes; `npm test` green; `npm run dev` serves and the
  app renders in the browser.

## Out of scope (separate, deferred by request)

How the **inside** of a block looks (the stack of internals/members within a
component box). This plan only fixes block placement, edge routing, and the
re-layout-on-expand behavior.
