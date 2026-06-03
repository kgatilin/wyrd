import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { relatedIds, deriveChanges, addInternalsOfExpanded, initialExpanded, seedMarkers } from './derive';

function graph(overrides?: Partial<UIGraph>): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'bc1', name: 'Core' }],
    components: [
      { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] },
      { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
    ],
    edges: [{ id: 'e1', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' }],
    comments: [],
    ...overrides,
  };
}

describe('relatedIds', () => {
  it('returns null when nothing is focused', () => {
    expect(relatedIds(graph(), null)).toBeNull();
  });
  it('returns the focused node plus its edge neighbours', () => {
    expect(relatedIds(graph(), 'a')).toEqual(new Set(['a', 'b']));
  });
});

describe('deriveChanges', () => {
  it('walks the graph for diff-flagged elements', () => {
    const g = graph({
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
      ],
      edges: [],
    });
    const changes = deriveChanges(g);
    expect(changes).toHaveLength(1);
    expect(changes[0]).toMatchObject({ id: 'cmp-a', kind: 'added', name: 'A', cmp: 'a' });
  });
});

describe('addInternalsOfExpanded', () => {
  it('adds internals of expanded components (add-only, preserves prior)', () => {
    const result = addInternalsOfExpanded(graph(), new Set(['a']), new Set(['old']));
    expect(result).toEqual(new Set(['old', 'a.i']));
  });
});

describe('initialExpanded', () => {
  it('falls back to the first component when no "orders" exists', () => {
    expect(initialExpanded(graph())).toEqual(['a']);
  });
});

describe('seedMarkers', () => {
  it('positions a comment marker beside its host component using laid geometry', () => {
    const g = graph({
      comments: [{ id: 'cm1', target: { type: 'component', id: 'a' }, body: 'hi' }],
    });
    const laid = { ...g, components: g.components.map((c) => (c.id === 'a' ? { ...c, x: 100, y: 200, w: 220 } : c)) };
    const markers = seedMarkers(g, laid);
    expect(markers).toHaveLength(1);
    expect(markers[0]).toMatchObject({ id: 'seed-0', n: 1, target: { type: 'component', id: 'a' }, body: 'hi' });
    expect(markers[0].x).toBe(100 + 220 + 8);
    expect(markers[0].y).toBe(200 - 10);
  });

  it('falls back to a default offset when the target/host is not laid out', () => {
    const g = graph({ comments: [{ id: 'cm1', target: { type: 'component', id: 'ghost' }, body: 'x' }] });
    const markers = seedMarkers(g, null);
    expect(markers).toHaveLength(1);
    expect(markers[0].x).toBe(80);
    expect(markers[0].y).toBe(30);
  });
});
