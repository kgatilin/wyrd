import { ComponentHarness, type TestElement } from './test-element';

/**
 * The architecture review report (`.hf-report`) — the overlay the app bar's
 * ArchMotif button opens. Rooted at `.hifi`.
 */
export class ArchMotifPanelHarness extends ComponentHarness {
  async isPresent(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-report').count()) > 0;
  }

  /** Open it from the app bar. */
  async open(): Promise<void> {
    await (await this.env.rootLocator('.hf-appbar .hf-btn').filterByText('ArchMotif').first()).click();
  }

  async close(): Promise<void> {
    await (await this.env.rootLocator('.hf-motif-overlay-close').first()).click();
  }

  /** Wait until the report itself is on screen rather than a spinner. */
  async waitForReport(): Promise<void> {
    await this.env.waitUntil(async () => (await this.env.rootLocator('.hf-report-head').count()) > 0, {
      message: 'the review report never rendered',
    });
  }

  /** "this branch" or "whole repository", or null before the report lands. */
  async mode(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-report-mode').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-report-mode').first()).text();
  }

  /** The comparison line, present only in review mode. */
  async base(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-report-base').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-report-base').first()).text();
  }

  /** Labels of the panel's own buttons — what it offers besides its rows. */
  async buttons(): Promise<string[]> {
    const els = await this.env.rootLocator('.hf-report .hf-source-action').all();
    return Promise.all(els.map((el) => el.text()));
  }

  async refresh(): Promise<void> {
    await (await this.env.rootLocator('.hf-report .hf-source-action').filterByText('Refresh').first()).click();
  }

  /** The read error shown in place of a report, or null. */
  async error(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-report-error').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-report-error').first()).text();
  }

  /** Analysis the server reported it could not run. */
  async warnings(): Promise<string[]> {
    const els = await this.env.rootLocator('.hf-report-warning').all();
    return Promise.all(els.map((el) => el.text()));
  }

  /** The embedding-index line, shown only while the index is in the way. */
  async indexNote(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-report-index').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-report-index').first()).text();
  }

  async totals(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-report-totals').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-report-totals').first()).text();
  }

  /** Section titles, in render order. */
  async sectionTitles(): Promise<string[]> {
    const els = await this.env.rootLocator('.hf-report-section-title').all();
    return Promise.all(els.map((el) => el.text()));
  }

  async sections(): Promise<ReportSectionHarness[]> {
    const els = await this.env.rootLocator('.hf-report-section').all();
    return els.map((el) => new ReportSectionHarness(el, this.env));
  }

  /** One section by its server-side id (`group_cycles`, `impact`, …). */
  async section(id: string): Promise<ReportSectionHarness> {
    for (const section of await this.sections()) {
      if ((await section.id()) === id) return section;
    }
    throw new Error(`report section "${id}" not found`);
  }
}

/** One actionable state: a single "ok" line, or a headline and its rows. */
export class ReportSectionHarness extends ComponentHarness {
  async id(): Promise<string | null> {
    return this.root.getAttribute('data-section');
  }

  async title(): Promise<string> {
    return (await this.root.locator('.hf-report-section-title').first()).text();
  }

  /** "ok" when the section's state has not occurred, else "flag". */
  async state(): Promise<'ok' | 'flag'> {
    return (await this.root.hasClass('ok')) ? 'ok' : 'flag';
  }

  /** The single "none" line, or the headline above the rows. */
  async summary(): Promise<string> {
    return (await this.root.locator('.hf-report-summary').first()).text();
  }

  async count(): Promise<number | null> {
    if ((await this.root.locator('.hf-report-count').count()) === 0) return null;
    return parseInt((await (await this.root.locator('.hf-report-count').first()).text()) || '0', 10);
  }

  /** The "and N more" line of a capped section, or null. */
  async more(): Promise<string | null> {
    if ((await this.root.locator('.hf-report-more').count()) === 0) return null;
    return (await this.root.locator('.hf-report-more').first()).text();
  }

  async rows(): Promise<ReportRowHarness[]> {
    const els = await this.root.locator('.hf-report-row').all();
    return els.map((el) => new ReportRowHarness(el, this.env));
  }

  /** Row texts, in render order. */
  async rowTexts(): Promise<string[]> {
    const els = await this.root.locator('.hf-report-row-text').all();
    return Promise.all(els.map((el) => el.text()));
  }

  async row(text: string): Promise<ReportRowHarness> {
    for (const row of await this.rows()) {
      if ((await row.text()) === text) return row;
    }
    throw new Error(`report row "${text}" not found`);
  }
}

/** One finding: what happened, what to do, and the gestures it offers. */
export class ReportRowHarness extends ComponentHarness {
  async text(): Promise<string> {
    return (await this.root.locator('.hf-report-row-text').first()).text();
  }

  async detail(): Promise<string | null> {
    if ((await this.root.locator('.hf-report-row-detail').count()) === 0) return null;
    return (await this.root.locator('.hf-report-row-detail').first()).text();
  }

  async tag(): Promise<string | null> {
    if ((await this.root.locator('.hf-report-tag').count()) === 0) return null;
    return (await this.root.locator('.hf-report-tag').first()).text();
  }

  /** Run the row's own gesture. */
  async click(): Promise<void> {
    await (await this.main()).click();
  }

  /** What the row itself does, read off its tooltip. */
  async title(): Promise<string | null> {
    return (await this.main()).getAttribute('title');
  }

  async isClickable(): Promise<boolean> {
    return (await (await this.main()).getAttribute('disabled')) == null;
  }

  /** Kinds of the further gestures beside the row (`focus`, `source`, …). */
  async actions(): Promise<string[]> {
    const els = await this.root.locator('.hf-report-act').all();
    const kinds: string[] = [];
    for (const el of els) {
      const classes = await el.classes();
      kinds.push(classes.find((name) => name !== 'hf-report-act') ?? '');
    }
    return kinds;
  }

  async clickAction(kind: string): Promise<void> {
    const els = await this.root.locator(`.hf-report-act.${kind}`).all();
    if (els.length === 0) throw new Error(`row offers no "${kind}" action`);
    await els[0].click();
  }

  private async main(): Promise<TestElement> {
    return this.root.locator('.hf-report-row-main').first();
  }
}
