# Frontend Architecture — Layered "Humble Object" Core (Elm/TEA)

- **Date:** 2026-06-02
- **Status:** Accepted (design/ADR only — implementation deferred)
- **Scope:** `web/` (archai review UI). No code changes in this document.
- **Decision driver:** moderate-growth single-user UI; lean dependency philosophy.

---

## Context

The backend is a clean hexagonal architecture (domain at the centre, `ModelReader`/
`ModelWriter` ports, adapters for Go/D2, service layer, CLI wiring). The frontend grew
organically as a POC and never got the same treatment.

We want the frontend to follow the same spirit — a Domain-Driven, layered architecture
where **business logic lives only in a pure domain**, the view is a *humble object* that
renders a state object and proxies events, and all side effects are isolated next to the
domain. This is structurally the **Elm Architecture (TEA)** / **Redux** model: a single
state object, a pure `update(state, event)`, a humble view, and an effects layer that
subscribes to events and dispatches follow-up events.

### How the idea maps onto today's code

Half of this architecture already emerged on its own:

- **`UIGraph` (`web/src/types.ts`) is already the "humble object"** — a big serialisable
  state container (`pr`, `boundedContexts`, `components`, `edges`, `comments`, geometry).
  Pure data, zero behaviour.
- **Pure domain functions already exist, just scattered:** `layout()`
  (`web/src/layout/layout.ts`) is a pure `(graph, interaction) → graph-with-geometry`;
  `deriveChanges()` (`web/src/components/ChangesPanel.tsx`) derives the change list;
  `useFocus`'s `related` computation is pure.
- **The view is already nearly "dumb":** components take props and render, proxying events
  up via callbacks (`onChangeClick`, `onFocus`, `onSelect`, `onToggleExpand`,
  `onAddComment`).

The gap:

- **`App.tsx` is a 758-line god component** mixing three layers inline: scattered
  interaction state (a dozen `useState`/`useRef`), interaction business logic
  (`goToChange`, `focusFromTree`, `handleSelectComponent`, `startComment`…), and side
  effects (ELK layout, scroll, pan, wheel-zoom).
- **No single state object for interaction state** — only the *data* (`UIGraph`) is
  unified; "what is expanded / focused / zoomed / which panel is open" lives in loose
  `useState`.
- **No event bus and no isolated effects layer** — effects are wired into `useEffect`/
  inline handlers, not subscribed to events.

The work is therefore **completion of layers that already started to form**, not a
reinvention: pull state and logic out of `App.tsx` into a pure domain, and separate the
effects.

---

## Decision

Adopt a **small custom Elm/TEA core (~150–250 LOC, zero runtime dependencies)**:

- A single `AppState` object (the humble object).
- A pure `update(state, event): AppState`, composed of slice reducers.
- An effects layer: subscriptions keyed by event type, the only impure place; they call
  **ports** and dispatch follow-up events.
- A tiny store/runtime + a React binding via `useSyncExternalStore`.
- `elkjs` and `fetch` become **adapters behind ports**, mirroring the backend's
  `ModelReader`/`ModelWriter`.

Rationale: moderate growth does not justify heavy infrastructure; the project's dependency
philosophy is austere (`web/package.json` carries only `react`, `react-dom`, `elkjs`); and
the layer boundaries (events / pure `update` / effects) are identical across all
candidates, so swapping the custom store for Redux Toolkit later is mechanical and
domain-isolated.

---

## Architecture

### Layers & dependency rule (mirror of the backend hexagon)

```
   view (React, humble) ──emit event──► runtime/store ──► update (pure)
        ▲                                    │                  │
        │ render(state)                      ├── feeds ──► effects (impure) ──┐
        └────────────────────────────────────┘                               │
                                              ▲                               │
                                              └────── dispatch follow-up ◄─────┘

   domain (pure TS): State, Event, update, derivations   ← centre, zero deps
   ports:  LayoutPort, GraphSourcePort, ViewportPort      ← interfaces
   adapters: elkLayout(elkjs), httpGraphSource(fetch), domViewport(DOM)  ← edge
```

**Rule:** the domain knows nothing about React, elk, or the DOM. Effects depend on ports
(interfaces); adapters implement ports. The view depends only on domain types + `dispatch`.

### The state object (humble object)

One `AppState`:

```ts
interface AppState {
  graph: UIGraph | null;                 // raw logical graph (existing type)
  ui: {
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
  };
  markers: Marker[];
  pendingComment: PendingComment | null;
  geometry: {
    laid: UIGraph | null;                // ELK-resolved geometry, filled by the layout effect
    status: 'idle' | 'computing' | 'error';
    error: string | null;
  };
  load: { status: 'loading' | 'ready' | 'error'; error: string | null };
}
```

**Geometry lives inside the same `AppState`** (`geometry.laid`), but is filled by an
**effect**, not a synchronous reducer (ELK is async + expensive). The view stays 100%
dumb and renders one object.

**Pragmatic concession (explicit):** raw `scrollLeft/scrollTop` during a pan-drag and
wheel-zoom anchoring are 60fps DOM imperatives. They are **not** in the store (per-frame
deltas would thrash it). The store holds *intent* (`zoom`, "scroll to component X"); the
drag itself lives in a small imperative controller (`domViewport`) that dispatches only
coarse events (`ZoomChanged`), not per frame. The humble object is not for 60fps deltas.

### Events (discriminated union)

Extracted from today's handlers:

- Lifecycle: `GraphRequested`, `GraphLoaded(graph)`, `GraphLoadFailed(err)`
- Chrome: `ThemeToggled`, `LevelChanged(n)`, `LeftTabChanged`, `LeftCollapsedToggled`,
  `RightCollapsedToggled`
- Expansion: `ComponentToggled(id)`, `InternalWideToggled(id)`, `ComponentAllWideSet(id, wide)`
- Focus/nav: `ComponentSelected(id)`, `FocusCleared`, `ChangeActivated(change)` (=`goToChange`),
  `TreeFocusRequested(target)` (=`focusFromTree`), `ScrollToComponentRequested(id)`,
  `MarkerActivated(id)`
- Zoom: `ZoomChanged(z)`, `ZoomFitRequested`
- Comments: `CommentStarted(target, anchor)`, `CommentSubmitted(text)`, `CommentCancelled`
- Internal (layout): `LayoutRequested`, `LayoutComputed(laidGraph)`, `LayoutFailed(err)`

### `update` — pure reducers by slice

`update(state, event): AppState`, composed from `uiReducer`, `geometryReducer`,
`commentsReducer`, `loadReducer`. Each pure.

Compound flows split cleanly: e.g. `ChangeActivated` = set focus + maybe toggle expand +
scroll. The **reducer performs only the pure state transition** (focus, expanded); the
**DOM scroll is done by an effect subscribed to the same event**. One event → the reducer
owns state, the effect owns the DOM.

### Effects layer (the only impure place)

Subscriptions keyed by event type, run after `update`, may dispatch follow-ups. Signature
`(event, getState, dispatch) => void | Promise<void>`.

- **layoutEffect** ← `GraphLoaded` / `ComponentToggled` / `InternalWideToggled` /
  `ComponentAllWideSet`. Debounced. Calls `LayoutPort.compute(graph, interaction)` →
  dispatches `LayoutComputed` / `LayoutFailed`. Race-guarded by a token (the current
  `cancelled` flag, generalised).
- **loadEffect** ← `GraphRequested` → `GraphSourcePort.load()` → `GraphLoaded` /
  `GraphLoadFailed`.
- **scrollEffect** ← `ChangeActivated` / `TreeFocusRequested` /
  `ScrollToComponentRequested` / `MarkerActivated`. Waits for the next `LayoutComputed`
  (replacing the current `setTimeout(150)`) before calling
  `ViewportPort.scrollToComponent()`.
- *(later)* **persistEffect** for undo/redo and persisting comments to the backend.

Ports (interfaces at the domain/app boundary):

```ts
interface LayoutPort       { compute(graph: UIGraph, interaction: Interaction): Promise<UIGraph>; }
interface GraphSourcePort  { load(): Promise<UIGraph>; }
interface ViewportPort     { scrollToComponent(id: string, geometry: UIGraph): void; /* … */ }
```

Adapters: `elkLayout` wraps `layout/layout.ts`/elkjs; `httpGraphSource` wraps
`data/load.ts`/fetch; `domViewport` does DOM scrollTo / zoom anchoring / pan (the
imperative controller).

### Runtime / store (~50–80 LOC) + React binding

```ts
function createStore(initial, update, effects) {
  let state = initial; const listeners = new Set();
  const getState = () => state;
  const dispatch = (event) => {
    state = update(state, event);
    listeners.forEach((l) => l());
    for (const fx of effects) fx(event, getState, dispatch);
  };
  const subscribe = (fn) => { listeners.add(fn); return () => listeners.delete(fn); };
  return { getState, dispatch, subscribe };
}
```

React binding: `useStore(selector)` via `useSyncExternalStore(subscribe, () =>
selector(getState()))` so components subscribe to thin slices (minimal re-render);
`useDispatch()` returns `dispatch`.

### View layer (humble)

`App.tsx` shrinks from 758 lines to a thin composition: subscribe slices, render panes,
mount the `domViewport` controller. No inline business logic — `goToChange`/`startComment`
become `dispatch(ChangeActivated(...))` etc. Existing components (`Component`, `Tree`,
`ChangesPanel`, `EdgeLayer`, …) change minimally: swap ~12 callback props for `dispatch`.
They are already dumb renderers.

### Folder structure

```
web/src/
  domain/      state.ts · events.ts · update.ts · derive.ts · ports.ts
  effects/     layout.ts · load.ts · viewport.ts · index.ts
  adapters/    elkLayout.ts · httpGraphSource.ts · domViewport.ts
  runtime/     store.ts · react.ts
  view/        App.tsx · AppBar.tsx · Component.tsx · …   (was components/)
  main.tsx     ← composition root: wires adapters → effects → store → <App/>
```

`main.tsx` is the composition root — the frontend analogue of "the CLI does the wiring".

---

## Error handling

Effects catch errors and dispatch `*Failed` events; they never throw into React. A layout
error sets `geometry.status='error'` but **keeps the last good `laid`** (today's "canvas
never flashes empty" behaviour moves into the reducer). A load error sets
`load.status='error'` and the view shows the existing message.

---

## Testing

- **Domain** — pure unit tests on `update(state, event)` per slice + derivations. No
  React, no mocks, table-driven. This is the main win: all interaction logic becomes
  trivially testable.
- **Effects** — fake in-memory ports (`LayoutPort`/`GraphSourcePort`) + a recording
  `dispatch`; assert the dispatched follow-ups. Deterministic, no jsdom.
- **View / e2e** — the existing Testing Library + Playwright harnesses
  (`web/testing/harness`, `web/e2e`) remain, only thinner. `layout/layout.test.ts` stays
  as an adapter-level test.

---

## Alternatives considered

| Option | What it buys | Why not chosen |
|---|---|---|
| **Redux Toolkit + listener middleware** | Battle-tested; `createListenerMiddleware` is a 1:1 fit for "events → effects → follow-up event"; Redux DevTools time-travel; `redux-undo`. | Adds dependencies (RTK + immer) and ceremony; against the project's ultra-lean dependency posture. |
| **Effector** | Tiny runtime; `createEvent`/`createStore`/`createEffect` map closely to the desired model; built-in effect isolation; great TS. | Another paradigm, less team/ecosystem familiarity. The best off-the-shelf fallback if "batteries-included" is later wanted without Redux ceremony. |
| **XState** | Statecharts fit if interaction *modes* multiply (focus/comment/pan are literally states); invoked actors = effects. | "One big state object" is less natural inside machine `context`; steeper learning curve; mode-heavy more than data-heavy. |

**Migration path away from the custom core:** because the events / `update` / effects
boundaries are identical, replacing the custom store with RTK (or Effector) is mechanical
and does not touch the domain.

---

## Migration outline (orientation only — implementation deferred)

- **Phase 0** — extract pure derivations (`deriveChanges`, `related`, layout inputs) into `domain/derive.ts`.
- **Phase 1** — introduce store + events for one slice (focus/expand) end-to-end as the reference vertical.
- **Phase 2** — move layout + load into `effects/` behind ports/adapters.
- **Phase 3** — retire the scattered `useState`/`useRef` in `App.tsx`.
- **Phase 4** — collapse `App.tsx` into the thin composition root.

Each phase keeps the app shippable and the existing e2e harnesses green.
