import type { UIGraph } from '../types';
import type { ViewportPort } from '../domain/ports';
import { PAN_MARGIN, ZOOM_MIN } from '../view/viewportConstants';

/** What the DOM viewport needs from the live React tree to compute scroll/zoom. */
export interface ViewportHandle {
  el: HTMLElement; // the .hf-canvas-wrap scroller
  getZoom: () => number;
  getCanvasDimensions: () => { width: number; height: number };
}

export interface DomViewport extends ViewportPort {
  /** Called by App on mount to attach the live canvas element; null on unmount. */
  bind(handle: ViewportHandle | null): void;
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
      const c = laid.components.find((cc) => cc.id === id);
      if (!c) return;
      const zoom = handle.getZoom();
      const x = c.x ?? 0;
      const y = c.y ?? 0;
      const w = c.w ?? 220;
      const h = c.h ?? 86;
      handle.el.scrollTo({
        left: (PAN_MARGIN + x + w / 2) * zoom - handle.el.clientWidth / 2,
        top: (PAN_MARGIN + y + h / 2) * zoom - handle.el.clientHeight / 2,
        behavior: 'smooth',
      });
    },
    fitZoom(_laid: UIGraph): number | null {
      if (!handle) return null;
      const dims = handle.getCanvasDimensions();
      const fit = Math.min(handle.el.clientWidth / dims.width, handle.el.clientHeight / dims.height, 1);
      return Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    },
  };
}
