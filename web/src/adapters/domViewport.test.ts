import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { createDomViewport } from './domViewport';
import { PAN_MARGIN } from '../view/viewportConstants';

const laid: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 100, y: 200, w: 220, h: 86 }],
  edges: [],
  comments: [],
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

describe('createDomViewport', () => {
  it('does nothing when unbound', () => {
    const vp = createDomViewport();
    expect(() => vp.scrollToComponent('a', laid)).not.toThrow();
    expect(vp.fitZoom(laid)).toBeNull();
  });

  it('scrollToComponent centers the laid component (accounting for PAN_MARGIN + zoom)', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind({ el, getZoom: () => 1, getCanvasDimensions: () => ({ width: 1000, height: 800 }) });
    vp.scrollToComponent('a', laid);
    expect(calls).toHaveLength(1);
    expect(calls[0].left).toBe((PAN_MARGIN + 100 + 110) * 1 - 400);
    expect(calls[0].top).toBe((PAN_MARGIN + 200 + 43) * 1 - 300);
    expect(calls[0].behavior).toBe('smooth');
  });

  it('scrollToComponent is a no-op for an unknown id', () => {
    const vp = createDomViewport();
    const { el, calls } = fakeEl();
    vp.bind({ el, getZoom: () => 1, getCanvasDimensions: () => ({ width: 1000, height: 800 }) });
    vp.scrollToComponent('nope', laid);
    expect(calls).toHaveLength(0);
  });

  it('fitZoom returns a clamped fit ratio from the bound element + canvas dimensions', () => {
    const vp = createDomViewport();
    const { el } = fakeEl();
    vp.bind({ el, getZoom: () => 1, getCanvasDimensions: () => ({ width: 4000, height: 4000 }) });
    expect(vp.fitZoom(laid)).toBe(0.4);
  });
});
