# Frontend View Cutover — Plan 2a (store as source of truth, viewport+comments local)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TEA store (built in Plan 1) the single source of truth for the app's **data, layout, and semantic interaction state**, replacing `App.tsx`'s scattered `useState`/`useEffect`/`useExpansion`/`useFocus` with `useStore`/`dispatch` — while keeping the imperative viewport (zoom/pan/scroll) and comments (markers/popover/seeding) local for now.

**Architecture:** `App` self-bootstraps the store (`createAppStore()`), dispatches `GraphRequested` on mount, and wraps the tree in `<StoreProvider>`. `AppContent` reads migrated state via `useStore` selectors and emits `dispatch(event)`. Load + layout become store effects (the old load/layout `useEffect`s are deleted). Two islands stay local and migrate in later plans: **viewport** (zoom/pan/wheel/scroll — Plan 2b) and **comments** (markers/pendingComment/seedMarkers — Plan 2c). This is a behavior-preserving refactor verified by the existing jsdom harness + Playwright e2e.

**Tech Stack:** React 18 (`useSyncExternalStore` via the Plan-1 binding), Vitest + jsdom (harness smoke), Playwright e2e, the Plan-1 store/domain/effects/adapters.

**Reference:** `docs/plans/2026-06-02-frontend-architecture-design.md` (ADR), `docs/plans/2026-06-02-frontend-architecture-impl.md` (Plan 1, complete).

---

## Context

Plan 1 built the TEA core (store, pure `update` reducer with 5 slices, ports, elk/http adapters, load/layout/viewport effects, React binding) fully tested **in isolation** — `App.tsx` still runs on the old scattered-`useState` code and does not consume any of it. Plan 2a is the first cutover: route the app's data/layout/semantic state through the store. The running app and all existing tests must stay green at every commit.

### What moves to the store in 2a
`graph` (load), `geometry.laid` (layout), `load`/`geometry` status, `ui.expanded` / `ui.internalExpanded` / `ui.internalWide`, `ui.focusId` (+ `related` via `relatedIds`), `ui.theme`, `ui.level`, `ui.leftTab`, `ui.leftCollapsed`, `ui.rightCollapsed`, `ui.activeChangeId`. Derived `changes` (via `deriveChanges`) and the context tree are already pure and just read `graph`.

### What stays LOCAL in 2a (deliberately deferred)
- **Viewport (→ Plan 2b):** `zoom` `useState`, `zoomBy`/`fitZoom`, the wheel-zoom `useEffect` + `pendingScrollRef`, the pan-drag `useEffect`, `scrollToComponent`, the init-scroll effect, `canvasWrapRef`, `PAN_MARGIN`, `didPanRef`. `ui.zoom`/`ViewportPort`/`ScrollToComponentRequested`/`ZoomFitRequested` exist in the store but are NOT wired yet (a no-op `ViewportPort` is supplied).
- **Comments (→ Plan 2c):** `markers`, `pendingComment`, `activeMarkerId`, `seedMarkers`, `startComment`, `submitComment`, marker-card handlers. (`seedMarkers` needs laid geometry; migrating it cleanly is a Plan-2c concern. The store's `commentsSlice` stays unused until then.)

### Bootstrapping constraint (why `App` builds its own store)
The jsdom harness (`testing/harness/dom-env.ts` `mountAppDom`) stubs `fetch` and renders `<App/>` **directly** — it does NOT go through `main.tsx`. So `App` must self-bootstrap the store. The store's `httpGraphSource` adapter wraps `loadGraph()` (which fetches `/archgraph.json`), so the harness's existing fetch-stub keeps feeding the store with zero harness changes. `main.tsx` stays `render(<App/>)`.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `web/src/runtime/createAppStore.ts` | Create | Factory: build elk+http adapters + no-op viewport + effects + store. The 2a composition root (App-level). |
| `web/src/runtime/createAppStore.test.ts` | Create | Asserts the factory boots and loads+lays out a graph. |
| `web/src/App.tsx` | Rewrite | Bootstrap store + `StoreProvider`; `AppContent` consumes store for migrated state; viewport+comments stay local. |
| `web/src/state/hooks.ts` | Modify | Remove now-dead `useExpanded`/`useExpansion`/`useFocus`; KEEP `computeExpandedHeight` (EdgeLayer uses it). |
| `web/src/main.tsx` | Unchanged | Stays `render(<App/>)` — App self-bootstraps. |

**Verification model (this is a behavior-preserving refactor):** each task's gate is **`./node_modules/.bin/tsc --noEmit` clean + the existing test suite green** (the jsdom harness smoke test `src/components/__tests__/harness-smoke.harness.test.tsx` exercises load/auto-expand/expand-all/diff through the real `<App/>`), plus a final **Playwright e2e** checkpoint (`canvas`, `component-card`, `context-tree`, `diff-mode`). Run from `/Users/forkiy/Projects/archai/web`; call binaries directly (`./node_modules/.bin/...`) — npx misbehaves in this shell. Commit onto branch `poc/arch-review-ui` (do NOT branch).

---

## Task 1: `createAppStore` factory (the 2a composition root)

**Files:**
- Create: `web/src/runtime/createAppStore.ts`
- Test: `web/src/runtime/createAppStore.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/runtime/createAppStore.test.ts
import { describe, it, expect } from 'vitest';
import { createAppStore } from './createAppStore';

const flush = () => new Promise((r) => setTimeout(r));

describe('createAppStore', () => {
  it('boots, and on GraphRequested loads a graph and lays it out (fixture fallback under jsdom)', async () => {
    const store = createAppStore();
    expect(store.getState().graph).toBeNull();

    store.dispatch({ type: 'GraphRequested' });
    // loadEffect → GraphLoaded → layoutEffect → LayoutComputed (real elk under jsdom)
    await flush();
    await flush();
    // elk layout is async; poll briefly until geometry lands
    for (let i = 0; i < 50 && store.getState().geometry.laid == null; i++) await flush();

    expect(store.getState().graph).not.toBeNull();
    expect(store.getState().load.status).toBe('ready');
    expect(store.getState().geometry.laid).not.toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./node_modules/.bin/vitest run src/runtime/createAppStore.test.ts`
Expected: FAIL — `Failed to resolve import "./createAppStore"`.

- [ ] **Step 3: Write the factory**

```ts
// web/src/runtime/createAppStore.ts
import { createStore } from './store';
import type { AppStore } from './react';
import { initialState } from '../domain/state';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import { createElkLayout } from '../adapters/elkLayout';
import { createHttpGraphSource } from '../adapters/httpGraphSource';
import type { ViewportPort } from '../domain/ports';

/**
 * App-level composition root (Plan 2a). Wires the real elk + http adapters into
 * the store. The viewport is a no-op for now: App keeps pan/zoom/scroll imperative
 * and local; Plan 2b replaces this with a real `domViewport` adapter.
 */
const noopViewport: ViewportPort = {
  scrollToComponent: () => {},
  fitZoom: () => null,
};

export function createAppStore(): AppStore {
  const effects = createEffects({
    graphSource: createHttpGraphSource(),
    layout: createElkLayout(),
    viewport: noopViewport,
  });
  return createStore(initialState, update, effects);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./node_modules/.bin/vitest run src/runtime/createAppStore.test.ts`
Expected: PASS. (The http adapter's `loadGraph()` has no network under jsdom and falls back to the built-in fixture; real elk lays it out.)

- [ ] **Step 5: tsc + full suite + commit**

Run: `./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run` → clean + all pass.
```bash
git add web/src/runtime/createAppStore.ts web/src/runtime/createAppStore.test.ts
git commit -m "feat(web): createAppStore factory (Plan 2a composition root)"
```

---

## Task 2: Rewrite `App.tsx` — bootstrap store + consume migrated state

This is the core cutover. `App.tsx` is rewritten so the store owns data/layout/semantic state; viewport and comments stay local. The verification gate is **tsc clean + the harness smoke test green** (it drives the real `<App/>` through load/auto-expand/expand-all/tabs/diff).

**Files:**
- Modify: `web/src/App.tsx` (full rewrite of the state wiring; rendering/JSX structure preserved)

### Transformation spec (apply ALL of these)

**2.1 — New imports.** At the top of `App.tsx`, add:
```tsx
import { createAppStore } from './runtime/createAppStore';
import { StoreProvider, useStore, useDispatch } from './runtime/react';
import { relatedIds, deriveChanges } from './domain/derive';
```
Remove: `import { useExpansion, useFocus } from './state/hooks';`. Keep `import { layout } from './layout/layout';`? NO — delete it (the layout effect now owns layout). Keep `import { loadGraph } from './data/load';`? NO — delete it (the load effect owns loading). Keep all component imports (`AppBar`, `PrHeader`, `BCGroups`, `Component`, `EdgeLayer`, `Legend`, `CanvasToolbar`, `Tree`, `TreeFocusTarget`, `ChangesPanel`, `deriveChanges`/`ChangeEntry` — NOTE: import `ChangeEntry` type from `./components/ChangesPanel` as before, but import `deriveChanges` from `./domain/derive` per above; remove the duplicate `deriveChanges` import from ChangesPanel to avoid a name clash), `InlinePopover`/`PendingComment`, `PinnedMarker`/`Marker`).

**2.2 — Bootstrap shell.** Replace the current `export default function App()` (the one with `graph`/`error`/`theme`/`level` `useState` + the `loadGraph` `useEffect`) with:
```tsx
export default function App() {
  const [store] = useState(() => createAppStore());
  useEffect(() => {
    store.dispatch({ type: 'GraphRequested' });
  }, [store]);
  return (
    <StoreProvider store={store}>
      <AppRoot />
    </StoreProvider>
  );
}

function AppRoot() {
  const theme = useStore((s) => s.ui.theme);
  const load = useStore((s) => s.load);
  const graph = useStore((s) => s.graph);

  if (load.status === 'error') {
    return (
      <div className={`hifi v4 theme-${theme}`} style={{ padding: 20 }}>
        <p style={{ color: 'var(--rem-fg)' }}>Error: {load.error}</p>
      </div>
    );
  }
  if (!graph) {
    return (
      <div className={`hifi v4 theme-${theme}`} style={{ padding: 20 }}>
        <p>Loading...</p>
      </div>
    );
  }
  return <AppContent graph={graph} />;
}
```
Delete the old `AppContentProps` interface fields for `theme`/`level`/`onLevelChange`/`onThemeToggle` — `AppContent` now takes only `{ graph }` and reads the rest from the store.

**2.3 — `AppContent` signature + selectors.** Change `function AppContent({ graph, theme, level, onLevelChange, onThemeToggle }: AppContentProps)` to `function AppContent({ graph }: { graph: UIGraph })`. Immediately inside, add the store reads + dispatch:
```tsx
  const dispatch = useDispatch();
  const theme = useStore((s) => s.ui.theme);
  const level = useStore((s) => s.ui.level);
  const expanded = useStore((s) => s.ui.expanded);
  const internalExpanded = useStore((s) => s.ui.internalExpanded);
  const internalWide = useStore((s) => s.ui.internalWide);
  const focusId = useStore((s) => s.ui.focusId);
  const leftTab = useStore((s) => s.ui.leftTab);
  const leftCollapsed = useStore((s) => s.ui.leftCollapsed);
  const rightCollapsed = useStore((s) => s.ui.rightCollapsed);
  const activeChangeId = useStore((s) => s.ui.activeChangeId);
  const laid = useStore((s) => s.geometry.laid);
  const layoutError = useStore((s) => (s.geometry.status === 'error' ? s.geometry.error : null));
  const related = useMemo(() => relatedIds(graph, focusId), [graph, focusId]);
```
> Selectors return referentially-stable values: the reducer preserves Set/object references for untouched slices, so `useSyncExternalStore` won't churn. `related` is a `useMemo` (same as the old `useFocus`).

**2.4 — Delete the old state machinery in `AppContent`:**
- DELETE the `useExpansion(graph, initialExpanded)` call and its destructure (`expanded`, `toggle`, `internalExpanded`, `internalWide`, `toggleInternalWide`, `setComponentWide`).
- DELETE `useFocus(graph)` and its destructure (`focusId`, `setFocusId`, `related`).
- DELETE the `initialExpanded` `useMemo` (the reducer seeds initial expansion on `GraphLoaded`).
- DELETE the `const [laid, setLaid] = useState(...)`, `const [layoutError, setLayoutError] = useState(...)`, and the entire `useEffect(() => { ... layout(graph, ...) ... }, [graph, expanded, internalExpanded, internalWide])` layout effect.
- DELETE the `const [leftTab, setLeftTab] = useState(...)`, `const [leftCollapsed, setLeftCollapsed] = useState(false)`, `const [rightCollapsed, setRightCollapsed] = useState(false)`, `const [activeChangeId, setActiveChangeId] = useState(...)`.
- KEEP `showDiff` but recompute from graph: `const showDiff = graph.pr != null;` (unchanged).
- KEEP `bcNameById` useMemo (unchanged).
- KEEP `const changes = useMemo(() => deriveChanges(graph), [graph]);` (now `deriveChanges` comes from `./domain/derive`).

**2.5 — KEEP local (viewport island, unchanged):** `canvasWrapRef`, `PAN_MARGIN`, `didPanRef`, the `ZOOM_*` consts, `const [zoom, setZoom] = useState(1)`, `zoomBy`, `fitZoom`, `pendingScrollRef`, the wheel-zoom `useLayoutEffect`/`useEffect`, the pan-drag `useEffect`, `scrollToComponent`, the init-scroll `useLayoutEffect`, and `canvasDimensions` `useMemo`. These are unchanged — they read the local `zoom` and the `laid` selector.

**2.6 — KEEP local (comments island, unchanged):** `const [pendingComment, setPendingComment] = useState(...)`, `const [activeMarkerId, setActiveMarkerId] = useState(...)`, `seedMarkers` useMemo, `const [markers, setMarkers] = useState(seedMarkers)`, the `useEffect(() => setMarkers(seedMarkers), [seedMarkers])`, `commentTargets`, `startComment`, `submitComment`, `handleMarkerCardClick`. Unchanged.

**2.7 — Rewrite the handlers (state → dispatch; keep local scroll glue).** Replace each handler body:

```tsx
  // goToChange: dispatch ChangeActivated (reducer sets activeChangeId+focus+expand),
  // then keep the existing local scroll-after-relayout glue.
  const goToChange = (ch: ChangeEntry) => {
    dispatch({ type: 'ChangeActivated', change: ch });
    setTimeout(() => scrollToComponent(ch.cmp), 150);
  };

  // focusFromTree: dispatch TreeFocusRequested (reducer focuses + expands on drill-in),
  // then local scroll glue (longer delay when an expand re-layout is pending).
  const focusFromTree = (t: TreeFocusTarget) => {
    const drillIn = !!(t.internalId || t.memberId);
    dispatch({ type: 'TreeFocusRequested', target: t });
    setTimeout(() => scrollToComponent(t.componentId), drillIn ? 200 : 0);
  };

  const handleSelectComponent = (cmp: ComponentType) => {
    dispatch({ type: 'ComponentSelected', id: cmp.id });
  };

  // Canvas background click: clear focus (store) + local comment UI. Skipped right
  // after a pan-drag. (Uses FocusCleared, not CanvasCleared, because comments are
  // still local in 2a — CanvasCleared would clear unused store comment fields.)
  const handleCanvasClick = () => {
    if (didPanRef.current) {
      didPanRef.current = false;
      return;
    }
    dispatch({ type: 'FocusCleared' });
    setPendingComment(null);
    setActiveMarkerId(null);
  };
```

**2.8 — Rewrite the inline callback props in JSX to dispatch:**
- `<AppBar level={level} onLevelChange={(l) => dispatch({ type: 'LevelChanged', level: l })} theme={theme} onThemeToggle={() => dispatch({ type: 'ThemeToggled' })} commentCount={markers.length} pr={graph.pr} />`
- Left collapse button: `onClick={() => dispatch({ type: 'LeftCollapsedToggled' })}`.
- Tabs: `onClick={() => dispatch({ type: 'LeftTabChanged', tab: 'changes' })}` and `... tab: 'tree' }`.
- Right collapse button: `onClick={() => dispatch({ type: 'RightCollapsedToggled' })}`.
- `<Component ... onToggleExpand={(id) => dispatch({ type: 'ComponentToggled', id })} onToggleWide={(id) => dispatch({ type: 'InternalWideToggled', id })} onSetAllWide={(componentId, wide) => dispatch({ type: 'ComponentAllWideSet', id: componentId, wide })} onSelect={handleSelectComponent} ... />` — keep all geometry/diff/focus props (`focused={focusId === c.id}`, `dimmed={!!(focusId && related && !related.has(c.id))}`, etc.) reading the new selectors.
- `<ChangesPanel ... onChangeClick={goToChange} />` and `<Tree ... activeId={focusId} onFocus={focusFromTree} />` (unchanged call sites; the handlers now dispatch).
> NOTE on `onLevelChange`/`onSetAllWide` arg names: match the existing prop signatures (`onLevelChange: (level: number) => void`, `onSetAllWide: (componentId: string, wide: boolean) => void`).

**2.9 — Loading/error placeholders inside the canvas.** The `laid == null` branch already renders "Laying out…" / `layoutError`. `layoutError` now comes from the selector in 2.3. Keep the JSX as-is.

- [ ] **Step 1: Apply the transformation** above to `web/src/App.tsx`. Remove the now-unused `useState`/`useEffect`/`useLayoutEffect` only where 2.4 says to (the viewport/comment ones stay). Ensure remaining imports are all used (`useState`, `useEffect`, `useLayoutEffect`, `useMemo`, `useRef` are all still needed by the viewport/comment islands).

- [ ] **Step 2: Type-check**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean. Common slips to fix: a leftover reference to a deleted setter (`setFocusId`, `setLeftTab`, `setLaid`, `toggle`, `setComponentWide`, `toggleInternalWide`) — every such call must have become a `dispatch(...)`; an unused import (`layout`, `loadGraph`, `useExpansion`, `useFocus`) — delete it.

- [ ] **Step 3: Run the harness smoke test (the behavior gate)**

Run: `./node_modules/.bin/vitest run src/components/__tests__/harness-smoke.harness.test.tsx`
Expected: PASS — the diagram loads with 5 components, `OrderService` is auto-expanded with parentInitial "O", expand-all glyph round-trips, tabs/diff behave. If `waitForLoaded` times out, the store isn't loading: confirm `App` dispatches `GraphRequested` on mount and the http adapter is reached (the harness fetch-stub returns the graph for `archgraph.json`).

- [ ] **Step 4: Full suite**

Run: `./node_modules/.bin/vitest run`
Expected: all pass (Plan-1 unit tests + harness smoke). No regressions.

- [ ] **Step 5: Commit**

```bash
git add web/src/App.tsx
git commit -m "feat(web): cut App over to the store for data/layout/semantic state (Plan 2a)"
```

---

## Task 3: Prune dead hooks from `state/hooks.ts`

After Task 2, `App.tsx` no longer imports `useExpansion`/`useFocus`. `useExpanded` only backed those two. `computeExpandedHeight` is still used by `EdgeLayer.tsx` and must remain.

**Files:**
- Modify: `web/src/state/hooks.ts`

- [ ] **Step 1: Confirm the dead set**

Run: `grep -rn "useExpansion\|useFocus\|useExpanded" web/src/`
Expected: matches ONLY inside `web/src/state/hooks.ts` (no consumers left). If `App.tsx` still matches, Task 2 missed a reference — fix that first.

- [ ] **Step 2: Remove the dead exports**

In `web/src/state/hooks.ts`, DELETE the `useExpanded`, `useFocus`, and `useExpansion` functions entirely. KEEP `computeExpandedHeight` (and its imports `Component`, plus drop now-unused imports: `useState`, `useMemo`, `useEffect`, `useCallback`, `UIGraph` if they become unused — keep only what `computeExpandedHeight` needs, which is the `Component` type). The file should end up as just the `computeExpandedHeight` function + its single type import.

- [ ] **Step 3: Type-check + suite**

Run: `./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run`
Expected: clean tsc (EdgeLayer still imports `computeExpandedHeight` fine) + all tests pass.

- [ ] **Step 4: Commit**

```bash
git add web/src/state/hooks.ts
git commit -m "refactor(web): drop dead useExpansion/useFocus/useExpanded hooks (Plan 2a)"
```

---

## Task 4: End-to-end verification checkpoint

No code — prove the real app still works in a browser via the existing Playwright e2e (the jsdom harness already passed in Tasks 2–3).

**Files:** none.

- [ ] **Step 1: Run the e2e suite**

Run: `./node_modules/.bin/playwright test` (from `web/`). If browsers aren't installed: `./node_modules/.bin/playwright install` first.
Expected: `canvas.spec.ts`, `component-card.spec.ts`, `context-tree.spec.ts`, `diff-mode.spec.ts` all green — the real `<App/>` loads via `routeGraph` + `page.goto('/')`, components render, pan/zoom/cursor work (viewport is still the unchanged local code), context tree focuses, diff mode shows PR header/tabs/legend/colors.

- [ ] **Step 2: If e2e cannot run in this environment** (no browser/display), record that explicitly and fall back to the jsdom harness as the behavior gate. Do NOT claim e2e passed if it did not run. Note the skipped check so a human can run `npm run e2e` before merging.

- [ ] **Step 3 (manual smoke, optional):** `./node_modules/.bin/vite` dev server, open `http://localhost:5174/` (the URL the project grants perms for), confirm: diagram loads, expand/collapse + expand-all work, focus dims neighbors, theme toggle, level switch, left/right panel collapse, tabs, change-list navigation scrolls + focuses, pan-drag + ctrl-wheel zoom + fit button still work, comments still pin (local island). No commit.

---

## Done criteria (Plan 2a)

- `./node_modules/.bin/tsc --noEmit` clean; `./node_modules/.bin/vitest run` green (Plan-1 units + harness smoke).
- e2e green (or explicitly recorded as not-run with the jsdom harness as the gate).
- The store drives data/layout/expansion/focus/theme/level/panels/tabs/changes/tree; `App.tsx` holds no `useState` for those.
- Viewport (zoom/pan/scroll) and comments (markers/popover/seeding) still work, unchanged, local.
- `main.tsx` unchanged; `<App/>` self-bootstraps; the harness needs no changes.

## Deferred to later plans (tracked, not dropped)

- **Plan 2b:** `adapters/domViewport.ts` implementing `ViewportPort` (scroll/fit + pan/wheel controller bound to the canvas via a shared handle); migrate `zoom` into `ui.zoom`; wire `CanvasToolbar`/wheel/fit to `ZoomChanged`/`ZoomFitRequested`; route scroll through the viewport effect; replace the `setTimeout(150/200)` scroll glue with scroll-on-next-`LayoutComputed`; swap the no-op viewport in `createAppStore` for the real adapter.
- **Plan 2c:** migrate comments to the store (`markers`/`pendingComment`/`activeMarkerId` via `commentsSlice`); seed markers from `graph.comments` near laid geometry as a derive+effect; switch `handleCanvasClick` to `CanvasCleared`; collapse `App.tsx`/`AppContent` into the thin composition root (move composition to `main.tsx`); optional `components/` → `view/` rename.
