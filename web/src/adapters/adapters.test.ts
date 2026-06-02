import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { createElkLayout } from './elkLayout';
import { createHttpGraphSource } from './httpGraphSource';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'bc1', name: 'Core' }],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] }],
  edges: [],
  comments: [],
};

describe('createElkLayout', () => {
  it('lays out a graph, assigning geometry to components', async () => {
    const port = createElkLayout();
    const laid = await port.compute(graph, { expanded: new Set(), internalExpanded: new Set(), internalWide: new Set() });
    const a = laid.components.find((c) => c.id === 'a')!;
    expect(typeof a.x).toBe('number');
    expect(typeof a.y).toBe('number');
  });
});

describe('createHttpGraphSource', () => {
  it('loads a UIGraph (falls back to the built-in fixture when no network)', async () => {
    const port = createHttpGraphSource();
    const result = await port.load();
    expect(result.schema.startsWith('archai.uigraph/')).toBe(true);
    expect(Array.isArray(result.components)).toBe(true);
  });
});
