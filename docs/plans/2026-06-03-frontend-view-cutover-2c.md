# Frontend View Cutover — Plan 2c (comments → store; finish the cutover)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Migrate the last local island — **comments** (markers / pending comment / active marker / seeding) — into the store via the Plan-1 `commentsSlice`, plus a small DRY consolidation of duplicated types. After this, **all** app state lives in the store; `App.tsx` holds only DOM-imperative refs (canvas ref, pan/wheel/anchoring) and pure view derivations.

**Architecture:** Marker seeding becomes a pure `seedMarkers(graph, laid)` derivation + a `commentsSeed` effect that seeds **once per loaded graph** (on the first `LayoutComputed` after a graph loads), so user-added comments persist. `markers`/`pendingComment`/`activeMarkerId` are read via `useStore`; `startComment`/`submitComment`/marker clicks/canvas-clear dispatch events the `commentsSlice` reducer already handles. The marker-card scroll stays a small local DOM scroll (imperative, like pan).

**Tech Stack:** React 18, the Plan-1/2a/2b store/effects/adapters, Vitest+jsdom, Playwright e2e (the comment-popover + canvas specs gate comments).

**Reference:** `docs/plans/2026-06-03-frontend-view-cutover-2b.md` (Plan 2b, complete). The `commentsSlice` (`CommentStarted`/`CommentSubmitted`/`CommentCancelled`/`MarkerActivated`/`CanvasCleared`) already exists and is unit-tested from Plan 1 — this plan WIRES it. Run from `/Users/forkiy/Projects/archai/web`; binaries directly. Commit onto `poc/arch-review-ui`.

---

## Context

Plan 2a put data/layout/semantic state in the store; Plan 2b added zoom + navigation scroll. Comments stayed local because `seedMarkers` needs laid geometry. Plan 2c finishes the cutover. Old local behavior: markers were seeded from `graph.comments` near laid geometry and **re-seeded whenever `laid` changed** (`App.tsx` `useEffect(() => setMarkers(seedMarkers), [seedMarkers])`), which wiped user-added comments on every re-layout. Now that Plan 2b made navigation (`ChangeActivated`/`TreeFocusRequested`) a layout trigger, re-seed-on-every-layout would wipe user comments on every tree/change click — clearly wrong. So the `commentsSeed` effect **seeds once per loaded graph** (first `LayoutComputed` after a graph loads): markers position from the first proper layout and user-added comments persist. This is a small, intentional improvement over the old quirk (the e2e doesn't depend on re-seed-on-relayout).

**Verification model:** tsc clean + vitest suite (incl. harness smoke) green + Playwright **e2e** green (comment-popover open/submit, marker rendering, canvas-clear). Run from `web/`.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `web/src/domain/derive.ts` | Modify | Add pure `seedMarkers(graph, laid): Marker[]` (moved from App's `seedMarkers` useMemo). |
| `web/src/domain/events.ts` | Modify | Add `{ type: 'MarkersSeeded'; markers: Marker[] }`. |
| `web/src/domain/update.ts` | Modify | Handle `MarkersSeeded` in `commentsSlice`. |
| `web/src/effects/comments.ts` | Create | `createCommentsSeedEffect()` — on `LayoutComputed`, dispatch `MarkersSeeded(seedMarkers(graph, laid))`. |
| `web/src/effects/comments.test.ts` | Create | Effect re-seeds on `LayoutComputed`. |
| `web/src/effects/index.ts` | Modify | Add the seed effect to `createEffects`. |
| `web/src/App.tsx` | Modify | Comments from the store; handlers dispatch; remove local marker state + seed memo. |
| `web/src/components/Tree.tsx` | Modify | Re-export `TreeFocusTarget` from `domain/events` (DRY). |

---

## Task 1: `seedMarkers` pure derivation

**Files:** Modify `web/src/domain/derive.ts`; modify `web/src/domain/derive.test.ts`.

- [ ] **Step 1 — add the failing test** to `web/src/domain/derive.test.ts` (append; `graph()` helper exists). Import `seedMarkers` (add to the existing import from `./derive`):
```ts
describe('seedMarkers', () => {
  it('positions a comment marker beside its host component using laid geometry', () => {
    const g = graph({
      comments: [{ id: 'cm1', target: { type: 'component', id: 'a' }, body: 'hi' }],
    });
    const laid = { ...g, components: g.components.map((c) => (c.id === 'a' ? { ...c, x: 100, y: 200, w: 220 } : c)) };
    const markers = seedMarkers(g, laid);
    expect(markers).toHaveLength(1);
    expect(markers[0]).toMatchObject({ id: 'seed-0', n: 1, target: { type: 'component', id: 'a' }, body: 'hi' });
    expect(markers[0].x).toBe(100 + 220 + 8); // host.x + host.w + 8
    expect(markers[0].y).toBe(200 - 10); // host.y - 10
  });

  it('falls back to a default offset when the target/host is not laid out', () => {
    const g = graph({ comments: [{ id: 'cm1', target: { type: 'component', id: 'ghost' }, body: 'x' }] });
    const markers = seedMarkers(g, null);
    expect(markers).toHaveLength(1);
    expect(markers[0].x).toBe(80); // 80 + 0*130
    expect(markers[0].y).toBe(30); // 30 + (0%2)*40
  });
});
```

- [ ] **Step 2 — run, verify FAIL:** `./node_modules/.bin/vitest run src/domain/derive.test.ts -t "seedMarkers"` → `seedMarkers is not a function`.

- [ ] **Step 3 — add to `web/src/domain/derive.ts`** (add the `Marker` + `ComponentDef` imports as needed: `import type { UIGraph, Diff } from '../types';` already exists — extend to also import `Component as ComponentDef` from `'../types'`; and `import type { Marker } from './state';`). Add:
```ts
/**
 * Seed comment markers from `graph.comments`, positioned beside their host
 * component using laid geometry (falls back to a staggered default offset).
 * Moved verbatim from App's seedMarkers useMemo.
 */
export function seedMarkers(graph: UIGraph, laid: UIGraph | null): Marker[] {
  const laidComponents = laid?.components ?? graph.components;
  const laidEdges = laid?.edges ?? graph.edges;

  return graph.comments.map((cm, i) => {
    let host: ComponentDef | undefined = laidComponents.find((c) => c.id === cm.target.id);
    if (!host) {
      host = laidComponents.find(
        (c) =>
          c.internals.some(
            (it) =>
              it.id === cm.target.id || (it.members ?? []).some((mm) => mm.id === cm.target.id)
          ) || c.ports.some((p) => p.id === cm.target.id)
      );
    }
    if (!host && cm.target.type === 'edge') {
      const edge = laidEdges.find((e) => e.id === cm.target.id);
      if (edge) host = laidComponents.find((c) => c.id === edge.from);
    }

    let x = 80 + i * 130;
    let y = 30 + (i % 2) * 40;
    if (host && host.x != null && host.y != null && host.w != null) {
      x = host.x + host.w + 8;
      y = host.y - 10;
    }

    return { id: `seed-${i}`, n: i + 1, x, y, target: cm.target, body: cm.body, author: '@you', when: '2m' };
  });
}
```

- [ ] **Step 4 — run, verify PASS:** `./node_modules/.bin/vitest run src/domain/derive.test.ts`.

- [ ] **Step 5 — tsc + suite + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run
git add web/src/domain/derive.ts web/src/domain/derive.test.ts
git commit -m "feat(web): seedMarkers pure derivation (Plan 2c)"
```

---

## Task 2: `MarkersSeeded` event + reducer

**Files:** Modify `web/src/domain/events.ts`, `web/src/domain/update.ts`, `web/src/domain/update.test.ts`.

- [ ] **Step 1 — add the failing test** to `web/src/domain/update.test.ts` (append to the `describe('update — comments slice', ...)` block; `withGraph()` + the `Marker` shape are available):
```ts
  it('MarkersSeeded replaces the markers array', () => {
    const markers = [{ id: 'seed-0', n: 1, x: 1, y: 2, target: { type: 'component', id: 'a' }, body: 'b', author: '@you', when: '2m' }];
    const s = update(withGraph(), { type: 'MarkersSeeded', markers });
    expect(s.markers).toBe(markers);
  });
```

- [ ] **Step 2 — run, verify FAIL:** `./node_modules/.bin/vitest run src/domain/update.test.ts -t "MarkersSeeded"` (the event isn't handled / isn't in the union → TS error or no-op).

- [ ] **Step 3 — add the event** to `web/src/domain/events.ts`. In the comments section of the `Event` union add:
```ts
  | { type: 'MarkersSeeded'; markers: Marker[] }
```
And add the import at the top: `import type { Marker } from './state';` (alongside the existing imports).

- [ ] **Step 4 — handle it in `web/src/domain/update.ts`** `commentsSlice` (add a case before `default`):
```ts
    case 'MarkersSeeded':
      return { ...state, markers: event.markers };
```

- [ ] **Step 5 — run, verify PASS:** `./node_modules/.bin/vitest run src/domain/update.test.ts`.

- [ ] **Step 6 — tsc + suite + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run
git add web/src/domain/events.ts web/src/domain/update.ts web/src/domain/update.test.ts
git commit -m "feat(web): MarkersSeeded event + reducer (Plan 2c)"
```

---

## Task 3: comments seed effect

**Files:** Create `web/src/effects/comments.ts` + `web/src/effects/comments.test.ts`; modify `web/src/effects/index.ts`.

- [ ] **Step 1 — write the failing test** `web/src/effects/comments.test.ts`:
```ts
import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import { createCommentsSeedEffect } from './comments';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 10, y: 20, w: 100 }],
  edges: [],
  comments: [{ id: 'cm1', target: { type: 'component', id: 'a' }, body: 'hi' }],
};
const laid = { ...graph };
const stateReady = (): AppState => ({ ...initialState, graph, geometry: { laid, status: 'ready', error: null } });

describe('createCommentsSeedEffect', () => {
  it('on the first LayoutComputed for a graph, dispatches MarkersSeeded with seeded markers', () => {
    const dispatch = vi.fn();
    createCommentsSeedEffect()({ type: 'LayoutComputed', laid }, stateReady, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledTimes(1);
    const call = dispatch.mock.calls[0][0];
    expect(call.type).toBe('MarkersSeeded');
    expect(call.markers).toHaveLength(1);
    expect(call.markers[0]).toMatchObject({ id: 'seed-0', target: { type: 'component', id: 'a' } });
  });

  it('seeds only ONCE per graph (a later LayoutComputed for the same graph does not re-seed)', () => {
    const dispatch = vi.fn();
    const effect = createCommentsSeedEffect();
    effect({ type: 'LayoutComputed', laid }, stateReady, dispatch as (e: Event) => void);
    effect({ type: 'LayoutComputed', laid }, stateReady, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledTimes(1); // user-added comments persist across re-layouts
  });

  it('ignores non-LayoutComputed events and does nothing without a graph', () => {
    const dispatch = vi.fn();
    const effect = createCommentsSeedEffect();
    effect({ type: 'ThemeToggled' }, stateReady, dispatch as (e: Event) => void);
    effect({ type: 'LayoutComputed', laid }, () => initialState, dispatch as (e: Event) => void);
    expect(dispatch).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2 — run, verify FAIL:** `./node_modules/.bin/vitest run src/effects/comments.test.ts` → `Failed to resolve import "./comments"`.

- [ ] **Step 3 — write `web/src/effects/comments.ts`:**
```ts
import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { UIGraph } from '../types';
import { seedMarkers } from '../domain/derive';

/**
 * Seeds comment markers from graph.comments ONCE per loaded graph — on the first
 * `LayoutComputed` after a graph loads, so positions use real laid geometry and
 * user-added comments persist across re-layouts (navigation now re-lays out).
 */
export function createCommentsSeedEffect(): Effect<AppState, Event> {
  let seededGraph: UIGraph | null = null;
  return (event, getState, dispatch) => {
    if (event.type !== 'LayoutComputed') return;
    const state = getState();
    if (!state.graph || state.graph === seededGraph) return;
    seededGraph = state.graph;
    dispatch({ type: 'MarkersSeeded', markers: seedMarkers(state.graph, state.geometry.laid) });
  };
}
```

- [ ] **Step 4 — register it in `web/src/effects/index.ts`.** Add the import `import { createCommentsSeedEffect } from './comments';` and append `createCommentsSeedEffect()` to the returned array in `createEffects`:
```ts
  return [
    createLoadEffect(ports.graphSource),
    createLayoutEffect(ports.layout),
    createViewportEffect(ports.viewport),
    createCommentsSeedEffect(),
  ];
```

- [ ] **Step 5 — run + tsc + suite + commit:**
```
./node_modules/.bin/vitest run src/effects/comments.test.ts
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run
git add web/src/effects/comments.ts web/src/effects/comments.test.ts web/src/effects/index.ts
git commit -m "feat(web): comments seed effect (re-seed markers on layout) (Plan 2c)"
```

> Note: `MarkersSeeded` fires once per graph, on the first `LayoutComputed`, right after the layout effect dispatches it. Both run within the same dispatch cascade; the store's single-notification batching means the view sees graph + laid + seeded markers in one render.

---

## Task 4: wire comments into `App.tsx`

Gate: tsc clean + harness smoke green + e2e (comment-popover + canvas) green.

**Files:** Modify `web/src/App.tsx`.

### Transformation spec

**4.1 — Selectors.** In `AppContent`, ADD near the other selectors:
```tsx
  const markers = useStore((s) => s.markers);
  const pendingComment = useStore((s) => s.pendingComment);
  const activeMarkerId = useStore((s) => s.ui.activeMarkerId);
```
REMOVE: `const [pendingComment, setPendingComment] = useState(...)`, `const [activeMarkerId, setActiveMarkerId] = useState(...)`, the `seedMarkers` useMemo, `const [markers, setMarkers] = useState(seedMarkers)`, and the `useEffect(() => setMarkers(seedMarkers), [seedMarkers])`. (`commentTargets` useMemo over `markers` STAYS — it now reads the selector.)

**4.2 — `startComment`** keeps its DOM coord math but dispatches instead of `setPendingComment`. Replace the final line `setPendingComment({ target, x, y });` with:
```tsx
    dispatch({ type: 'CommentStarted', target, anchor: { x, y } });
```
(The reducer stores `pendingComment = { target, x: anchor.x, y: anchor.y }`.)

**4.3 — `submitComment`** → dispatch (the reducer appends the marker + clears pending + sets activeMarkerId). Replace the whole function body with:
```tsx
  const submitComment = (text: string) => {
    dispatch({ type: 'CommentSubmitted', text });
  };
```

**4.4 — `handleCanvasClick`** → `CanvasCleared` (the reducer clears focus + pendingComment + activeMarkerId). Replace the dispatch + local clears with:
```tsx
  const handleCanvasClick = () => {
    if (didPanRef.current) {
      didPanRef.current = false;
      return;
    }
    dispatch({ type: 'CanvasCleared' });
  };
```

**4.5 — `handleMarkerCardClick`** → dispatch `MarkerActivated`, keep the local DOM scroll to the marker:
```tsx
  const handleMarkerCardClick = (marker: Marker) => {
    dispatch({ type: 'MarkerActivated', id: marker.id });
    if (canvasWrapRef.current) {
      canvasWrapRef.current.scrollTo({
        left: (PAN_MARGIN + marker.x) * zoom - canvasWrapRef.current.clientWidth / 2,
        top: (PAN_MARGIN + marker.y) * zoom - canvasWrapRef.current.clientHeight / 2,
        behavior: 'smooth',
      });
    }
  };
```

**4.6 — JSX.** The `<PinnedMarker ... onClick={(mm) => setActiveMarkerId(mm.id)} />` → `onClick={(mm) => dispatch({ type: 'MarkerActivated', id: mm.id })}`. The `<InlinePopover ... onCancel={() => setPendingComment(null)} ... />` → `onCancel={() => dispatch({ type: 'CommentCancelled' })}`. Everything else (`markers.map`, the right-panel comment cards, `commentCount={markers.length}`) is unchanged — they read the selector values.

- [ ] **Step 1 — apply 4.1–4.6.** Confirm NO leftover `setMarkers`/`setPendingComment`/`setActiveMarkerId`/`seedMarkers` reference remains (grep — all gone). `useState`/`useMemo`/`useEffect`/`useLayoutEffect`/`useRef` may still be imported (used by the viewport island); confirm none is now unused.
- [ ] **Step 2 — tsc:** `./node_modules/.bin/tsc --noEmit` → clean.
- [ ] **Step 3 — harness smoke:** `./node_modules/.bin/vitest run src/components/__tests__/harness-smoke.harness.test.tsx` → PASS.
- [ ] **Step 4 — full suite:** `./node_modules/.bin/vitest run` → green.
- [ ] **Step 5 — commit:**
```bash
git add web/src/App.tsx
git commit -m "feat(web): cut comments over to the store (Plan 2c)"
```

---

## Task 5: DRY — consolidate `TreeFocusTarget` onto the domain

**Files:** Modify `web/src/components/Tree.tsx`.

`TreeFocusTarget` is declared in both `domain/events.ts` (canonical, used by the `Event` union) and `components/Tree.tsx`. Make Tree re-export the domain one so there's a single definition.

- [ ] **Step 1 — in `web/src/components/Tree.tsx`:** delete the local `export interface TreeFocusTarget { ... }` and replace with a re-export + local import at the top:
```tsx
export type { TreeFocusTarget } from '../domain/events';
import type { TreeFocusTarget } from '../domain/events';
```
Keep the rest of the file unchanged (`TreeProps` and the component use `TreeFocusTarget` locally — the `import type` provides the binding; the `export type` keeps `App.tsx`'s `import { Tree, TreeFocusTarget } from './components/Tree'` resolving).

- [ ] **Step 2 — verify + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run   # clean + all pass
git add web/src/components/Tree.tsx
git commit -m "refactor(web): single TreeFocusTarget definition in domain (DRY, Plan 2c)"
```

---

## Task 6: End-to-end verification

**Files:** none.

- [ ] **Step 1 — run the full e2e:** `./node_modules/.bin/playwright test` (from `web/`). Expected: 21/21 green — especially `component-card.spec.ts` "clicking a port opens the comment popover with the right target" (CommentStarted), and the canvas/diff specs. If a browser/display isn't available, record it and rely on the jsdom harness; do NOT claim e2e passed if it did not run.

---

## Done criteria (Plan 2c)

- tsc clean; vitest green; e2e green (or recorded as not-run with harness as the gate).
- `App.tsx` holds NO `useState` for comments — `markers`/`pendingComment`/`activeMarkerId` come from the store; `startComment`/`submitComment`/marker-clicks/canvas-clear dispatch events. Marker seeding is a derive + effect.
- The ONLY remaining local state in `App.tsx` is `store` (composition) and the DOM-imperative refs (`canvasWrapRef`, `pendingScrollRef`, `didPanRef`, `didInitScrollRef`); all app *data/UI* state lives in the store.

## Cutover complete

After 2c, the TEA store is the single source of truth for the entire app (data, layout, semantic, zoom, comments). Remaining optional polish (not required for the cutover, can be separate small PRs): collapse `App.tsx`/`AppContent` into smaller view components; move the composition root to `main.tsx` (requires teaching the harness to mount the composed root); import `Marker`/`PendingComment` directly from `domain/state` at call sites.
