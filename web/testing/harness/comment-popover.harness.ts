import { ComponentHarness } from './test-element';

/** The inline comment popover (`.hf-popover`). Rooted at `.hifi`. */
export class CommentPopoverHarness extends ComponentHarness {
  async isOpen(): Promise<boolean> {
    return (await this.root.locator('.hf-popover').count()) > 0;
  }
  async tag(): Promise<string> {
    return (await this.root.locator('.hf-popover-tag').first()).text();
  }
  async target(): Promise<string> {
    return (await this.root.locator('.hf-popover-target').first()).text();
  }
  async type(text: string): Promise<void> {
    await (await this.root.locator('.hf-popover textarea').first()).fill(text);
  }
  async submit(): Promise<void> {
    await (await this.root.locator('.hf-popover-actions .hf-btn.primary').first()).click();
  }
  async cancel(): Promise<void> {
    await (await this.root.locator('.hf-popover-actions .hf-btn').first()).click();
  }
}
