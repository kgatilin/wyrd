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
