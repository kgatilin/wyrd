import { ComponentHarness } from './test-element';

/**
 * The ask panel (`.hf-ask`) in the left rail — the question box and its ranked
 * answer. Rooted at `.hifi`.
 */
export class AskPanelHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-ask').count()) > 0;
  }

  /** Switch the left rail to the ask tab. */
  async open(): Promise<void> {
    await (await this.env.rootLocator('.hf-tabs button').filterByText('ASK').first()).click();
  }

  async ask(query: string): Promise<void> {
    const field = await this.env.rootLocator('.hf-ask-field').first();
    await field.fill(query);
    await field.press('Enter');
  }

  async clear(): Promise<void> {
    await (await this.env.rootLocator('.hf-ask-clear').first()).click();
  }

  async meta(): Promise<string> {
    return (await this.env.rootLocator('.hf-ask-meta').first()).text();
  }

  /** Package headers of the answer, in rank order. */
  async groups(): Promise<string[]> {
    const heads = await this.env.rootLocator('.hf-ask-group-name').all();
    return Promise.all(heads.map((head) => head.text()));
  }

  /** Hit symbol names, in rank order. */
  async hits(): Promise<string[]> {
    const names = await this.env.rootLocator('.hf-ask-hit-name').all();
    return Promise.all(names.map((name) => name.text()));
  }

  async clickHit(name: string): Promise<void> {
    await (await this.env.rootLocator('.hf-ask-hit').filterByText(name).first()).click();
  }

  async toggleDetail(): Promise<void> {
    await (await this.env.rootLocator('.hf-ask-toggle').first()).click();
  }

  /** The question chip in the review bar; null when no answer is projecting. */
  async reviewBarQuery(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-reviewbar-ask').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-reviewbar-ask').first()).text();
  }
}
