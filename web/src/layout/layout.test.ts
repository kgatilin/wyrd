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

  // --- internals layout (Problem 1) ---

  it('expanded component with >=2 internals: each internal has numeric x/y/w/h and internals do not overlap', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [
            { id: 'int1', kind: 'class', name: 'Foo', members: [] },
            { id: 'int2', kind: 'iface', name: 'Bar', members: [] },
            { id: 'int3', kind: 'class', name: 'Baz', members: [{ id: 'm1', kind: 'method', name: 'DoIt' }] },
          ],
          ports: [],
        },
      ],
    });

    const result = await layout(input, {
      expanded: new Set(['c1']),
      internalExpanded: new Set(['int3']), // int3 is expanded, has members
    });

    const cmp = result.components.find((c) => c.id === 'c1')!;

    // Each internal must have numeric x, y, w, h
    for (const internal of cmp.internals) {
      expect(typeof internal.x, `internal ${internal.id} x`).toBe('number');
      expect(typeof internal.y, `internal ${internal.id} y`).toBe('number');
      expect(typeof internal.w, `internal ${internal.id} w`).toBe('number');
      expect(typeof internal.h, `internal ${internal.id} h`).toBe('number');
      expect(internal.x!).toBeGreaterThanOrEqual(0);
      expect(internal.y!).toBeGreaterThanOrEqual(0);
      expect(internal.w!).toBeGreaterThan(0);
      expect(internal.h!).toBeGreaterThan(0);
    }

    // Internals must not overlap
    const internals = cmp.internals;
    for (let i = 0; i < internals.length; i++) {
      for (let j = i + 1; j < internals.length; j++) {
        const a = internals[i];
        const b = internals[j];
        const overlaps =
          a.x! < b.x! + b.w! - 1 &&
          a.x! + a.w! - 1 > b.x! &&
          a.y! < b.y! + b.h! - 1 &&
          a.y! + a.h! - 1 > b.y!;
        expect(overlaps, `${a.id} and ${b.id} must not overlap`).toBe(false);
      }
    }

    // Each internal must lie within the component's content box
    // Content box = component minus header (36px) minus padding (8px on each side)
    // Internals are positioned relative to canvas (origin at canvas content area)
    const canvasHeight = cmp.h! - 36 - 16; // 36px header, 8px top + 8px bottom padding
    const canvasWidth = cmp.w! - 16; // 8px left + 8px right padding
    for (const internal of cmp.internals) {
      expect(internal.x!, `${internal.id} left inside canvas`).toBeGreaterThanOrEqual(0);
      expect(internal.y!, `${internal.id} top inside canvas`).toBeGreaterThanOrEqual(0);
      expect(
        internal.x! + internal.w!,
        `${internal.id} right inside canvas (x=${internal.x} w=${internal.w} canvasW=${canvasWidth})`
      ).toBeLessThanOrEqual(canvasWidth + 1); // 1px tolerance
      expect(
        internal.y! + internal.h!,
        `${internal.id} bottom inside canvas (y=${internal.y} h=${internal.h} canvasH=${canvasHeight})`
      ).toBeLessThanOrEqual(canvasHeight + 1); // 1px tolerance
    }
  });

  it('expanded component size is derived from internal layout, not heuristic', async () => {
    // Component with 4 internals that would require significant space
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [
            { id: 'int1', kind: 'class', name: 'Foo', members: [{ id: 'm1', kind: 'method', name: 'A' }] },
            { id: 'int2', kind: 'iface', name: 'Bar', members: [{ id: 'm2', kind: 'method', name: 'B' }] },
            { id: 'int3', kind: 'class', name: 'Baz', members: [{ id: 'm3', kind: 'method', name: 'C' }] },
            { id: 'int4', kind: 'class', name: 'Qux', members: [{ id: 'm4', kind: 'method', name: 'D' }] },
          ],
          ports: [],
          hx: 100, // Tiny heuristic that would be too small
        },
      ],
    });

    const result = await layout(input, {
      expanded: new Set(['c1']),
      internalExpanded: new Set(['int1', 'int2', 'int3', 'int4']), // All expanded
    });

    const cmp = result.components.find((c) => c.id === 'c1')!;

    // Component must be large enough to fit all internals
    // Find bounding box of internals
    const internals = cmp.internals;
    let maxRight = 0;
    let maxBottom = 0;
    for (const internal of internals) {
      maxRight = Math.max(maxRight, (internal.x ?? 0) + (internal.w ?? 0));
      maxBottom = Math.max(maxBottom, (internal.y ?? 0) + (internal.h ?? 0));
    }

    // Component size should accommodate internals + header + padding
    expect(cmp.h!, 'height fits internals').toBeGreaterThanOrEqual(36 + 8 + maxBottom + 8);
    expect(cmp.w!, 'width fits internals').toBeGreaterThanOrEqual(8 + maxRight + 8);
  });

  // --- synthesized inbound ports (Problem 2a) ---

  it('synthesizes inbound port when toPort is not declared, routes edge to it', async () => {
    // Real-world scenario: fromPort is declared, toPort is a placeholder
    const input = minimalGraph({
      boundedContexts: [
        { id: 'bc1', name: 'BC One' },
        { id: 'bc2', name: 'BC Two' },
      ],
      components: [
        {
          id: 'source-cmp',
          name: 'Source',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [
            { id: 'source-cmp:out:target-cmp', side: 'right', kind: 'out', name: 'out' },
          ],
        },
        {
          id: 'target-cmp',
          name: 'Target',
          tech: 'Go',
          desc: '',
          bc: 'bc2',
          internals: [],
          ports: [
            // NO inbound port declared - toPort below is a placeholder
            { id: 'target-cmp:out:other', side: 'right', kind: 'out', name: 'other' },
          ],
        },
      ],
      edges: [
        {
          id: 'e1',
          from: 'source-cmp',
          to: 'target-cmp',
          fromPort: 'source-cmp:out:target-cmp', // valid, declared
          toPort: 'target-cmp:in:source-cmp',   // INVALID - not declared
          label: 'uses',
        },
      ],
    });

    const result = await layout(input);

    // Target component should now have a synthesized inbound port
    const target = result.components.find((c) => c.id === 'target-cmp')!;
    const synthPort = target.ports.find(
      (p) => p.side === 'left' && p.kind === 'in' && p.id !== 'target-cmp:out:other'
    );
    expect(synthPort, 'synthesized inbound port exists').toBeDefined();
    expect(synthPort!.side).toBe('left');
    expect(synthPort!.kind).toBe('in');
    expect(typeof synthPort!.y).toBe('number');

    // Edge should have routed points (port-to-port routing worked)
    const edge = result.edges.find((e) => e.id === 'e1')!;
    expect(Array.isArray(edge.points), 'edge has points array').toBe(true);
    expect(edge.points!.length, 'edge has >= 2 points').toBeGreaterThanOrEqual(2);
  });

  it('synthesizes multiple inbound ports for multiple inbound edges', async () => {
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC' }],
      components: [
        {
          id: 'a', name: 'A', tech: 'Go', desc: '', bc: 'bc1', internals: [],
          ports: [{ id: 'a:out:domain', side: 'right', kind: 'out', name: 'out' }],
        },
        {
          id: 'b', name: 'B', tech: 'Go', desc: '', bc: 'bc1', internals: [],
          ports: [{ id: 'b:out:domain', side: 'right', kind: 'out', name: 'out' }],
        },
        {
          id: 'domain', name: 'Domain', tech: 'Go', desc: '', bc: 'bc1', internals: [],
          ports: [], // No declared ports - heavily depended on
        },
      ],
      edges: [
        { id: 'e1', from: 'a', to: 'domain', fromPort: 'a:out:domain', toPort: 'domain:in:a', label: '' },
        { id: 'e2', from: 'b', to: 'domain', fromPort: 'b:out:domain', toPort: 'domain:in:b', label: '' },
      ],
    });

    const result = await layout(input);

    const domain = result.components.find((c) => c.id === 'domain')!;
    const inboundPorts = domain.ports.filter((p) => p.side === 'left' && p.kind === 'in');

    // Should have 2 synthesized inbound ports (one per inbound edge)
    expect(inboundPorts.length).toBe(2);

    // Both edges should have routed points
    for (const edge of result.edges) {
      expect(Array.isArray(edge.points), `edge ${edge.id} has points`).toBe(true);
      expect(edge.points!.length).toBeGreaterThanOrEqual(2);
    }
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

  // --- BUG 1: edge endpoints must align with port positions ---

  it('edge first point aligns with source out-port absolute position (same BC)', async () => {
    // Setup: One BC with two components, an edge from c1's out-port to c2's in-port.
    // Both components in the same BC - this is where LCA offset was missing.
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        {
          id: 'c1',
          name: 'Source',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [{ id: 'c1:out:c2', side: 'right', kind: 'out', name: 'out' }],
        },
        {
          id: 'c2',
          name: 'Target',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [],
          ports: [{ id: 'c2:in:c1', side: 'left', kind: 'in', name: 'in' }],
        },
      ],
      edges: [
        {
          id: 'e1',
          from: 'c1',
          to: 'c2',
          fromPort: 'c1:out:c2',
          toPort: 'c2:in:c1',
          label: 'uses',
        },
      ],
    });

    const result = await layout(input);

    const c1 = result.components.find((c) => c.id === 'c1')!;
    const c2 = result.components.find((c) => c.id === 'c2')!;
    const edge = result.edges.find((e) => e.id === 'e1')!;

    // Source out-port is on the right side of c1
    const srcPort = c1.ports.find((p) => p.id === 'c1:out:c2')!;
    const srcPortAbsX = c1.x! + c1.w!; // right edge of component
    const srcPortAbsY = c1.y! + srcPort.y!;

    // Target in-port is on the left side of c2
    const tgtPort = c2.ports.find((p) => p.id === 'c2:in:c1')!;
    const tgtPortAbsX = c2.x!; // left edge of component
    const tgtPortAbsY = c2.y! + tgtPort.y!;

    expect(edge.points, 'edge should have points').toBeDefined();
    expect(edge.points!.length).toBeGreaterThanOrEqual(2);

    const firstPt = edge.points![0];
    const lastPt = edge.points![edge.points!.length - 1];

    // Edge first point should be at/near the source out-port (tolerance 3px)
    expect(
      Math.abs(firstPt.x - srcPortAbsX),
      `edge start X (${firstPt.x}) should be near source port X (${srcPortAbsX})`
    ).toBeLessThanOrEqual(3);
    expect(
      Math.abs(firstPt.y - srcPortAbsY),
      `edge start Y (${firstPt.y}) should be near source port Y (${srcPortAbsY})`
    ).toBeLessThanOrEqual(3);

    // Edge last point should be at/near the target in-port (tolerance 3px)
    expect(
      Math.abs(lastPt.x - tgtPortAbsX),
      `edge end X (${lastPt.x}) should be near target port X (${tgtPortAbsX})`
    ).toBeLessThanOrEqual(3);
    expect(
      Math.abs(lastPt.y - tgtPortAbsY),
      `edge end Y (${lastPt.y}) should be near target port Y (${tgtPortAbsY})`
    ).toBeLessThanOrEqual(3);
  });

  // --- BUG 2: internals should use multi-column layout for n>=4 ---

  it('expanded component with >=4 internals uses multiple columns', async () => {
    // Component with 4 internals should NOT stack in a single column
    const input = minimalGraph({
      boundedContexts: [{ id: 'bc1', name: 'BC One' }],
      components: [
        {
          id: 'c1',
          name: 'C1',
          tech: 'Go',
          desc: '',
          bc: 'bc1',
          internals: [
            { id: 'int1', kind: 'class', name: 'Foo', members: [] },
            { id: 'int2', kind: 'class', name: 'Bar', members: [] },
            { id: 'int3', kind: 'class', name: 'Baz', members: [] },
            { id: 'int4', kind: 'class', name: 'Qux', members: [] },
          ],
          ports: [],
        },
      ],
    });

    const result = await layout(input, {
      expanded: new Set(['c1']),
      internalExpanded: new Set(),
    });

    const cmp = result.components.find((c) => c.id === 'c1')!;
    const internals = cmp.internals;

    // Count unique X positions to determine number of columns used
    const uniqueXPositions = new Set(internals.map((int) => int.x));

    // With 4 internals, should use at least 2 columns (not a single column)
    expect(
      uniqueXPositions.size,
      `internals should use multiple columns (found ${uniqueXPositions.size} unique x positions)`
    ).toBeGreaterThan(1);
  });
});
