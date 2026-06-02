# Frontend TEA Core Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the pure domain + store + ports/adapters/effects of the Elm/TEA frontend architecture as a fully unit-tested foundation, without yet rewiring the running `App.tsx`.

**Architecture:** A single `AppState` humble object, a pure `update(state, event)` composed of slice reducers, an impure effects layer that calls ports and dispatches follow-up events, and a tiny store with a React binding. `elkjs` and `fetch` sit behind ports as adapters. This plan builds the foundation in isolation; the view cutover is a separate follow-on plan (Plan 2), so the app and its e2e harnesses stay green throughout.

**Tech Stack:** TypeScript, React 18 (`useSyncExternalStore`), Vitest + jsdom, `@testing-library/react`, existing `elkjs` layout and `loadGraph` fetch.

**Reference design:** `docs/plans/2026-06-02-frontend-architecture-design.md`

**Scope boundary (what this plan does NOT do):**
- Does not modify `App.tsx`, `main.tsx`, or any rendering path. The app keeps running on the current code.
- Does not build the DOM `domViewport` adapter (needs the canvas ref/zoom math that lives in the view) — that is Plan 2.
- Does not implement scroll-after-relayout (replacing the current `setTimeout(150)`); Plan 1's viewport effect scrolls immediately against current geometry. The "wait for next `LayoutComputed`" refinement is Plan 2.
- Does not implement layout debounce — matches today's behavior (re-layout per change, race-guarded).

---

## File Structure

Created by this plan (all under `web/src/`):

| File | Responsibility |
|---|---|
| `runtime/store.ts` | `createStore`, `Store`/`Reducer`/`Effect` types |
| `runtime/react.tsx` | `StoreProvider`, `useStore(selector)`, `useDispatch` |
| `domain/state.ts` | `AppState`, `AppUI`, `Interaction`, `Marker`, `PendingComment`, `initialState` |
| `domain/events.ts` | `Event` discriminated union, `TreeFocusTarget` |
| `domain/ports.ts` | `LayoutPort`, `GraphSourcePort`, `ViewportPort` |
| `domain/derive.ts` | `relatedIds`, `deriveChanges`, `ChangeEntry`, `toInteraction`, `addInternalsOfExpanded`, `initialExpanded` |
| `domain/update.ts` | root `update` + slice reducers (focus, expansion, chrome/zoom, load/geometry, comments) |
| `adapters/elkLayout.ts` | implements `LayoutPort` via existing `layout()` |
| `adapters/httpGraphSource.ts` | implements `GraphSourcePort` via existing `loadGraph()` |
| `effects/load.ts` | `createLoadEffect` |
| `effects/layout.ts` | `createLayoutEffect` (race-guarded) |
| `effects/viewport.ts` | `createViewportEffect` |
| `effects/index.ts` | `Ports`, `createEffects` registry |

Minimally modified (DRY re-exports — no runtime change, app stays green):
- `components/ChangesPanel.tsx` — drop local `deriveChanges`/`ChangeEntry`, re-export from `domain/derive`.
- `components/PinnedMarker.tsx` — re-export `Marker` from `domain/state`.
- `components/InlinePopover.tsx` — re-export `PendingComment` from `domain/state`.

**Test commands** (run from `web/`):
- Single file: `npx vitest run src/<path>.test.ts`
- Single group: `npx vitest run src/<path>.test.ts -t "<name>"`
- All unit: `npx vitest run`
- App still builds: `npx tsc --noEmit`

---

## Task 1: Store core

**Files:**
- Create: `web/src/runtime/store.ts`
- Test: `web/src/runtime/store.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/runtime/store.test.ts
import { describe, it, expect, vi } from 'vitest';
import { createStore } from './store';

type S = { count: number };
type E = { type: 'inc' } | { type: 'set'; value: number };

const update = (s: S, e: E): S => {
  switch (e.type) {
    case 'inc': return { count: s.count + 1 };
    case 'set': return { count: e.value };
    default: return s;
  }
};

describe('createStore', () => {
  it('applies update and exposes new state via getState', () => {
    const store = createStore<S, E>({ count: 0 }, update, []);
    store.dispatch({ type: 'inc' });
    expect(store.getState()).toEqual({ count: 1 });
  });

  it('notifies subscribers on dispatch and stops after unsubscribe', () => {
    const store = createStore<S, E>({ count: 0 }, update, []);
    const listener = vi.fn();
    const unsub = store.subscribe(listener);
    store.dispatch({ type: 'inc' });
    expect(listener).toHaveBeenCalledTimes(1);
    unsub();
    store.dispatch({ type: 'inc' });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('runs effects after update, giving them getState and dispatch', () => {
    const seen: Array<{ type: string; count: number }> = [];
    const effect = (e: E, getState: () => S, dispatch: (e: E) => void) => {
      seen.push({ type: e.type, count: getState().count });
      if (e.type === 'inc' && getState().count === 1) dispatch({ type: 'set', value: 99 });
    };
    const store = createStore<S, E>({ count: 0 }, update, [effect]);
    store.dispatch({ type: 'inc' });
    expect(seen[0]).toEqual({ type: 'inc', count: 1 }); // state already updated when effect runs
    expect(store.getState()).toEqual({ count: 99 }); // follow-up dispatch applied
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/runtime/store.test.ts`
Expected: FAIL — `Failed to resolve import "./store"` / `createStore is not a function`.

- [ ] **Step 3: Write minimal implementation**

```ts
// web/src/runtime/store.ts
export interface Store<S, E> {
  getState(): S;
  dispatch(event: E): void;
  subscribe(listener: () => void): () => void;
}

export type Reducer<S, E> = (state: S, event: E) => S;
export type Effect<S, E> = (event: E, getState: () => S, dispatch: (event: E) => void) => void;

export function createStore<S, E>(
  initial: S,
  update: Reducer<S, E>,
  effects: Effect<S, E>[] = []
): Store<S, E> {
  let state = initial;
  const listeners = new Set<() => void>();

  const getState = () => state;

  const dispatch = (event: E): void => {
    state = update(state, event);
    listeners.forEach((l) => l());
    for (const fx of effects) fx(event, getState, dispatch);
  };

  const subscribe = (listener: () => void): (() => void) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  };

  return { getState, dispatch, subscribe };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/runtime/store.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/runtime/store.ts web/src/runtime/store.test.ts
git commit -m "feat(web): TEA store core (createStore + effect runner)"
```

---

## Task 2: Domain contracts (state, events, ports)

No behavior — these are the type contracts every later task depends on. Verified by compilation and by the reducer tests that follow.

**Files:**
- Create: `web/src/domain/state.ts`
- Create: `web/src/domain/events.ts`
- Create: `web/src/domain/ports.ts`

- [ ] **Step 1: Write `domain/state.ts`**

```ts
// web/src/domain/state.ts
import type { UIGraph } from '../types';

/** Comment marker placed on the canvas. Canonical home (was components/PinnedMarker). */
export interface Marker {
  id: string;
  n: number;
  x: number;
  y: number;
  target: { type: string; id: string };
  body: string;
  author: string;
  when: string;
}

/** A comment being authored. Canonical home (was components/InlinePopover). */
export interface PendingComment {
  x: number;
  y: number;
  target: { type: string; id: string };
}

/** The expansion inputs the layout engine needs. */
export interface Interaction {
  expanded: ReadonlySet<string>;
  internalExpanded: ReadonlySet<string>;
  internalWide: ReadonlySet<string>;
}

export interface AppUI {
  level: number;
  theme: 'dark' | 'light';
  focusId: string | null;
  expanded: ReadonlySet<string>;
  internalExpanded: ReadonlySet<string>;
  internalWide: ReadonlySet<string>;
  leftTab: 'changes' | 'tree';
  leftCollapsed: boolean;
  rightCollapsed: boolean;
  activeChangeId: string | null;
  activeMarkerId: string | null;
  zoom: number;
}

export interface AppState {
  graph: UIGraph | null;
  ui: AppUI;
  markers: Marker[];
  pendingComment: PendingComment | null;
  geometry: { laid: UIGraph | null; status: 'idle' | 'ready' | 'error'; error: string | null };
  load: { status: 'loading' | 'ready' | 'error'; error: string | null };
}

export const initialState: AppState = {
  graph: null,
  ui: {
    level: 2,
    theme: 'dark',
    focusId: null,
    expanded: new Set(),
    internalExpanded: new Set(),
    internalWide: new Set(),
    leftTab: 'tree',
    leftCollapsed: false,
    rightCollapsed: false,
    activeChangeId: null,
    activeMarkerId: null,
    zoom: 1,
  },
  markers: [],
  pendingComment: null,
  geometry: { laid: null, status: 'idle', error: null },
  load: { status: 'loading', error: null },
};
```

- [ ] **Step 2: Write `domain/events.ts`**

```ts
// web/src/domain/events.ts
import type { UIGraph } from '../types';
import type { ChangeEntry } from './derive';

/** Identifies which canvas object a context-tree row points at. Canonical home (was components/Tree). */
export interface TreeFocusTarget {
  componentId: string;
  internalId?: string;
  memberId?: string;
}

export type Event =
  // lifecycle
  | { type: 'GraphRequested' }
  | { type: 'GraphLoaded'; graph: UIGraph }
  | { type: 'GraphLoadFailed'; error: string }
  // chrome
  | { type: 'ThemeToggled' }
  | { type: 'LevelChanged'; level: number }
  | { type: 'LeftTabChanged'; tab: 'changes' | 'tree' }
  | { type: 'LeftCollapsedToggled' }
  | { type: 'RightCollapsedToggled' }
  | { type: 'ZoomChanged'; zoom: number }
  | { type: 'ZoomFitRequested' }
  // expansion
  | { type: 'ComponentToggled'; id: string }
  | { type: 'InternalWideToggled'; id: string }
  | { type: 'ComponentAllWideSet'; id: string; wide: boolean }
  // focus / navigation
  | { type: 'ComponentSelected'; id: string }
  | { type: 'FocusCleared' }
  | { type: 'CanvasCleared' }
  | { type: 'ChangeActivated'; change: ChangeEntry }
  | { type: 'TreeFocusRequested'; target: TreeFocusTarget }
  | { type: 'ScrollToComponentRequested'; id: string }
  | { type: 'MarkerActivated'; id: string }
  // comments
  | { type: 'CommentStarted'; target: { type: string; id: string }; anchor: { x: number; y: number } }
  | { type: 'CommentSubmitted'; text: string }
  | { type: 'CommentCancelled' }
  // layout (internal, posted by the layout effect)
  | { type: 'LayoutComputed'; laid: UIGraph }
  | { type: 'LayoutFailed'; error: string };
```

- [ ] **Step 3: Write `domain/ports.ts`**

```ts
// web/src/domain/ports.ts
import type { UIGraph } from '../types';
import type { Interaction } from './state';

export interface LayoutPort {
  compute(graph: UIGraph, interaction: Interaction): Promise<UIGraph>;
}

export interface GraphSourcePort {
  load(): Promise<UIGraph>;
}

export interface ViewportPort {
  scrollToComponent(id: string, laid: UIGraph): void;
  /** Returns a fit-to-screen zoom level, or null if it cannot be computed. */
  fitZoom(laid: UIGraph): number | null;
}
```

- [ ] **Step 4: Verify it compiles (no test yet — contracts only)**

Run: `cd web && npx tsc --noEmit`
Expected: PASS (no errors). `domain/derive.ts` is referenced by `events.ts` but does not exist yet — so this step is expected to FAIL with `Cannot find module './derive'`. That is fine: it is resolved in Task 3. Proceed to commit after Task 3, or temporarily expect this single unresolved import.

> Note: To keep the commit green, commit Task 2 and Task 3 together (the `ChangeEntry` type they share). The commit step is at the end of Task 3.

---

## Task 3: Derivations (`domain/derive.ts`) + DRY re-export

**Files:**
- Create: `web/src/domain/derive.ts`
- Create: `web/src/domain/derive.test.ts`
- Modify: `web/src/components/ChangesPanel.tsx` (drop local copy, re-export)

- [ ] **Step 1: Write the failing test**

```ts
// web/src/domain/derive.test.ts
import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { relatedIds, deriveChanges, addInternalsOfExpanded, initialExpanded } from './derive';

function graph(overrides?: Partial<UIGraph>): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'bc1', name: 'Core' }],
    components: [
      { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] },
      { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
    ],
    edges: [{ id: 'e1', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' }],
    comments: [],
    ...overrides,
  };
}

describe('relatedIds', () => {
  it('returns null when nothing is focused', () => {
    expect(relatedIds(graph(), null)).toBeNull();
  });
  it('returns the focused node plus its edge neighbours', () => {
    expect(relatedIds(graph(), 'a')).toEqual(new Set(['a', 'b']));
  });
});

describe('deriveChanges', () => {
  it('walks the graph for diff-flagged elements', () => {
    const g = graph({
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
      ],
      edges: [],
    });
    const changes = deriveChanges(g);
    expect(changes).toHaveLength(1);
    expect(changes[0]).toMatchObject({ id: 'cmp-a', kind: 'added', name: 'A', cmp: 'a' });
  });
});

describe('addInternalsOfExpanded', () => {
  it('adds internals of expanded components (add-only, preserves prior)', () => {
    const result = addInternalsOfExpanded(graph(), new Set(['a']), new Set(['old']));
    expect(result).toEqual(new Set(['old', 'a.i']));
  });
});

describe('initialExpanded', () => {
  it('falls back to the first component when no "orders" exists', () => {
    expect(initialExpanded(graph())).toEqual(['a']);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/domain/derive.test.ts`
Expected: FAIL — `Failed to resolve import "./derive"`.

- [ ] **Step 3: Write `domain/derive.ts`**

```ts
// web/src/domain/derive.ts
import type { UIGraph, Diff } from '../types';
import type { AppUI, Interaction } from './state';

/** Focused component + its direct edge neighbours; null when nothing is focused. */
export function relatedIds(graph: UIGraph, focusId: string | null): Set<string> | null {
  if (!focusId) return null;
  const r = new Set<string>([focusId]);
  for (const edge of graph.edges) {
    if (edge.from === focusId) r.add(edge.to);
    if (edge.to === focusId) r.add(edge.from);
  }
  return r;
}

/** Project the UI slice down to the inputs the layout engine needs. */
export function toInteraction(ui: AppUI): Interaction {
  return { expanded: ui.expanded, internalExpanded: ui.internalExpanded, internalWide: ui.internalWide };
}

/** Union `prev` with the internals of every currently-expanded component (add-only). */
export function addInternalsOfExpanded(
  graph: UIGraph,
  expanded: ReadonlySet<string>,
  prev: ReadonlySet<string>
): Set<string> {
  const next = new Set(prev);
  for (const c of graph.components) {
    if (expanded.has(c.id)) {
      for (const internal of c.internals) next.add(internal.id);
    }
  }
  return next;
}

/** Which components start expanded after a graph loads ("orders" if present, else first). */
export function initialExpanded(graph: UIGraph): string[] {
  const orders = graph.components.find((c) => c.id === 'orders');
  if (orders) return ['orders'];
  if (graph.components.length > 0) return [graph.components[0].id];
  return [];
}

/** A change entry derived from graph elements with diff flags. */
export interface ChangeEntry {
  id: string;
  kind: Diff;
  name: string;
  where: string;
  cmp: string;
  internal?: string;
  member?: string;
  port?: string;
}

/** Walk the graph for diff-flagged elements. Moved verbatim from components/ChangesPanel. */
export function deriveChanges(graph: UIGraph): ChangeEntry[] {
  const out: ChangeEntry[] = [];

  for (const c of graph.components) {
    const bcName = graph.boundedContexts.find((b) => b.id === c.bc)?.name ?? c.bc;

    if (c.diff) {
      out.push({ id: `cmp-${c.id}`, kind: c.diff, name: c.name, where: `component - ${bcName}`, cmp: c.id });
    }

    for (const i of c.internals) {
      if (i.diff) {
        out.push({ id: `int-${i.id}`, kind: i.diff, name: i.name, where: `${i.kind} - ${c.name}`, cmp: c.id, internal: i.id });
      }
      for (const m of i.members ?? []) {
        if (m.diff) {
          out.push({ id: `mem-${m.id}`, kind: m.diff, name: m.name, where: `${m.kind} - ${i.name}`, cmp: c.id, internal: i.id, member: m.id });
        }
      }
    }

    for (const p of c.ports) {
      if (p.diff) {
        out.push({ id: `port-${p.id}`, kind: p.diff, name: p.name, where: `port - ${c.name}`, cmp: c.id, port: p.id });
      }
    }
  }

  for (const e of graph.edges) {
    if (e.diff) {
      const fromName = graph.components.find((c) => c.id === e.from)?.name ?? e.from;
      const toName = graph.components.find((c) => c.id === e.to)?.name ?? e.to;
      out.push({ id: `edg-${e.id}`, kind: e.diff, name: `${fromName} -> ${toName}`, where: `connection - ${e.label || ''}`, cmp: e.from });
    }
  }

  return out;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/domain/derive.test.ts`
Expected: PASS.

- [ ] **Step 5: Make `ChangesPanel` re-export from `derive` (DRY)**

In `web/src/components/ChangesPanel.tsx`: delete the local `ChangeEntry` interface (lines ~3-23) and the local `deriveChanges` function (lines ~25-107). Replace the top of the file so existing importers (`App.tsx` imports `deriveChanges, ChangeEntry` from here) keep working:

```tsx
// web/src/components/ChangesPanel.tsx  (top of file)
import type { UIGraph } from '../types';
export { deriveChanges } from '../domain/derive';
export type { ChangeEntry } from '../domain/derive';
import type { ChangeEntry } from '../domain/derive';

// ...the rest of the file (ChangesPanelProps + ChangesPanel component) stays unchanged.
```

- [ ] **Step 6: Verify the app still type-checks and existing tests pass**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS — no errors; all existing tests (including `layout.test.ts` and harness smoke) green.

- [ ] **Step 7: Commit (Tasks 2 + 3 together)**

```bash
git add web/src/domain/state.ts web/src/domain/events.ts web/src/domain/ports.ts \
        web/src/domain/derive.ts web/src/domain/derive.test.ts web/src/components/ChangesPanel.tsx
git commit -m "feat(web): domain contracts + pure derivations (DRY deriveChanges)"
```

---

## Task 4: Root `update` + focus/navigation slice

**Files:**
- Create: `web/src/domain/update.ts`
- Create: `web/src/domain/update.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/domain/update.test.ts
import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from './state';
import { update } from './update';

function withGraph(): AppState {
  const graph: UIGraph = {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'bc1', name: 'Core' }],
    components: [
      { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] },
    ],
    edges: [],
    comments: [],
  };
  return { ...initialState, graph };
}

describe('update — focus slice', () => {
  it('ComponentSelected sets focus; selecting the focused one clears it', () => {
    let s = update(withGraph(), { type: 'ComponentSelected', id: 'a' });
    expect(s.ui.focusId).toBe('a');
    s = update(s, { type: 'ComponentSelected', id: 'a' });
    expect(s.ui.focusId).toBeNull();
  });

  it('FocusCleared and CanvasCleared clear focus', () => {
    const s = update({ ...withGraph(), ui: { ...initialState.ui, focusId: 'a' } }, { type: 'FocusCleared' });
    expect(s.ui.focusId).toBeNull();
  });

  it('ChangeActivated sets active change, focuses the component, and expands when drilling in', () => {
    const s = update(withGraph(), {
      type: 'ChangeActivated',
      change: { id: 'mem-x', kind: 'added', name: 'x', where: '', cmp: 'a', internal: 'a.i' },
    });
    expect(s.ui.activeChangeId).toBe('mem-x');
    expect(s.ui.focusId).toBe('a');
    expect(s.ui.expanded.has('a')).toBe(true);
  });

  it('TreeFocusRequested focuses and expands when drilling into an internal', () => {
    const s = update(withGraph(), { type: 'TreeFocusRequested', target: { componentId: 'a', internalId: 'a.i' } });
    expect(s.ui.focusId).toBe('a');
    expect(s.ui.expanded.has('a')).toBe(true);
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('returns the same state object for unknown events', () => {
    const s = withGraph();
    expect(update(s, { type: 'ZoomFitRequested' })).toBe(s);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/domain/update.test.ts`
Expected: FAIL — `Failed to resolve import "./update"`.

- [ ] **Step 3: Write `domain/update.ts` (root + focus slice)**

```ts
// web/src/domain/update.ts
import type { AppState } from './state';
import type { Event } from './events';
import { addInternalsOfExpanded } from './derive';

function expandComponent(state: AppState, id: string): AppState {
  if (!state.graph || state.ui.expanded.has(id)) return state;
  const expanded = new Set(state.ui.expanded);
  expanded.add(id);
  const internalExpanded = addInternalsOfExpanded(state.graph, expanded, state.ui.internalExpanded);
  return { ...state, ui: { ...state.ui, expanded, internalExpanded } };
}

function focusSlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'ComponentSelected': {
      const focusId = state.ui.focusId === event.id ? null : event.id;
      return { ...state, ui: { ...state.ui, focusId } };
    }
    case 'FocusCleared':
      return { ...state, ui: { ...state.ui, focusId: null } };
    case 'CanvasCleared':
      return { ...state, ui: { ...state.ui, focusId: null, activeMarkerId: null }, pendingComment: null };
    case 'ChangeActivated': {
      const { change } = event;
      const drillIn = !!(change.internal || change.member || change.port);
      let next: AppState = { ...state, ui: { ...state.ui, activeChangeId: change.id, focusId: change.cmp } };
      if (drillIn) next = expandComponent(next, change.cmp);
      return next;
    }
    case 'TreeFocusRequested': {
      const { target } = event;
      const drillIn = !!(target.internalId || target.memberId);
      let next: AppState = { ...state, ui: { ...state.ui, activeChangeId: null, focusId: target.componentId } };
      if (drillIn) next = expandComponent(next, target.componentId);
      return next;
    }
    default:
      return state;
  }
}

export function update(state: AppState, event: Event): AppState {
  return focusSlice(state, event);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/domain/update.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/domain/update.ts web/src/domain/update.test.ts
git commit -m "feat(web): update root + focus/navigation slice"
```

---

## Task 5: Expansion slice

**Files:**
- Modify: `web/src/domain/update.ts`
- Modify: `web/src/domain/update.test.ts`

- [ ] **Step 1: Add the failing test**

Append to `web/src/domain/update.test.ts`:

```ts
describe('update — expansion slice', () => {
  it('ComponentToggled expands and auto-expands the component internals', () => {
    const s = update(withGraph(), { type: 'ComponentToggled', id: 'a' });
    expect(s.ui.expanded.has('a')).toBe(true);
    expect(s.ui.internalExpanded.has('a.i')).toBe(true);
  });

  it('ComponentToggled collapses an expanded component (internalExpanded is add-only)', () => {
    const opened = update(withGraph(), { type: 'ComponentToggled', id: 'a' });
    const closed = update(opened, { type: 'ComponentToggled', id: 'a' });
    expect(closed.ui.expanded.has('a')).toBe(false);
    expect(closed.ui.internalExpanded.has('a.i')).toBe(true); // not removed, matching current behaviour
  });

  it('InternalWideToggled toggles one internal in fit-width mode', () => {
    let s = update(withGraph(), { type: 'InternalWideToggled', id: 'a.i' });
    expect(s.ui.internalWide.has('a.i')).toBe(true);
    s = update(s, { type: 'InternalWideToggled', id: 'a.i' });
    expect(s.ui.internalWide.has('a.i')).toBe(false);
  });

  it('ComponentAllWideSet adds/removes every internal of a component', () => {
    let s = update(withGraph(), { type: 'ComponentAllWideSet', id: 'a', wide: true });
    expect(s.ui.internalWide.has('a.i')).toBe(true);
    s = update(s, { type: 'ComponentAllWideSet', id: 'a', wide: false });
    expect(s.ui.internalWide.has('a.i')).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/domain/update.test.ts -t "expansion slice"`
Expected: FAIL — `expected false to be true` (events fall through to default, no-op).

- [ ] **Step 3: Add the expansion slice to `domain/update.ts`**

Add this function above `export function update` and chain it in (see Step 4):

```ts
function expansionSlice(state: AppState, event: Event): AppState {
  if (!state.graph) return state;
  switch (event.type) {
    case 'ComponentToggled': {
      const expanded = new Set(state.ui.expanded);
      if (expanded.has(event.id)) expanded.delete(event.id);
      else expanded.add(event.id);
      const internalExpanded = addInternalsOfExpanded(state.graph, expanded, state.ui.internalExpanded);
      return { ...state, ui: { ...state.ui, expanded, internalExpanded } };
    }
    case 'InternalWideToggled': {
      const internalWide = new Set(state.ui.internalWide);
      if (internalWide.has(event.id)) internalWide.delete(event.id);
      else internalWide.add(event.id);
      return { ...state, ui: { ...state.ui, internalWide } };
    }
    case 'ComponentAllWideSet': {
      const comp = state.graph.components.find((c) => c.id === event.id);
      if (!comp) return state;
      const internalWide = new Set(state.ui.internalWide);
      for (const internal of comp.internals) {
        if (event.wide) internalWide.add(internal.id);
        else internalWide.delete(internal.id);
      }
      return { ...state, ui: { ...state.ui, internalWide } };
    }
    default:
      return state;
  }
}
```

- [ ] **Step 4: Chain the slice into the root `update`**

Replace the body of `update`:

```ts
export function update(state: AppState, event: Event): AppState {
  let next = focusSlice(state, event);
  next = expansionSlice(next, event);
  return next;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `npx vitest run src/domain/update.test.ts`
Expected: PASS (focus + expansion groups).

- [ ] **Step 6: Commit**

```bash
git add web/src/domain/update.ts web/src/domain/update.test.ts
git commit -m "feat(web): expansion slice (toggle + add-only internal auto-expand + wide)"
```

---

## Task 6: Chrome + zoom slice

**Files:**
- Modify: `web/src/domain/update.ts`
- Modify: `web/src/domain/update.test.ts`

- [ ] **Step 1: Add the failing test**

Append to `web/src/domain/update.test.ts`:

```ts
describe('update — chrome + zoom slice', () => {
  it('ThemeToggled flips dark/light', () => {
    const s = update(withGraph(), { type: 'ThemeToggled' });
    expect(s.ui.theme).toBe('light');
  });
  it('LevelChanged sets the level', () => {
    expect(update(withGraph(), { type: 'LevelChanged', level: 1 }).ui.level).toBe(1);
  });
  it('LeftTabChanged / collapse toggles', () => {
    let s = update(withGraph(), { type: 'LeftTabChanged', tab: 'changes' });
    expect(s.ui.leftTab).toBe('changes');
    s = update(s, { type: 'LeftCollapsedToggled' });
    expect(s.ui.leftCollapsed).toBe(true);
    s = update(s, { type: 'RightCollapsedToggled' });
    expect(s.ui.rightCollapsed).toBe(true);
  });
  it('ZoomChanged sets the zoom level', () => {
    expect(update(withGraph(), { type: 'ZoomChanged', zoom: 0.5 }).ui.zoom).toBe(0.5);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/domain/update.test.ts -t "chrome + zoom slice"`
Expected: FAIL — `expected 'dark' to be 'light'`.

- [ ] **Step 3: Add the chrome slice to `domain/update.ts`**

```ts
function chromeSlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'ThemeToggled':
      return { ...state, ui: { ...state.ui, theme: state.ui.theme === 'dark' ? 'light' : 'dark' } };
    case 'LevelChanged':
      return { ...state, ui: { ...state.ui, level: event.level } };
    case 'LeftTabChanged':
      return { ...state, ui: { ...state.ui, leftTab: event.tab } };
    case 'LeftCollapsedToggled':
      return { ...state, ui: { ...state.ui, leftCollapsed: !state.ui.leftCollapsed } };
    case 'RightCollapsedToggled':
      return { ...state, ui: { ...state.ui, rightCollapsed: !state.ui.rightCollapsed } };
    case 'ZoomChanged':
      return { ...state, ui: { ...state.ui, zoom: event.zoom } };
    default:
      return state;
  }
}
```

- [ ] **Step 4: Chain into root `update`**

```ts
export function update(state: AppState, event: Event): AppState {
  let next = focusSlice(state, event);
  next = expansionSlice(next, event);
  next = chromeSlice(next, event);
  return next;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `npx vitest run src/domain/update.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/domain/update.ts web/src/domain/update.test.ts
git commit -m "feat(web): chrome + zoom slice"
```

---

## Task 7: Load + geometry slice

**Files:**
- Modify: `web/src/domain/update.ts`
- Modify: `web/src/domain/update.test.ts`

- [ ] **Step 1: Add the failing test**

Append to `web/src/domain/update.test.ts`:

```ts
describe('update — load + geometry slice', () => {
  const graph = withGraph().graph!;

  it('GraphRequested sets load status to loading', () => {
    const s = update({ ...initialState, load: { status: 'ready', error: null } }, { type: 'GraphRequested' });
    expect(s.load.status).toBe('loading');
  });

  it('GraphLoaded stores the graph, marks ready, and seeds initial expansion', () => {
    const s = update(initialState, { type: 'GraphLoaded', graph });
    expect(s.graph).toBe(graph);
    expect(s.load.status).toBe('ready');
    expect(s.ui.expanded.has('a')).toBe(true); // initialExpanded → first component
  });

  it('GraphLoaded selects the changes tab when the graph carries a PR', () => {
    const prGraph = { ...graph, pr: { title: 't', branch: 'b', agent: 'x', summary: '', stats: { added: 0, removed: 0, changed: 0, comments: 0 } } };
    const s = update(initialState, { type: 'GraphLoaded', graph: prGraph });
    expect(s.ui.leftTab).toBe('changes');
  });

  it('GraphLoadFailed records the error', () => {
    const s = update(initialState, { type: 'GraphLoadFailed', error: 'boom' });
    expect(s.load).toEqual({ status: 'error', error: 'boom' });
  });

  it('LayoutComputed stores geometry and marks ready', () => {
    const s = update(initialState, { type: 'LayoutComputed', laid: graph });
    expect(s.geometry.laid).toBe(graph);
    expect(s.geometry.status).toBe('ready');
  });

  it('LayoutFailed keeps the last good laid graph and records the error', () => {
    const ready = update(initialState, { type: 'LayoutComputed', laid: graph });
    const failed = update(ready, { type: 'LayoutFailed', error: 'elk-died' });
    expect(failed.geometry.laid).toBe(graph); // last good preserved (no empty flash)
    expect(failed.geometry.status).toBe('error');
    expect(failed.geometry.error).toBe('elk-died');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/domain/update.test.ts -t "load + geometry slice"`
Expected: FAIL — events fall through to default.

- [ ] **Step 3: Add the load/geometry slice to `domain/update.ts`**

Add the import at the top (extend the existing derive import):

```ts
import { addInternalsOfExpanded, initialExpanded } from './derive';
```

Add the slice:

```ts
function loadGeometrySlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'GraphRequested':
      return { ...state, load: { status: 'loading', error: null } };
    case 'GraphLoaded': {
      const graph = event.graph;
      const expanded = new Set(initialExpanded(graph));
      const internalExpanded = addInternalsOfExpanded(graph, expanded, new Set());
      const leftTab = graph.pr != null ? 'changes' : state.ui.leftTab;
      return {
        ...state,
        graph,
        load: { status: 'ready', error: null },
        ui: { ...state.ui, expanded, internalExpanded, leftTab },
      };
    }
    case 'GraphLoadFailed':
      return { ...state, load: { status: 'error', error: event.error } };
    case 'LayoutComputed':
      return { ...state, geometry: { laid: event.laid, status: 'ready', error: null } };
    case 'LayoutFailed':
      return { ...state, geometry: { ...state.geometry, status: 'error', error: event.error } };
    default:
      return state;
  }
}
```

- [ ] **Step 4: Chain into root `update`**

```ts
export function update(state: AppState, event: Event): AppState {
  let next = focusSlice(state, event);
  next = expansionSlice(next, event);
  next = chromeSlice(next, event);
  next = loadGeometrySlice(next, event);
  return next;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `npx vitest run src/domain/update.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/domain/update.ts web/src/domain/update.test.ts
git commit -m "feat(web): load + geometry slice (keeps last good layout on failure)"
```

---

## Task 8: Comments slice

**Files:**
- Modify: `web/src/domain/update.ts`
- Modify: `web/src/domain/update.test.ts`

- [ ] **Step 1: Add the failing test**

Append to `web/src/domain/update.test.ts`:

```ts
describe('update — comments slice', () => {
  it('CommentStarted opens a pending comment at the anchor', () => {
    const s = update(withGraph(), { type: 'CommentStarted', target: { type: 'component', id: 'a' }, anchor: { x: 10, y: 20 } });
    expect(s.pendingComment).toEqual({ target: { type: 'component', id: 'a' }, x: 10, y: 20 });
  });

  it('CommentSubmitted appends a deterministically-id\'d marker, clears pending, activates it', () => {
    const started = update(withGraph(), { type: 'CommentStarted', target: { type: 'component', id: 'a' }, anchor: { x: 10, y: 20 } });
    const s = update(started, { type: 'CommentSubmitted', text: 'hi' });
    expect(s.pendingComment).toBeNull();
    expect(s.markers).toHaveLength(1);
    expect(s.markers[0]).toMatchObject({ id: 'm-1', n: 1, x: 10, y: 12, body: 'hi', target: { type: 'component', id: 'a' } });
    expect(s.ui.activeMarkerId).toBe('m-1');
  });

  it('CommentSubmitted is a no-op when there is no pending comment', () => {
    const s = withGraph();
    expect(update(s, { type: 'CommentSubmitted', text: 'hi' })).toBe(s);
  });

  it('CommentCancelled clears pending', () => {
    const started = update(withGraph(), { type: 'CommentStarted', target: { type: 'component', id: 'a' }, anchor: { x: 1, y: 2 } });
    expect(update(started, { type: 'CommentCancelled' }).pendingComment).toBeNull();
  });

  it('MarkerActivated sets the active marker', () => {
    expect(update(withGraph(), { type: 'MarkerActivated', id: 'm-7' }).ui.activeMarkerId).toBe('m-7');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/domain/update.test.ts -t "comments slice"`
Expected: FAIL — pending comment stays null / no marker appended.

- [ ] **Step 3: Add the comments slice to `domain/update.ts`**

```ts
function commentsSlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'CommentStarted':
      return { ...state, pendingComment: { target: event.target, x: event.anchor.x, y: event.anchor.y } };
    case 'CommentSubmitted': {
      if (!state.pendingComment) return state;
      const n = state.markers.length + 1;
      const marker = {
        id: `m-${n}`,
        n,
        x: state.pendingComment.x,
        y: state.pendingComment.y - 8,
        target: state.pendingComment.target,
        body: event.text,
        author: '@you',
        when: 'just now',
      };
      return { ...state, markers: [...state.markers, marker], pendingComment: null, ui: { ...state.ui, activeMarkerId: marker.id } };
    }
    case 'CommentCancelled':
      return { ...state, pendingComment: null };
    case 'MarkerActivated':
      return { ...state, ui: { ...state.ui, activeMarkerId: event.id } };
    default:
      return state;
  }
}
```

> Note: marker `id` is `m-${n}` (deterministic), not `m-${Date.now()}` as in today's `App.tsx`. This keeps the reducer pure; ids stay unique within a session.

- [ ] **Step 4: Chain into root `update`**

```ts
export function update(state: AppState, event: Event): AppState {
  let next = focusSlice(state, event);
  next = expansionSlice(next, event);
  next = chromeSlice(next, event);
  next = loadGeometrySlice(next, event);
  next = commentsSlice(next, event);
  return next;
}
```

- [ ] **Step 5: Run the full reducer suite**

Run: `npx vitest run src/domain/update.test.ts`
Expected: PASS (all five slice groups).

- [ ] **Step 6: Commit**

```bash
git add web/src/domain/update.ts web/src/domain/update.test.ts
git commit -m "feat(web): comments slice (pure deterministic markers)"
```

---

## Task 9: Adapters (elk layout + http graph source)

**Files:**
- Create: `web/src/adapters/elkLayout.ts`
- Create: `web/src/adapters/httpGraphSource.ts`
- Test: `web/src/adapters/adapters.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/adapters/adapters.test.ts
import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { createElkLayout } from './elkLayout';
import { createHttpGraphSource } from './httpGraphSource';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'bc1', name: 'Core' }],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] }],
  edges: [],
  comments: [],
};

describe('createElkLayout', () => {
  it('lays out a graph, assigning geometry to components', async () => {
    const port = createElkLayout();
    const laid = await port.compute(graph, { expanded: new Set(), internalExpanded: new Set(), internalWide: new Set() });
    const a = laid.components.find((c) => c.id === 'a')!;
    expect(typeof a.x).toBe('number');
    expect(typeof a.y).toBe('number');
  });
});

describe('createHttpGraphSource', () => {
  it('loads a UIGraph (falls back to the built-in fixture when no network)', async () => {
    const port = createHttpGraphSource();
    const result = await port.load();
    expect(result.schema.startsWith('archai.uigraph/')).toBe(true);
    expect(Array.isArray(result.components)).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/adapters/adapters.test.ts`
Expected: FAIL — `Failed to resolve import "./elkLayout"`.

- [ ] **Step 3: Write the adapters**

```ts
// web/src/adapters/elkLayout.ts
import { layout } from '../layout/layout';
import type { LayoutPort } from '../domain/ports';

/** LayoutPort backed by elkjs. Copies the readonly interaction sets into the
 *  mutable Sets the layout() function expects (the adapter is the boundary). */
export function createElkLayout(): LayoutPort {
  return {
    compute(graph, interaction) {
      return layout(graph, {
        expanded: new Set(interaction.expanded),
        internalExpanded: new Set(interaction.internalExpanded),
        internalWide: new Set(interaction.internalWide),
      });
    },
  };
}
```

```ts
// web/src/adapters/httpGraphSource.ts
import { loadGraph } from '../data/load';
import type { GraphSourcePort } from '../domain/ports';

/** GraphSourcePort backed by the existing fetch-with-fallback loader. */
export function createHttpGraphSource(): GraphSourcePort {
  return { load: () => loadGraph() };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/adapters/adapters.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/adapters/elkLayout.ts web/src/adapters/httpGraphSource.ts web/src/adapters/adapters.test.ts
git commit -m "feat(web): elk layout + http graph-source adapters behind ports"
```

---

## Task 10: Load effect

**Files:**
- Create: `web/src/effects/load.ts`
- Test: `web/src/effects/load.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/effects/load.test.ts
import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort } from '../domain/ports';
import { createLoadEffect } from './load';

const graph: UIGraph = { schema: 'archai.uigraph/v0', boundedContexts: [], components: [], edges: [], comments: [] };
const flush = () => new Promise((r) => setTimeout(r));

describe('createLoadEffect', () => {
  it('on GraphRequested, loads and dispatches GraphLoaded', async () => {
    const port: GraphSourcePort = { load: () => Promise.resolve(graph) };
    const dispatch = vi.fn();
    const effect = createLoadEffect(port);
    effect({ type: 'GraphRequested' }, () => initialState, dispatch as (e: Event) => void);
    await flush();
    expect(dispatch).toHaveBeenCalledWith({ type: 'GraphLoaded', graph });
  });

  it('on load failure, dispatches GraphLoadFailed', async () => {
    const port: GraphSourcePort = { load: () => Promise.reject(new Error('nope')) };
    const dispatch = vi.fn();
    createLoadEffect(port)({ type: 'GraphRequested' }, () => initialState, dispatch as (e: Event) => void);
    await flush();
    expect(dispatch).toHaveBeenCalledWith({ type: 'GraphLoadFailed', error: 'Error: nope' });
  });

  it('ignores unrelated events', () => {
    const port: GraphSourcePort = { load: vi.fn() };
    createLoadEffect(port)({ type: 'ThemeToggled' }, () => initialState, vi.fn());
    expect(port.load).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/effects/load.test.ts`
Expected: FAIL — `Failed to resolve import "./load"`.

- [ ] **Step 3: Write `effects/load.ts`**

```ts
// web/src/effects/load.ts
import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort } from '../domain/ports';

export function createLoadEffect(port: GraphSourcePort): Effect<AppState, Event> {
  return (event, _getState, dispatch) => {
    if (event.type !== 'GraphRequested') return;
    port.load().then(
      (graph) => dispatch({ type: 'GraphLoaded', graph }),
      (err) => dispatch({ type: 'GraphLoadFailed', error: String(err) })
    );
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/effects/load.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/effects/load.ts web/src/effects/load.test.ts
git commit -m "feat(web): load effect (GraphRequested -> GraphLoaded/Failed)"
```

---

## Task 11: Layout effect (race-guarded)

**Files:**
- Create: `web/src/effects/layout.ts`
- Test: `web/src/effects/layout.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/effects/layout.test.ts
import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { LayoutPort } from '../domain/ports';
import { createLayoutEffect } from './layout';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] }],
  edges: [],
  comments: [],
};
const stateWith = (graphIn: UIGraph | null): AppState => ({ ...initialState, graph: graphIn });
const flush = () => new Promise((r) => setTimeout(r));

describe('createLayoutEffect', () => {
  it('on a trigger event, computes layout and dispatches LayoutComputed', async () => {
    const laid = { ...graph };
    const port: LayoutPort = { compute: vi.fn().mockResolvedValue(laid) };
    const dispatch = vi.fn();
    createLayoutEffect(port)({ type: 'GraphLoaded', graph }, () => stateWith(graph), dispatch as (e: Event) => void);
    await flush();
    expect(port.compute).toHaveBeenCalledTimes(1);
    expect(dispatch).toHaveBeenCalledWith({ type: 'LayoutComputed', laid });
  });

  it('does nothing when there is no graph', () => {
    const port: LayoutPort = { compute: vi.fn() };
    createLayoutEffect(port)({ type: 'GraphLoaded', graph }, () => stateWith(null), vi.fn());
    expect(port.compute).not.toHaveBeenCalled();
  });

  it('drops a stale result when a newer trigger superseded it (race guard)', async () => {
    let resolveFirst!: (g: UIGraph) => void;
    const first = new Promise<UIGraph>((r) => { resolveFirst = r; });
    const second = Promise.resolve({ ...graph, schema: 'second' });
    const compute = vi.fn().mockReturnValueOnce(first).mockReturnValueOnce(second);
    const port: LayoutPort = { compute };
    const dispatch = vi.fn();
    const effect = createLayoutEffect(port);
    const get = () => stateWith(graph);

    effect({ type: 'ComponentToggled', id: 'a' }, get, dispatch as (e: Event) => void); // seq 1
    effect({ type: 'ComponentToggled', id: 'a' }, get, dispatch as (e: Event) => void); // seq 2
    await flush();
    resolveFirst(graph); // stale seq-1 resolves last
    await flush();

    const laidDispatches = dispatch.mock.calls.filter((c) => c[0].type === 'LayoutComputed');
    expect(laidDispatches).toHaveLength(1);
    expect(laidDispatches[0][0].laid.schema).toBe('second'); // only the latest wins
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/effects/layout.test.ts`
Expected: FAIL — `Failed to resolve import "./layout"`.

- [ ] **Step 3: Write `effects/layout.ts`**

```ts
// web/src/effects/layout.ts
import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { LayoutPort } from '../domain/ports';
import { toInteraction } from '../domain/derive';

const LAYOUT_TRIGGERS: ReadonlySet<Event['type']> = new Set([
  'GraphLoaded',
  'ComponentToggled',
  'InternalWideToggled',
  'ComponentAllWideSet',
]);

export function createLayoutEffect(port: LayoutPort): Effect<AppState, Event> {
  let seq = 0;
  return (event, getState, dispatch) => {
    if (!LAYOUT_TRIGGERS.has(event.type)) return;
    const state = getState();
    if (!state.graph) return;
    const mySeq = ++seq;
    port.compute(state.graph, toInteraction(state.ui)).then(
      (laid) => { if (mySeq === seq) dispatch({ type: 'LayoutComputed', laid }); },
      (err) => { if (mySeq === seq) dispatch({ type: 'LayoutFailed', error: String(err) }); }
    );
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/effects/layout.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/effects/layout.ts web/src/effects/layout.test.ts
git commit -m "feat(web): layout effect with latest-wins race guard"
```

---

## Task 12: Viewport effect

**Files:**
- Create: `web/src/effects/viewport.ts`
- Test: `web/src/effects/viewport.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/effects/viewport.test.ts
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
  it('scrolls to the component on ChangeActivated', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    createViewportEffect(port)(
      { type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } },
      withLaid,
      vi.fn()
    );
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
  });

  it('scrolls on ScrollToComponentRequested and TreeFocusRequested', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    const effect = createViewportEffect(port);
    effect({ type: 'ScrollToComponentRequested', id: 'a' }, withLaid, vi.fn());
    effect({ type: 'TreeFocusRequested', target: { componentId: 'a' } }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledTimes(2);
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

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/effects/viewport.test.ts`
Expected: FAIL — `Failed to resolve import "./viewport"`.

- [ ] **Step 3: Write `effects/viewport.ts`**

```ts
// web/src/effects/viewport.ts
import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';

const SCROLL_TRIGGERS: ReadonlySet<Event['type']> = new Set([
  'ChangeActivated',
  'TreeFocusRequested',
  'ScrollToComponentRequested',
]);

export function createViewportEffect(port: ViewportPort): Effect<AppState, Event> {
  return (event, getState, dispatch) => {
    const state = getState();
    const laid = state.geometry.laid;

    if (event.type === 'ZoomFitRequested') {
      if (!laid) return;
      const z = port.fitZoom(laid);
      if (z != null) dispatch({ type: 'ZoomChanged', zoom: z });
      return;
    }

    if (!SCROLL_TRIGGERS.has(event.type) || !laid) return;

    const id =
      event.type === 'ChangeActivated' ? event.change.cmp
      : event.type === 'TreeFocusRequested' ? event.target.componentId
      : event.type === 'ScrollToComponentRequested' ? event.id
      : null;

    if (id) port.scrollToComponent(id, laid);
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/effects/viewport.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/effects/viewport.ts web/src/effects/viewport.test.ts
git commit -m "feat(web): viewport effect (scroll-to-component + zoom-fit)"
```

---

## Task 13: Effects registry

**Files:**
- Create: `web/src/effects/index.ts`

- [ ] **Step 1: Write `effects/index.ts`** (no separate test — exercised by the Task 15 integration test)

```ts
// web/src/effects/index.ts
import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort, LayoutPort, ViewportPort } from '../domain/ports';
import { createLoadEffect } from './load';
import { createLayoutEffect } from './layout';
import { createViewportEffect } from './viewport';

export interface Ports {
  graphSource: GraphSourcePort;
  layout: LayoutPort;
  viewport: ViewportPort;
}

export function createEffects(ports: Ports): Effect<AppState, Event>[] {
  return [
    createLoadEffect(ports.graphSource),
    createLayoutEffect(ports.layout),
    createViewportEffect(ports.viewport),
  ];
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd web && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/effects/index.ts
git commit -m "feat(web): effects registry (createEffects)"
```

---

## Task 14: React binding

**Files:**
- Create: `web/src/runtime/react.tsx`
- Test: `web/src/runtime/react.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
// web/src/runtime/react.test.tsx
import { describe, it, expect } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { createStore } from './store';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import { update } from '../domain/update';
import { StoreProvider, useStore, useDispatch } from './react';

function Probe() {
  const focusId = useStore((s: AppState) => s.ui.focusId);
  const dispatch = useDispatch();
  return (
    <button onClick={() => dispatch({ type: 'ComponentSelected', id: 'a' })}>
      {focusId ?? 'none'}
    </button>
  );
}

describe('react binding', () => {
  it('renders selected state and re-renders on dispatch', () => {
    const store = createStore<AppState, Event>(initialState, update, []);
    render(
      <StoreProvider store={store}>
        <Probe />
      </StoreProvider>
    );
    expect(screen.getByRole('button').textContent).toBe('none');
    act(() => {
      store.dispatch({ type: 'ComponentSelected', id: 'a' });
    });
    expect(screen.getByRole('button').textContent).toBe('a');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/runtime/react.test.tsx`
Expected: FAIL — `Failed to resolve import "./react"`.

- [ ] **Step 3: Write `runtime/react.tsx`**

```tsx
// web/src/runtime/react.tsx
import { createContext, useContext, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import type { Store } from './store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';

export type AppStore = Store<AppState, Event>;

const StoreContext = createContext<AppStore | null>(null);

export function StoreProvider({ store, children }: { store: AppStore; children: ReactNode }) {
  return <StoreContext.Provider value={store}>{children}</StoreContext.Provider>;
}

function useStoreInstance(): AppStore {
  const store = useContext(StoreContext);
  if (!store) throw new Error('useStore/useDispatch must be used within <StoreProvider>');
  return store;
}

/**
 * Subscribe to a slice of state. The selector MUST return a referentially-stable
 * value (a primitive, or the same Set/object reference when unchanged) — reducers
 * already preserve references for untouched slices. Object-constructing selectors
 * need memoization (add a withSelector helper later if that becomes common).
 */
export function useStore<T>(selector: (state: AppState) => T): T {
  const store = useStoreInstance();
  return useSyncExternalStore(store.subscribe, () => selector(store.getState()));
}

export function useDispatch(): (event: Event) => void {
  return useStoreInstance().dispatch;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/runtime/react.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/runtime/react.tsx web/src/runtime/react.test.tsx
git commit -m "feat(web): React binding (StoreProvider/useStore/useDispatch)"
```

---

## Task 15: Integration vertical (load → layout → toggle)

Proves the whole architecture end-to-end (view aside) with fake ports.

**Files:**
- Test: `web/src/runtime/integration.test.ts`

- [ ] **Step 1: Write the integration test**

```ts
// web/src/runtime/integration.test.ts
import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { createStore } from './store';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import { update } from '../domain/update';
import { createEffects } from '../effects';
import type { GraphSourcePort, LayoutPort, ViewportPort } from '../domain/ports';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'bc1', name: 'Core' }],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] }],
  edges: [],
  comments: [],
};
const flush = () => new Promise((r) => setTimeout(r));

// Fake layout: echoes whether each component was expanded into its x coordinate,
// so the test can prove the effect re-ran with the new interaction state.
const fakeLayout: LayoutPort = {
  compute: (g, interaction) =>
    Promise.resolve({ ...g, components: g.components.map((c) => ({ ...c, x: interaction.expanded.has(c.id) ? 100 : 0 })) }),
};
const fakeGraphSource: GraphSourcePort = { load: () => Promise.resolve(graph) };
const fakeViewport: ViewportPort = { scrollToComponent: () => {}, fitZoom: () => null };

describe('integration: load → layout → toggle', () => {
  it('loads a graph, lays it out, and re-lays out on expand', async () => {
    const effects = createEffects({ graphSource: fakeGraphSource, layout: fakeLayout, viewport: fakeViewport });
    const store = createStore<AppState, Event>(initialState, update, effects);

    store.dispatch({ type: 'GraphRequested' });
    await flush(); // loadEffect → GraphLoaded → layoutEffect
    await flush(); // layout promise resolves → LayoutComputed

    expect(store.getState().graph).toBe(graph);
    const laid1 = store.getState().geometry.laid!;
    // 'a' is the first component → seeded expanded by initialExpanded
    expect(laid1.components.find((c) => c.id === 'a')!.x).toBe(100);

    // Collapse it and confirm the layout effect re-ran with the new state.
    store.dispatch({ type: 'ComponentToggled', id: 'a' });
    await flush();
    const laid2 = store.getState().geometry.laid!;
    expect(laid2.components.find((c) => c.id === 'a')!.x).toBe(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails (or passes immediately)**

Run: `npx vitest run src/runtime/integration.test.ts`
Expected: PASS (all building blocks exist by now). If it FAILS, the failure pinpoints a wiring bug across store/update/effects — fix before committing.

- [ ] **Step 3: Run the entire suite + type-check**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS — every new unit test plus all pre-existing tests (`layout.test.ts`, harness smoke) green. The app is unchanged and still builds.

- [ ] **Step 4: Commit**

```bash
git add web/src/runtime/integration.test.ts
git commit -m "test(web): integration vertical (load -> layout -> re-layout on expand)"
```

---

## Done criteria

- `npx tsc --noEmit` clean; `npx vitest run` green (new + existing).
- `web/src/{domain,runtime,effects,adapters}/` exist, each unit-tested.
- `App.tsx` / `main.tsx` / rendering untouched — app still runs, e2e harnesses unaffected.

## Follow-on (Plan 2 — view cutover, separate plan)

Out of scope here; flagged so nothing is silently dropped:
- `runtime/main.tsx` composition root wiring real adapters (`createElkLayout`, `createHttpGraphSource`, `domViewport`) into `createEffects` + `createStore` + `<StoreProvider>`.
- `adapters/domViewport.ts` — DOM scroll/zoom-anchor/pan controller (needs canvas ref, `PAN_MARGIN`, zoom math from current `App.tsx`).
- Scroll-after-relayout: replace the current `setTimeout(150)` with a one-shot scroll on the next `LayoutComputed`.
- Marker seeding from `graph.comments` near laid geometry (`seedMarkers`), as a derive + effect.
- Rewire `view/` components from callback props to `useDispatch`/`useStore`; collapse `App.tsx` to the thin composition; retire `state/hooks.ts` and the duplicate `relatedIds` in `useFocus`.
- Move `Marker`/`PendingComment`/`TreeFocusTarget` consumers to import from `domain/*` (canonical homes) and drop the temporary re-exports.
```
