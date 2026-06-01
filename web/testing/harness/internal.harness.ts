import { ComponentHarness, DiffState, diffStateFromClasses, parsePx } from './test-element';
import { MemberHarness } from './member.harness';

/** An internal card inside an expanded component (`.hf-internal`). */
export class InternalHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-internal-name').first()).text();
  }
  async kind(): Promise<'iface' | 'class'> {
    return (await this.root.hasClass('iface')) ? 'iface' : 'class';
  }
  /** Effective (possibly derived) diff state carried on the card class. */
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  /** The `title` tooltip on the internal name. */
  async nameTitle(): Promise<string | null> {
    return (await this.root.locator('.hf-internal-name').first()).getAttribute('title');
  }
  async members(): Promise<MemberHarness[]> {
    const rows = await this.root.locator('.hf-member').all();
    return rows.map((r) => new MemberHarness(r, this.env));
  }
  async member(name: string): Promise<MemberHarness> {
    for (const m of await this.members()) {
      if ((await m.name()).includes(name)) return m;
    }
    throw new Error(`member "${name}" not found in internal`);
  }
  /** Members are visible only when the internal is expanded. */
  async isExpanded(): Promise<boolean> {
    return (await this.root.locator('.hf-member-list').count()) > 0;
  }
  async toggleFitWidth(): Promise<void> {
    await (await this.root.locator('.hf-internal-toggle').first()).click();
  }
  /** '+' (fixed width) or '−' (fit-width on). */
  async fitWidthGlyph(): Promise<string> {
    return (await this.root.locator('.hf-internal-toggle').first()).text();
  }
  /** Inline width in px (ELK/layout output). */
  async width(): Promise<number> {
    return parsePx(await this.root.styleProp('width'));
  }
}
