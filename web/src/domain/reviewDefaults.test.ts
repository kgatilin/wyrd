import { describe, expect, it } from 'vitest';
import type { UIGraph } from '../types';
import {
  buildReviewDefaultsKey,
  loadReviewDefaults,
  saveReviewDefaults,
} from './reviewDefaults';

function graph(overrides?: Partial<UIGraph>): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    repo: { root: '/repo', activeWorktree: 'feature' },
    reviewScopes: [
      { id: 'top_level_public_api', title: 'Top-level Public API' },
      { id: 'all_public_api', title: 'All Public API' },
    ],
    reviewViews: [
      { id: 'framework', title: 'Framework', defaultScope: 'top_level_public_api', groupBy: 'review_view', componentIds: ['a'], componentCount: 1 },
      { id: 'runtime', title: 'Runtime', defaultScope: 'all_public_api', groupBy: 'directory', componentIds: ['a'], componentCount: 1 },
    ],
    reviewGroupings: [
      { id: 'review_view', title: 'Review View', groups: [] },
      { id: 'directory', title: 'Directory', groups: [] },
      { id: 'package_owner', title: 'Package Owner', groups: [] },
    ],
    boundedContexts: [],
    components: [],
    edges: [],
    comments: [],
    ...overrides,
  };
}

describe('review default scope keys', () => {
  it('does not include active worktree so selections persist across branches', () => {
    const a = buildReviewDefaultsKey(graph());
    const b = buildReviewDefaultsKey(graph({ repo: { root: '/repo', activeWorktree: 'other' } }));
    expect(a).toBe(b);
  });
});

describe('review default storage', () => {
  it('round-trips valid selections and drops ids unknown to the current graph', () => {
    const storage = window.localStorage;
    storage.clear();
    const key = 'review-defaults:test';

    saveReviewDefaults(
      key,
      {
        reviewViewId: 'runtime',
        scopeByView: { runtime: 'all_public_api', missing: 'all_public_api' },
        groupingByView: { runtime: 'package_owner', framework: 'missing' },
      },
      graph(),
      storage
    );

    expect(loadReviewDefaults(key, graph(), storage)).toEqual({
      reviewViewId: 'runtime',
      scopeByView: { runtime: 'all_public_api' },
      groupingByView: { runtime: 'package_owner' },
    });

    storage.setItem(key, '{not-json');
    expect(loadReviewDefaults(key, graph(), storage)).toEqual({});
  });
});
