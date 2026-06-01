import { ComponentHarness } from './test-element';

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
