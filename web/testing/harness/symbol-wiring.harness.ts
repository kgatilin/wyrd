import { ComponentHarness, type TestElement } from './test-element';

/**
 * The symbol wiring overlay (`.hf-symbol-overlay`) — one symbol's first-level
 * neighbours as package blocks. Rooted at `.hifi`.
 */
export class SymbolWiringHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-symbol-overlay').count()) > 0;
  }

  /** The focused symbol's name. */
  async anchorName(): Promise<string> {
    return (await this.env.rootLocator('.hf-symbol-title').first()).text();
  }

  async anchorPackage(): Promise<string> {
    return (await this.env.rootLocator('.hf-symbol-subtitle').first()).text();
  }

  /** Header counters, in order: incoming, outgoing, cross-package. */
  async stats(): Promise<string[]> {
    const chips = await this.env.rootLocator('.hf-symbol-stat').all();
    return Promise.all(chips.map((chip) => chip.text()));
  }

  /** The anchor's declaration block, above both columns. */
  definition(): SymbolDefinitionHarness {
    return new SymbolDefinitionHarness(this.root, this.env);
  }

  incoming(): WiringColumnHarness {
    return new WiringColumnHarness(this.root, this.env, 'in');
  }

  outgoing(): WiringColumnHarness {
    return new WiringColumnHarness(this.root, this.env, 'out');
  }

  async toggleCrossPackageOnly(): Promise<void> {
    await (await this.env.rootLocator('.hf-symbol-filter').first()).click();
  }

  async isCrossPackageOnly(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-symbol-filter.on').count()) > 0;
  }

  async hasBack(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-symbol-back').count()) > 0;
  }

  async back(): Promise<void> {
    await (await this.env.rootLocator('.hf-symbol-back').first()).click();
  }

  async close(): Promise<void> {
    await (await this.env.rootLocator('.hf-symbol-close').first()).click();
  }

  async emptyMessage(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-symbol-empty').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-symbol-empty').first()).text();
  }
}

/**
 * The declaration block: where the anchor is written, its signature, and its
 * own source.
 */
export class SymbolDefinitionHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-symbol-def').count()) > 0;
  }

  /** `file:line` of the declaration; null while it is unknown. */
  async location(): Promise<string | null> {
    const button = this.env.rootLocator('.hf-symbol-def-open');
    if ((await button.count()) === 0) return null;
    return (await button.first()).text();
  }

  /** Open the whole file the anchor is declared in. */
  async openFile(): Promise<void> {
    await (await this.env.rootLocator('.hf-symbol-def-open').first()).click();
  }

  async signature(): Promise<string | null> {
    const sig = this.env.rootLocator('.hf-symbol-def-sig');
    if ((await sig.count()) === 0) return null;
    return (await sig.first()).text();
  }

  async doc(): Promise<string | null> {
    const doc = this.env.rootLocator('.hf-symbol-def-doc');
    if ((await doc.count()) === 0) return null;
    return (await doc.first()).text();
  }

  /** The declaration's source lines, in order. */
  async sourceLines(): Promise<string[]> {
    const rows = await this.env.rootLocator('.hf-symbol-def-src').all();
    return Promise.all(rows.map((row) => row.text()));
  }

  /** Line numbers drawn next to the source — the file's, not the block's. */
  async lineNumbers(): Promise<string[]> {
    const rows = await this.env.rootLocator('.hf-symbol-def-no').all();
    return Promise.all(rows.map((row) => row.text()));
  }

  /**
   * The note shown when the declaration on screen is the type containing the
   * anchor rather than the anchor itself; null when it is the anchor's own.
   */
  async fallbackNote(): Promise<string | null> {
    const tag = this.env.rootLocator('.hf-symbol-def-tag');
    if ((await tag.count()) === 0) return null;
    return (await tag.first()).text();
  }

  /** Whatever the block says while it has no declaration to show. */
  async state(): Promise<string | null> {
    const state = this.env.rootLocator('.hf-symbol-def-state');
    if ((await state.count()) === 0) return null;
    return (await state.first()).text();
  }

  async toggleSource(): Promise<void> {
    await (await this.env.rootLocator('.hf-symbol-def-toggle').first()).click();
  }
}

/** One side of the panel: incoming (callers) or outgoing (dependencies). */
export class WiringColumnHarness extends ComponentHarness {
  constructor(root: TestElement, env: SymbolWiringHarness['env'], private readonly side: 'in' | 'out') {
    super(root, env);
  }

  private get selector(): string {
    return `.hf-symbol-col.${this.side}`;
  }

  async count(): Promise<string> {
    return (await this.env.rootLocator(`${this.selector} .hf-symbol-col-count`).first()).text();
  }

  /** Package names of the blocks on this side, in render order. */
  async packages(): Promise<string[]> {
    const heads = await this.env.rootLocator(`${this.selector} .hf-symbol-group-pkg`).all();
    return Promise.all(heads.map((head) => head.text()));
  }

  /** Package names of the blocks flagged as crossing the package boundary. */
  async crossPackages(): Promise<string[]> {
    const heads = await this.env.rootLocator(`${this.selector} .hf-symbol-group.cross .hf-symbol-group-pkg`).all();
    return Promise.all(heads.map((head) => head.text()));
  }

  async isEmpty(): Promise<boolean> {
    return (await this.env.rootLocator(`${this.selector} .hf-symbol-none`).count()) > 0;
  }

  async links(): Promise<WiringLinkHarness[]> {
    const els = await this.env.rootLocator(`${this.selector} .hf-symbol-link`).all();
    return els.map((el) => new WiringLinkHarness(el, this.env));
  }

  async linkNames(): Promise<string[]> {
    const els = await this.env.rootLocator(`${this.selector} .hf-symbol-link-name`).all();
    return Promise.all(els.map((el) => el.text()));
  }

  async link(name: string): Promise<WiringLinkHarness> {
    for (const link of await this.links()) {
      if ((await link.name()) === name) return link;
    }
    throw new Error(`wiring link "${name}" not found in the ${this.side} column`);
  }
}

/** One neighbour row inside a package block. */
export class WiringLinkHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-symbol-link-name').first()).text();
  }

  /** Relation kinds carried by this neighbour (`calls`, `uses`, `implements`, …). */
  async relations(): Promise<string[]> {
    const chips = await this.root.locator('.hf-symbol-rel').all();
    return Promise.all(chips.map((chip) => chip.text()));
  }

  async symbolKind(): Promise<string> {
    return (await this.root.locator('.hf-symbol-kind').first()).text();
  }

  async symbolVisibility(): Promise<'public' | 'internal' | 'unknown'> {
    const classes = await (await this.root.locator('.hf-symbol-kind').first()).classes();
    if (classes.includes('symbol-public')) return 'public';
    if (classes.includes('symbol-internal')) return 'internal';
    return 'unknown';
  }

  /** The anchor's own member that carries the edge, when the anchor is a type. */
  async via(): Promise<string | null> {
    if ((await this.root.locator('.hf-symbol-link-via').count()) === 0) return null;
    return (await this.root.locator('.hf-symbol-link-via').first()).text();
  }

  async isWalkable(): Promise<boolean> {
    return (await this.root.hasClass('walkable'));
  }

  /** Re-anchor the panel on this neighbour. */
  async walk(): Promise<void> {
    await this.root.click();
  }
}
