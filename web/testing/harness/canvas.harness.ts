import { ComponentHarness, parsePx, TestElement } from './test-element';

/** The canvas viewport (`.hf-canvas-viewport`) — pan/zoom/cursor/sizer. */
export class CanvasHarness extends ComponentHarness {
  private wrap(): Promise<TestElement> {
    return this.root.locator('.hf-canvas-wrap').first();
  }
  /** Computed cursor on the scroller (e2e: 'grab'). */
  async cursor(): Promise<string> {
    return (await this.wrap()).computedStyleProp('cursor');
  }
  async isPanning(): Promise<boolean> {
    return (await this.wrap()).hasClass('panning');
  }
  async scrollPosition(): Promise<{ left: number; top: number }> {
    return (await this.wrap()).scrollPosition();
  }
  /** Drag the empty canvas background by (dx, dy). */
  async pan(dx: number, dy: number): Promise<void> {
    await this.env.panDrag(await this.wrap(), dx, dy);
  }
  /** Sizer reserves slack on every side, so it is always wider than content. */
  async sizerExceedsContent(): Promise<boolean> {
    const sizerW = parsePx(await (await this.root.locator('.hf-canvas-sizer').first()).styleProp('width'));
    const contentW = parsePx(await (await this.root.locator('.hf-canvas').first()).styleProp('width'));
    return sizerW > contentW;
  }
  // ── Zoom toolbar ─────────────────────────────────────────────────────────
  async zoomLabel(): Promise<string> {
    return (await this.root.locator('.hf-canvas-toolbar .zoom').first()).text();
  }
  async zoomIn(): Promise<void> {
    await (await this.root.locator('.hf-canvas-toolbar button[title="Zoom in"]').first()).click();
  }
  async zoomOut(): Promise<void> {
    await (await this.root.locator('.hf-canvas-toolbar button[title="Zoom out"]').first()).click();
  }
  async fit(): Promise<void> {
    await (await this.root.locator('.hf-canvas-toolbar button[title="Fit"]').first()).click();
  }
  /** Ctrl+wheel over the scroller (zoom gesture). */
  async ctrlWheelZoom(deltaY: number): Promise<void> {
    await this.env.ctrlWheel(await this.wrap(), deltaY);
  }
  /** Inline scale factor parsed from the `.hf-canvas` transform. */
  async canvasTransform(): Promise<string> {
    return (await this.root.locator('.hf-canvas').first()).styleProp('transform');
  }
}
