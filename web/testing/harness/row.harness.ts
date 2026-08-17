import { ComponentHarness, DiffState, diffStateFromClasses } from './test-element';

/** One row of a class body (`.hf-row`): kind glyph, name, type column. */
export class RowHarness extends ComponentHarness {
  /** Left column: bare name, with the parameter list for a method. */
  async name(): Promise<string> {
    return (await this.root.locator('.hf-row-name').first()).text();
  }
  /** Right column: return types, field type, constant type. '' when hidden. */
  async type(): Promise<string> {
    const els = await this.root.locator('.hf-row-type').all();
    return els.length > 0 ? els[0].text() : '';
  }
  async kind(): Promise<string> {
    const classes = await this.root.classes();
    for (const kind of ['method', 'prop', 'param', 'return', 'const', 'type', 'symbol']) {
      if (classes.includes(kind)) return kind;
    }
    return '';
  }
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  async symbolVisibility(): Promise<'public' | 'internal' | 'unknown'> {
    const classes = await this.root.classes();
    if (classes.includes('symbol-public')) return 'public';
    if (classes.includes('symbol-internal')) return 'internal';
    return 'unknown';
  }
  /** The `title` tooltip on the row (full one-line text of the row). */
  async rowTitle(): Promise<string | null> {
    return this.root.getAttribute('title');
  }
  /** Computed text-decoration — used in e2e to prove removed rows are NOT struck. */
  async textDecoration(): Promise<string> {
    return this.root.computedStyleProp('text-decoration');
  }

  /** Click the row to open the comment popover. */
  async comment(): Promise<void> {
    await this.root.click();
  }

  async focusSymbol(): Promise<void> {
    await this.root.click();
  }
}
