import { ComponentHarness } from './test-element';

/** The file-diff overlay (`.hf-diff-overlay`). */
export class DiffOverlayHarness extends ComponentHarness {
  async isOpen(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-diff-overlay').count()) > 0;
  }
  /** Path of the file whose patch is on screen; null when none is shown. */
  async activePath(): Promise<string | null> {
    const path = this.env.rootLocator('.hf-diff-filehead .path');
    if ((await path.count()) === 0) return null;
    return (await path.first()).text();
  }
  /** The names in the file rail, in list order. */
  async fileNames(): Promise<string[]> {
    const files = await this.env.rootLocator('.hf-diff-file .name').all();
    return Promise.all(files.map((file) => file.text()));
  }
  /** Whatever the overlay says instead of a patch (empty diff, unchanged file). */
  async note(): Promise<string | null> {
    const note = this.env.rootLocator('.hf-diff-note');
    if ((await note.count()) === 0) return null;
    return (await note.first()).text();
  }
  async close(): Promise<void> {
    await (await this.env.rootLocator('.hf-diff-close').first()).click();
  }
}
