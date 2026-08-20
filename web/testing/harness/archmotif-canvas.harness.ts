import { ComponentHarness, type TestElement } from './test-element';

/**
 * The domains canvas (`.hf-domains`) — structural clusters × semantic clusters
 * as a grid, in place of the review canvas. Rooted at `.hifi`.
 */
export class ArchMotifCanvasHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-domains').count()) > 0;
  }

  /** Open it from the app bar. */
  async open(): Promise<void> {
    await (await this.env.rootLocator('.hf-appbar .hf-btn').filterByText('Domains').first()).click();
  }

  async close(): Promise<void> {
    await (await this.env.rootLocator('.hf-domains-close').first()).click();
  }

  /** Wait until a grid (rather than a readiness message) is on screen. */
  async waitForGrid(): Promise<void> {
    await this.env.waitUntil(async () => (await this.cellIds()).length > 0, {
      message: 'domains grid never rendered any cells',
    });
  }

  async verdict(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-domains-verdict').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-domains-verdict').first()).text();
  }

  /** Header figures, in render order (AMI, Q, blob, nodes, …). */
  async stats(): Promise<string[]> {
    const chips = await this.env.rootLocator('.hf-domains-stat').all();
    return Promise.all(chips.map((chip) => chip.text()));
  }

  /** The readiness message shown in place of a grid, or null when one is drawn. */
  async readiness(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-domains-state').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-domains-state').first()).text();
  }

  async readinessKind(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-domains-state').count()) === 0) return null;
    const classes = await (await this.env.rootLocator('.hf-domains-state').first()).classes();
    for (const kind of ['no-embedder', 'indexing', 'error', 'loading', 'empty']) {
      if (classes.includes(kind)) return kind;
    }
    return null;
  }

  /** Glue symbol names named in the header, highest fan-in first. */
  async glue(): Promise<string[]> {
    const nodes = await this.env.rootLocator('.hf-domains-glue-node').all();
    return Promise.all(nodes.map((node) => node.text()));
  }

  /** Semantic cluster labels across the top, in column order. */
  async columns(): Promise<string[]> {
    const heads = await this.env.rootLocator('.hf-domains-colhead .hf-domains-band-name').all();
    return Promise.all(heads.map((head) => head.text()));
  }

  /** Structural cluster labels down the side, in row order. */
  async rows(): Promise<string[]> {
    const heads = await this.env.rootLocator('.hf-domains-rowhead .hf-domains-band-name').all();
    return Promise.all(heads.map((head) => head.text()));
  }

  /** Cell identifiers (`S0·M1`), in row-major order. */
  async cellIds(): Promise<string[]> {
    const ids = await this.env.rootLocator('.hf-domains-cell-id').all();
    return Promise.all(ids.map((id) => id.text()));
  }

  async cells(): Promise<DomainCellHarness[]> {
    const els = await this.env.rootLocator('.hf-domains-cell').all();
    return els.map((el) => new DomainCellHarness(el, this.env));
  }

  async cell(id: string): Promise<DomainCellHarness> {
    for (const cell of await this.cells()) {
      if ((await cell.id()) === id) return cell;
    }
    throw new Error(`domains cell "${id}" not found`);
  }

  /** Flow lines currently drawn — only the selected or hovered cell's. */
  async edgeCount(): Promise<number> {
    return this.env.rootLocator('.hf-domains-edge').count();
  }

  async edgeLabels(): Promise<string[]> {
    const titles = await this.env.rootLocator('.hf-domains-edge title').all();
    return Promise.all(titles.map((title) => title.text()));
  }

  // ── Scope switch ──────────────────────────────────────────────────────
  async scope(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-domains-scope-btn.on').count()) > 0) {
      return (await this.env.rootLocator('.hf-domains-scope-btn.on').first()).text();
    }
    if ((await this.env.rootLocator('.hf-domains-scope-pkg.on').count()) > 0) return 'package';
    return null;
  }

  async setScope(label: string): Promise<void> {
    await (await this.env.rootLocator('.hf-domains-scope-btn').filterByText(label).first()).click();
  }

  async setPackageScope(packageId: string): Promise<void> {
    await (await this.env.rootLocator('.hf-domains-scope-pkg').first()).fill(packageId);
  }

  async isScopeEnabled(label: string): Promise<boolean> {
    const button = await this.env.rootLocator('.hf-domains-scope-btn').filterByText(label).first();
    return (await button.getAttribute('disabled')) == null;
  }
}

/** One (structural × semantic) intersection, drawn as a card. */
export class DomainCellHarness extends ComponentHarness {
  async id(): Promise<string> {
    return (await this.root.locator('.hf-domains-cell-id').first()).text();
  }

  async size(): Promise<string> {
    return (await this.root.locator('.hf-domains-cell-size').first()).text();
  }

  /** Package headers on the card, in render order. */
  async packages(): Promise<string[]> {
    const heads = await this.root.locator('.hf-domains-pkg-head').all();
    return Promise.all(heads.map((head) => head.text()));
  }

  /** Symbol names listed on the card. */
  async symbols(): Promise<string[]> {
    const names = await this.root.locator('.hf-domains-sym-name').all();
    return Promise.all(names.map((name) => name.text()));
  }

  /** Names of the symbols badged as glue. */
  async glueSymbols(): Promise<string[]> {
    const names = await this.root.locator('.hf-domains-sym.glue .hf-domains-sym-name').all();
    return Promise.all(names.map((name) => name.text()));
  }

  /** Fan-in badges on this cell's glue symbols, in the same order. */
  async glueFanIn(): Promise<string[]> {
    const badges = await this.root.locator('.hf-domains-sym.glue .hf-domains-sym-fanin').all();
    return Promise.all(badges.map((badge) => badge.text()));
  }

  /** Symbols the review marks as changed. */
  async changedSymbols(): Promise<string[]> {
    const names = await this.root
      .locator('.hf-domains-sym.diff-added .hf-domains-sym-name, .hf-domains-sym.diff-changed .hf-domains-sym-name, .hf-domains-sym.diff-removed .hf-domains-sym-name')
      .all();
    return Promise.all(names.map((name) => name.text()));
  }

  async onDiagonal(): Promise<boolean> {
    return this.root.hasClass('diagonal');
  }

  async isSelected(): Promise<boolean> {
    return this.root.hasClass('selected');
  }

  /** The "+N more" line, or null when the card lists everything. */
  async overflow(): Promise<string | null> {
    if ((await this.root.locator('.hf-domains-more').count()) === 0) return null;
    return (await this.root.locator('.hf-domains-more').first()).text();
  }

  /** Select the cell, which draws its cross-cell flow. */
  async select(): Promise<void> {
    await this.root.click();
  }

  /** Open a symbol's wiring panel. */
  async clickSymbol(name: string): Promise<void> {
    await (await this.symbolRow(name)).click();
  }

  private async symbolRow(name: string): Promise<TestElement> {
    for (const row of await this.root.locator('.hf-domains-sym').all()) {
      const label = await (await row.locator('.hf-domains-sym-name').first()).text();
      if (label === name) return row;
    }
    throw new Error(`symbol "${name}" not found on this cell`);
  }
}
