import { ComponentHarness } from './test-element';
import { DiagramHarness } from './diagram.harness';
import { CanvasHarness } from './canvas.harness';
import { LegendHarness } from './legend.harness';
import { ChangesPanelHarness } from './changes-panel.harness';
import { ContextTreeHarness } from './context-tree.harness';
import { CommentPopoverHarness } from './comment-popover.harness';

/** Top-level harness rooted at `.hifi`. Entry point: env.load(AppHarness). */
export class AppHarness extends ComponentHarness {
  /** Resolve once ELK has laid out the diagram (components mounted). */
  async waitForLoaded(): Promise<void> {
    await this.env.waitUntil(async () => (await this.root.locator('.hf-cmp').count()) >= 1, {
      message: 'diagram never rendered any components',
    });
  }

  // ── PR header / app bar ────────────────────────────────────────────────
  async hasPrHeader(): Promise<boolean> {
    return (await this.root.locator('.hf-prheader').count()) > 0;
  }
  async prTitle(): Promise<string> {
    return (await this.root.locator('.hf-pr-title').first()).text();
  }
  async branchCrumb(): Promise<string | null> {
    if ((await this.root.locator('.hf-crumbs .branch').count()) === 0) return null;
    return (await this.root.locator('.hf-crumbs .branch').first()).text();
  }

  // ── Left panel tabs ────────────────────────────────────────────────────
  async hasChangesTab(): Promise<boolean> {
    return (await this.root.locator('.hf-tabs button').filterByText('CHANGES').count()) > 0;
  }
  async changesTabCount(): Promise<number> {
    const btn = this.root.locator('.hf-tabs button').filterByText('CHANGES');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async contextsTabCount(): Promise<number> {
    const btn = this.root.locator('.hf-tabs button').filterByText('CONTEXTS');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async openChangesTab(): Promise<void> {
    await (await this.root.locator('.hf-tabs button').filterByText('CHANGES').first()).click();
    await this.env.waitUntil(async () => (await this.root.locator('.hf-change-card').count()) >= 1, {
      message: 'CHANGES list never rendered',
    });
  }
  async openContextsTab(): Promise<void> {
    await (await this.root.locator('.hf-tabs button').filterByText('CONTEXTS').first()).click();
    await this.env.waitUntil(async () => (await this.root.locator('.hf-tree').count()) >= 1, {
      message: 'CONTEXTS tree never rendered',
    });
  }

  // ── Sub-harnesses ────────────────────────────────────────────────────────
  async diagram(): Promise<DiagramHarness> {
    const canvas = await this.root.locator('.hf-canvas').first();
    return new DiagramHarness(canvas, this.env);
  }
  async canvas(): Promise<CanvasHarness> {
    const viewport = await this.root.locator('.hf-canvas-viewport').first();
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
}
