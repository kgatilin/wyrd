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
  // --- async contract ---

  it('returns a Promise', () => {
    const result = layout(minimalGraph());
    expect(result).toBeInstanceOf(Promise);
  });

  // --- numeric geometry for BCs and components ---

  it('returns a graph where every BC has numeric positive x, y, w, h', async () => {
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

    const result = await layout(input);

    for (const bc of result.boundedContexts) {
      expect(typeof bc.x, `bc ${bc.id} x`).toBe('number');
      expect(typeof bc.y, `bc ${bc.id} y`).toBe('number');
      expect(typeof bc.w, `bc ${bc.id} w`).toBe('number');
      expect(typeof bc.h, `bc ${bc.id} h`).toBe('number');
      expect(bc.x).toBeGreaterThanOrEqual(0);
      expect(bc.y).toBeGreaterThanOrEqual(0);
      expect(bc.w).toBeGreaterThan(0);
      expect(bc.h).toBeGreaterThan(0);
    }
  });

  it('returns a graph where every component has numeric positive x, y, w, h', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result = await layout(input);

    for (const c of result.components) {
      expect(typeof c.x, `cmp ${c.id} x`).toBe('number');
      expect(typeof c.y, `cmp ${c.id} y`).toBe('number');
      expect(typeof c.w, `cmp ${c.id} w`).toBe('number');
      expect(typeof c.h, `cmp ${c.id} h`).toBe('number');
      expect(c.x).toBeGreaterThanOrEqual(0);
      expect(c.y).toBeGreaterThanOrEqual(0);
      expect(c.w).toBeGreaterThan(0);
      expect(c.h).toBeGreaterThan(0);
    }
  });

  // --- component inside its BC box ---

  it('every component lies inside its bounded context box (absolute coords)', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c3', name: 'C3', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result = await layout(input);

    const bc = result.boundedContexts.find((b) => b.id === 'bc1')!;
    for (const c of result.components.filter((cmp) => cmp.bc === 'bc1')) {
      expect(c.x!, `${c.id} left edge inside bc`).toBeGreaterThanOrEqual(bc.x!);
      expect(c.y!, `${c.id} top edge inside bc`).toBeGreaterThanOrEqual(bc.y!);
      expect(c.x! + c.w!, `${c.id} right edge inside bc`).toBeLessThanOrEqual(bc.x! + bc.w!);
      expect(c.y! + c.h!, `${c.id} bottom edge inside bc`).toBeLessThanOrEqual(bc.y! + bc.h!);
    }
  });

  // --- no component–component overlap ---

  it('no two components overlap', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c3', name: 'C3', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c4', name: 'C4', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result = await layout(input);
    const comps = result.components;

    for (let i = 0; i < comps.length; i++) {
      for (let j = i + 1; j < comps.length; j++) {
        const a = comps[i];
        const b = comps[j];
        // Allow 1px tolerance for floating-point edge touching
        const overlaps =
          a.x! < b.x! + b.w! - 1 &&
          a.x! + a.w! - 1 > b.x! &&
          a.y! < b.y! + b.h! - 1 &&
          a.y! + a.h! - 1 > b.y!;
        expect(overlaps, `${a.id} and ${b.id} must not overlap`).toBe(false);
      }
    }
  });

  // --- no BC–BC overlap ---

  it('no two bounded contexts overlap', async () => {
    const input = minimalGraph({
      boundedContexts: [
        { id: 'bc1', name: 'BC One' },
        { id: 'bc2', name: 'BC Two' },
        { id: 'bc3', name: 'BC Three' },
      ],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc2', internals: [], ports: [] },
        { id: 'c3', name: 'C3', tech: 'Go', desc: '', bc: 'bc3', internals: [], ports: [] },
      ],
    });

    const result = await layout(input);
    const bcs = result.boundedContexts;

    for (let i = 0; i < bcs.length; i++) {
      for (let j = i + 1; j < bcs.length; j++) {
        const a = bcs[i];
        const b = bcs[j];
        const overlaps =
          a.x! < b.x! + b.w! - 1 &&
          a.x! + a.w! - 1 > b.x! &&
          a.y! < b.y! + b.h! - 1 &&
          a.y! + a.h! - 1 > b.y!;
        expect(overlaps, `${a.id} and ${b.id} must not overlap`).toBe(false);
      }
    }
  });

  // --- ports get numeric y (component-relative) ---

  it('ports have numeric y relative to their component', async () => {
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
            { id: 'p1', side: 'left', kind: 'in', name: 'in-port' },
            { id: 'p2', side: 'right', kind: 'out', name: 'out-port' },
          ],
        },
        {
          id: 'c2',
          name: 'C2',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [
            { id: 'p3', side: 'left', kind: 'in', name: 'in-port2' },
          ],
        },
      ],
    });

    const result = await layout(input);

    for (const c of result.components) {
      for (const p of c.ports) {
        expect(typeof p.y, `port ${p.id} y`).toBe('number');
        // y is component-relative: should be inside component height
        expect(p.y!).toBeGreaterThanOrEqual(0);
        expect(p.y!).toBeLessThanOrEqual(c.h!);
      }
    }
  });

  // --- edges get routed points ---

  it('every edge has points with >= 2 entries when ports exist', async () => {
    const input = minimalGraph({
      boundedContexts: [
        { id: 'bc1', name: 'BC One' },
        { id: 'bc2', name: 'BC Two' },
      ],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [{ id: 'p-out', side: 'right', kind: 'out', name: 'out' }],
        },
        {
          id: 'c2',
          name: 'C2',
          tech: 'Go',
          desc: '',
          bc: 'bc2',
          internals: [],
          ports: [{ id: 'p-in', side: 'left', kind: 'in', name: 'in' }],
        },
      ],
      edges: [
        { id: 'e1', from: 'c1', to: 'c2', fromPort: 'p-out', toPort: 'p-in', label: '' },
      ],
    });

    const result = await layout(input);

    for (const edge of result.edges) {
      expect(Array.isArray(edge.points), `edge ${edge.id} has points array`).toBe(true);
      expect(edge.points!.length, `edge ${edge.id} has >= 2 points`).toBeGreaterThanOrEqual(2);
      for (const pt of edge.points!) {
        expect(typeof pt.x).toBe('number');
        expect(typeof pt.y).toBe('number');
      }
    }
  });

  // --- determinism ---

  it('is deterministic: same input produces same output geometry', async () => {
    const input = minimalGraph({
      boundedContexts: [
        { id: 'bc1', name: 'BC One' },
        { id: 'bc2', name: 'BC Two' },
      ],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c2', name: 'C2', tech: 'Go', desc: '', bc: 'bc2', internals: [], ports: [] },
        { id: 'c3', name: 'C3', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const result1 = await layout(input);
    const result2 = await layout(input);

    expect(result1.boundedContexts).toEqual(result2.boundedContexts);
    expect(result1.components).toEqual(result2.components);
    expect(result1.edges).toEqual(result2.edges);
  });

  // --- resilient edge: dangling toPort falls back to component node ---

  it('resolves when an edge toPort does not exist (falls back to component node) and edge still gets points', async () => {
    // Reproduce the real-data defect: fromPort is declared, toPort is a
    // placeholder ":in:..." id that is NOT in any component's ports list.
    const input = minimalGraph({
      boundedContexts: [
        { id: 'bc1', name: 'BC One' },
        { id: 'bc2', name: 'BC Two' },
      ],
      components: [
        {
          id: 'internal/adapter/uigraph',
          name: 'uigraph',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [
            { id: 'internal/adapter/uigraph:out:internal/diff', side: 'right', kind: 'out', name: 'out' },
          ],
        },
        {
          id: 'internal/diff',
          name: 'diff',
          tech: 'Go',
          desc: '',
          bc: 'bc2',
          internals: [],
          ports: [
            // Note: the ":in:..." port is intentionally NOT declared here
            { id: 'internal/diff:out:something', side: 'right', kind: 'out', name: 'out' },
          ],
        },
      ],
      edges: [
        {
          id: 'e-dangling-toport',
          from: 'internal/adapter/uigraph',
          to: 'internal/diff',
          fromPort: 'internal/adapter/uigraph:out:internal/diff', // valid, declared
          toPort: 'internal/diff:in:internal/adapter/uigraph',   // INVALID — never declared
          label: '',
        },
      ],
    });

    // Must resolve (not throw/reject)
    const result = await layout(input);

    // Edge must still exist in output
    const edge = result.edges.find((e) => e.id === 'e-dangling-toport');
    expect(edge).toBeDefined();

    // Edge must have routed points (fell back to target component node)
    expect(Array.isArray(edge!.points), 'edge has points array').toBe(true);
    expect(edge!.points!.length, 'edge has >= 2 points').toBeGreaterThanOrEqual(2);
  });

  // --- input not mutated ---

  it('does not mutate the input graph', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC' }],
      components: [
        { id: 'c1', name: 'C1', tech: 'Go', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
    });

    const inputSnap = JSON.stringify(input);
    await layout(input);
    expect(JSON.stringify(input)).toBe(inputSnap);
  });

  // --- expanded options affect component size ---

  it('uses expanded dimensions when component id is in opts.expanded', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC' }],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [],
          wx: 300,
          hx: 250,
        },
      ],
    });

    const collapsed = await layout(input);
    const expanded = await layout(input, {
      expanded: new Set(['c1']),
      internalExpanded: new Set(),
    });

    const collapsedCmp = collapsed.components[0];
    const expandedCmp = expanded.components[0];

    // Expanded component should be larger
    expect(expandedCmp.w!).toBeGreaterThanOrEqual(collapsedCmp.w!);
    expect(expandedCmp.h!).toBeGreaterThanOrEqual(collapsedCmp.h!);
  });

  // --- empty graph does not throw ---

  it('handles empty graph without throwing', async () => {
    const result = await layout(minimalGraph());
    expect(result.boundedContexts).toEqual([]);
    expect(result.components).toEqual([]);
    expect(result.edges).toEqual([]);
  });
});
