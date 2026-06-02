import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { relatedIds, deriveChanges, addInternalsOfExpanded, initialExpanded } from './derive';

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
