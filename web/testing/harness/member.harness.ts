import { ComponentHarness, DiffState, diffStateFromClasses } from './test-element';

/** A member row inside an expanded internal (`.hf-member`). */
export class MemberHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-member-name').first()).text();
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
  /** The `title` tooltip on the row (full member name). */
  async rowTitle(): Promise<string | null> {
    return this.root.getAttribute('title');
  }
  /** Computed text-decoration — used in e2e to prove removed rows are NOT struck. */
  async textDecoration(): Promise<string> {
    return this.root.computedStyleProp('text-decoration');
  }

  /** Click the member row to open the comment popover (tag 'member'). */
  async comment(): Promise<void> {
    await this.root.click();
  }

  async focusSymbol(): Promise<void> {
    await this.root.click();
  }
}
