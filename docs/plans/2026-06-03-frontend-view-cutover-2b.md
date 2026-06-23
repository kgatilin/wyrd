# Frontend View Cutover — Plan 2b (viewport → store: zoom + navigation scroll)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Move the `zoom` value into the store (`ui.zoom`) and route **navigation scroll** (scroll-to-component on change/tree drill-in) through a real `domViewport` adapter + the viewport effect, replacing the local `scrollToComponent` + `setTimeout` glue with scroll-on-next-`LayoutComputed`. The DOM-imperative scroll-anchoring (pan-drag, wheel/fit `pendingScrollRef`, init-scroll, fit corner-scroll) stays local — per the design's "60fps DOM stays imperative" concession.

**Architecture:** `createAppStore()` now builds a real `domViewport` adapter (implements `ViewportPort`, bound to the canvas element + a `getZoom`/`getCanvasDimensions` accessor) and returns it alongside the store so `App` can bind its canvas ref. The viewport effect is rewritten: `ChangeActivated`/`TreeFocusRequested` record a pending scroll target and the scroll fires on the next `LayoutComputed` (those events now re-lay out — fixed in commit `d52a29e`); `ScrollToComponentRequested` scrolls immediately. `zoom` is read via `useStore` and changed via `dispatch(ZoomChanged)`; the wheel handler reads the live zoom imperatively via a new `useStoreApi()`.

**Tech Stack:** React 18, the Plan-1/2a store/effects/adapters, Vitest+jsdom, Playwright e2e (the canvas spec is the gate for pan/zoom/scroll).

**Reference:** `docs/plans/2026-06-03-frontend-view-cutover-2a.md` (Plan 2a, complete). Run from `web/`; binaries directly (`./node_modules/.bin/...`). Commit onto `poc/arch-review-ui`.

---

## Context

Plan 2a put data/layout/semantic state in the store; the viewport (zoom/pan/scroll) and comments stayed local. Plan 2b migrates the viewport's **logical** part (zoom value + navigation scroll) into the store. The genuinely DOM-imperative anchoring math (which manipulates `scrollLeft/scrollTop` in a layout effect after the zoom-driven re-render) stays local — moving it would fight React's render timing for no benefit. Comments stay local (Plan 2c).

**Verification model:** tsc clean + vitest suite (incl. harness smoke) green + the Playwright **e2e canvas spec** green (it asserts grab cursor, oversized sizer, pan-drag scrolls + clears `.panning`, pan-drag doesn't clear focus, zoom buttons + ctrl+wheel change the zoom label/scale). The context-tree spec (row click focuses + scrolls) gates navigation scroll.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `web/src/view/viewportConstants.ts` | Create | Shared `PAN_MARGIN`, `ZOOM_MIN`, `ZOOM_MAX`, `ZOOM_STEP` (used by `App` + `domViewport`). |
| `web/src/adapters/domViewport.ts` | Create | `createDomViewport()` → `ViewportPort` + `bind(handle)`; `scrollToComponent` (smooth-scroll to a laid component) and `fitZoom`. |
| `web/src/adapters/domViewport.test.ts` | Create | Drives the adapter against a jsdom element + fake handle. |
| `web/src/effects/viewport.ts` | Rewrite | Scroll-after-relayout: navigation events queue a pending scroll consumed on the next `LayoutComputed`; `ScrollToComponentRequested` immediate; `ZoomFitRequested` → fit. |
| `web/src/effects/viewport.test.ts` | Rewrite | Covers the deferred-scroll, immediate-scroll, and fit paths. |
| `web/src/runtime/react.tsx` | Modify | Add `useStoreApi()` returning the store instance (imperative reads in event handlers). |
| `web/src/runtime/createAppStore.ts` | Modify | Build `createDomViewport()`, wire into effects, return `{ store, viewport }`. |
| `web/src/runtime/createAppStore.test.ts` | Modify | Adjust for the `{ store, viewport }` return shape. |
| `web/src/App.tsx` | Modify | `zoom` from store; toolbar/wheel/fit dispatch `ZoomChanged`; bind `viewport` to the canvas; drop local `scrollToComponent` + `setTimeout` glue (navigation scroll via the effect). Anchoring/pan/init-scroll/fit-corner stay local. |

---

## Task 1: Extract shared viewport constants

**Files:** Create `web/src/view/viewportConstants.ts`; modify `web/src/App.tsx`.

- [ ] **Step 1 — create the constants module:**
```ts
// web/src/view/viewportConstants.ts
/** Empty slack (unscaled px) reserved around the diagram so the canvas feels
 *  "borderless"; all viewport↔content scroll math adds this. */
export const PAN_MARGIN = 1200;
export const ZOOM_MIN = 0.4;
export const ZOOM_MAX = 2;
export const ZOOM_STEP = 0.1;
```

- [ ] **Step 2 — use them in `App.tsx`:** add `import { PAN_MARGIN, ZOOM_MIN, ZOOM_MAX, ZOOM_STEP } from './view/viewportConstants';` and DELETE the four local `const PAN_MARGIN = 1200;` / `const ZOOM_MIN = 0.4;` / `const ZOOM_MAX = 2;` / `const ZOOM_STEP = 0.1;` declarations in `AppContent`. No behavior change.

- [ ] **Step 3 — verify + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run   # clean + all pass (80)
git add web/src/view/viewportConstants.ts web/src/App.tsx
git commit -m "refactor(web): extract shared viewport constants (Plan 2b)"
```

---

## Task 2: `domViewport` adapter

**Files:** Create `web/src/adapters/domViewport.ts` + `web/src/adapters/domViewport.test.ts`.

- [ ] **Step 1 — write the failing test:**
```ts
// web/src/adapters/domViewport.test.ts
import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { createDomViewport } from './domViewport';
import { PAN_MARGIN } from '../view/viewportConstants';

const laid: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 100, y: 200, w: 220, h: 86 }],
  edges: [],
  comments: [],
};

function fakeEl() {
  const calls: any[] = [];
  const el = {
    clientWidth: 800,
    clientHeight: 600,
    scrollTo: (opts: any) => calls.push(opts),
  } as unknown as HTMLElement;
  return { el, calls };
}

describe('createDomViewport', () => {
  it('does nothing when unbound', () => {
    const vp = createDomViewport();
    expect(() => vp.scrollToComponent('a', laid)).not.toThrow();
    expect(vp.fitZoom(laid)).toBeNull();
  });

  it('scrollToComponent centers the laid component (accounting for PAN_MARGIN + zoom)', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind({ el, getZoom: () => 1, getCanvasDimensions: () => ({ width: 1000, height: 800 }) });
    vp.scrollToComponent('a', laid);
    expect(calls).toHaveLength(1);
    // left = (PAN_MARGIN + x + w/2)*zoom - clientWidth/2
    expect(calls[0].left).toBe((PAN_MARGIN + 100 + 110) * 1 - 400);
    expect(calls[0].top).toBe((PAN_MARGIN + 200 + 43) * 1 - 300);
    expect(calls[0].behavior).toBe('smooth');
  });

  it('scrollToComponent is a no-op for an unknown id', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind({ el, getZoom: () => 1, getCanvasDimensions: () => ({ width: 1000, height: 800 }) });
    vp.scrollToComponent('nope', laid);
    expect(calls).toHaveLength(0);
  });

  it('fitZoom returns a clamped fit ratio from the bound element + canvas dimensions', () => {
    const vp = createDomViewport();
    const { el } = fakeEl();
    vp.bind({ el, getZoom: () => 1, getCanvasDimensions: () => ({ width: 4000, height: 4000 }) });
    // min(800/4000, 600/4000, 1) = 0.15 → rounded 0.15, clamped to >= ZOOM_MIN(0.4) → 0.4
    expect(vp.fitZoom(laid)).toBe(0.4);
  });
});
```

- [ ] **Step 2 — run, verify FAIL:** `./node_modules/.bin/vitest run src/adapters/domViewport.test.ts` → `Failed to resolve import "./domViewport"`.

- [ ] **Step 3 — write the adapter:**
```ts
// web/src/adapters/domViewport.ts
import type { UIGraph } from '../types';
import type { ViewportPort } from '../domain/ports';
import { PAN_MARGIN, ZOOM_MIN } from '../view/viewportConstants';

/** What the DOM viewport needs from the live React tree to compute scroll/zoom. */
export interface ViewportHandle {
  el: HTMLElement; // the .hf-canvas-wrap scroller
  getZoom: () => number;
  getCanvasDimensions: () => { width: number; height: number };
}

export interface DomViewport extends ViewportPort {
  /** Called by App on mount to attach the live canvas element; null on unmount. */
  bind(handle: ViewportHandle | null): void;
}

/**
 * ViewportPort backed by the real DOM. Created in createAppStore (so the viewport
 * effect can call it) and bound to the canvas by App. Smooth-scroll math mirrors
 * the old local scrollToComponent (content shifted by PAN_MARGIN, scaled by zoom).
 */
export function createDomViewport(): DomViewport {
  let handle: ViewportHandle | null = null;
  return {
    bind(h) {
      handle = h;
    },
    scrollToComponent(id: string, laid: UIGraph) {
      if (!handle) return;
      const c = laid.components.find((cc) => cc.id === id);
      if (!c) return;
      const zoom = handle.getZoom();
      const x = c.x ?? 0;
      const y = c.y ?? 0;
      const w = c.w ?? 220;
      const h = c.h ?? 86;
      handle.el.scrollTo({
        left: (PAN_MARGIN + x + w / 2) * zoom - handle.el.clientWidth / 2,
        top: (PAN_MARGIN + y + h / 2) * zoom - handle.el.clientHeight / 2,
        behavior: 'smooth',
      });
    },
    fitZoom(_laid: UIGraph): number | null {
      if (!handle) return null;
      const dims = handle.getCanvasDimensions();
      const fit = Math.min(handle.el.clientWidth / dims.width, handle.el.clientHeight / dims.height, 1);
      return Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    },
  };
}
```
> `fitZoom` returns the ratio only (no scroll). App's fit button keeps its local corner-scroll (it needs React's layout-effect timing), so it does NOT route through `ZoomFitRequested`; `fitZoom`/`ZoomFitRequested` remain available for future use.

- [ ] **Step 4 — run, verify PASS:** `./node_modules/.bin/vitest run src/adapters/domViewport.test.ts`.

- [ ] **Step 5 — tsc + suite + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run
git add web/src/adapters/domViewport.ts web/src/adapters/domViewport.test.ts
git commit -m "feat(web): domViewport adapter (scroll-to-component + fit, bound to canvas)"
```

---

## Task 3: Rewrite the viewport effect (scroll-after-relayout)

**Files:** Rewrite `web/src/effects/viewport.ts` + `web/src/effects/viewport.test.ts`.

- [ ] **Step 1 — rewrite the test** `web/src/effects/viewport.test.ts`:
```ts
import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';
import { createViewportEffect } from './viewport';

const laid: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 0, y: 0, w: 10, h: 10 }],
  edges: [],
  comments: [],
};
const withLaid = (): AppState => ({ ...initialState, geometry: { laid, status: 'ready', error: null } });

describe('createViewportEffect', () => {
  it('defers ChangeActivated scroll to the next LayoutComputed', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    const effect = createViewportEffect(port);
    // navigation event: records the pending target, does NOT scroll yet
    effect({ type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } }, withLaid, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
    // the relayout that ChangeActivated triggered now lands → scroll fires on final geometry
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
    // a second LayoutComputed (unrelated) does not scroll again
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledTimes(1);
  });

  it('defers TreeFocusRequested scroll to the next LayoutComputed', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    const effect = createViewportEffect(port);
    effect({ type: 'TreeFocusRequested', target: { componentId: 'a' } }, withLaid, vi.fn());
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
  });

  it('ScrollToComponentRequested scrolls immediately', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    createViewportEffect(port)({ type: 'ScrollToComponentRequested', id: 'a' }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
  });

  it('does nothing before layout exists', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    createViewportEffect(port)({ type: 'ScrollToComponentRequested', id: 'a' }, () => initialState, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
  });

  it('on ZoomFitRequested, dispatches ZoomChanged with the fit zoom', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn().mockReturnValue(0.5) };
    const dispatch = vi.fn();
    createViewportEffect(port)({ type: 'ZoomFitRequested' }, withLaid, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledWith({ type: 'ZoomChanged', zoom: 0.5 });
  });
});
```

- [ ] **Step 2 — run, verify FAIL** (new expectations): `./node_modules/.bin/vitest run src/effects/viewport.test.ts`.

- [ ] **Step 3 — rewrite `web/src/effects/viewport.ts`:**
```ts
import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';

/**
 * Routes navigation scroll through the ViewportPort. `ChangeActivated` /
 * `TreeFocusRequested` re-lay out (they may expand a component), so their scroll
 * is DEFERRED to the next `LayoutComputed` — landing on the final geometry. A bare
 * `ScrollToComponentRequested` scrolls immediately. `ZoomFitRequested` → fit zoom.
 */
export function createViewportEffect(port: ViewportPort): Effect<AppState, Event> {
  let pendingScrollId: string | null = null;
  return (event, getState, dispatch) => {
    const state = getState();
    const laid = state.geometry.laid;

    switch (event.type) {
      case 'LayoutComputed':
        if (pendingScrollId && state.geometry.laid) {
          port.scrollToComponent(pendingScrollId, state.geometry.laid);
          pendingScrollId = null;
        }
        return;
      case 'LayoutFailed':
        pendingScrollId = null; // don't scroll against stale geometry on failure
        return;
      case 'ChangeActivated':
        pendingScrollId = event.change.cmp; // scroll after the re-layout this triggers
        return;
      case 'TreeFocusRequested':
        pendingScrollId = event.target.componentId;
        return;
      case 'ScrollToComponentRequested':
        if (laid) port.scrollToComponent(event.id, laid);
        return;
      case 'ZoomFitRequested': {
        if (!laid) return;
        const z = port.fitZoom(laid);
        if (z != null) dispatch({ type: 'ZoomChanged', zoom: z });
        return;
      }
      default:
        return;
    }
  };
}
```

- [ ] **Step 4 — run, verify PASS:** `./node_modules/.bin/vitest run src/effects/viewport.test.ts`.

- [ ] **Step 5 — tsc + suite + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run
git add web/src/effects/viewport.ts web/src/effects/viewport.test.ts
git commit -m "feat(web): viewport effect scroll-after-relayout (Plan 2b)"
```

---

## Task 4: `useStoreApi` + `createAppStore` returns `{ store, viewport }`

**Files:** Modify `web/src/runtime/react.tsx`, `web/src/runtime/createAppStore.ts`, `web/src/runtime/createAppStore.test.ts`.

- [ ] **Step 1 — add `useStoreApi` to `web/src/runtime/react.tsx`** (export the store instance for imperative reads in event handlers). After `useDispatch`, add:
```tsx
/** The store instance, for imperative reads (`getState`) inside event handlers
 *  where a `useStore` selector subscription is not wanted (e.g. wheel-zoom). */
export function useStoreApi(): AppStore {
  return useStoreInstance();
}
```
(`useStoreInstance` already exists and throws outside the provider.)

- [ ] **Step 2 — update `createAppStore.ts`** to build the real viewport and return both:
```ts
import { createStore } from './store';
import type { AppStore } from './react';
import { initialState } from '../domain/state';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import { createElkLayout } from '../adapters/elkLayout';
import { createHttpGraphSource } from '../adapters/httpGraphSource';
import { createDomViewport, type DomViewport } from '../adapters/domViewport';

/**
 * App-level composition root. Builds the real elk + http + DOM-viewport adapters,
 * wires them into the store, and returns the store plus the viewport (App binds
 * the viewport to its canvas element on mount).
 */
export function createAppStore(): { store: AppStore; viewport: DomViewport } {
  const viewport = createDomViewport();
  const effects = createEffects({
    graphSource: createHttpGraphSource(),
    layout: createElkLayout(),
    viewport,
  });
  const store = createStore(initialState, update, effects);
  return { store, viewport };
}
```

- [ ] **Step 3 — update `createAppStore.test.ts`** for the new return shape: change `const store = createAppStore();` to `const { store } = createAppStore();` (everything else in that test stays; the unbound viewport is harmless — the load+layout assertions are unaffected).

- [ ] **Step 4 — verify + commit:**
```
./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run   # clean + all pass
git add web/src/runtime/react.tsx web/src/runtime/createAppStore.ts web/src/runtime/createAppStore.test.ts
git commit -m "feat(web): useStoreApi + createAppStore returns {store, viewport} (Plan 2b)"
```

---

## Task 5: Wire zoom + navigation scroll into `App.tsx`

The verification gate is **tsc clean + harness smoke green + e2e canvas/context-tree green**.

**Files:** Modify `web/src/App.tsx`.

### Transformation spec

**5.1 — Bootstrap.** Change `const [store] = useState(() => createAppStore());` to:
```tsx
  const [{ store, viewport }] = useState(() => createAppStore());
```
Keep the `GraphRequested` mount effect. Pass `viewport` down: change `<AppRoot />` to `<AppRoot viewport={viewport} />`, `AppRoot`'s signature to `function AppRoot({ viewport }: { viewport: DomViewport })`, and `<AppContent graph={graph} />` to `<AppContent graph={graph} viewport={viewport} />`. Update `AppContent`'s signature to `function AppContent({ graph, viewport }: { graph: UIGraph; viewport: DomViewport })`. Add imports: `import { createAppStore } from './runtime/createAppStore';` is already there — add `import { StoreProvider, useStore, useDispatch, useStoreApi } from './runtime/react';` (add `useStoreApi`) and `import type { DomViewport } from './adapters/domViewport';`.

**5.2 — zoom from the store.** In `AppContent`: REMOVE `const [zoom, setZoom] = useState(1);`. ADD `const zoom = useStore((s) => s.ui.zoom);` (near the other selectors) and `const storeApi = useStoreApi();`. Replace `zoomBy`:
```tsx
  const zoomBy = (delta: number) => {
    const z = storeApi.getState().ui.zoom;
    const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round((z + delta) * 100) / 100));
    if (next !== z) dispatch({ type: 'ZoomChanged', zoom: next });
  };
```
Replace `fitZoom` (keep the local corner-scroll via `pendingScrollRef`, just dispatch instead of `setZoom`):
```tsx
  const fitZoom = () => {
    const wrap = canvasWrapRef.current;
    if (!wrap) {
      dispatch({ type: 'ZoomChanged', zoom: 1 });
      return;
    }
    const fit = Math.min(wrap.clientWidth / canvasDimensions.width, wrap.clientHeight / canvasDimensions.height, 1);
    const z = Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    pendingScrollRef.current = { left: PAN_MARGIN * z - 40, top: PAN_MARGIN * z - 40 };
    dispatch({ type: 'ZoomChanged', zoom: z });
  };
```

**5.3 — wheel handler.** In the wheel `useEffect`, the `setZoom((z) => {...})` becomes an imperative read + dispatch (the effect has `[]` deps; read live zoom via `storeApi`):
```tsx
    const onWheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      const rect = wrap.getBoundingClientRect();
      const cx = e.clientX - rect.left;
      const cy = e.clientY - rect.top;
      const step = e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP;
      const z = storeApi.getState().ui.zoom;
      const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round((z + step) * 100) / 100));
      if (next === z) return;
      const contentX = (wrap.scrollLeft + cx) / z - PAN_MARGIN;
      const contentY = (wrap.scrollTop + cy) / z - PAN_MARGIN;
      pendingScrollRef.current = {
        left: (contentX + PAN_MARGIN) * next - cx,
        top: (contentY + PAN_MARGIN) * next - cy,
      };
      dispatch({ type: 'ZoomChanged', zoom: next });
    };
```
Add `storeApi` and `dispatch` to this effect's dependency array (both are stable, so it still runs once): `}, [storeApi, dispatch]);`. The `pendingScrollRef` layout effect on `[zoom]` is UNCHANGED (it now reads the store-backed `zoom`).

**5.4 — bind the viewport to the canvas.** Add a layout effect (after `canvasWrapRef` is declared) that binds the adapter to the live element:
```tsx
  useLayoutEffect(() => {
    viewport.bind({
      el: canvasWrapRef.current!,
      getZoom: () => storeApi.getState().ui.zoom,
      getCanvasDimensions: () => canvasDimensions,
    });
    return () => viewport.bind(null);
  }, [viewport, storeApi, canvasDimensions]);
```
(`canvasDimensions` is in the dep array so the bound accessor reflects re-layouts. `canvasWrapRef.current` is set by render before this layout effect runs.)

**5.5 — navigation scroll via the effect (drop the local glue).** DELETE the local `scrollToComponent` function. Simplify the handlers to dispatch only:
```tsx
  const goToChange = (ch: ChangeEntry) => {
    dispatch({ type: 'ChangeActivated', change: ch });
  };
  const focusFromTree = (t: TreeFocusTarget) => {
    dispatch({ type: 'TreeFocusRequested', target: t });
  };
```
(The viewport effect now scrolls on the next `LayoutComputed`; `expanded` is no longer needed by `focusFromTree`.)

**5.6 — KEEP local & unchanged:** the pan-drag effect, the `pendingScrollRef` layout effect, the init-scroll layout effect, `canvasDimensions`, and the comments island. `handleMarkerCardClick` still scrolls locally (comments → Plan 2c) — leave it, but it now reads the store-backed `zoom` (no change needed). `startComment`'s coord math reads `zoom` (store-backed; no change needed). The JSX (sizer/transform/CanvasToolbar `zoom={zoom}`) is unchanged — `zoom` is now the selector value.

- [ ] **Step 1 — apply 5.1–5.6** to `App.tsx`.
- [ ] **Step 2 — tsc:** `./node_modules/.bin/tsc --noEmit` → clean. Fix any leftover `setZoom`/`scrollToComponent` reference (must be gone) or unused import.
- [ ] **Step 3 — harness smoke:** `./node_modules/.bin/vitest run src/components/__tests__/harness-smoke.harness.test.tsx` → PASS.
- [ ] **Step 4 — full suite:** `./node_modules/.bin/vitest run` → all green.
- [ ] **Step 5 — e2e (the real gate for viewport):** `./node_modules/.bin/playwright test e2e/canvas.spec.ts e2e/context-tree.spec.ts` → green (grab cursor, oversized sizer, pan-drag scrolls + clears `.panning`, pan-drag keeps focus, zoom buttons + ctrl+wheel change the label/scale; tree row click focuses + scrolls). If a browser/display isn't available, record it and rely on the harness; do NOT claim e2e passed if it did not run.
- [ ] **Step 6 — commit:**
```bash
git add web/src/App.tsx
git commit -m "feat(web): zoom into the store + navigation scroll via domViewport (Plan 2b)"
```

---

## Done criteria (Plan 2b)

- tsc clean; vitest green; e2e canvas + context-tree green (or recorded as not-run with harness as the gate).
- `zoom` lives in `ui.zoom`; `App` has no `const [zoom, setZoom]`. Toolbar/wheel/fit dispatch `ZoomChanged`.
- Navigation scroll (change/tree drill-in) goes through `domViewport` via the viewport effect, landing on post-relayout geometry; the local `scrollToComponent` + `setTimeout` glue is gone.
- Pan-drag, wheel/fit anchoring, init-scroll remain local DOM mechanics, working unchanged.

## Deferred to Plan 2c

Comments → store (`commentsSlice`): `markers`/`pendingComment`/`activeMarkerId`, `seedMarkers` as a derive+effect, `startComment`/`submitComment` → dispatch, `handleCanvasClick` → `CanvasCleared`, `handleMarkerCardClick` → a marker-scroll path. Then collapse `App.tsx`/`AppContent`, move composition to `main.tsx`, consolidate `TreeFocusTarget`/`Marker`/`PendingComment` imports onto `domain/*`, retire any remaining transitional shims.
