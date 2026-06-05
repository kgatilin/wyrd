import { ComponentHarness } from './test-element';

/** The REVIEW package tree (`.hf-tree`). Rooted at `.hifi`. */
export class ContextTreeHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-tree').count()) > 0;
  }
  async boundedContextRowCount(): Promise<number> {
    return this.env.rootLocator('.hf-tree-row.bc').count();
  }
  async componentRowCount(): Promise<number> {
    return this.env.rootLocator('.hf-tree-row.cmp').count();
  }
  async fileRowCount(): Promise<number> {
    return this.env.rootLocator('.hf-tree-row.file').count();
  }
  async internalRowCount(): Promise<number> {
    return this.env.rootLocator('.hf-tree-row.internal').count();
  }
  async memberRowCount(): Promise<number> {
    return this.env.rootLocator('.hf-tree-row.member').count();
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
    const rows = await this.env.rootLocator('.hf-tree .hf-tree-row').all();
    for (const row of rows) {
      const nameEl = await row.locator('.name').first();
      if ((await nameEl.text()) === name) return row;
    }
    throw new Error(`tree row "${name}" not found`);
  }
}
