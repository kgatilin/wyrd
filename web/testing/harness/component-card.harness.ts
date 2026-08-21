import {
  BoundingBox,
  ComponentHarness,
  DiffState,
  diffStateFromClasses,
  parsePx,
  TestElement,
} from './test-element';
import { BlockHarness } from './block.harness';
import { FileContainerHarness } from './file-container.harness';

/** A component card on the canvas (`.hf-cmp`). */
export class ComponentCardHarness extends ComponentHarness {
  /** The package basename — excludes the muted directory-path prefix. */
  async name(): Promise<string> {
    return (await this.root.locator('.hf-cmp-name .hf-cmp-base').first()).text();
  }
  /** The muted directory prefix before the basename ('' when top-level). */
  async pathPrefix(): Promise<string> {
    const els = await this.root.locator('.hf-cmp-name .hf-cmp-path').all();
    return els.length > 0 ? els[0].text() : '';
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
  /** Select the card. Clicks the header: focusing a package expands it, and
   *  the centre of an expanded card is a file container that answers for
   *  itself, so a click there never reaches the card. */
  async focus(): Promise<void> {
    await (await this.root.locator('.hf-cmp-head').first()).click();
  }
  /** Expanded ⇒ the card canvas with its file containers is present. */
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

  // ── Source-file containers and their class shapes ────────────────────────
  async fileCount(): Promise<number> {
    return this.root.locator('.hf-file').count();
  }
  async files(): Promise<FileContainerHarness[]> {
    const files = await this.root.locator('.hf-file').all();
    return files.map((f) => new FileContainerHarness(f, this.env));
  }
  async file(label: string): Promise<FileContainerHarness> {
    for (const f of await this.files()) {
      if ((await f.label()) === label) return f;
    }
    throw new Error(`file container "${label}" not found in component`);
  }
  async fileLabels(): Promise<string[]> {
    const out: string[] = [];
    for (const f of await this.files()) out.push(await f.label());
    return out;
  }
  async blockCount(): Promise<number> {
    return this.root.locator('.hf-block').count();
  }
  async blocks(): Promise<BlockHarness[]> {
    const blocks = await this.root.locator('.hf-block').all();
    return blocks.map((b) => new BlockHarness(b, this.env));
  }
  async block(name: string): Promise<BlockHarness> {
    for (const b of await this.blocks()) {
      if ((await b.name()) === name) return b;
    }
    throw new Error(`block "${name}" not found in component`);
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
