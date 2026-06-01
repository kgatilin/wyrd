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
