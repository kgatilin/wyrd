import { ComponentHarness } from './test-element';

/** A numbered pin marker on the canvas (`.hf-pin-marker`). */
export class MarkerHarness extends ComponentHarness {
  async number(): Promise<string> {
    return (await this.root.locator('.hf-pin-marker-num').first()).text();
  }
  async isActive(): Promise<boolean> {
    return this.root.hasClass('active');
  }
  async click(): Promise<void> {
    await this.root.dispatchClick();
  }
}
