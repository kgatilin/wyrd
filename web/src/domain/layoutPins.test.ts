import { describe, expect, it } from 'vitest';
import type { UIGraph } from '../types';
import {
  applyLayoutPins,
  buildLayoutRepoKeyPrefix,
  buildLayoutScopeKey,
  clearLayoutPinsForRepo,
  loadLayoutPins,
  saveLayoutPins,
} from './layoutPins';

function graph(overrides?: Partial<UIGraph>): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    repo: { root: '/repo', activeWorktree: 'feature' },
    defaultReviewView: 'top_level',
    defaultReviewScope: 'top_level_public_api',
    defaultGrouping: 'directory',
    boundedContexts: [{ id: 'bc1', name: 'Core', x: 10, y: 10, w: 300, h: 200 }],
    components: [
      { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', x: 50, y: 60, w: 100, h: 80, internals: [], ports: [] },
      { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', x: 220, y: 60, w: 100, h: 80, internals: [], ports: [] },
    ],
    edges: [{ id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: '', points: [{ x: 150, y: 100 }, { x: 220, y: 100 }] }],
    comments: [],
    ...overrides,
  };
}

describe('layout pin scope keys', () => {
  it('does not include active worktree so layouts persist across branches', () => {
    const a = buildLayoutScopeKey(graph(), 'top_level', 'all_public_api', 'directory');
    const b = buildLayoutScopeKey(
      graph({ repo: { root: '/repo', activeWorktree: 'other' } }),
      'top_level',
      'all_public_api',
      'directory'
    );
    expect(a).toBe(b);
  });

  it('builds a repo prefix that matches every review layout key for that repo only', () => {
    const prefix = buildLayoutRepoKeyPrefix(graph());
    expect(buildLayoutScopeKey(graph(), 'top_level', 'all_public_api', 'directory').startsWith(prefix)).toBe(true);
    expect(buildLayoutScopeKey(graph({ repo: { root: '/other' } }), 'top_level', 'all_public_api', 'directory').startsWith(prefix)).toBe(false);
  });
});

describe('applyLayoutPins', () => {
  it('moves pinned components, expands their group, and reconnects edge endpoints', () => {
    const selected = applyLayoutPins(graph(), { a: { x: 400, y: 300 } });

    expect(selected.components.find((c) => c.id === 'a')).toMatchObject({ x: 400, y: 300 });
    expect(selected.boundedContexts[0]).toMatchObject({ x: 10, y: 10 });
    expect(selected.boundedContexts[0].w).toBeGreaterThanOrEqual(520);
    expect(selected.boundedContexts[0].h).toBeGreaterThanOrEqual(400);
    expect(selected.edges[0].points?.[0]).toEqual({ x: 500, y: 340 });
    expect(selected.edges[0].points?.[1]).toEqual({ x: 220, y: 100 });
  });

  it('moves unpinned components around pinned components and reconnects their edge endpoints', () => {
    const selected = applyLayoutPins(graph(), { a: { x: 220, y: 60 } });

    const pinned = selected.components.find((c) => c.id === 'a');
    const unpinned = selected.components.find((c) => c.id === 'b');

    expect(pinned).toMatchObject({ x: 220, y: 60 });
    expect(unpinned).toMatchObject({ x: 220, y: 164 });
    expect(selected.edges[0].points?.[0]).toEqual({ x: 320, y: 100 });
    expect(selected.edges[0].points?.[1]).toEqual({ x: 220, y: 204 });
    expect(selected.boundedContexts[0].h).toBeGreaterThanOrEqual(254);
  });
});

describe('layout pin storage', () => {
  it('round-trips normalized pins and ignores invalid data', () => {
    const storage = window.localStorage;
    storage.clear();
    saveLayoutPins('pins:test', { a: { x: 10.4, y: 20.6 } }, storage);
    expect(loadLayoutPins('pins:test', storage)).toEqual({ a: { x: 10, y: 21 } });

    storage.setItem('pins:bad', '{"a":{"x":"nope","y":1},"b":{"x":2,"y":3}}');
    expect(loadLayoutPins('pins:bad', storage)).toEqual({ b: { x: 2, y: 3 } });

    saveLayoutPins('pins:test', {}, storage);
    expect(storage.getItem('pins:test')).toBeNull();
  });

  it('clears every saved layout for a repo without touching other repos', () => {
    const storage = window.localStorage;
    storage.clear();
    const repoKeyA = buildLayoutScopeKey(graph(), 'top_level', 'all_public_api', 'directory');
    const repoKeyB = buildLayoutScopeKey(graph(), 'runtime', 'everything', 'layer');
    const otherKey = buildLayoutScopeKey(graph({ repo: { root: '/other' } }), 'top_level', 'all_public_api', 'directory');
    storage.setItem(repoKeyA, '{"a":{"x":1,"y":2}}');
    storage.setItem(repoKeyB, '{"b":{"x":3,"y":4}}');
    storage.setItem(otherKey, '{"c":{"x":5,"y":6}}');

    clearLayoutPinsForRepo(graph(), storage);

    expect(storage.getItem(repoKeyA)).toBeNull();
    expect(storage.getItem(repoKeyB)).toBeNull();
    expect(storage.getItem(otherKey)).not.toBeNull();
  });
});
