import { ComponentHarness } from './test-element';

/**
 * Right-side comments panel (`.hf-side.right`). Doc-scoped singleton.
 * All queries must use `this.env.rootLocator(...)` — the root element can go
 * stale after the graph swap that happens on load.
 */
export class CommentsPanelHarness extends ComponentHarness {
  async cardCount(): Promise<number> {
    return this.env.rootLocator('.hf-side.right .hf-card').count();
  }

  async badgeCount(): Promise<number> {
    const text = await (await this.env.rootLocator('.hf-side.right .hf-tabs .count').first()).text();
    return parseInt(text, 10);
  }

  async cardTargets(): Promise<string[]> {
    const els = await this.env.rootLocator('.hf-side.right .hf-card-target').all();
    return Promise.all(els.map((el) => el.text()));
  }

  async clickCardByNumber(n: string): Promise<void> {
    const cards = await this.env.rootLocator('.hf-side.right .hf-card').all();
    for (const card of cards) {
      const mini = await card.locator('.hf-pin-marker-mini').first();
      if ((await mini.text()) === n) {
        await card.click();
        return;
      }
    }
    throw new Error(`comment card with number "${n}" not found`);
  }

  async isCardActiveByNumber(n: string): Promise<boolean> {
    const cards = await this.env.rootLocator('.hf-side.right .hf-card').all();
    for (const card of cards) {
      const mini = await card.locator('.hf-pin-marker-mini').first();
      if ((await mini.text()) === n) {
        return card.hasClass('active');
      }
    }
    return false;
  }

  async isCollapsed(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-side.right.collapsed').count()) > 0;
  }

  async collapse(): Promise<void> {
    if (await this.isCollapsed()) return;
    await (await this.env.rootLocator('.hf-side-toggle.right').first()).click();
  }

  async expand(): Promise<void> {
    if (!(await this.isCollapsed())) return;
    await (await this.env.rootLocator('.hf-side-toggle.right').first()).click();
  }

  async collapsedLabel(): Promise<string> {
    return (await this.env.rootLocator('.hf-side.right .hf-side-vlabel').first()).text();
  }
}
