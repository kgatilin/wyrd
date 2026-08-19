import { ComponentHarness } from './test-element';

/** The source viewer drawer (`.hf-source-drawer`). */
export class SourceDrawerHarness extends ComponentHarness {
  async isOpen(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-source-drawer').count()) > 0;
  }
  /** Path of the file on screen; null when the drawer is closed. */
  async path(): Promise<string | null> {
    const title = this.env.rootLocator('.hf-source-title');
    if ((await title.count()) === 0) return null;
    return (await title.first()).text();
  }
  /** The file's lines, in order. */
  async lines(): Promise<string[]> {
    const rows = await this.env.rootLocator('.hf-source-code').all();
    return Promise.all(rows.map((row) => row.text()));
  }
  async close(): Promise<void> {
    await (await this.env.rootLocator('.hf-source-close').first()).click();
  }
}
