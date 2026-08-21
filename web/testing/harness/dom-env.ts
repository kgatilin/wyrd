import React from 'react';
import { render, fireEvent } from '@testing-library/react';
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
  async forceClick(): Promise<void> {
    fireEvent.click(this.el);
  }
  async dispatchClick(): Promise<void> {
    fireEvent.click(this.el);
  }
  async hover(): Promise<void> {
    fireEvent.mouseOver(this.el);
    fireEvent.mouseEnter(this.el);
  }
  async dblclick(): Promise<void> {
    fireEvent.doubleClick(this.el);
  }
  async press(key: string): Promise<void> {
    const hasMeta = key.includes('Meta+');
    const hasCtrl = key.includes('Control+');
    const baseKey = key.replace(/^(Meta|Control)\+/, '');
    fireEvent.keyDown(this.el, { key: baseKey, metaKey: hasMeta, ctrlKey: hasCtrl });
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
    // Plain polling loop (NOT RTL waitFor — its async-callback path does not
    // re-poll reliably here and hangs). Yielding to a real setTimeout lets the
    // stubbed fetch + the async ELK layout `.then(setLaid)` chain progress and
    // React flush between probes. State updates land outside act() (React still
    // applies them; it only logs a warning), which is fine for the test tier.
    const timeout = opts?.timeout ?? 5000;
    const interval = opts?.interval ?? 30;
    const deadline = Date.now() + timeout;
    for (;;) {
      if (await predicate()) return;
      if (Date.now() >= deadline) {
        throw new Error(opts?.message ?? 'waitUntil predicate not satisfied');
      }
      await new Promise((resolve) => setTimeout(resolve, interval));
    }
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

  async wheel(target: TestElement, deltaY: number): Promise<void> {
    const el = (target as DomElement).el;
    fireEvent.wheel(el, { deltaY });
  }

  async load<T extends ComponentHarness>(ctor: HarnessConstructor<T>): Promise<T> {
    const root = await this.rootLocator('.hifi').first();
    return new ctor(root, this);
  }
}

/**
 * Stub fetch so loadGraph() resolves to `graph` (App fetches /api/uigraph
 * first), render <App/>, and return a DomEnvironment. Call cleanup() and
 * vi.unstubAllGlobals() in the test's afterEach.
 */
export interface MountOptions {
  /**
   * Answers POST /api/search. Without it the ask panel's requests fail, which
   * is itself a state worth testing.
   */
  search?: (query: string, k: number) => { hits: unknown[]; dense: boolean };
  /**
   * Answers GET /api/gitdiff. Without it the file diff reports a read error,
   * which is itself a state worth testing.
   */
  gitDiff?: () => unknown;
  /**
   * Answers GET /api/source. Without it the source drawer reports a read
   * error, which is itself a state worth testing.
   */
  source?: (path: string) => { path?: string; content: string; hash?: string };
  /**
   * Answers GET /api/node/{id} — one symbol's declaration, as the wiring
   * panel reads it. Return undefined for an id the graph has no node for
   * (a field, an interface method): the panel acts on that, it is not an
   * error.
   */
  node?: (id: string) => unknown;
  /**
   * Answers GET /api/archmotif/report — the architecture review report.
   * `fresh` is what the reviewer's Refresh sends: rebuild rather than answer
   * from the daemon's warmed copy. Without a responder the panel reports a
   * read error, which is itself a state worth testing.
   */
  report?: (baseRef: string, fresh: boolean) => unknown;
  /**
   * Answers POST /api/mcp/tools/call — the daemon's analysis lenses. Return
   * the payload; the MCP ToolResult envelope is added here, so a spec writes
   * the tool's own shape. Without it the domains canvas reports the lens
   * failed, which is itself a state worth testing.
   */
  lens?: (name: string, args: Record<string, unknown>) => unknown;
  /**
   * Answers GET /api/archmotif/domains — the latent-domains partition the
   * canvas draws. Separate from `lens` because it is a separate endpoint: the
   * canvas asks the lens surface for readiness and this one for the partition,
   * which does not fit under the lens surface's result budget. Without a
   * responder the canvas reports the read failed, which is itself a state
   * worth testing.
   */
  domains?: (query: URLSearchParams) => unknown;
}

export async function mountAppDom(graph: UIGraph, options?: MountOptions): Promise<DomEnvironment> {
  // jsdom implements no scrolling API. The viewport effect centers the canvas
  // on a card after a relayout, and an exception there aborts the rest of the
  // store's subscribers — including React — so the DOM would silently freeze on
  // the pre-layout render. A no-op keeps the effect chain intact.
  if (typeof Element.prototype.scrollTo !== 'function') {
    Element.prototype.scrollTo = () => {};
  }
  const okJson = (data: unknown) =>
    ({ ok: true, json: async () => data } as unknown as Response);
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.includes('/api/uigraph')) return okJson(graph);
    if (url.includes('/api/gitdiff') && options?.gitDiff) return okJson(options.gitDiff());
    if (url.includes('/api/source') && options?.source) {
      const file = new URLSearchParams(url.split('?')[1] ?? '').get('file') ?? '';
      return okJson({ path: file, ...options.source(file) });
    }
    if (url.includes('/api/node/') && options?.node) {
      const found = options.node(decodeURIComponent(url.split('/api/node/')[1] ?? ''));
      if (found == null) {
        return {
          ok: false,
          status: 404,
          json: async () => ({ error: 'node not found' }),
          text: async () => 'node not found',
        } as unknown as Response;
      }
      return okJson(found);
    }
    if (url.includes('/api/archmotif/report') && options?.report) {
      const base = new URLSearchParams(url.split('?')[1] ?? '').get('base') ?? '';
      const headers = (init?.headers ?? {}) as Record<string, string>;
      const fresh = String(headers['Cache-Control'] ?? '').includes('no-cache');
      return okJson(options.report(base, fresh));
    }
    if (url.includes('/api/archmotif/domains') && options?.domains) {
      return okJson(options.domains(new URLSearchParams(url.split('?')[1] ?? '')));
    }
    if (url.includes('/api/mcp/tools/call') && options?.lens) {
      const body = JSON.parse(String(init?.body ?? '{}')) as {
        name?: string;
        arguments?: Record<string, unknown>;
      };
      const payload = options.lens(body.name ?? '', body.arguments ?? {});
      return okJson({ content: [{ type: 'text', text: JSON.stringify(payload) }] });
    }
    if (url.includes('/api/search') && options?.search) {
      const body = JSON.parse(String(init?.body ?? '{}')) as { query?: string; k?: number };
      return okJson(options.search(body.query ?? '', body.k ?? 0));
    }
    // No responder: a real failure, with a status, so a spec exercises the
    // error path the way the daemon would produce it.
    return {
      ok: false,
      status: 503,
      json: async () => ({}),
      text: async () => '',
    } as unknown as Response;
  });
  render(React.createElement(App));
  return new DomEnvironment();
}
