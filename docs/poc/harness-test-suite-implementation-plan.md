# Harness-Based Test Suite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The design rationale lives in the sibling file `docs/poc/harness-test-suite-plan.md`; this file is the executable, no-placeholder plan.

**Goal:** Lock every UI behavior shipped this session behind an automated regression suite that speaks only through a hard test↔implementation boundary (Angular CDK-style Component Harnesses), with the *same* harnesses driving both Playwright e2e and Vitest+RTL unit tiers.

**Architecture:** A tiny async `TestElement`/`Locator`/`HarnessEnvironment` abstraction has two adapters — Playwright (`Locator`-backed) and jsdom+RTL (`Element`-backed). Domain harnesses (App, Diagram, ComponentCard, Internal, Member, Legend, ChangesPanel, ContextTree, Canvas, CommentPopover) are written once against that abstraction. Specs never touch the DOM — all `.hf-*` selector knowledge lives inside harness files. No `data-testid` is added to production (repo rule).

**Tech Stack:** TypeScript, React 18, Vite 5, Vitest 2, `@testing-library/react` 16, `@playwright/test` 1.60 (system Chrome via `channel:'chrome'`, no bundled download), elkjs (runs main-thread in jsdom).

**Toolchain note (nvm is broken non-interactively):** always run npm/npx via the absolute Homebrew paths and project-local binaries:
- `/opt/homebrew/bin/npm`, `/opt/homebrew/bin/npx`, `/opt/homebrew/bin/node`
- `./node_modules/.bin/tsc`, `./node_modules/.bin/vitest`
- All commands below run from `/Users/forkiy/Projects/archai/web`.

**Branch:** `poc/arch-review-ui`.

---

## ⚠️ Corrections applied during execution

Two issues surfaced while making the unit slice (Task 9) green in jsdom. The code blocks below in Tasks 4/7/8 are written as originally planned; apply these two corrections (the committed source already has them):

1. **`DomEnvironment.waitUntil` (Task 4)** — RTL's `waitFor` with an **async** callback does not re-poll reliably here and hangs. Use a plain `setTimeout` poll loop instead (same shape as the Playwright adapter), and import `act` is **not** needed. Replace the `waitFor` import with `import { render, fireEvent } from '@testing-library/react';` and the body with:
   ```ts
   const timeout = opts?.timeout ?? 5000;
   const interval = opts?.interval ?? 30;
   const deadline = Date.now() + timeout;
   for (;;) {
     if (await predicate()) return;
     if (Date.now() >= deadline) throw new Error(opts?.message ?? 'waitUntil predicate not satisfied');
     await new Promise((resolve) => setTimeout(resolve, interval));
   }
   ```

2. **Singleton harnesses must query document-scoped (Tasks 7 & 8)** — `App` renders a *Loading* `.hifi` div first, then swaps it for `AppContent`'s own `.hifi` once the graph resolves. The DOM adapter captures a concrete `Element` at `load()`, so a root captured pre-load goes stale after the swap (Playwright's lazy locators don't hit this). Therefore the page-level singleton harnesses — **`AppHarness`, `LegendHarness`, `ChangesPanelHarness`, `ContextTreeHarness`, `CommentPopoverHarness`** — use `this.env.rootLocator(...)` (document-scoped, live in both tiers) instead of `this.root.locator(...)`. The **element-scoped** harnesses (`DiagramHarness`, `ComponentCardHarness`, `InternalHarness`, `MemberHarness`, `CanvasHarness`) keep `this.root.locator(...)` — their roots are captured *after* load, address stable subtrees, and MUST stay subtree-scoped.

---

## File structure

```
web/
  playwright.config.ts                 # Task 1  (e2e runner config; webServer = vite :4317)
  vite.config.ts                        # Task 1  (modify: scope vitest test.include)
  package.json                          # Task 1  (modify: e2e scripts)
  testing/
    fixtures.ts                         # Task 2  (diffGraph, nonDiffGraph, longMemberGraph)
    harness/
      test-element.ts                   # Task 3  (TestElement + Locator + env + base ComponentHarness)
      dom-env.ts                        # Task 4  (DomElement/DomLocator/DomEnvironment + mountAppDom)
      playwright-env.ts                 # Task 5  (PlaywrightElement/Locator/Environment + routeGraph)
      member.harness.ts                 # Task 6
      internal.harness.ts               # Task 6
      component-card.harness.ts         # Task 6
      legend.harness.ts                 # Task 7
      changes-panel.harness.ts          # Task 7
      context-tree.harness.ts           # Task 7
      comment-popover.harness.ts        # Task 7
      canvas.harness.ts                 # Task 7
      diagram.harness.ts                # Task 8
      app.harness.ts                    # Task 8
  e2e/
    diff-mode.spec.ts                   # Task 10
    component-card.spec.ts              # Task 11
    canvas.spec.ts                      # Task 12
    context-tree.spec.ts               # Task 13
  src/components/__tests__/
    harness-smoke.harness.test.tsx      # Task 9  (unit slice — TDD red→green anchor for Tasks 3–8)
```

### Fixture facts the specs assert against (from `src/data/fixture.ts`)
- 5 components: `CheckoutAPI` (bc `ordering`, tech `Go - gRPC`, **no own diff** but child `IEventBus`+port added ⇒ **derived `changed`**), `OrderService` (bc `ordering`, `Go`, diff `changed`, **auto-expanded**), `OrderEvents` (bc `ordering`, diff `added`), `PaymentService` (bc `payments`, diff `changed`; internal `IGateway` has **no own diff** but members add/add/remove ⇒ **derived `changed`**), `Notifier` (bc `notify`, diff `changed`).
- Parent-initials: ordering→`O`, payments→`P`, notify→`N`.
- 3 bounded contexts. 6 edges, **5 with a diff** (only `e1` has none).
- `deriveChanges(fixture)` length = **38** (4 component + 6 internal + 12 member + 11 port + 5 edge).
- Removed-not-struck targets: member `charge(amt)` in `PaymentService→IGateway`; removed port labels `charge()`/`ordersDB read`.

---

## Task 1: Tooling config (Playwright + Vitest scoping + scripts)

**Files:**
- Create: `web/playwright.config.ts`
- Modify: `web/vite.config.ts`
- Modify: `web/package.json`

- [ ] **Step 1: Create `web/playwright.config.ts`**

```ts
import { defineConfig, devices } from '@playwright/test';

/**
 * E2E runner. Uses the already-installed system Chrome (channel:'chrome'),
 * so `npx playwright install` is NOT required. The dev server is started on a
 * fixed strict port (4317) via the project-local vite binary — NOT npm — to
 * sidestep the broken nvm shim in non-interactive shells.
 */
export default defineConfig({
  testDir: './e2e',
  testMatch: /.*\.spec\.ts$/,
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'line' : [['list'], ['html', { open: 'never' }]],
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: 'http://localhost:4317',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chrome',
      use: { ...devices['Desktop Chrome'], channel: 'chrome' },
    },
  ],
  webServer: {
    command: './node_modules/.bin/vite --port 4317 --strictPort',
    url: 'http://localhost:4317',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
```

- [ ] **Step 2: Modify `web/vite.config.ts` — scope vitest to unit files only**

Replace the whole file with:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: false,
  },
  test: {
    environment: 'jsdom',
    // Only run unit files — never Playwright's e2e/*.spec.ts (which import
    // @playwright/test and would crash under Vitest). The existing
    // src/layout/layout.test.ts keeps matching the {test} branch.
    include: ['src/**/*.{test,harness.test}.{ts,tsx}'],
  },
});
```

- [ ] **Step 3: Modify `web/package.json` — add e2e scripts**

In the `"scripts"` block, add the two e2e scripts so it reads:

```json
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "e2e": "playwright test",
    "e2e:headed": "playwright test --headed"
  },
```

(Leave `dependencies`/`devDependencies` unchanged — `@playwright/test`, `@testing-library/react`, `@testing-library/dom` are already installed.)

- [ ] **Step 4: Verify config compiles and existing unit tests still pass**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: no output (clean).

Run: `./node_modules/.bin/vitest run`
Expected: `src/layout/layout.test.ts` → 20 passed; no Playwright spec picked up.

- [ ] **Step 5: Commit**

```bash
git add web/playwright.config.ts web/vite.config.ts web/package.json
git commit -m "test(web): add playwright config + scope vitest to unit files"
```

---

## Task 2: Test fixtures

**Files:**
- Create: `web/testing/fixtures.ts`

- [ ] **Step 1: Create `web/testing/fixtures.ts`**

```ts
import type { UIGraph } from '../src/types';
import { fixture } from '../src/data/fixture';

/**
 * The rich diff dataset (PR + diffs + comments). Re-exported from the app's own
 * fixture so the suite and the app stay in lock-step. 5 components, 3 bounded
 * contexts, 6 edges (5 diffed), deriveChanges length 38.
 */
export const diffGraph: UIGraph = fixture;

/**
 * Non-diff graph: no `pr` (so showDiff=false → no PR header, no CHANGES tab, no
 * legend, no diff classes), 1 bounded context, 2 plain components.
 */
export const nonDiffGraph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'core', name: 'Core', x: 40, y: 40, w: 520, h: 240 }],
  components: [
    {
      id: 'svcA',
      name: 'ServiceA',
      tech: 'Go',
      desc: 'Plain service A.',
      bc: 'core',
      x: 70,
      y: 90,
      w: 220,
      h: 86,
      wx: 280,
      hx: 200,
      internals: [
        {
          id: 'svcA.Impl',
          kind: 'class',
          name: 'Impl',
          x: 16,
          y: 40,
          w: 110,
          h: 36,
          members: [{ id: 'svcA.Impl.run', kind: 'method', name: 'run()' }],
        },
      ],
      ports: [{ id: 'svcA.in', side: 'left', y: 58, name: 'callA', kind: 'in' }],
    },
    {
      id: 'svcB',
      name: 'ServiceB',
      tech: 'Go',
      desc: 'Plain service B.',
      bc: 'core',
      x: 340,
      y: 90,
      w: 220,
      h: 86,
      wx: 280,
      hx: 200,
      internals: [
        {
          id: 'svcB.Impl',
          kind: 'class',
          name: 'Impl',
          x: 16,
          y: 40,
          w: 110,
          h: 36,
          members: [{ id: 'svcB.Impl.run', kind: 'method', name: 'run()' }],
        },
      ],
      ports: [{ id: 'svcB.in', side: 'left', y: 58, name: 'callB', kind: 'in' }],
    },
  ],
  edges: [
    { id: 'e1', from: 'svcA', to: 'svcB', fromPort: 'svcA.out', toPort: 'svcB.in', label: 'calls' },
  ],
  comments: [],
};

/**
 * One component / one internal whose member name is 50+ chars, so toggling
 * fit-width visibly grows the internal card width (INTERNAL_W=180 → much wider).
 * Component id is the first (and only) one, which App auto-expands on load.
 */
const LONG_MEMBER_NAME =
  'reconcileOutstandingSettlementsWithLedgerSnapshot(window)'; // 56 chars

export const longMemberGraph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'ledger', name: 'Ledger', x: 40, y: 40, w: 600, h: 320 }],
  components: [
    {
      id: 'reconciler',
      name: 'Reconciler',
      tech: 'Go',
      desc: 'Reconciles settlements.',
      bc: 'ledger',
      x: 70,
      y: 90,
      w: 220,
      h: 86,
      wx: 320,
      hx: 220,
      internals: [
        {
          id: 'reconciler.Engine',
          kind: 'class',
          name: 'Engine',
          x: 16,
          y: 40,
          w: 110,
          h: 36,
          members: [
            { id: 'reconciler.Engine.long', kind: 'method', name: LONG_MEMBER_NAME },
            { id: 'reconciler.Engine.tick', kind: 'method', name: 'tick()' },
          ],
        },
      ],
      ports: [{ id: 'reconciler.in', side: 'left', y: 58, name: 'run', kind: 'in' }],
    },
  ],
  edges: [],
  comments: [],
};
```

- [ ] **Step 2: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add web/testing/fixtures.ts
git commit -m "test(web): add harness fixtures (diff/non-diff/long-member)"
```

---

## Task 3: Harness core abstraction

**Files:**
- Create: `web/testing/harness/test-element.ts`

This file defines the *only* contract specs and harnesses are allowed to know. Every method name here is referenced verbatim by later tasks — do not rename.

- [ ] **Step 1: Create `web/testing/harness/test-element.ts`**

```ts
/**
 * Environment-agnostic test abstraction (Angular CDK Component-Harness style).
 *
 * Harnesses are written ONCE against these interfaces. The concrete TestElement
 * / Locator / HarnessEnvironment are swapped per tier:
 *   - Playwright (Locator-backed) for e2e (real browser geometry).
 *   - jsdom + @testing-library/react (Element-backed) for unit.
 *
 * Specs NEVER touch the DOM directly — all `.hf-*` selector knowledge lives in
 * the harness files, so a UI refactor means updating one harness, not rewriting
 * tests.
 */

export type DiffState = 'added' | 'removed' | 'changed';

export interface BoundingBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** A single element, wrapped so the same calls work in both tiers. */
export interface TestElement {
  click(): Promise<void>;
  hover(): Promise<void>;
  /** Set the value of an input/textarea. */
  fill(value: string): Promise<void>;
  /** Trimmed textContent. */
  text(): Promise<string>;
  getAttribute(name: string): Promise<string | null>;
  classes(): Promise<string[]>;
  hasClass(name: string): Promise<boolean>;
  isVisible(): Promise<boolean>;
  /** Inline style property, e.g. styleProp('width') → '220px'. Carries ELK
   *  geometry in BOTH tiers (React writes geometry as inline styles). */
  styleProp(name: string): Promise<string>;
  /** Computed style. Only meaningful in e2e — jsdom does not apply stylesheets. */
  computedStyleProp(name: string): Promise<string>;
  /** Rendered box. Only meaningful in e2e. */
  boundingBox(): Promise<BoundingBox | null>;
  /** scrollLeft/scrollTop of this element. */
  scrollPosition(): Promise<{ left: number; top: number }>;
  /** Scoped sub-query (re-evaluated lazily). */
  locator(selector: string): Locator;
}

/** A lazy, re-evaluated query for zero or more elements. */
export interface Locator {
  all(): Promise<TestElement[]>;
  count(): Promise<number>;
  /** nth match; throws if out of range. */
  nth(index: number): Promise<TestElement>;
  /** Shorthand for nth(0). */
  first(): Promise<TestElement>;
  /** Keep only elements whose textContent contains `substring`. */
  filterByText(substring: string): Locator;
  /** Chained scoping. */
  locator(selector: string): Locator;
}

export interface WaitOptions {
  timeout?: number;
  interval?: number;
  message?: string;
}

/** Per-tier capabilities harnesses need beyond a single element. */
export interface HarnessEnvironment {
  /** Document-scoped query. */
  rootLocator(selector: string): Locator;
  /** Retry `predicate` until it resolves true, or throw after `timeout`. */
  waitUntil(predicate: () => Promise<boolean>, opts?: WaitOptions): Promise<void>;
  /** Grab the empty top-left margin of `target` and drag by (dx, dy) px.
   *  Real mouse in e2e; event-dispatch in unit (exposed, not asserted there). */
  panDrag(target: TestElement, dx: number, dy: number): Promise<void>;
  /** Ctrl+wheel over the center of `target` (zoom gesture). */
  ctrlWheel(target: TestElement, deltaY: number): Promise<void>;
  /** Construct the top-level harness rooted at `.hifi`. */
  load<T extends ComponentHarness>(ctor: HarnessConstructor<T>): Promise<T>;
}

export type HarnessConstructor<T extends ComponentHarness> = new (
  root: TestElement,
  env: HarnessEnvironment
) => T;

/** Base class: every harness owns a root element + its environment.
 *  `env` is public so specs can `await app.env.waitUntil(...)` for ad-hoc
 *  state transitions (ELK is async). `root` stays protected — selector
 *  knowledge never leaks past a harness. */
export abstract class ComponentHarness {
  constructor(
    protected readonly root: TestElement,
    public readonly env: HarnessEnvironment
  ) {}
}

/** Parse a px-suffixed inline style value ('220px' → 220, '' → NaN). */
export function parsePx(value: string): number {
  return parseFloat(value);
}

/** Pick the diff state token out of a class list, if any. */
export function diffStateFromClasses(classes: string[]): DiffState | null {
  if (classes.includes('added')) return 'added';
  if (classes.includes('removed')) return 'removed';
  if (classes.includes('changed')) return 'changed';
  return null;
}
```

- [ ] **Step 2: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add web/testing/harness/test-element.ts
git commit -m "test(web): add TestElement/Locator/HarnessEnvironment abstraction"
```

---

## Task 4: jsdom + RTL environment adapter

**Files:**
- Create: `web/testing/harness/dom-env.ts`

- [ ] **Step 1: Create `web/testing/harness/dom-env.ts`**

```ts
import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { vi } from 'vitest';
import App from '../../src/App';
import type { UIGraph } from '../../src/types';
import {
  ComponentHarness,
  HarnessConstructor,
  HarnessEnvironment,
  Locator,
  TestElement,
  BoundingBox,
  WaitOptions,
} from './test-element';

/** A real DOM Element wrapped behind the async TestElement surface. */
export class DomElement implements TestElement {
  constructor(readonly el: Element) {}

  async click(): Promise<void> {
    fireEvent.click(this.el); // RTL auto-wraps in act()
  }
  async hover(): Promise<void> {
    fireEvent.mouseOver(this.el);
    fireEvent.mouseEnter(this.el);
  }
  async fill(value: string): Promise<void> {
    fireEvent.change(this.el, { target: { value } });
  }
  async text(): Promise<string> {
    return (this.el.textContent ?? '').trim();
  }
  async getAttribute(name: string): Promise<string | null> {
    return this.el.getAttribute(name);
  }
  async classes(): Promise<string[]> {
    return Array.from(this.el.classList);
  }
  async hasClass(name: string): Promise<boolean> {
    return this.el.classList.contains(name);
  }
  async isVisible(): Promise<boolean> {
    const e = this.el as HTMLElement;
    if (!e.isConnected) return false;
    const cs = getComputedStyle(e);
    if (cs.display === 'none' || cs.visibility === 'hidden') return false;
    if (e.style && e.style.opacity === '0') return false;
    return true;
  }
  async styleProp(name: string): Promise<string> {
    return (this.el as HTMLElement).style.getPropertyValue(name);
  }
  async computedStyleProp(name: string): Promise<string> {
    return getComputedStyle(this.el).getPropertyValue(name);
  }
  async boundingBox(): Promise<BoundingBox | null> {
    const r = this.el.getBoundingClientRect();
    return { x: r.x, y: r.y, width: r.width, height: r.height };
  }
  async scrollPosition(): Promise<{ left: number; top: number }> {
    return { left: (this.el as HTMLElement).scrollLeft, top: (this.el as HTMLElement).scrollTop };
  }
  locator(selector: string): Locator {
    return new DomLocator(() => Array.from(this.el.querySelectorAll(selector)));
  }
}

/** A lazy query over the live DOM (re-runs on each terminal op so it reflects
 *  React re-renders). */
export class DomLocator implements Locator {
  constructor(private readonly resolve: () => Element[]) {}

  async all(): Promise<TestElement[]> {
    return this.resolve().map((el) => new DomElement(el));
  }
  async count(): Promise<number> {
    return this.resolve().length;
  }
  async nth(index: number): Promise<TestElement> {
    const els = this.resolve();
    const el = els[index];
    if (!el) throw new Error(`DomLocator.nth(${index}): only ${els.length} match(es)`);
    return new DomElement(el);
  }
  first(): Promise<TestElement> {
    return this.nth(0);
  }
  filterByText(substring: string): Locator {
    return new DomLocator(() =>
      this.resolve().filter((el) => (el.textContent ?? '').includes(substring))
    );
  }
  locator(selector: string): Locator {
    return new DomLocator(() =>
      this.resolve().flatMap((el) => Array.from(el.querySelectorAll(selector)))
    );
  }
}

export class DomEnvironment implements HarnessEnvironment {
  rootLocator(selector: string): Locator {
    return new DomLocator(() => Array.from(document.querySelectorAll(selector)));
  }

  async waitUntil(predicate: () => Promise<boolean>, opts?: WaitOptions): Promise<void> {
    await waitFor(
      async () => {
        if (!(await predicate())) {
          throw new Error(opts?.message ?? 'waitUntil predicate not satisfied');
        }
      },
      { timeout: opts?.timeout ?? 5000, interval: opts?.interval ?? 30 }
    );
  }

  async panDrag(target: TestElement, dx: number, dy: number): Promise<void> {
    const el = (target as DomElement).el;
    const box = el.getBoundingClientRect();
    const sx = box.x + 8;
    const sy = box.y + 8;
    fireEvent.mouseDown(el, { button: 0, clientX: sx, clientY: sy });
    fireEvent.mouseMove(window, { clientX: sx + dx, clientY: sy + dy });
    fireEvent.mouseUp(window, { clientX: sx + dx, clientY: sy + dy });
  }

  async ctrlWheel(target: TestElement, deltaY: number): Promise<void> {
    const el = (target as DomElement).el;
    fireEvent.wheel(el, { deltaY, ctrlKey: true });
  }

  async load<T extends ComponentHarness>(ctor: HarnessConstructor<T>): Promise<T> {
    const root = await this.rootLocator('.hifi').first();
    return new ctor(root, this);
  }
}

/**
 * Stub fetch so loadGraph() resolves to `graph` (App fetches /archgraph.json
 * first), render <App/>, and return a DomEnvironment. Call cleanup() and
 * vi.unstubAllGlobals() in the test's afterEach.
 */
export async function mountAppDom(graph: UIGraph): Promise<DomEnvironment> {
  const okJson = (data: unknown) =>
    ({ ok: true, json: async () => data } as unknown as Response);
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes('archgraph.json')) return okJson(graph);
    return { ok: false, json: async () => ({}) } as unknown as Response;
  });
  render(React.createElement(App));
  return new DomEnvironment();
}
```

- [ ] **Step 2: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add web/testing/harness/dom-env.ts
git commit -m "test(web): add jsdom+RTL harness environment adapter"
```

---

## Task 5: Playwright environment adapter

**Files:**
- Create: `web/testing/harness/playwright-env.ts`

- [ ] **Step 1: Create `web/testing/harness/playwright-env.ts`**

```ts
import type { Page, Locator as PWLocator } from '@playwright/test';
import type { UIGraph } from '../../src/types';
import {
  ComponentHarness,
  HarnessConstructor,
  HarnessEnvironment,
  Locator,
  TestElement,
  BoundingBox,
  WaitOptions,
} from './test-element';

class PlaywrightElement implements TestElement {
  constructor(readonly loc: PWLocator) {}

  async click(): Promise<void> {
    await this.loc.click();
  }
  async hover(): Promise<void> {
    await this.loc.hover();
  }
  async fill(value: string): Promise<void> {
    await this.loc.fill(value);
  }
  async text(): Promise<string> {
    return ((await this.loc.textContent()) ?? '').trim();
  }
  async getAttribute(name: string): Promise<string | null> {
    return this.loc.getAttribute(name);
  }
  async classes(): Promise<string[]> {
    const c = await this.loc.getAttribute('class');
    return c ? c.split(/\s+/).filter(Boolean) : [];
  }
  async hasClass(name: string): Promise<boolean> {
    return (await this.classes()).includes(name);
  }
  async isVisible(): Promise<boolean> {
    return this.loc.isVisible();
  }
  async styleProp(name: string): Promise<string> {
    return this.loc.evaluate((el, n) => (el as HTMLElement).style.getPropertyValue(n), name);
  }
  async computedStyleProp(name: string): Promise<string> {
    return this.loc.evaluate((el, n) => getComputedStyle(el as Element).getPropertyValue(n), name);
  }
  async boundingBox(): Promise<BoundingBox | null> {
    return this.loc.boundingBox();
  }
  async scrollPosition(): Promise<{ left: number; top: number }> {
    return this.loc.evaluate((el) => ({ left: el.scrollLeft, top: el.scrollTop }));
  }
  locator(selector: string): Locator {
    return new PlaywrightLocator(this.loc.locator(selector));
  }
}

class PlaywrightLocator implements Locator {
  constructor(readonly loc: PWLocator) {}

  async all(): Promise<TestElement[]> {
    return (await this.loc.all()).map((l) => new PlaywrightElement(l));
  }
  count(): Promise<number> {
    return this.loc.count();
  }
  async nth(index: number): Promise<TestElement> {
    return new PlaywrightElement(this.loc.nth(index));
  }
  async first(): Promise<TestElement> {
    return new PlaywrightElement(this.loc.first());
  }
  filterByText(substring: string): Locator {
    return new PlaywrightLocator(this.loc.filter({ hasText: substring }));
  }
  locator(selector: string): Locator {
    return new PlaywrightLocator(this.loc.locator(selector));
  }
}

export class PlaywrightEnvironment implements HarnessEnvironment {
  constructor(private readonly page: Page) {}

  rootLocator(selector: string): Locator {
    return new PlaywrightLocator(this.page.locator(selector));
  }

  async waitUntil(predicate: () => Promise<boolean>, opts?: WaitOptions): Promise<void> {
    const timeout = opts?.timeout ?? 10_000;
    const interval = opts?.interval ?? 50;
    const deadline = Date.now() + timeout;
    for (;;) {
      if (await predicate()) return;
      if (Date.now() > deadline) throw new Error(opts?.message ?? 'waitUntil timed out');
      await this.page.waitForTimeout(interval);
    }
  }

  async panDrag(target: TestElement, dx: number, dy: number): Promise<void> {
    const box = await target.boundingBox();
    if (!box) throw new Error('panDrag: target has no bounding box');
    // Grab the empty top-left margin (the diagram is shifted in by PAN_MARGIN).
    const sx = box.x + 8;
    const sy = box.y + 8;
    await this.page.mouse.move(sx, sy);
    await this.page.mouse.down();
    await this.page.mouse.move(sx + dx, sy + dy, { steps: 8 });
    await this.page.mouse.up();
  }

  async ctrlWheel(target: TestElement, deltaY: number): Promise<void> {
    const box = await target.boundingBox();
    if (!box) throw new Error('ctrlWheel: target has no bounding box');
    await this.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await this.page.keyboard.down('Control');
    await this.page.mouse.wheel(0, deltaY);
    await this.page.keyboard.up('Control');
  }

  async load<T extends ComponentHarness>(ctor: HarnessConstructor<T>): Promise<T> {
    const root = await this.rootLocator('.hifi').first();
    return new ctor(root, this);
  }
}

/**
 * Register routes so the app loads `graph` deterministically. Independent of the
 * gitignored public/archgraph.json. MUST be called BEFORE page.goto('/').
 */
export async function routeGraph(page: Page, graph: UIGraph): Promise<void> {
  await page.route('**/archgraph.json', (route) => route.fulfill({ json: graph as unknown as object }));
  await page.route('**/archgraph.sample.json', (route) => route.abort());
}
```

- [ ] **Step 2: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add web/testing/harness/playwright-env.ts
git commit -m "test(web): add Playwright harness environment adapter"
```

---

## Task 6: Card harnesses (Member, Internal, ComponentCard)

**Files:**
- Create: `web/testing/harness/member.harness.ts`
- Create: `web/testing/harness/internal.harness.ts`
- Create: `web/testing/harness/component-card.harness.ts`

- [ ] **Step 1: Create `web/testing/harness/member.harness.ts`**

```ts
import { ComponentHarness, DiffState, diffStateFromClasses } from './test-element';

/** A member row inside an expanded internal (`.hf-member`). */
export class MemberHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-member-name').first()).text();
  }
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  /** The `title` tooltip on the row (full member name). */
  async rowTitle(): Promise<string | null> {
    return this.root.getAttribute('title');
  }
  /** Computed text-decoration — used in e2e to prove removed rows are NOT struck. */
  async textDecoration(): Promise<string> {
    return this.root.computedStyleProp('text-decoration');
  }
}
```

- [ ] **Step 2: Create `web/testing/harness/internal.harness.ts`**

```ts
import { ComponentHarness, DiffState, diffStateFromClasses, parsePx } from './test-element';
import { MemberHarness } from './member.harness';

/** An internal card inside an expanded component (`.hf-internal`). */
export class InternalHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-internal-name').first()).text();
  }
  async kind(): Promise<'iface' | 'class'> {
    return (await this.root.hasClass('iface')) ? 'iface' : 'class';
  }
  /** Effective (possibly derived) diff state carried on the card class. */
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  /** The `title` tooltip on the internal name. */
  async nameTitle(): Promise<string | null> {
    return (await this.root.locator('.hf-internal-name').first()).getAttribute('title');
  }
  async members(): Promise<MemberHarness[]> {
    const rows = await this.root.locator('.hf-member').all();
    return rows.map((r) => new MemberHarness(r, this.env));
  }
  async member(name: string): Promise<MemberHarness> {
    for (const m of await this.members()) {
      if ((await m.name()).includes(name)) return m;
    }
    throw new Error(`member "${name}" not found in internal`);
  }
  /** Members are visible only when the internal is expanded. */
  async isExpanded(): Promise<boolean> {
    return (await this.root.locator('.hf-member-list').count()) > 0;
  }
  async toggleFitWidth(): Promise<void> {
    await (await this.root.locator('.hf-internal-toggle').first()).click();
  }
  /** '+' (fixed width) or '−' (fit-width on). */
  async fitWidthGlyph(): Promise<string> {
    return (await this.root.locator('.hf-internal-toggle').first()).text();
  }
  /** Inline width in px (ELK/layout output). */
  async width(): Promise<number> {
    return parsePx(await this.root.styleProp('width'));
  }
}
```

- [ ] **Step 3: Create `web/testing/harness/component-card.harness.ts`**

```ts
import {
  BoundingBox,
  ComponentHarness,
  DiffState,
  diffStateFromClasses,
  parsePx,
  TestElement,
} from './test-element';
import { InternalHarness } from './internal.harness';

/** A component card on the canvas (`.hf-cmp`). */
export class ComponentCardHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-cmp-name').first()).text();
  }
  async tech(): Promise<string> {
    return (await this.root.locator('.hf-cmp-tech').first()).text();
  }
  /** Header icon letter — the PARENT (bounded context) initial. */
  async parentInitial(): Promise<string> {
    return (await this.root.locator('.hf-cmp-icon').first()).text();
  }
  /** Effective (possibly derived) component diff state from the card class. */
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  async isFocused(): Promise<boolean> {
    return this.root.hasClass('focused');
  }
  /** Click the card to focus it (focus mode). */
  async focus(): Promise<void> {
    await this.root.click();
  }
  /** Expanded ⇒ the internals mini-canvas is present. */
  async isExpanded(): Promise<boolean> {
    return (await this.root.locator('.hf-cmp-canvas').count()) > 0;
  }
  async toggleExpand(): Promise<void> {
    await (await this.root.locator('.hf-cmp-expand').first()).click();
  }
  async expandButtonGlyph(): Promise<string> {
    return (await this.root.locator('.hf-cmp-expand').first()).text();
  }

  // ── Action group (top-right, OUTSIDE the clipped inner) ──────────────────
  /** Count of <button>s in the floating action group (info is a <div>). */
  async actionButtonCount(): Promise<number> {
    return this.root.locator('.hf-cmp-actions button').count();
  }
  async hasExpandAllButton(): Promise<boolean> {
    return (await this.root.locator('.hf-cmp-expand-all').count()) > 0;
  }
  async expandAll(): Promise<void> {
    await (await this.root.locator('.hf-cmp-expand-all').first()).click();
  }
  /** '«»' (none wide) or '»«' (all wide). */
  async expandAllGlyph(): Promise<string> {
    return (await this.root.locator('.hf-cmp-expand-all').first()).text();
  }
  async hasInfoButton(): Promise<boolean> {
    return (await this.root.locator('.hf-cmp-info').count()) > 0;
  }
  async hoverInfo(): Promise<void> {
    await (await this.root.locator('.hf-cmp-info-icon').first()).hover();
  }
  async infoIconBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-info-icon').first()).boundingBox();
  }
  async infoPopover(): Promise<TestElement> {
    return this.root.locator('.hf-cmp-info-pop').first();
  }
  async expandAllBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-expand-all').first()).boundingBox();
  }
  async expandBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-expand').first()).boundingBox();
  }
  async box(): Promise<BoundingBox | null> {
    return this.root.boundingBox();
  }
  async techBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-tech').first()).boundingBox();
  }

  /** Legacy in-card NEW/MOD plaques were removed — this must be 0. */
  async inCardTagCount(): Promise<number> {
    return this.root.locator('.hf-cmp-diff-tag').count();
  }

  async width(): Promise<number> {
    return parsePx(await this.root.styleProp('width'));
  }
  async height(): Promise<number> {
    return parsePx(await this.root.styleProp('height'));
  }

  // ── Internals ────────────────────────────────────────────────────────────
  async internalCount(): Promise<number> {
    return this.root.locator('.hf-internal').count();
  }
  async internals(): Promise<InternalHarness[]> {
    const cards = await this.root.locator('.hf-internal').all();
    return cards.map((c) => new InternalHarness(c, this.env));
  }
  async internal(name: string): Promise<InternalHarness> {
    for (const it of await this.internals()) {
      if ((await it.name()) === name) return it;
    }
    throw new Error(`internal "${name}" not found in component`);
  }

  // ── Ports ──────────────────────────────────────────────────────────────
  /** The `.hf-port-label` element whose text contains `name`. */
  async portLabel(name: string): Promise<TestElement> {
    const ports = await this.root.locator('.hf-port').all();
    for (const p of ports) {
      const label = await p.locator('.hf-port-label').first();
      if ((await label.text()).includes(name)) return label;
    }
    throw new Error(`port label "${name}" not found`);
  }
  /** A removed port's label element (for the not-struck e2e assertion). */
  async removedPortLabel(): Promise<TestElement> {
    return this.root.locator('.hf-port.removed .hf-port-label').first();
  }
  /** Click a port row by its label text (opens the comment popover). */
  async clickPort(name: string): Promise<void> {
    const ports = await this.root.locator('.hf-port').all();
    for (const p of ports) {
      const label = await p.locator('.hf-port-label').first();
      if ((await label.text()).includes(name)) {
        await p.click();
        return;
      }
    }
    throw new Error(`port "${name}" not found`);
  }
  /** Hover the card so port labels (opacity:0 by default) reveal. */
  async hoverCard(): Promise<void> {
    await this.root.hover();
  }
}
```

- [ ] **Step 4: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/testing/harness/member.harness.ts web/testing/harness/internal.harness.ts web/testing/harness/component-card.harness.ts
git commit -m "test(web): add card harnesses (member/internal/component)"
```

---

## Task 7: Chrome/panel harnesses (Legend, ChangesPanel, ContextTree, CommentPopover, Canvas)

These harnesses are rooted at `.hifi` (or `.hf-canvas-viewport`) and query their own subtree lazily, so they never `.first()` an absent element (they use `.count()` first). That lets `AppHarness.legend()` etc. be constructed even when the feature is absent (non-diff graph).

**Files:**
- Create: `web/testing/harness/legend.harness.ts`
- Create: `web/testing/harness/changes-panel.harness.ts`
- Create: `web/testing/harness/context-tree.harness.ts`
- Create: `web/testing/harness/comment-popover.harness.ts`
- Create: `web/testing/harness/canvas.harness.ts`

- [ ] **Step 1: Create `web/testing/harness/legend.harness.ts`**

```ts
import { ComponentHarness } from './test-element';

/** The on-canvas diff legend (`.hf-canvas-legend`). Rooted at `.hifi`. */
export class LegendHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.root.locator('.hf-canvas-legend').count()) > 0;
  }
  async itemTexts(): Promise<string[]> {
    const items = await this.root.locator('.hf-canvas-legend .hf-legend-item').all();
    return Promise.all(items.map((i) => i.text()));
  }
}
```

- [ ] **Step 2: Create `web/testing/harness/changes-panel.harness.ts`**

```ts
import { ComponentHarness, TestElement } from './test-element';

/** The left-panel CHANGES list (`.hf-change-card` rows). Rooted at `.hifi`. */
export class ChangesPanelHarness extends ComponentHarness {
  /** Number of change entries. */
  async count(): Promise<number> {
    return this.root.locator('.hf-change-card').count();
  }
  /** Display names of all entries (`.hf-change-name`). */
  async entryNames(): Promise<string[]> {
    const names = await this.root.locator('.hf-change-name').all();
    return Promise.all(names.map((n) => n.text()));
  }
  /** Click the entry card whose name contains `name`. */
  async clickEntry(name: string): Promise<void> {
    const cards = await this.root.locator('.hf-side').first();
    const rows = await cards.locator('.hf-card').all();
    for (const row of rows) {
      const nameEl = await row.locator('.hf-change-name').count();
      if (nameEl > 0) {
        const t = await (await row.locator('.hf-change-name').first()).text();
        if (t.includes(name)) {
          await row.click();
          return;
        }
      }
    }
    throw new Error(`change entry "${name}" not found`);
  }
  /** The de-duplicated PR summary must NOT appear inside the left panel. */
  async hasPrSummary(): Promise<boolean> {
    const left = await this.root.locator('.hf-side').first();
    return (await left.locator('.hf-pr-tag').count()) > 0;
  }
}
```

- [ ] **Step 3: Create `web/testing/harness/context-tree.harness.ts`**

```ts
import { ComponentHarness } from './test-element';

/** The CONTEXTS tree (`.hf-tree`). Rooted at `.hifi`. */
export class ContextTreeHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.root.locator('.hf-tree').count()) > 0;
  }
  async boundedContextRowCount(): Promise<number> {
    return this.root.locator('.hf-tree-row.bc').count();
  }
  async componentRowCount(): Promise<number> {
    return this.root.locator('.hf-tree-row.cmp').count();
  }
  async internalRowCount(): Promise<number> {
    return this.root.locator('.hf-tree-row.internal').count();
  }
  async memberRowCount(): Promise<number> {
    return this.root.locator('.hf-tree-row.member').count();
  }
  /** Expand the row whose `.name` equals `name` (clicks its chevron). */
  async expand(name: string): Promise<void> {
    const row = await this.rowByName(name);
    await (await row.locator('.chev').first()).click();
  }
  /** Click the row body whose `.name` equals `name` (focuses the canvas object). */
  async clickRow(name: string): Promise<void> {
    await (await this.rowByName(name)).click();
  }
  /** Diff badge text ('+', '-', '~') of the row whose `.name` equals `name`. */
  async badge(name: string): Promise<string | null> {
    const row = await this.rowByName(name);
    if ((await row.locator('.badge').count()) === 0) return null;
    return (await row.locator('.badge').first()).text();
  }

  private async rowByName(name: string) {
    const rows = await this.root.locator('.hf-tree .hf-tree-row').all();
    for (const row of rows) {
      const nameEl = await row.locator('.name').first();
      if ((await nameEl.text()) === name) return row;
    }
    throw new Error(`tree row "${name}" not found`);
  }
}
```

- [ ] **Step 4: Create `web/testing/harness/comment-popover.harness.ts`**

```ts
import { ComponentHarness } from './test-element';

/** The inline comment popover (`.hf-popover`). Rooted at `.hifi`. */
export class CommentPopoverHarness extends ComponentHarness {
  async isOpen(): Promise<boolean> {
    return (await this.root.locator('.hf-popover').count()) > 0;
  }
  async tag(): Promise<string> {
    return (await this.root.locator('.hf-popover-tag').first()).text();
  }
  async target(): Promise<string> {
    return (await this.root.locator('.hf-popover-target').first()).text();
  }
  async type(text: string): Promise<void> {
    await (await this.root.locator('.hf-popover textarea').first()).fill(text);
  }
  async submit(): Promise<void> {
    await (await this.root.locator('.hf-popover-actions .hf-btn.primary').first()).click();
  }
  async cancel(): Promise<void> {
    await (await this.root.locator('.hf-popover-actions .hf-btn').first()).click();
  }
}
```

- [ ] **Step 5: Create `web/testing/harness/canvas.harness.ts`**

```ts
import { ComponentHarness, parsePx, TestElement } from './test-element';

/** The canvas viewport (`.hf-canvas-viewport`) — pan/zoom/cursor/sizer. */
export class CanvasHarness extends ComponentHarness {
  private wrap(): Promise<TestElement> {
    return this.root.locator('.hf-canvas-wrap').first();
  }
  /** Computed cursor on the scroller (e2e: 'grab'). */
  async cursor(): Promise<string> {
    return (await this.wrap()).computedStyleProp('cursor');
  }
  async isPanning(): Promise<boolean> {
    return (await this.wrap()).hasClass('panning');
  }
  async scrollPosition(): Promise<{ left: number; top: number }> {
    return (await this.wrap()).scrollPosition();
  }
  /** Drag the empty canvas background by (dx, dy). */
  async pan(dx: number, dy: number): Promise<void> {
    await this.env.panDrag(await this.wrap(), dx, dy);
  }
  /** Sizer reserves slack on every side, so it is always wider than content. */
  async sizerExceedsContent(): Promise<boolean> {
    const sizerW = parsePx(await (await this.root.locator('.hf-canvas-sizer').first()).styleProp('width'));
    const contentW = parsePx(await (await this.root.locator('.hf-canvas').first()).styleProp('width'));
    return sizerW > contentW;
  }
  // ── Zoom toolbar ─────────────────────────────────────────────────────────
  async zoomLabel(): Promise<string> {
    return (await this.root.locator('.hf-canvas-toolbar .zoom').first()).text();
  }
  async zoomIn(): Promise<void> {
    await (await this.root.locator('.hf-canvas-toolbar button[title="Zoom in"]').first()).click();
  }
  async zoomOut(): Promise<void> {
    await (await this.root.locator('.hf-canvas-toolbar button[title="Zoom out"]').first()).click();
  }
  async fit(): Promise<void> {
    await (await this.root.locator('.hf-canvas-toolbar button[title="Fit"]').first()).click();
  }
  /** Ctrl+wheel over the scroller (zoom gesture). */
  async ctrlWheelZoom(deltaY: number): Promise<void> {
    await this.env.ctrlWheel(await this.wrap(), deltaY);
  }
  /** Inline scale factor parsed from the `.hf-canvas` transform. */
  async canvasTransform(): Promise<string> {
    return (await this.root.locator('.hf-canvas').first()).styleProp('transform');
  }
}
```

- [ ] **Step 6: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add web/testing/harness/legend.harness.ts web/testing/harness/changes-panel.harness.ts web/testing/harness/context-tree.harness.ts web/testing/harness/comment-popover.harness.ts web/testing/harness/canvas.harness.ts
git commit -m "test(web): add chrome/panel harnesses (legend/changes/tree/popover/canvas)"
```

---

## Task 8: App + Diagram harnesses (top-level entry points)

**Files:**
- Create: `web/testing/harness/diagram.harness.ts`
- Create: `web/testing/harness/app.harness.ts`

- [ ] **Step 1: Create `web/testing/harness/diagram.harness.ts`**

```ts
import { ComponentHarness } from './test-element';
import { ComponentCardHarness } from './component-card.harness';

/** The laid-out diagram (`.hf-canvas`). */
export class DiagramHarness extends ComponentHarness {
  async componentCount(): Promise<number> {
    return this.root.locator('.hf-cmp').count();
  }
  async components(): Promise<ComponentCardHarness[]> {
    const cards = await this.root.locator('.hf-cmp').all();
    return cards.map((c) => new ComponentCardHarness(c, this.env));
  }
  async componentNames(): Promise<string[]> {
    return Promise.all((await this.components()).map((c) => c.name()));
  }
  async component(name: string): Promise<ComponentCardHarness> {
    for (const c of await this.components()) {
      if ((await c.name()) === name) return c;
    }
    throw new Error(`component "${name}" not found on canvas`);
  }
  async boundedContextNames(): Promise<string[]> {
    const labels = await this.root.locator('.hf-bc-group .hf-bc-label').all();
    return Promise.all(labels.map((l) => l.text()));
  }
  /** Count of main edge paths (`.hf-edge`; excludes arrow markers + hit paths). */
  async edgeCount(): Promise<number> {
    return this.root.locator('.hf-edge').count();
  }
  /** Count of edges carrying a diff class. */
  async diffEdgeCount(): Promise<number> {
    return this.root.locator('.hf-edge.added, .hf-edge.removed, .hf-edge.changed').count();
  }
}
```

- [ ] **Step 2: Create `web/testing/harness/app.harness.ts`**

```ts
import { ComponentHarness } from './test-element';
import { DiagramHarness } from './diagram.harness';
import { CanvasHarness } from './canvas.harness';
import { LegendHarness } from './legend.harness';
import { ChangesPanelHarness } from './changes-panel.harness';
import { ContextTreeHarness } from './context-tree.harness';
import { CommentPopoverHarness } from './comment-popover.harness';

/** Top-level harness rooted at `.hifi`. Entry point: env.load(AppHarness). */
export class AppHarness extends ComponentHarness {
  /** Resolve once ELK has laid out the diagram (components mounted). */
  async waitForLoaded(): Promise<void> {
    await this.env.waitUntil(async () => (await this.root.locator('.hf-cmp').count()) >= 1, {
      message: 'diagram never rendered any components',
    });
  }

  // ── PR header / app bar ────────────────────────────────────────────────
  async hasPrHeader(): Promise<boolean> {
    return (await this.root.locator('.hf-prheader').count()) > 0;
  }
  async prTitle(): Promise<string> {
    return (await this.root.locator('.hf-pr-title').first()).text();
  }
  async branchCrumb(): Promise<string | null> {
    if ((await this.root.locator('.hf-crumbs .branch').count()) === 0) return null;
    return (await this.root.locator('.hf-crumbs .branch').first()).text();
  }

  // ── Left panel tabs ────────────────────────────────────────────────────
  async hasChangesTab(): Promise<boolean> {
    return (await this.root.locator('.hf-tabs button').filterByText('CHANGES').count()) > 0;
  }
  async changesTabCount(): Promise<number> {
    const btn = this.root.locator('.hf-tabs button').filterByText('CHANGES');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async contextsTabCount(): Promise<number> {
    const btn = this.root.locator('.hf-tabs button').filterByText('CONTEXTS');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async openChangesTab(): Promise<void> {
    await (await this.root.locator('.hf-tabs button').filterByText('CHANGES').first()).click();
    await this.env.waitUntil(async () => (await this.root.locator('.hf-change-card').count()) >= 1, {
      message: 'CHANGES list never rendered',
    });
  }
  async openContextsTab(): Promise<void> {
    await (await this.root.locator('.hf-tabs button').filterByText('CONTEXTS').first()).click();
    await this.env.waitUntil(async () => (await this.root.locator('.hf-tree').count()) >= 1, {
      message: 'CONTEXTS tree never rendered',
    });
  }

  // ── Sub-harnesses ────────────────────────────────────────────────────────
  async diagram(): Promise<DiagramHarness> {
    const canvas = await this.root.locator('.hf-canvas').first();
    return new DiagramHarness(canvas, this.env);
  }
  async canvas(): Promise<CanvasHarness> {
    const viewport = await this.root.locator('.hf-canvas-viewport').first();
    return new CanvasHarness(viewport, this.env);
  }
  legend(): LegendHarness {
    return new LegendHarness(this.root, this.env);
  }
  changesPanel(): ChangesPanelHarness {
    return new ChangesPanelHarness(this.root, this.env);
  }
  contextTree(): ContextTreeHarness {
    return new ContextTreeHarness(this.root, this.env);
  }
  commentPopover(): CommentPopoverHarness {
    return new CommentPopoverHarness(this.root, this.env);
  }
}
```

- [ ] **Step 3: Verify compiles**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add web/testing/harness/diagram.harness.ts web/testing/harness/app.harness.ts
git commit -m "test(web): add app + diagram harnesses"
```

---

## Task 9: Unit slice — `harness-smoke.harness.test.tsx` (TDD red→green anchor)

This is the first executable test. It proves the *same* harnesses run in jsdom and retroactively validates Tasks 3–8. Structural subset only (no rendered geometry / pan / computed strike-through — jsdom has no layout engine and Vite CSS isn't applied).

**Files:**
- Create: `web/src/components/__tests__/harness-smoke.harness.test.tsx`

- [ ] **Step 1: Write the test**

```tsx
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import { diffGraph, nonDiffGraph } from '../../../testing/fixtures';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('harness smoke (jsdom) — diffGraph', () => {
  it('loads the diagram with 5 components', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();
    expect(await diagram.componentCount()).toBe(5);
  });

  it('OrderService is auto-expanded and carries parentInitial "O"', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const svc = await (await app.diagram()).component('OrderService');
    expect(await svc.isExpanded()).toBe(true);
    expect(await svc.parentInitial()).toBe('O');
  });

  it('expand-all glyph round-trips «» → »« → «»', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const svc = await (await app.diagram()).component('OrderService');
    expect(await svc.expandAllGlyph()).toBe('«»');
    await svc.expandAll();
    expect(await svc.expandAllGlyph()).toBe('»«');
    await svc.expandAll();
    expect(await svc.expandAllGlyph()).toBe('«»');
  });

  it('derives changed: PaymentService/IGateway and CheckoutAPI', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();

    // CheckoutAPI has no own diff but an added child internal + added port.
    expect(await (await diagram.component('CheckoutAPI')).diffState()).toBe('changed');

    // Expand PaymentService and assert IGateway (no own diff, members add/add/remove).
    const pay = await diagram.component('PaymentService');
    await pay.toggleExpand();
    await app.env.waitUntil(async () => (await pay.internalCount()) >= 1, {
      message: 'PaymentService internals never rendered',
    });
    expect(await (await pay.internal('IGateway')).diffState()).toBe('changed');
  });

  it('emits no in-card diff tags', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    for (const c of await (await app.diagram()).components()) {
      expect(await c.inCardTagCount()).toBe(0);
    }
  });

  it('Changes panel shows 38 entries and no duplicated PR summary', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    expect(await app.hasChangesTab()).toBe(true);
    await app.openChangesTab();
    const changes = app.changesPanel();
    expect(await changes.count()).toBe(38);
    expect(await changes.hasPrSummary()).toBe(false);
  });

  it('CONTEXTS tab switch reveals the tree with 3 bounded contexts', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    await app.openContextsTab();
    const tree = app.contextTree();
    expect(await tree.isPresent()).toBe(true);
    expect(await tree.boundedContextRowCount()).toBe(3);
  });
});

describe('harness smoke (jsdom) — nonDiffGraph', () => {
  it('has no PR header, no CHANGES tab, and no legend', async () => {
    const env = await mountAppDom(nonDiffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    expect(await app.hasPrHeader()).toBe(false);
    expect(await app.hasChangesTab()).toBe(false);
    expect(await app.legend().isPresent()).toBe(false);
    expect(await app.branchCrumb()).toBeNull();
  });
});
```

> Note: the test reaches `app.env.waitUntil(...)` for ad-hoc state transitions. `env` is `public readonly` on `ComponentHarness` (Task 3), so this compiles directly.

- [ ] **Step 2: Run the test — expect GREEN (infra from Tasks 3–8 already exists)**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

Run: `./node_modules/.bin/vitest run src/components/__tests__/harness-smoke.harness.test.tsx`
Expected: 8 passed.

Run: `./node_modules/.bin/vitest run`
Expected: 20 (layout) + 8 (harness-smoke) passed.

- [ ] **Step 3: Commit**

```bash
git add web/testing/harness/test-element.ts web/src/components/__tests__/harness-smoke.harness.test.tsx
git commit -m "test(web): unit harness-smoke slice proving harnesses run in jsdom"
```

---

## Task 10: e2e `diff-mode.spec.ts`

**Files:**
- Create: `web/e2e/diff-mode.spec.ts`

- [ ] **Step 1: Write the spec**

```ts
import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph, nonDiffGraph } from '../testing/fixtures';

test.describe('diff mode (diffGraph)', () => {
  test('PR header, tabs, legend, diff colors, edges, no in-card tags', async ({ page }) => {
    await routeGraph(page, diffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();

    // PR header + branch crumb
    expect(await app.hasPrHeader()).toBe(true);
    expect(await app.prTitle()).toContain('OrderEvents');
    expect(await app.branchCrumb()).toBe('agent/order-events-2026-04-30');

    // Tabs
    expect(await app.hasChangesTab()).toBe(true);
    expect(await app.changesTabCount()).toBe(38);
    expect(await app.contextsTabCount()).toBe(3);

    // Legend: 3 items, diff-only
    const legend = app.legend();
    expect(await legend.isPresent()).toBe(true);
    expect(await legend.itemTexts()).toEqual(['added', 'removed', 'changed']);

    const diagram = await app.diagram();
    expect(await diagram.componentCount()).toBe(5);

    // Component diff colors (explicit + derived)
    expect(await (await diagram.component('OrderEvents')).diffState()).toBe('added');
    expect(await (await diagram.component('OrderService')).diffState()).toBe('changed');
    expect(await (await diagram.component('PaymentService')).diffState()).toBe('changed');
    expect(await (await diagram.component('Notifier')).diffState()).toBe('changed');
    expect(await (await diagram.component('CheckoutAPI')).diffState()).toBe('changed'); // derived

    // Edges
    expect(await diagram.edgeCount()).toBe(6);
    expect(await diagram.diffEdgeCount()).toBe(5);

    // No in-card NEW/MOD tags
    for (const c of await diagram.components()) {
      expect(await c.inCardTagCount()).toBe(0);
    }
  });

  test('IGateway derives changed; removed member + removed port are NOT struck through', async ({ page }) => {
    await routeGraph(page, diffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();

    const pay = await diagram.component('PaymentService');
    await pay.toggleExpand();
    await app.env.waitUntil(async () => (await pay.internalCount()) >= 1, {
      message: 'PaymentService internals never rendered',
    });

    const gateway = await pay.internal('IGateway');
    expect(await gateway.diffState()).toBe('changed'); // derived from members

    // Removed member charge(amt): colored but NOT line-through.
    const charge = await gateway.member('charge');
    expect(await charge.diffState()).toBe('removed');
    expect(await charge.textDecoration()).not.toContain('line-through');

    // Removed port label: NOT line-through.
    const removedPort = await pay.removedPortLabel();
    const portDecoration = await removedPort.computedStyleProp('text-decoration');
    expect(portDecoration).not.toContain('line-through');
  });

  test('Changes panel lists entries without a duplicated PR summary', async ({ page }) => {
    await routeGraph(page, diffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();
    await app.openChangesTab();
    const changes = app.changesPanel();
    expect(await changes.count()).toBe(38);
    expect(await changes.hasPrSummary()).toBe(false);
  });
});

test.describe('non-diff mode (nonDiffGraph)', () => {
  test('no PR header, no CHANGES tab, no legend, zero diff classes', async ({ page }) => {
    await routeGraph(page, nonDiffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();

    expect(await app.hasPrHeader()).toBe(false);
    expect(await app.hasChangesTab()).toBe(false);
    expect(await app.legend().isPresent()).toBe(false);

    const diagram = await app.diagram();
    for (const c of await diagram.components()) {
      expect(await c.diffState()).toBeNull();
    }
    expect(await diagram.diffEdgeCount()).toBe(0);
  });
});
```

- [ ] **Step 2: Run the spec**

Run: `/opt/homebrew/bin/npx playwright test e2e/diff-mode.spec.ts --project=chrome`
Expected: all tests pass (webServer auto-starts vite on :4317). On failure: `/opt/homebrew/bin/npx playwright show-report`.

- [ ] **Step 3: Commit**

```bash
git add web/e2e/diff-mode.spec.ts
git commit -m "test(web): e2e diff-mode coverage via harnesses"
```

---

## Task 11: e2e `component-card.spec.ts`

**Files:**
- Create: `web/e2e/component-card.spec.ts`

- [ ] **Step 1: Write the spec**

```ts
import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph, longMemberGraph } from '../testing/fixtures';

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  return app;
}

test('OrderService is auto-expanded; parent initials are O/P/N', async ({ page }) => {
  const app = await loadDiff(page);
  const diagram = await app.diagram();
  expect(await (await diagram.component('OrderService')).isExpanded()).toBe(true);
  expect(await (await diagram.component('CheckoutAPI')).parentInitial()).toBe('O');
  expect(await (await diagram.component('PaymentService')).parentInitial()).toBe('P');
  expect(await (await diagram.component('Notifier')).parentInitial()).toBe('N');
});

test('grouped actions (i)/«»/− do not overlap and sit within the card', async ({ page }) => {
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService'); // expanded → all 3 present
  const card = await svc.box();
  const info = await svc.infoIconBox();
  const all = await svc.expandAllBox();
  const exp = await svc.expandBox();
  expect(card && info && all && exp).toBeTruthy();
  if (!card || !info || !all || !exp) return;

  const right = (b: { x: number; width: number }) => b.x + b.width;
  // Horizontal order: info left of expand-all left of expand, no overlap.
  expect(right(info)).toBeLessThanOrEqual(all.x + 1);
  expect(right(all)).toBeLessThanOrEqual(exp.x + 1);
  // All within the card's horizontal extent.
  for (const b of [info, all, exp]) {
    expect(b.x).toBeGreaterThanOrEqual(card.x - 1);
    expect(right(b)).toBeLessThanOrEqual(right(card) + 1);
  }
});

test('(i) popover opens ABOVE the info icon on hover', async ({ page }) => {
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService');
  await svc.hoverInfo();
  const pop = await svc.infoPopover();
  await app.env.waitUntil(async () => (await pop.computedStyleProp('visibility')) === 'visible', {
    message: 'info popover never became visible',
  });
  const popBox = await pop.boundingBox();
  const iconBox = await svc.infoIconBox();
  expect(popBox && iconBox).toBeTruthy();
  if (!popBox || !iconBox) return;
  // Popover bottom is at/above the icon top (CSS bottom: calc(100% + 6px)).
  expect(popBox.y + popBox.height).toBeLessThanOrEqual(iconBox.y + 2);
});

test('collapsed card: single + action, one-line header (tech not wrapped), width > 220', async ({ page }) => {
  const app = await loadDiff(page);
  const api = await (await app.diagram()).component('CheckoutAPI'); // collapsed
  expect(await api.isExpanded()).toBe(false);
  expect(await api.actionButtonCount()).toBe(1);
  expect(await api.expandButtonGlyph()).toBe('+');
  expect(await api.hasExpandAllButton()).toBe(false);
  expect(await api.width()).toBeGreaterThan(220);
  expect(await api.height()).toBeGreaterThanOrEqual(120);
  const techBox = await api.techBox();
  expect(techBox).toBeTruthy();
  if (techBox) expect(techBox.height).toBeLessThan(24); // single line
});

test('expand/collapse toggle flips the glyph and internals', async ({ page }) => {
  const app = await loadDiff(page);
  const api = await (await app.diagram()).component('CheckoutAPI');
  expect(await api.expandButtonGlyph()).toBe('+');
  await api.toggleExpand();
  await app.env.waitUntil(async () => await api.isExpanded(), { message: 'did not expand' });
  expect(await api.expandButtonGlyph()).toBe('−');
  await api.toggleExpand();
  await app.env.waitUntil(async () => !(await api.isExpanded()), { message: 'did not collapse' });
  expect(await api.expandButtonGlyph()).toBe('+');
});

test('expand-all glyph round-trips with no console errors', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService');
  expect(await svc.expandAllGlyph()).toBe('«»');
  await svc.expandAll();
  await app.env.waitUntil(async () => (await svc.expandAllGlyph()) === '»«', { message: 'glyph did not flip' });
  await svc.expandAll();
  await app.env.waitUntil(async () => (await svc.expandAllGlyph()) === '«»', { message: 'glyph did not reset' });
  expect(errors).toEqual([]);
});

test('fit-width widens an internal then restores it (longMemberGraph)', async ({ page }) => {
  await routeGraph(page, longMemberGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  const cmp = await (await app.diagram()).component('Reconciler'); // first cmp → auto-expanded
  const engine = await cmp.internal('Engine');
  const base = await engine.width();
  expect(base).toBeLessThanOrEqual(185); // ~INTERNAL_W 180

  await engine.toggleFitWidth();
  await app.env.waitUntil(async () => (await engine.width()) > base + 50, {
    message: 'internal did not widen on fit-width',
  });
  expect(await engine.fitWidthGlyph()).toBe('−');

  await engine.toggleFitWidth();
  await app.env.waitUntil(async () => (await engine.width()) <= base + 5, {
    message: 'internal width did not restore',
  });
  expect(await engine.fitWidthGlyph()).toBe('+');
});

test('member and internal carry title tooltips', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.toggleExpand();
  await app.env.waitUntil(async () => (await pay.internalCount()) >= 1, { message: 'no internals' });
  const gateway = await pay.internal('IGateway');
  expect(await gateway.nameTitle()).toBe('IGateway');
  const member = await gateway.member('refund');
  expect(await member.rowTitle()).toContain('refund');
});

test('clicking a port opens the comment popover with the right target', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.clickPort('authorize()');
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => await popover.isOpen(), { message: 'popover did not open' });
  expect(await popover.tag()).toBe('port');
  expect(await popover.target()).toBe('pay.auth');
});
```

- [ ] **Step 2: Run the spec**

Run: `/opt/homebrew/bin/npx playwright test e2e/component-card.spec.ts --project=chrome`
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add web/e2e/component-card.spec.ts
git commit -m "test(web): e2e component-card coverage via harnesses"
```

---

## Task 12: e2e `canvas.spec.ts`

**Files:**
- Create: `web/e2e/canvas.spec.ts`

- [ ] **Step 1: Write the spec**

```ts
import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  return app;
}

test('canvas shows a grab cursor and an oversized sizer', async ({ page }) => {
  const app = await loadDiff(page);
  const canvas = await app.canvas();
  expect(await canvas.cursor()).toBe('grab');
  expect(await canvas.sizerExceedsContent()).toBe(true);
});

test('dragging the background pans the canvas and clears .panning', async ({ page }) => {
  const app = await loadDiff(page);
  const canvas = await app.canvas();
  const before = await canvas.scrollPosition();
  await canvas.pan(-160, -120); // drag up-left → scroll increases
  await app.env.waitUntil(
    async () => {
      const p = await canvas.scrollPosition();
      return Math.abs(p.left - before.left) > 20 || Math.abs(p.top - before.top) > 20;
    },
    { message: 'pan did not move the scroll position' }
  );
  expect(await canvas.isPanning()).toBe(false);
});

test('a pan-drag does not clear an existing focus', async ({ page }) => {
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService');
  await svc.focus();
  await app.env.waitUntil(async () => await svc.isFocused(), { message: 'card did not focus' });
  const canvas = await app.canvas();
  await canvas.pan(-140, -100);
  expect(await svc.isFocused()).toBe(true);
});

test('zoom buttons and ctrl+wheel change the zoom label/scale', async ({ page }) => {
  const app = await loadDiff(page);
  const canvas = await app.canvas();
  const initial = await canvas.zoomLabel();

  await canvas.zoomIn();
  await app.env.waitUntil(async () => (await canvas.zoomLabel()) !== initial, {
    message: 'zoom in did not change label',
  });
  const afterIn = await canvas.zoomLabel();

  await canvas.zoomOut();
  await app.env.waitUntil(async () => (await canvas.zoomLabel()) !== afterIn, {
    message: 'zoom out did not change label',
  });

  await canvas.fit();
  const beforeWheel = await canvas.canvasTransform();
  await canvas.ctrlWheelZoom(-120); // zoom in
  await app.env.waitUntil(async () => (await canvas.canvasTransform()) !== beforeWheel, {
    message: 'ctrl+wheel did not change transform',
  });
  // Page itself must not scroll (ctrl+wheel is intercepted).
  const pageScrollY = await page.evaluate(() => window.scrollY);
  expect(pageScrollY).toBe(0);
});

test('port labels reveal on card hover', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  const label = await pay.portLabel('refund()');
  expect(await label.computedStyleProp('opacity')).toBe('0');
  await pay.hoverCard();
  await app.env.waitUntil(async () => (await label.computedStyleProp('opacity')) === '1', {
    message: 'port label did not reveal on hover',
  });
});
```

`focus()` and `isFocused()` already exist on `ComponentCardHarness` (Task 6), so the spec compiles as written.

- [ ] **Step 2: Run the spec**

Run: `/opt/homebrew/bin/npx playwright test e2e/canvas.spec.ts --project=chrome`
Expected: all pass. (Pan direction: dragging up-left scrolls content so `scrollLeft/Top` grow; the assertion tolerates either axis moving > 20px.)

- [ ] **Step 3: Commit**

```bash
git add web/e2e/canvas.spec.ts
git commit -m "test(web): e2e canvas pan/zoom/cursor coverage via harnesses"
```

---

## Task 13: e2e `context-tree.spec.ts`

**Files:**
- Create: `web/e2e/context-tree.spec.ts`

- [ ] **Step 1: Write the spec**

```ts
import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

async function loadTree(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  await app.openContextsTab();
  return app;
}

test('CONTEXTS tab shows bounded-context rows; drilling reveals cmp → internal → member', async ({ page }) => {
  const app = await loadTree(page);
  const tree = app.contextTree();
  expect(await tree.boundedContextRowCount()).toBe(3);
  // Bounded contexts start open → components visible.
  expect(await tree.componentRowCount()).toBeGreaterThanOrEqual(5);

  // Expand a component → its internals appear.
  await tree.expand('PaymentService');
  await app.env.waitUntil(async () => (await tree.internalRowCount()) >= 1, {
    message: 'internal rows never appeared',
  });

  // Expand an internal → its members appear.
  await tree.expand('IGateway');
  await app.env.waitUntil(async () => (await tree.memberRowCount()) >= 1, {
    message: 'member rows never appeared',
  });
});

test('clicking a component row focuses that card on the canvas', async ({ page }) => {
  const app = await loadTree(page);
  await app.contextTree().clickRow('Notifier');
  const notifier = await (await app.diagram()).component('Notifier');
  await app.env.waitUntil(async () => await notifier.isFocused(), {
    message: 'clicking the tree row did not focus the card',
  });
  expect(await notifier.isFocused()).toBe(true);
});

test('diff badges render on changed rows', async ({ page }) => {
  const app = await loadTree(page);
  const tree = app.contextTree();
  expect(await tree.badge('OrderService')).toBe('~'); // changed
  expect(await tree.badge('OrderEvents')).toBe('+'); // added
});
```

- [ ] **Step 2: Run the spec**

Run: `/opt/homebrew/bin/npx playwright test e2e/context-tree.spec.ts --project=chrome`
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add web/e2e/context-tree.spec.ts
git commit -m "test(web): e2e context-tree coverage via harnesses"
```

---

## Task 14: Full-suite verification + final commit

**Files:** none (verification only).

- [ ] **Step 1: Typecheck the whole project**

Run: `./node_modules/.bin/tsc --noEmit`
Expected: clean.

- [ ] **Step 2: Run the full unit tier**

Run: `./node_modules/.bin/vitest run`
Expected: 20 (layout) + 8 (harness-smoke) passed; no Playwright spec collected.

- [ ] **Step 3: Run the full e2e tier on Chrome**

Run: `/opt/homebrew/bin/npx playwright test --project=chrome`
Expected: every spec in `e2e/` green. On failure: `/opt/homebrew/bin/npx playwright show-report`.

- [ ] **Step 4: Sanity-check the boundary**

Confirm no spec references a raw `.hf-*` selector or calls `.click()`/`.locator()` on a DOM node directly:

Run: `grep -RnE "\.hf-|page\.locator|querySelector" web/e2e web/src/components/__tests__ || echo "CLEAN: specs speak only through harnesses"`
Expected: `CLEAN: specs speak only through harnesses` (all `.hf-*` knowledge lives under `web/testing/harness/`).

- [ ] **Step 5: Final commit (if anything is uncommitted)**

```bash
git status --short
git add -A web/testing web/e2e web/src/components/__tests__ web/playwright.config.ts web/vite.config.ts web/package.json
git commit -m "test(web): harness-based e2e+unit suite (full coverage)" || echo "nothing to commit"
```

---

## Self-review (spec coverage)

| Design-doc requirement | Task |
|---|---|
| Playwright config, system Chrome, vite :4317 webServer | 1 |
| Vitest scoped to unit files (no e2e collision) | 1 |
| Fixtures: diffGraph / nonDiffGraph / longMemberGraph | 2 |
| TestElement + Locator + HarnessEnvironment + base + helpers | 3 |
| jsdom+RTL adapter + mountAppDom (fetch stub) | 4 |
| Playwright adapter + routeGraph (route before goto) | 5 |
| Member/Internal/ComponentCard harnesses | 6 |
| Legend/ChangesPanel/ContextTree/CommentPopover/Canvas harnesses | 7 |
| App/Diagram harnesses (waitForLoaded, tabs, sub-harness getters) | 8 |
| Unit slice proving harnesses run in jsdom | 9 |
| diff-mode: PR header, tabs(38/3), legend, derived diffs, edges(6/5), no in-card tags, removed-not-struck, no PR dup; non-diff negatives | 10 |
| component-card: auto-expand, initials, grouped actions no-overlap, info popover above, collapsed single-+, one-line header >220, expand toggle, expand-all round-trip, fit-width widen/restore, tooltips, port→popover | 11 |
| canvas: grab cursor, oversized sizer, drag pans + .panning cleared, pan preserves focus, zoom ±/fit/ctrl-wheel (no page scroll), port labels on hover | 12 |
| context-tree: BC rows, drill cmp→internal→member, row-click focuses card, diff badges | 13 |
| Full verification + boundary grep | 14 |

**Anti-flake:** routes registered before `goto`; every read gated on `waitForLoaded()`/`waitUntil(...)` because ELK is async; structure/class/`styleProp` preferred over pixels; geometry comparisons use ±1–2px tolerances; `channel:'chrome'` avoids browser download.
