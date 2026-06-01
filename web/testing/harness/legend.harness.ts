import { ComponentHarness } from './test-element';

/** The on-canvas diff legend (`.hf-canvas-legend`). Rooted at `.hifi`. */
export class LegendHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.root.locator('.hf-canvas-legend').count()) > 0;
  }
  async itemTexts(): Promise<string[]> {
    const items = await this.root.locator('.hf-canvas-legend .hf-legend-item').all();
    return Promise.all(items.map((i) => i.text()));
  }
}
