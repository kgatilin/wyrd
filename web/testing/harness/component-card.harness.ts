import {
  BoundingBox,
  ComponentHarness,
  DiffState,
  diffStateFromClasses,
  parsePx,
  TestElement,
} from './test-element';
import { InternalHarness } from './internal.harness';

/** A component card on the canvas (`.hf-cmp`). */
export class ComponentCardHarness extends ComponentHarness {
  async name(): Promise<string> {
    return (await this.root.locator('.hf-cmp-name').first()).text();
  }
  async tech(): Promise<string> {
    return (await this.root.locator('.hf-cmp-tech').first()).text();
  }
  async packageLayer(): Promise<string> {
    return (await this.root.locator('.hf-cmp-layer').first()).text();
  }
  /** Header icon letter — the PARENT (bounded context) initial. */
  async parentInitial(): Promise<string> {
    return (await this.root.locator('.hf-cmp-icon').first()).text();
  }
  /** Effective (possibly derived) component diff state from the card class. */
  async diffState(): Promise<DiffState | null> {
    return diffStateFromClasses(await this.root.classes());
  }
  async isFocused(): Promise<boolean> {
    return this.root.hasClass('focused');
  }
  async isDimmed(): Promise<boolean> {
    return this.root.hasClass('dimmed');
  }
  /** Click the card to focus it (focus mode). */
  async focus(): Promise<void> {
    await this.root.click();
  }
  /** Expanded ⇒ the internals mini-canvas is present. */
  async isExpanded(): Promise<boolean> {
    return (await this.root.locator('.hf-cmp-canvas').count()) > 0;
  }
  async toggleExpand(): Promise<void> {
    await (await this.root.locator('.hf-cmp-expand').first()).click();
  }
  async expandButtonGlyph(): Promise<string> {
    return (await this.root.locator('.hf-cmp-expand').first()).text();
  }

  // ── Action group (top-right, OUTSIDE the clipped inner) ──────────────────
  /** Count of <button>s in the floating action group (info is a <div>). */
  async actionButtonCount(): Promise<number> {
    return this.root.locator('.hf-cmp-actions button').count();
  }
  async hasExpandAllButton(): Promise<boolean> {
    return (await this.root.locator('.hf-cmp-expand-all').count()) > 0;
  }
  async expandAll(): Promise<void> {
    await (await this.root.locator('.hf-cmp-expand-all').first()).click();
  }
  /** '«»' (none wide) or '»«' (all wide). */
  async expandAllGlyph(): Promise<string> {
    return (await this.root.locator('.hf-cmp-expand-all').first()).text();
  }
  async hasInfoButton(): Promise<boolean> {
    return (await this.root.locator('.hf-cmp-info').count()) > 0;
  }
  async hoverInfo(): Promise<void> {
    await (await this.root.locator('.hf-cmp-info-icon').first()).hover();
  }
  async infoIconBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-info-icon').first()).boundingBox();
  }
  async infoPopover(): Promise<TestElement> {
    return this.root.locator('.hf-cmp-info-pop').first();
  }
  async expandAllBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-expand-all').first()).boundingBox();
  }
  async expandBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-expand').first()).boundingBox();
  }
  async box(): Promise<BoundingBox | null> {
    return this.root.boundingBox();
  }
  async techBox(): Promise<BoundingBox | null> {
    return (await this.root.locator('.hf-cmp-tech').first()).boundingBox();
  }

  /** Legacy in-card NEW/MOD plaques were removed — this must be 0. */
  async inCardTagCount(): Promise<number> {
    return this.root.locator('.hf-cmp-diff-tag').count();
  }

  async width(): Promise<number> {
    return parsePx(await this.root.styleProp('width'));
  }
  async height(): Promise<number> {
    return parsePx(await this.root.styleProp('height'));
  }

  // ── Internals ────────────────────────────────────────────────────────────
  async internalCount(): Promise<number> {
    return this.root.locator('.hf-internal').count();
  }
  async internals(): Promise<InternalHarness[]> {
    const cards = await this.root.locator('.hf-internal').all();
    return cards.map((c) => new InternalHarness(c, this.env));
  }
  async internal(name: string): Promise<InternalHarness> {
    for (const it of await this.internals()) {
      if ((await it.name()) === name) return it;
    }
    throw new Error(`internal "${name}" not found in component`);
  }

  // ── Ports ──────────────────────────────────────────────────────────────
  /** The `.hf-port-label` element whose text contains `name`. */
  async portLabel(name: string): Promise<TestElement> {
    const ports = await this.root.locator('.hf-port').all();
    for (const p of ports) {
      const label = await p.locator('.hf-port-label').first();
      if ((await label.text()).includes(name)) return label;
    }
    throw new Error(`port label "${name}" not found`);
  }
  /** A removed port's label element (for the not-struck e2e assertion). */
  async removedPortLabel(): Promise<TestElement> {
    return this.root.locator('.hf-port.removed .hf-port-label').first();
  }
  /** Click a port row by its label text (opens the comment popover). */
  async clickPort(name: string): Promise<void> {
    const ports = await this.root.locator('.hf-port').all();
    for (const p of ports) {
      const label = await p.locator('.hf-port-label').first();
      if ((await label.text()).includes(name)) {
        await p.click();
        return;
      }
    }
    throw new Error(`port "${name}" not found`);
  }
  /** Hover the card so port labels (opacity:0 by default) reveal. */
  async hoverCard(): Promise<void> {
    await this.root.hover();
  }

  /** Double-click the component header to open the comment popover (tag 'cmp'). */
  async commentOnHeader(): Promise<void> {
    await (await this.root.locator('.hf-cmp-head').first()).dblclick();
  }
}
