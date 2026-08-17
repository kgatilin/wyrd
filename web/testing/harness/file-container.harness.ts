import { ComponentHarness, DiffState, diffStateFromClasses, parsePx } from './test-element';
import { BlockHarness } from './block.harness';

/** A source-file container inside an expanded package card (`.hf-file`). */
export class FileContainerHarness extends ComponentHarness {
  /** The file name shown in the container header, e.g. "options.go". */
  async label(): Promise<string> {
    return (await this.root.locator('.hf-file-name').first()).text();
  }
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  async blocks(): Promise<BlockHarness[]> {
    const blocks = await this.root.locator('.hf-block').all();
    return blocks.map((b) => new BlockHarness(b, this.env));
  }
  async block(name: string): Promise<BlockHarness> {
    for (const b of await this.blocks()) {
      if ((await b.name()) === name) return b;
    }
    throw new Error(`block "${name}" not found in file container`);
  }
  async width(): Promise<number> {
    return parsePx(await this.root.styleProp('width'));
  }
  async height(): Promise<number> {
    return parsePx(await this.root.styleProp('height'));
  }
}
