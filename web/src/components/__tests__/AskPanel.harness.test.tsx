import { afterEach, describe, expect, it } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { UIGraph } from '../../types';

/** Three packages; a question will match symbols in two of them. */
const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'transport',
      name: 'transport',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        { id: 'transport.Serve', kind: 'func', name: 'Serve', exported: true, members: [] },
        { id: 'transport.parseHeader', kind: 'func', name: 'parseHeader', exported: false, members: [] },
      ],
      ports: [],
    },
    {
      id: 'store',
      name: 'store',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [{ id: 'store.Put', kind: 'func', name: 'Put', exported: true, members: [] }],
      ports: [],
    },
    {
      id: 'unrelated',
      name: 'unrelated',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [{ id: 'unrelated.Noise', kind: 'func', name: 'Noise', exported: true, members: [] }],
      ports: [],
    },
  ],
  edges: [],
  comments: [],
};

const answer = {
  results: [
    { node_id: 'transport.Serve', kind: 'func', package: 'transport', name: 'Serve', file: 'transport/serve.go', line: 12, doc: 'Serve accepts connections.', score: 0.031 },
    { node_id: 'store.Put', kind: 'func', package: 'store', name: 'Put', file: 'store/store.go', line: 4, doc: '', score: 0.03 },
  ],
  dense: true,
};

async function mount() {
  const env = await mountAppDom(graph, { search: () => answer });
  const app = await env.load(AppHarness);
  await app.waitForLoaded();
  return { env, app };
}

afterEach(cleanup);

describe('ask panel', () => {
  it('answers a question with ranked hits grouped by package', async () => {
    const { app } = await mount();
    const ask = app.ask();
    await ask.open();
    await ask.ask('where are connections accepted');
    await app.env.waitUntil(async () => (await ask.hits()).length > 0, { message: 'no hits rendered' });

    expect(await ask.hits()).toEqual(['Serve', 'Put']);
    expect(await ask.groups()).toEqual(['transport', 'store']);
    expect(await ask.meta()).toContain('semantic');
  });

  it('draws only the matched packages, with only the matched symbols', async () => {
    const { app } = await mount();
    const ask = app.ask();
    await ask.open();
    await ask.ask('where are connections accepted');
    const diagram = await app.diagram();
    await app.env.waitUntil(async () => (await diagram.componentCount()) === 2, {
      message: 'canvas never narrowed to the answer',
    });

    expect((await diagram.componentNames()).sort()).toEqual(['store', 'transport']);
    const card = await diagram.component('transport');
    const blocks = await card.blocks();
    const names = await Promise.all(blocks.map((block) => block.name()));
    expect(names).toEqual(['Serve']);
  });

  it('names the active question in the review bar and restores the review on clear', async () => {
    const { app } = await mount();
    const ask = app.ask();
    await ask.open();
    await ask.ask('where are connections accepted');
    await app.env.waitUntil(async () => (await ask.reviewBarQuery()) != null, { message: 'no ask chip' });
    expect(await ask.reviewBarQuery()).toContain('where are connections accepted');

    await ask.clear();
    const diagram = await app.diagram();
    await app.env.waitUntil(async () => (await diagram.componentCount()) === 3, {
      message: 'review never came back',
    });
    expect(await ask.reviewBarQuery()).toBeNull();
  });
});
