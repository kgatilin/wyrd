import { ComponentHarness } from './test-element';

/** The left-panel CHANGES list (`.hf-change-card` rows). Rooted at `.hifi`. */
export class ChangesPanelHarness extends ComponentHarness {
  /** Number of change entries. */
  async count(): Promise<number> {
    return this.env.rootLocator('.hf-change-card').count();
  }
  /** Display names of all entries (`.hf-change-name`). */
  async entryNames(): Promise<string[]> {
    const names = await this.env.rootLocator('.hf-change-name').all();
    return Promise.all(names.map((n) => n.text()));
  }
  /** Click the entry card whose name contains `name`. */
  async clickEntry(name: string): Promise<void> {
    const cards = await this.env.rootLocator('.hf-side').first();
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
    const left = await this.env.rootLocator('.hf-side').first();
    return (await left.locator('.hf-pr-tag').count()) > 0;
  }

  /** Number of `.hf-card.active` elements in the left panel. */
  async activeCount(): Promise<number> {
    const left = await this.env.rootLocator('.hf-side').first();
    return left.locator('.hf-card.active').count();
  }

  /** Trimmed text of `.hf-change-name` inside the active card, or null if none active. */
  async activeEntryName(): Promise<string | null> {
    const left = await this.env.rootLocator('.hf-side').first();
    const active = left.locator('.hf-card.active .hf-change-name');
    if ((await active.count()) === 0) return null;
    return (await active.first()).text();
  }

  /** True if the active entry's `.hf-change-name` contains `name`. */
  async isEntryActive(name: string): Promise<boolean> {
    const active = await this.activeEntryName();
    return active !== null && active.includes(name);
  }
}
