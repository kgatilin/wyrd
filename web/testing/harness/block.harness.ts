import { ComponentHarness, DiffState, diffStateFromClasses, parsePx } from './test-element';
import { RowHarness } from './row.harness';

/**
 * A class shape inside a source-file container (`.hf-block`) — one symbol, or
 * one aggregate of a file's constants / variables / errors.
 */
export class BlockHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-block-name').first()).text();
  }
  /** Kind tag text: iface, struct, func, type, const, var, error. */
  async kindLabel(): Promise<string> {
    return (await this.root.locator('.hf-block-kind').first()).text();
  }
  async kind(): Promise<string> {
    const classes = await this.root.classes();
    for (const kind of ['iface', 'class', 'func', 'type', 'consts', 'vars', 'errors']) {
      if (classes.includes(kind)) return kind;
    }
    return '';
  }
  /** The DDD stereotype chip, or '' when the symbol carries none. */
  async stereotype(): Promise<string> {
    const els = await this.root.locator('.hf-block-stereo').all();
    return els.length > 0 ? els[0].text() : '';
  }
  /** Effective (possibly derived) diff state carried on the block class. */
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  async symbolVisibility(): Promise<'public' | 'internal' | 'unknown'> {
    const classes = await this.root.classes();
    if (classes.includes('symbol-public')) return 'public';
    if (classes.includes('symbol-internal')) return 'internal';
    return 'unknown';
  }
  /** The `title` tooltip on the block name. */
  async nameTitle(): Promise<string | null> {
    return (await this.root.locator('.hf-block-name').first()).getAttribute('title');
  }
  async rows(): Promise<RowHarness[]> {
    const rows = await this.root.locator('.hf-row').all();
    return rows.map((r) => new RowHarness(r, this.env));
  }
  async row(name: string): Promise<RowHarness> {
    for (const r of await this.rows()) {
      if ((await r.name()).includes(name)) return r;
    }
    throw new Error(`row "${name}" not found in block`);
  }
  /** A class body is rendered unless the card is in compact density. */
  async hasBody(): Promise<boolean> {
    return (await this.root.locator('.hf-block-rows').count()) > 0;
  }
  /** Inline width in px (layout output). */
  async width(): Promise<number> {
    return parsePx(await this.root.styleProp('width'));
  }
  async height(): Promise<number> {
    return parsePx(await this.root.styleProp('height'));
  }

  /** Double-click the block header to open the comment popover (tag 'internal'). */
  async commentOnHeader(): Promise<void> {
    await (await this.root.locator('.hf-block-head').first()).dblclick();
  }

  /** Single click on the header opens the symbol wiring graph. */
  async focusSymbol(): Promise<void> {
    await (await this.root.locator('.hf-block-head').first()).click();
  }
}
