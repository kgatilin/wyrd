import { ComponentHarness } from './test-element';

/**
 * The event canvas (`.hf-events`) — who appends what, and who it reaches, in
 * place of the review canvas. Rooted at `.hifi`.
 */
export class EventCanvasHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-events').count()) > 0;
  }

  /** Open it from the app bar. */
  async open(): Promise<void> {
    await (await this.env.rootLocator('.hf-appbar .hf-btn').filterByText('Events').first()).click();
  }

  async close(): Promise<void> {
    await (await this.env.rootLocator('.hf-events-close').first()).click();
  }

  /** Wait until ELK has placed the nodes, rather than a notice being up. */
  async waitForDiagram(): Promise<void> {
    await this.env.waitUntil(async () => (await this.nodeNames()).length > 0, {
      message: 'event canvas never rendered any component nodes',
    });
  }

  /** Component names on the diagram, in render order. */
  async nodeNames(): Promise<string[]> {
    const names = await this.env.rootLocator('.hf-events-node-name').all();
    return Promise.all(names.map((name) => name.text()));
  }

  /** Edge labels, in render order. */
  async linkLabels(): Promise<string[]> {
    const labels = await this.env.rootLocator('.hf-events-label-text').all();
    return Promise.all(labels.map((label) => label.text()));
  }

  /** Header figures ("3 components", "2 kinds", "2 imported", …). */
  async stats(): Promise<string[]> {
    const chips = await this.env.rootLocator('.hf-events-stat').all();
    return Promise.all(chips.map((chip) => chip.text()));
  }

  /** The notice shown in place of a diagram, or null when one is drawn. */
  async notice(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-events-notice').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-events-notice').first()).text();
  }

  async clickNode(name: string): Promise<void> {
    await (await this.env.rootLocator('.hf-events-node-name').filterByText(name).first()).click();
  }

  async clickLink(label: string): Promise<void> {
    await (await this.env.rootLocator('.hf-events-label-text').filterByText(label).first()).click();
  }

  /** How many nodes the current selection accents. */
  async accentedNodeCount(): Promise<number> {
    return this.env.rootLocator('.hf-events-node.on').count();
  }

  async isDetailOpen(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-events-detail').count()) > 0;
  }

  async detailTitle(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-events-detail-title').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-events-detail-title').first()).text();
  }

  /** Section headings in the detail rail ("Inputs …", "Outputs …", …). */
  async detailSections(): Promise<string[]> {
    const titles = await this.env.rootLocator('.hf-events-section-title').all();
    return Promise.all(titles.map((title) => title.text()));
  }

  /** Kind names listed in the detail rail. */
  async detailKinds(): Promise<string[]> {
    const kinds = await this.env.rootLocator('.hf-events-slot-kind').all();
    return Promise.all(kinds.map((kind) => kind.text()));
  }

  async clickDetailKind(kind: string): Promise<void> {
    await (await this.env.rootLocator('.hf-events-slot-kind').filterByText(kind).first()).click();
  }

  /** Fact values in the detail rail, paired with their labels. */
  async facts(): Promise<Record<string, string>> {
    const labels = await this.env.rootLocator('.hf-events-facts-label').all();
    const values = await this.env.rootLocator('.hf-events-facts-value').all();
    const out: Record<string, string> = {};
    for (let i = 0; i < Math.min(labels.length, values.length); i++) {
      out[await labels[i].text()] = await values[i].text();
    }
    return out;
  }
}
