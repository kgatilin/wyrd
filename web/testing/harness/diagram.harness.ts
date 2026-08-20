import { ComponentHarness } from './test-element';
import { ComponentCardHarness } from './component-card.harness';

/** The laid-out diagram (`.hf-canvas`). */
export class DiagramHarness extends ComponentHarness {
  async componentCount(): Promise<number> {
    return this.root.locator('.hf-cmp').count();
  }
  async components(): Promise<ComponentCardHarness[]> {
    const cards = await this.root.locator('.hf-cmp').all();
    return cards.map((c) => new ComponentCardHarness(c, this.env));
  }
  async componentNames(): Promise<string[]> {
    return Promise.all((await this.components()).map((c) => c.name()));
  }
  async component(name: string): Promise<ComponentCardHarness> {
    for (const c of await this.components()) {
      if ((await c.name()) === name) return c;
    }
    throw new Error(`component "${name}" not found on canvas`);
  }
  async boundedContextNames(): Promise<string[]> {
    const labels = await this.root.locator('.hf-bc-group .hf-bc-label').all();
    return Promise.all(labels.map((l) => l.text()));
  }
  /** Count of main edge paths (`.hf-edge`; excludes arrow markers + hit paths). */
  async edgeCount(): Promise<number> {
    return this.root.locator('.hf-edge').count();
  }
  /**
   * Ids of the edges the review report is pointing at, in render order. The
   * accent is what a highlight row is for, so the spec asks for the ids rather
   * than for a count.
   */
  async highlightedEdgeIds(): Promise<string[]> {
    const paths = await this.root.locator('.hf-edge-hot .hf-edge').all();
    const ids: string[] = [];
    for (const path of paths) {
      ids.push(((await path.getAttribute('id')) ?? '').replace(/^epath-/, ''));
    }
    return ids;
  }
  /** Count of edges carrying a diff class. */
  async diffEdgeCount(): Promise<number> {
    return this.root.locator('.hf-edge.added, .hf-edge.removed, .hf-edge.changed').count();
  }
}
