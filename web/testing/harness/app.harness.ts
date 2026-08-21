import { ComponentHarness } from './test-element';
import { DiagramHarness } from './diagram.harness';
import { CanvasHarness } from './canvas.harness';
import { LegendHarness } from './legend.harness';
import { ContextTreeHarness } from './context-tree.harness';
import { MarkerHarness } from './marker.harness';
import { SymbolWiringHarness } from './symbol-wiring.harness';
import { AskPanelHarness } from './ask-panel.harness';
import { ArchMotifCanvasHarness } from './archmotif-canvas.harness';
import { ArchMotifPanelHarness } from './archmotif-panel.harness';
import { DiffOverlayHarness } from './diff-overlay.harness';
import { SourceDrawerHarness } from './source-drawer.harness';

/** Top-level harness rooted at `.hifi`. Entry point: env.load(AppHarness). */
export class AppHarness extends ComponentHarness {
  /** Resolve once ELK has laid out the diagram (components mounted). */
  async waitForLoaded(): Promise<void> {
    await this.env.waitUntil(async () => (await this.env.rootLocator('.hf-cmp').count()) >= 1, {
      message: 'diagram never rendered any components',
    });
  }

  /** True iff the app is showing its load-error screen (an `Error: …` paragraph).
   *  NOTE: unreachable via routing today — loadGraph never rejects (falls back to
   *  the built-in fixture). Kept so the spec can assert the error screen is ABSENT. */
  async hasError(): Promise<boolean> {
    const ps = await this.env.rootLocator('.hifi p').all();
    for (const p of ps) {
      if ((await p.text()).startsWith('Error:')) return true;
    }
    return false;
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

  ask(): AskPanelHarness {
    return new AskPanelHarness(this.root, this.env);
  }

  /** The domains canvas (whether or not it is open). */
  domains(): ArchMotifCanvasHarness {
    return new ArchMotifCanvasHarness(this.root, this.env);
  }

  /** The architecture review report (whether or not it is open). */
  report(): ArchMotifPanelHarness {
    return new ArchMotifPanelHarness(this.root, this.env);
  }

  /** The file-diff overlay (whether or not it is open). */
  fileDiff(): DiffOverlayHarness {
    return new DiffOverlayHarness(this.root, this.env);
  }

  /** The source viewer drawer (whether or not it is open). */
  sourceDrawer(): SourceDrawerHarness {
    return new SourceDrawerHarness(this.root, this.env);
  }

  // ── Review bar "View" popover ───────────────────────────────────────────
  async openViewOptions(): Promise<void> {
    if ((await this.env.rootLocator('.hf-reviewbar-popover').count()) > 0) return;
    await (await this.env.rootLocator('.hf-reviewbar-toggle').first()).click();
  }
  /** Set one of the popover's selects by its label ("Details", "Focus", ...). */
  async setViewOption(label: string, value: string): Promise<void> {
    await this.openViewOptions();
    for (const row of await this.env.rootLocator('.hf-reviewbar-popover label').all()) {
      if ((await (await row.locator('span').first()).text()) === label) {
        await (await row.locator('select').first()).fill(value);
        return;
      }
    }
    throw new Error(`review view option "${label}" not found`);
  }

  // ── Left panel review tree ──────────────────────────────────────────────
  async hasReviewTab(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-tabs button').filterByText('REVIEW').count()) > 0;
  }
  async reviewTabCount(): Promise<number> {
    const btn = this.env.rootLocator('.hf-tabs button').filterByText('REVIEW');
    return parseInt((await (await btn.locator('.count').first()).text()) || '0', 10);
  }
  async openReviewTree(): Promise<void> {
    await this.env.waitUntil(async () => (await this.env.rootLocator('.hf-tree').count()) >= 1, {
      message: 'review tree never rendered',
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
  /** The symbol wiring overlay, opened by focusing a symbol on a card. */
  symbolWiring(): SymbolWiringHarness {
    return new SymbolWiringHarness(this.root, this.env);
  }

  legend(): LegendHarness {
    return new LegendHarness(this.root, this.env);
  }
  contextTree(): ContextTreeHarness {
    return new ContextTreeHarness(this.root, this.env);
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
  // ── Chrome: left panel collapse ───────────────────────────────────────────
  async isLeftCollapsed(): Promise<boolean> {
    return (await this.env.rootLocator('.hf-side.collapsed').count()) > 0;
  }
  async toggleLeftPanel(): Promise<void> {
    await (await this.env.rootLocator('.hf-side-toggle.left').first()).click();
  }
  async leftCollapsedLabel(): Promise<string> {
    return (await this.env.rootLocator('.hf-side .hf-side-vlabel').first()).text();
  }

  async commentOnFirstEdge(): Promise<void> {
    await (await this.env.rootLocator('.edges-svg .hf-edge-hit').first()).dispatchClick();
  }
}
