import { BoundingBox, ComponentHarness, TestElement } from './test-element';

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

  /** Where each component node sits, keyed by name — which way the flow runs. */
  async nodeBoxes(): Promise<Record<string, BoundingBox>> {
    const names = await this.env.rootLocator('.hf-events-node-name').all();
    const boxes: Record<string, BoundingBox> = {};
    for (const name of names) {
      const box = await name.boundingBox();
      if (box) boxes[await name.text()] = box;
    }
    return boxes;
  }

  /** Flip the layout between top-to-bottom and left-to-right. */
  async toggleDirection(): Promise<void> {
    await (await this.env.rootLocator('.hf-events-direction').first()).click();
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

  /** Legend figures ("3 components", "2 kinds", "2 imported", …). */
  async stats(): Promise<string[]> {
    const chips = await this.env.rootLocator('.hf-events-stat').all();
    return Promise.all(chips.map((chip) => chip.text()));
  }

  /** Click a legend chip — the ones that count something open a list of it. */
  async clickStat(text: string): Promise<void> {
    await (await this.env.rootLocator('.hf-events-stat').filterByText(text).first()).click();
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

  /** The zoom readout, e.g. "82%". Scoped past the review canvas's own copy of
   *  the shared toolbar, which is still in the DOM under this one. */
  async zoom(): Promise<string> {
    return (await this.toolbar('.zoom')).text();
  }

  async zoomIn(): Promise<void> {
    await (await this.toolbar('button[title="Zoom in"]')).click();
  }

  async fit(): Promise<void> {
    await (await this.toolbar('button[title="Fit"]')).click();
  }

  /** Scroll offsets of the pane the diagram sits in. */
  async scrollPosition(): Promise<{ left: number; top: number }> {
    return (await this.wrap()).scrollPosition();
  }

  /** Computed cursor on the scroller (e2e: 'grab'). */
  async cursor(): Promise<string> {
    return (await this.wrap()).computedStyleProp('cursor');
  }

  /** Drag the empty background by (dx, dy). */
  async pan(dx: number, dy: number): Promise<void> {
    await this.env.panDrag(await this.wrap(), dx, dy);
  }

  /** Ctrl+wheel over the scroller (the zoom gesture). */
  async ctrlWheelZoom(deltaY: number): Promise<void> {
    await this.env.ctrlWheel(await this.wrap(), deltaY);
  }

  /** Plain wheel over the scroller — scrolls the pane, never zooms. */
  async wheel(deltaY: number): Promise<void> {
    await this.env.wheel(await this.wrap(), deltaY);
  }

  private wrap(): Promise<TestElement> {
    return this.env.rootLocator('.hf-events-wrap').first();
  }

  private toolbar(selector: string): Promise<TestElement> {
    return this.env.rootLocator(`.hf-events-viewport .hf-canvas-toolbar ${selector}`).first();
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

  /** The payload blocks in the detail rail, in the order they are shown. */
  async payloads(): Promise<string[]> {
    const blocks = await this.env.rootLocator('.hf-events-schema').all();
    return Promise.all(blocks.map((block) => block.text()));
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
