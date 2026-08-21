import { describe, expect, it } from 'vitest';
import type { EventModel } from '../domain/eventModel';
import { layoutEventModel, linkPath, nodeHeight, NODE_WIDTH } from './eventLayout';

const model: EventModel = {
  components: [
    { id: 'billing', has_state: true, outputs: [{ kind: 'ledger.entry.post' }] },
    { id: 'ledger', has_state: true, inputs: [{ kind: 'ledger.entry.post' }] },
    { id: 'orphan', has_state: false },
  ],
  flows: [{ from: 'billing', to: 'ledger', kind: 'ledger.entry.post', trigger: true, health: 'ok' }],
  kinds: [],
};

describe('layoutEventModel', () => {
  it('places every component, including one no flow touches', async () => {
    const laid = await layoutEventModel(model);

    expect(laid.nodes.map((n) => n.id).sort()).toEqual(['billing', 'ledger', 'orphan']);
    for (const node of laid.nodes) {
      expect(node.width).toBe(NODE_WIDTH);
      expect(Number.isFinite(node.x)).toBe(true);
      expect(Number.isFinite(node.y)).toBe(true);
    }
  });

  it('routes the flow left to right, and gives it a label point on the line', async () => {
    const laid = await layoutEventModel(model);

    expect(laid.links).toHaveLength(1);
    const [link] = laid.links;
    expect(link.link.from).toBe('billing');
    expect(link.points.length).toBeGreaterThanOrEqual(2);
    expect(link.points[0].x).toBeLessThan(link.points[link.points.length - 1].x);
    expect(Number.isFinite(link.labelX)).toBe(true);
    expect(Number.isFinite(link.labelY)).toBe(true);
  });

  it('drops a flow naming a component no document declares', async () => {
    // A system is described one application at a time, so the other end of a
    // call may simply not be in this repo. The edge goes rather than pointing
    // at a node that is not on the canvas.
    const laid = await layoutEventModel({
      ...model,
      flows: [...model.flows, { from: 'billing', to: 'elsewhere', kind: 'x.y', trigger: true }],
    });

    expect(laid.links).toHaveLength(1);
    expect(laid.nodes.map((n) => n.id)).not.toContain('elsewhere');
  });

  it('is empty for an empty model rather than asking ELK for nothing', async () => {
    const laid = await layoutEventModel({ components: [], flows: [], kinds: [] });
    expect(laid).toEqual({ nodes: [], links: [], width: 0, height: 0 });
  });
});

describe('nodeHeight', () => {
  it('grows with what a component declares, then stops', () => {
    const empty = nodeHeight({ id: 'a', has_state: false });
    const some = nodeHeight({ id: 'b', has_state: false, inputs: [{ kind: 'x' }, { kind: 'y' }] });
    const many = nodeHeight({
      id: 'c',
      has_state: false,
      inputs: Array.from({ length: 40 }, (_, i) => ({ kind: `k${i}` })),
    });

    expect(some).toBeGreaterThan(empty);
    expect(many).toBeGreaterThan(some);
    expect(many).toBeLessThan(empty + 40 * 15);
  });
});

describe('linkPath', () => {
  it('writes a polyline the browser can draw', () => {
    expect(linkPath([{ x: 0, y: 1 }, { x: 2, y: 3 }, { x: 4, y: 5 }])).toBe('M0 1 L2 3 L4 5');
  });
});
