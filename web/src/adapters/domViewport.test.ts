import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { createDomViewport, type ViewportHandle } from './domViewport';
import { PAN_MARGIN } from '../view/viewportConstants';

const laid: UIGraph = {
  schema: 'wyrd.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 100, y: 200, w: 220, h: 86 }],
  edges: [],
  comments: [],
};

// Same component, expanded: the laid-out card is far bigger than its collapsed box.
const laidExpanded: UIGraph = {
  ...laid,
  components: [{ ...laid.components[0], wx: 1600, hx: 1200 }],
};

function fakeEl() {
  const calls: any[] = [];
  const el = {
    clientWidth: 800,
    clientHeight: 600,
    scrollTo: (opts: any) => calls.push(opts),
  } as unknown as HTMLElement;
  return { el, calls };
}

function handle(el: HTMLElement, zoom = 1): ViewportHandle & { scheduled: { left: number; top: number }[] } {
  const scheduled: { left: number; top: number }[] = [];
  return {
    el,
    getZoom: () => zoom,
    getCanvasDimensions: () => ({ width: 1000, height: 800 }),
    scheduleScroll: (target) => scheduled.push(target),
    scheduled,
  };
}

describe('createDomViewport', () => {
  it('does nothing when unbound', () => {
    const vp = createDomViewport();
    expect(() => vp.scrollToComponent('a', laid)).not.toThrow();
    expect(vp.focusComponent('a', laid)).toBeNull();
    expect(vp.fitZoom(laid)).toBeNull();
  });

  it('scrollToComponent centers the laid component (accounting for PAN_MARGIN + zoom)', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind(handle(el));
    vp.scrollToComponent('a', laid);
    expect(calls).toHaveLength(1);
    expect(calls[0].left).toBe((PAN_MARGIN + 100 + 110) * 1 - 400);
    expect(calls[0].top).toBe((PAN_MARGIN + 200 + 43) * 1 - 300);
    expect(calls[0].behavior).toBe('smooth');
  });

  it('scrollToComponent centers on the expanded card, not its collapsed box', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind(handle(el));
    vp.scrollToComponent('a', laidExpanded);
    expect(calls[0].left).toBe((PAN_MARGIN + 100 + 800) * 1 - 400);
    expect(calls[0].top).toBe((PAN_MARGIN + 200 + 600) * 1 - 300);
  });

  it('scrollToComponent is a no-op for an unknown id', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind(handle(el));
    vp.scrollToComponent('nope', laid);
    expect(calls).toHaveLength(0);
  });

  it('focusComponent keeps the zoom and scrolls when the card already fits', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind(handle(el));
    expect(vp.focusComponent('a', laid)).toBeNull();
    expect(calls).toHaveLength(1);
    expect(calls[0].left).toBe((PAN_MARGIN + 100 + 110) * 1 - 400);
    expect(calls[0].behavior).toBe('smooth');
  });

  it('focusComponent zooms out until the whole expanded card fits, and defers the scroll', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    const h = handle(el);
    vp.bind(h);
    // 600 / (1200 + 96) is the tighter constraint -> 0.46
    const zoom = vp.focusComponent('a', laidExpanded);
    expect(zoom).toBe(0.46);
    expect(calls).toHaveLength(0);
    expect(h.scheduled).toEqual([
      {
        left: (PAN_MARGIN + 100 + 800) * 0.46 - 400,
        top: (PAN_MARGIN + 200 + 600) * 0.46 - 300,
      },
    ]);
  });

  it('focusComponent is a no-op for an unknown id', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    const h = handle(el);
    vp.bind(h);
    expect(vp.focusComponent('nope', laid)).toBeNull();
    expect(calls).toHaveLength(0);
    expect(h.scheduled).toHaveLength(0);
  });

  it('fitZoom returns a clamped fit ratio from the bound element + canvas dimensions', () => {
    const vp = createDomViewport();
    const { el } = fakeEl();
    vp.bind({ ...handle(el), getCanvasDimensions: () => ({ width: 4000, height: 4000 }) });
    expect(vp.fitZoom(laid)).toBe(0.4);
  });
});
