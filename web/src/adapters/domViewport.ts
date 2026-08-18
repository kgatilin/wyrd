import type { UIGraph } from '../types';
import type { ViewportPort } from '../domain/ports';
import { PAN_MARGIN, ZOOM_MIN, ZOOM_MAX } from '../view/viewportConstants';

/** What the DOM viewport needs from the live React tree to compute scroll/zoom. */
export interface ViewportHandle {
  el: HTMLElement; // the .hf-canvas-wrap scroller
  getZoom: () => number;
  getCanvasDimensions: () => { width: number; height: number };
  /**
   * Park a scroll position to be applied once the canvas sizer has resized for a
   * newly dispatched zoom. Scrolling before that would clamp against the stale
   * (smaller) scroll extent.
   */
  scheduleScroll: (target: { left: number; top: number }) => void;
}

export interface DomViewport extends ViewportPort {
  /** Called by App on mount to attach the live canvas element; null on unmount. */
  bind(handle: ViewportHandle | null): void;
}

/** Slack (unscaled px) kept around a focused card so it doesn't touch the edges. */
const FOCUS_PAD = 48;

/** Laid-out box of a component, preferring expanded dimensions when present. */
function componentBox(laid: UIGraph, id: string) {
  const c = laid.components.find((cc) => cc.id === id);
  if (!c) return null;
  return {
    x: c.x ?? 0,
    y: c.y ?? 0,
    w: c.wx ?? c.w ?? 220,
    h: c.hx ?? c.h ?? 86,
  };
}

/**
 * ViewportPort backed by the real DOM. Created in createAppStore (so the viewport
 * effect can call it) and bound to the canvas by App. Smooth-scroll math mirrors
 * the old local scrollToComponent (content shifted by PAN_MARGIN, scaled by zoom).
 */
export function createDomViewport(): DomViewport {
  let handle: ViewportHandle | null = null;
  return {
    bind(h) {
      handle = h;
    },
    scrollToComponent(id: string, laid: UIGraph) {
      if (!handle) return;
      const box = componentBox(laid, id);
      if (!box) return;
      const zoom = handle.getZoom();
      handle.el.scrollTo({
        left: (PAN_MARGIN + box.x + box.w / 2) * zoom - handle.el.clientWidth / 2,
        top: (PAN_MARGIN + box.y + box.h / 2) * zoom - handle.el.clientHeight / 2,
        behavior: 'smooth',
      });
    },
    focusComponent(id: string, laid: UIGraph): number | null {
      if (!handle) return null;
      const box = componentBox(laid, id);
      if (!box) return null;
      const { clientWidth, clientHeight } = handle.el;
      if (clientWidth < 40 || clientHeight < 40) return null;

      // Zoom so the whole card (plus padding) fits, capped at 100% so a small
      // card doesn't blow up to fill the screen.
      const fit = Math.min(
        clientWidth / (box.w + FOCUS_PAD * 2),
        clientHeight / (box.h + FOCUS_PAD * 2),
        1
      );
      const zoom = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(fit * 100) / 100));
      const target = {
        left: (PAN_MARGIN + box.x + box.w / 2) * zoom - clientWidth / 2,
        top: (PAN_MARGIN + box.y + box.h / 2) * zoom - clientHeight / 2,
      };
      if (zoom === handle.getZoom()) {
        handle.el.scrollTo({ ...target, behavior: 'smooth' });
        return null;
      }
      handle.scheduleScroll(target);
      return zoom;
    },
    fitZoom(_laid: UIGraph): number | null {
      if (!handle) return null;
      const dims = handle.getCanvasDimensions();
      const fit = Math.min(handle.el.clientWidth / dims.width, handle.el.clientHeight / dims.height, 1);
      return Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    },
  };
}
