import { describe, it, expect } from 'vitest';
import { layout } from './layout';
import type { UIGraph } from '../types';

// Helper to create a minimal graph
function minimalGraph(overrides?: Partial<UIGraph>): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    boundedContexts: [],
    components: [],
    edges: [],
    comments: [],
    ...overrides,
  };
}

describe('layout', () => {
  it('returns a graph where every component has numeric x, y, w, h', () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result = layout(input);

    for (const c of result.components) {
      expect(typeof c.x).toBe('number');
      expect(typeof c.y).toBe('number');
      expect(typeof c.w).toBe('number');
      expect(typeof c.h).toBe('number');
      expect(c.x).toBeGreaterThanOrEqual(0);
      expect(c.y).toBeGreaterThanOrEqual(0);
      expect(c.w).toBeGreaterThan(0);
      expect(c.h).toBeGreaterThan(0);
    }
  });

  it('assigns components within their bounded context box', () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result = layout(input);

    const bc = result.boundedContexts.find((b) => b.id === 'bc1')!;
    expect(typeof bc.x).toBe('number');
    expect(typeof bc.y).toBe('number');
    expect(typeof bc.w).toBe('number');
    expect(typeof bc.h).toBe('number');

    for (const c of result.components.filter((cmp) => cmp.bc === 'bc1')) {
      // Component should be inside BC box
      expect(c.x).toBeGreaterThanOrEqual(bc.x!);
      expect(c.y).toBeGreaterThanOrEqual(bc.y!);
      expect(c.x! + c.w!).toBeLessThanOrEqual(bc.x! + bc.w!);
      expect(c.y! + c.h!).toBeLessThanOrEqual(bc.y! + bc.h!);
    }
  });

  it('prevents overlap between components in the same BC', () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c3', name: 'C3', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result = layout(input);
    const comps = result.components;

    // Check no two components overlap
    for (let i = 0; i < comps.length; i++) {
      for (let j = i + 1; j < comps.length; j++) {
        const a = comps[i];
        const b = comps[j];
        const overlaps =
          a.x! < b.x! + b.w! &&
          a.x! + a.w! > b.x! &&
          a.y! < b.y! + b.h! &&
          a.y! + a.h! > b.y!;
        expect(overlaps).toBe(false);
      }
    }
  });

  it('preserves pre-set geometry (fixture data)', () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC', x: 100, y: 100, w: 500, h: 400 }],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [],
          x: 150,
          y: 150,
          w: 200,
          h: 80,
        },
      ],
    });

    const result = layout(input);

    const c = result.components[0];
    expect(c.x).toBe(150);
    expect(c.y).toBe(150);
    expect(c.w).toBe(200);
    expect(c.h).toBe(80);

    const bc = result.boundedContexts[0];
    expect(bc.x).toBe(100);
    expect(bc.y).toBe(100);
    expect(bc.w).toBe(500);
    expect(bc.h).toBe(400);
  });

  it('assigns geometry to ports (y-position)', () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [
            { id: 'p1', side: 'left', kind: 'in', name: 'port1' },
            { id: 'p2', side: 'right', kind: 'out', name: 'port2' },
          ],
        },
      ],
    });

    const result = layout(input);
    const ports = result.components[0].ports;

    for (const p of ports) {
      expect(typeof p.y).toBe('number');
      expect(p.y).toBeGreaterThan(0);
    }
  });

  it('handles multiple bounded contexts', () => {
    const input = minimalGraph({
      boundedContexts: [
        { id: 'bc1', name: 'BC One' },
        { id: 'bc2', name: 'BC Two' },
      ],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc2', internals: [], ports: [] },
      ],
    });

    const result = layout(input);

    const bc1 = result.boundedContexts.find((b) => b.id === 'bc1')!;
    const bc2 = result.boundedContexts.find((b) => b.id === 'bc2')!;

    // Both BCs should have geometry
    expect(bc1.w).toBeGreaterThan(0);
    expect(bc2.w).toBeGreaterThan(0);

    // BCs should not overlap
    const overlaps =
      bc1.x! < bc2.x! + bc2.w! &&
      bc1.x! + bc1.w! > bc2.x! &&
      bc1.y! < bc2.y! + bc2.h! &&
      bc1.y! + bc1.h! > bc2.y!;
    expect(overlaps).toBe(false);
  });
});
