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
  async forceClick(): Promise<void> {
    await this.loc.click({ force: true });
  }
  async dispatchClick(): Promise<void> {
    await this.loc.dispatchEvent('click');
  }
  async hover(): Promise<void> {
    await this.loc.hover();
  }
  async dblclick(): Promise<void> {
    await this.loc.dblclick();
  }
  async press(key: string): Promise<void> {
    await this.loc.press(key);
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
