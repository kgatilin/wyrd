import { ComponentHarness } from './test-element';

/** The inline comment popover (`.hf-popover`). Rooted at `.hifi`. */
export class CommentPopoverHarness extends ComponentHarness {
  async isOpen(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-popover').count()) > 0;
  }
  async tag(): Promise<string> {
    return (await this.env.rootLocator('.hf-popover-tag').first()).text();
  }
  async target(): Promise<string> {
    return (await this.env.rootLocator('.hf-popover-target').first()).text();
  }
  async type(text: string): Promise<void> {
    await (await this.env.rootLocator('.hf-popover textarea').first()).fill(text);
  }
  async submit(): Promise<void> {
    await (await this.env.rootLocator('.hf-popover-actions .hf-btn.primary').first()).click();
  }
  async cancel(): Promise<void> {
    await (await this.env.rootLocator('.hf-popover-actions .hf-btn').first()).click();
  }

  /** True when the Comment button is disabled (textarea has no non-whitespace text). */
  async isCommentDisabled(): Promise<boolean> {
    const btn = await this.env.rootLocator('.hf-popover-actions .hf-btn.primary').first();
    return (await btn.getAttribute('disabled')) !== null;
  }

  /** Press Escape inside the textarea (cancels the popover). */
  async pressEscape(): Promise<void> {
    await (await this.env.rootLocator('.hf-popover textarea').first()).press('Escape');
  }

  /** Press ⌘/Ctrl+Enter inside the textarea (submits the comment). */
  async submitWithKeyboard(): Promise<void> {
    await (await this.env.rootLocator('.hf-popover textarea').first()).press('Meta+Enter');
  }
}
