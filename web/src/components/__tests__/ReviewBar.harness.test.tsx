import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { UIGraph } from '../../types';

/**
 * A changed package with an exported and an unexported symbol, a file each,
 * plus an untouched symbol; and a package the branch did not touch.
 */
const graph: UIGraph = {
  schema: 'wyrd.uigraph/v0',
  pr: {
    title: 'Add the cache',
    branch: 'feature',
    agent: 'agent',
    summary: '',
    stats: { added: 2, removed: 0, changed: 0, comments: 0 },
  },
  reviewViews: [
    { id: 'all', title: 'All', defaultScope: 'all_public_api', componentIds: ['cache', 'store'], componentCount: 2 },
  ],
  reviewScopes: [
    { id: 'all_public_api', title: 'All Public API' },
    { id: 'everything', title: 'Everything' },
  ],
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'cache',
      name: 'cache',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        { id: 'cache.New', kind: 'func', name: 'New', exported: true, sourceFile: 'cache.go', diff: 'added', members: [] },
        { id: 'cache.evict', kind: 'func', name: 'evict', exported: false, sourceFile: 'evict.go', diff: 'added', members: [] },
        { id: 'cache.Stats', kind: 'func', name: 'Stats', exported: true, sourceFile: 'stats.go', members: [] },
      ],
      ports: [],
    },
    {
      id: 'store',
      name: 'store',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [{ id: 'store.Put', kind: 'func', name: 'Put', exported: true, sourceFile: 'store.go', members: [] }],
      ports: [],
    },
  ],
  edges: [],
  comments: [],
};

async function mount() {
  const env = await mountAppDom(graph);
  const app = await env.load(AppHarness);
  await app.waitForLoaded();
  const card = async () => (await app.diagram()).component('cache');
  if (!(await (await card()).isExpanded())) await (await card()).toggleExpand();
  return { env, app, card };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('review bar view toggle', () => {
  it('opens a branch on its diff and draws the whole repository on Full', async () => {
    const { env, app, card } = await mount();
    expect(await app.reviewMode()).toBe('Diff');
    expect(await (await app.diagram()).componentCount()).toBe(1);
    expect(await (await card()).fileLabels()).toEqual(['cache.go']);

    await app.setReviewMode('Full');
    await env.waitUntil(async () => (await (await app.diagram()).componentCount()) === 2, {
      message: 'Full never drew the untouched package',
    });
    expect(await app.reviewMode()).toBe('Full');
    // The changed package is drawn whole: its untouched file is there too.
    expect(await (await card()).fileLabels()).toEqual(['cache.go', 'stats.go']);
  });
});

describe('review bar surface toggle', () => {
  it('opens on the exported surface and adds the unexported one on Public + Internal', async () => {
    const { env, app, card } = await mount();
    expect(await app.surface()).toBe('Public');
    expect(await (await card()).fileLabels()).toEqual(['cache.go']);

    await app.setSurface('Public + Internal');
    await env.waitUntil(async () => (await (await card()).fileCount()) === 2, {
      message: 'Internal never drew the unexported symbol',
    });
    expect(await app.surface()).toBe('Public + Internal');

    await app.setSurface('Public');
    await env.waitUntil(async () => (await (await card()).fileCount()) === 1, {
      message: 'Public kept the unexported symbol',
    });
  });
});
