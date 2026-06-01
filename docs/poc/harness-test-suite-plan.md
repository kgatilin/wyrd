# Plan: Harness-based test suite for the archai review UI

## Context

Over this session we shipped a large batch of UI behavior in `web/` (Vite+React+TS
architecture-review app) across several iterations: diff-mode rendering, removal of
in-card NEW/MOD tags, dropping strike-through on removed elements, deriving `changed`
from children, grouped header action buttons, expand-all + per-internal fit-width modes,
parent-initial header icons, collapsed-card header fitting, the pannable/zoomable canvas,
the context tree, and the de-duplicated Changes panel. All verified manually; nothing is
locked by an automated regression test except `src/layout/layout.test.ts` (20 unit tests).

The user wants more than tests — they want a **hard architectural boundary between tests
and the implementation**, exactly the Angular CDK Component-Harness pattern: describe the
UI **domain** (BoundedContext, Component, Internal, Member, Edge, Change, Canvas, panels),
give each a **harness** that hides the DOM, and have tests speak only through harnesses.
Crucially, the **same harnesses** drive both Playwright e2e and Vitest unit tests, so a UI
refactor means updating one harness, not rewriting tests; and changing a component's
external interaction surface breaks the fast unit tests first, forcing the harness update
that keeps e2e green.

**Approved scope:** Harness layer + both env adapters + **full e2e coverage** of every
session feature on harnesses + a **representative unit slice** (`*.harness.test.tsx`)
proving the same harnesses run in jsdom. Unit coverage expands later. **Browsers:** system
Chrome via `channel: 'chrome'` (no bundled download).

## Architecture: shared Component Harnesses (CDK-style)

A small `TestElement` abstraction (async) with two adapters; harnesses are written once
against it and the environment is swapped per test tier.

```
web/
  testing/
    fixtures.ts                  # diffGraph (= src fixture), nonDiffGraph, longMemberGraph
    harness/
      test-element.ts            # TestElement interface + HarnessLoader + base ComponentHarness
      playwright-env.ts          # PlaywrightElement(Locator) + env.load(AppHarness, page)
      dom-env.ts                 # DomElement(Element) + env.load(AppHarness) via RTL render
      app.harness.ts             # AppHarness: appBar, prHeader, leftPanel, diagram, legend
      diagram.harness.ts         # boundedContexts(), component(name), edges(), canvas()
      component-card.harness.ts  # name/tech/parentInitial/diffState/isExpanded/expand…
      internal.harness.ts        # name/kind/diffState/members/toggleFitWidth/width/title
      member.harness.ts          # name/diffState/rowTitle
      changes-panel.harness.ts   # entries(), hasPrSummary()
      context-tree.harness.ts    # boundedContexts(), expand(name), componentRows(), click…
      canvas.harness.ts          # cursor/sizerExceedsContent/pan/scrollPos/zoom*/ctrlWheel
      legend.harness.ts          # present(), items()
      comment-popover.harness.ts # tag(), target(), type(), submit(), cancel()
  e2e/        *.spec.ts           # Playwright tier — env = PlaywrightHarnessEnvironment
  src/        **/*.harness.test.tsx  # Vitest+RTL tier — env = DomHarnessEnvironment
```

### `TestElement` (async, both envs implement it)
`click()`, `hover()`, `text()`, `getAttribute(name)`, `hasClass(c)`, `classes()`,
`isVisible()`, `styleProp(name)` (inline `style.*` — carries ELK geometry in BOTH envs),
`boundingBox()` (real rendered box — meaningful in e2e). Plus a `HarnessLoader`:
`locator(sel)` → `{ all(), first(), nth(i), filterByText(s) }`. Base `ComponentHarness`
holds a root `TestElement` + child `HarnessLoader`.

- **PlaywrightElement** wraps a Playwright `Locator` (already async; all ops native).
- **DomElement** wraps a real `Element` from `@testing-library/react` render; same async
  surface over sync DOM (click via `fireEvent`, hover via `mouseover`, etc.).

### Harnesses speak the UI domain (no `.hf-*`/clicks in specs)
```ts
const app  = await env.load(AppHarness);
const svc  = await (await app.diagram()).component('OrderService');
expect(await svc.parentInitial()).toBe('O');
await svc.expandAll();
expect(await svc.expandAllGlyph()).toBe('»«');
expect(await (await svc.internal('IGateway')).diffState()).toBe('changed');
```
Selector knowledge (the `.hf-*` map) lives ONLY inside the harness files. Specs never
touch the DOM directly.

### Tier split (same harness, different assertion depth)
jsdom has no layout engine and Vite CSS isn't applied in vitest, so:

| Concern | Unit (jsdom) | E2E (Chrome) |
|---|---|---|
| structure/state/classes/text/attrs: diff derivation, expand/expand-all toggle, tabs, tree expand, removed-class, title tooltips, parent initial, no-in-card-tags, changes-panel de-dup | ✅ | ✅ |
| ELK geometry x/y/w/h via inline `styleProp()` (ELK computes numbers without DOM) | ✅ | ✅ |
| rendered box (button no-overlap, popover-above, real width growth), pan/drag/zoom, computed-style strike-through | exposed but not asserted | ✅ |

Geometry/visual harness methods (`boundingBox`, `pan`, computed strike-through) are
exercised in e2e; unit asserts the structural equivalent (class present, glyph toggled).

## Files to create

- `web/playwright.config.ts` — `testDir:'./e2e'`, `use.baseURL:'http://localhost:4317'`,
  `projects:[{name:'chrome', use:{...devices['Desktop Chrome'], channel:'chrome'}}]`,
  `webServer:{ command:'./node_modules/.bin/vite --port 4317 --strictPort',
  url:'http://localhost:4317', reuseExistingServer:!process.env.CI, timeout:120_000 }`.
- `web/testing/fixtures.ts` — `diffGraph` (`import { fixture }`), `nonDiffGraph` (1 BC, 2
  cmps, no `pr`/diffs), `longMemberGraph` (1 cmp/1 internal with a 50+ char member).
- `web/testing/harness/*` — the abstraction + env adapters + domain harnesses listed above.
- `web/e2e/*.spec.ts` — full coverage on harnesses (mapping below).
- `web/src/components/__tests__/harness-smoke.harness.test.tsx` — unit slice proving the
  same harnesses run under `DomHarnessEnvironment` for the structural subset.

### Data wiring
- E2E: `page.route('**/archgraph.json', r=>r.fulfill({json:graph}))` + route
  `**/archgraph.sample.json` → `abort()`, set BEFORE `goto('/')`. Independent of the
  gitignored `public/archgraph.json`.
- Unit: `vi.stubGlobal('fetch', …)` returning the graph (App calls `loadGraph()` →
  `/archgraph.json`); render `<App/>` with RTL; `await` until harness root resolves
  (ELK is async; elkjs runs main-thread in jsdom, no Worker).

## Coverage map (e2e specs, all via harnesses)

**diff-mode.spec.ts** (`diffGraph`, 5 cmps): PR header title + branch crumb; CHANGES tab
default + 38 entries + CONTEXTS tab present; legend 3 items (diff-only); component diff
colors (OrderEvents added; OrderService/PaymentService/Notifier changed); **derived**
CheckoutAPI=changed; edge diff count=5; **no in-card tags**; expand PaymentService →
IGateway **derived** changed; removed member + removed port label **not struck**
(computed style); Changes panel **no** PR-summary dup. Plus `nonDiffGraph`: no PR header,
no CHANGES tab, no legend, zero diff classes.

**component-card.spec.ts** (`diffGraph` unless noted): OrderService auto-expanded;
parent-initial icons O/P/N; grouped actions (i)+«»+− non-overlapping & within card;
(i) popover opens above on hover; collapsed actions = single `+`; collapsed header
one-line (tech not wrapped, card>220); expand/collapse toggle; expand-all glyph
round-trip + no console errors; **fit-width widen** (`longMemberGraph`) width grows then
restores; member/internal `title` tooltips; collapsed height ≥ ~120.

**canvas.spec.ts** (`diffGraph`): cursor `grab`; sizer > content; drag pans (scroll delta)
+ `.panning` cleared; drag preserves `.focused`; zoom +/−/fit label changes; ctrl+wheel
zoom changes scale (no page scroll); port labels visible on hover.

**context-tree.spec.ts** (`diffGraph`): CONTEXTS tab → BC rows; chevron expand → cmp →
internal → member rows; click component row → that card `.focused`; diff badges (+/-/~).

**harness-smoke.harness.test.tsx** (vitest+RTL, `diffGraph`): same `AppHarness` →
diagram loads 5 components; OrderService expanded; parentInitial 'O'; expandAll glyph
toggles; IGateway derived 'changed'; CheckoutAPI derived 'changed'; Changes entries
present & no PR-summary; CONTEXTS tab switch works. (Structural subset only.)

## Files to modify

- `web/package.json`: add devDeps `@playwright/test`, `@testing-library/react`,
  `@testing-library/dom`; scripts `"e2e":"playwright test"`,
  `"e2e:headed":"playwright test --headed"`.
- `web/vite.config.ts`: scope vitest so it ignores Playwright specs and only runs unit
  files — `test:{ environment:'jsdom', include:['src/**/*.{test,harness.test}.{ts,tsx}'] }`
  (the existing `src/layout/layout.test.ts` keeps matching).

## Anti-flake notes

Register routes before `goto`; gate every test on a `waitForLoaded()` harness method
(ELK is async — components mount only once laid); prefer class/text/`styleProp` over
rendered pixels; for true geometry (overlap, drag, popover-above) add a short settle and
use bounding-box comparisons with small tolerances. No `data-testid` added to production
(repo rule) — selector knowledge stays inside harnesses.

## Verification

From `web/` (absolute paths due to nvm breakage):
1. `/opt/homebrew/bin/npm i -D @playwright/test @testing-library/react @testing-library/dom`
   (no browser download — system Chrome via `channel:'chrome'`).
2. `./node_modules/.bin/tsc --noEmit` clean.
3. `./node_modules/.bin/vitest run` → existing 20 layout tests + new harness-smoke unit
   tests green (proves harnesses work in jsdom).
4. `/opt/homebrew/bin/npx playwright test` → full e2e suite green on Chrome
   (`--headed` to watch; `playwright show-report` on failure).
5. Commit config + `testing/` + `e2e/` + the unit slice + package.json/vite.config edits
   on `poc/arch-review-ui`. (Gitignored `public/archgraph.json` stays out.)
