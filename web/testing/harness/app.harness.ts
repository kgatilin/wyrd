import { ComponentHarness } from './test-element';
import { DiagramHarness } from './diagram.harness';
import { CanvasHarness } from './canvas.harness';
import { LegendHarness } from './legend.harness';
import { ChangesPanelHarness } from './changes-panel.harness';
import { ContextTreeHarness } from './context-tree.harness';
import { CommentPopoverHarness } from './comment-popover.harness';
import { CommentsPanelHarness } from './comments-panel.harness';
import { MarkerHarness } from './marker.harness';

/** Top-level harness rooted at `.hifi`. Entry point: env.load(AppHarness). */
export class AppHarness extends ComponentHarness {
  /** Resolve once ELK has laid out the diagram (components mounted). */
  async waitForLoaded(): Promise<void> {
    await this.env.waitUntil(async () => (await this.env.rootLocator('.hf-cmp').count()) >= 1, {
      message: 'diagram never rendered any components',
    });
  }

  // ── PR header / app bar ────────────────────────────────────────────────
  async hasPrHeader(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-prheader').count()) > 0;
  }
  async prTitle(): Promise<string> {
    return (await this.env.rootLocator('.hf-pr-title').first()).text();
  }
  async branchCrumb(): Promise<string | null> {
    if ((await this.env.rootLocator('.hf-crumbs .branch').count()) === 0) return null;
    return (await this.env.rootLocator('.hf-crumbs .branch').first()).text();
  }

  // ── Left panel tabs ────────────────────────────────────────────────────
  async hasChangesTab(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-tabs button').filterByText('CHANGES').count()) > 0;
  }
  async changesTabCount(): Promise<number> {
    const btn = this.env.rootLocator('.hf-tabs button').filterByText('CHANGES');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async contextsTabCount(): Promise<number> {
    const btn = this.env.rootLocator('.hf-tabs button').filterByText('CONTEXTS');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async openChangesTab(): Promise<void> {
    await (await this.env.rootLocator('.hf-tabs button').filterByText('CHANGES').first()).click();
    await this.env.waitUntil(async () => (await this.env.rootLocator('.hf-change-card').count()) >= 1, {
      message: 'CHANGES list never rendered',
    });
  }
  async openContextsTab(): Promise<void> {
    await (await this.env.rootLocator('.hf-tabs button').filterByText('CONTEXTS').first()).click();
    await this.env.waitUntil(async () => (await this.env.rootLocator('.hf-tree').count()) >= 1, {
      message: 'CONTEXTS tree never rendered',
    });
  }

  // ── Sub-harnesses ────────────────────────────────────────────────────────
  async diagram(): Promise<DiagramHarness> {
    const canvas = await this.env.rootLocator('.hf-canvas').first();
    return new DiagramHarness(canvas, this.env);
  }
  async canvas(): Promise<CanvasHarness> {
    const viewport = await this.env.rootLocator('.hf-canvas-viewport').first();
    return new CanvasHarness(viewport, this.env);
  }
  legend(): LegendHarness {
    return new LegendHarness(this.root, this.env);
  }
  changesPanel(): ChangesPanelHarness {
    return new ChangesPanelHarness(this.root, this.env);
  }
  contextTree(): ContextTreeHarness {
    return new ContextTreeHarness(this.root, this.env);
  }
  commentPopover(): CommentPopoverHarness {
    return new CommentPopoverHarness(this.root, this.env);
  }

  commentsPanel(): CommentsPanelHarness {
    return new CommentsPanelHarness(this.root, this.env);
  }

  async markers(): Promise<MarkerHarness[]> {
    const els = await this.env.rootLocator('.hf-pin-marker').all();
    return els.map((el) => new MarkerHarness(el, this.env));
  }

  async markerCount(): Promise<number> {
    return this.env.rootLocator('.hf-pin-marker').count();
  }

  async markerByNumber(n: string): Promise<MarkerHarness> {
    for (const m of await this.markers()) {
      if ((await m.number()) === n) return m;
    }
    throw new Error(`marker with number "${n}" not found`);
  }

  async submitReviewCount(): Promise<number> {
    const text = await (await this.env.rootLocator('.hf-appbar .hf-btn.primary .count').first()).text();
    return parseInt(text, 10);
  }

  // ── Chrome: theme ─────────────────────────────────────────────────────────
  async themeName(): Promise<'dark' | 'light'> {
    const el = await this.env.rootLocator('.hifi').first();
    const classes = await el.classes();
    return classes.includes('theme-dark') ? 'dark' : 'light';
  }
  async toggleTheme(): Promise<void> {
    await (await this.env.rootLocator('.hf-appbar .hf-btn[title="Toggle theme"]').first()).click();
  }

  // ── Chrome: level segmented control ──────────────────────────────────────
  async activeLevelIndex(): Promise<number> {
    const buttons = await this.env.rootLocator('.hf-appbar .hf-seg button').all();
    for (let i = 0; i < buttons.length; i++) {
      if (await buttons[i].hasClass('on')) return i;
    }
    return -1;
  }
  async setLevel(index: number): Promise<void> {
    await (await this.env.rootLocator('.hf-appbar .hf-seg button').nth(index)).click();
  }

  // ── Chrome: left panel collapse ───────────────────────────────────────────
  async isLeftCollapsed(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-side:not(.right).collapsed').count()) > 0;
  }
  async toggleLeftPanel(): Promise<void> {
    await (await this.env.rootLocator('.hf-side-toggle.left').first()).click();
  }
  async leftCollapsedLabel(): Promise<string> {
    return (await this.env.rootLocator('.hf-side:not(.right) .hf-side-vlabel').first()).text();
  }

  async commentOnFirstEdge(): Promise<void> {
    await (await this.env.rootLocator('.edges-svg .hf-edge-hit').first()).dispatchClick();
  }
}
