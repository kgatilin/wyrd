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
      internals: [
        { id: 'store.Put', kind: 'func', name: 'Put', exported: true, members: [] },
        {
          id: 'store.Cache',
          kind: 'class',
          name: 'Cache',
          exported: true,
          members: [{ id: 'store.Cache.Get', kind: 'method', name: 'Get' }],
        },
      ],
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

// The daemon answers with the query's own hits and the region the diffusion
// grew around them. Both are drawn: parseHeader is not a text match, it is
// what the graph reached from Serve.
const answer = {
  hits: [
    { id: 'transport.Serve', kind: 'func', package: 'transport', name: 'Serve', file: 'transport/serve.go', line: 12, doc: 'Serve accepts connections.', seed: true, text_score: 0.31, score: 0.031 },
    { id: 'store.Put', kind: 'func', package: 'store', name: 'Put', file: 'store/store.go', line: 4, doc: '', seed: true, text_score: 0.3, score: 0.03 },
    { id: 'transport.parseHeader', kind: 'func', package: 'transport', name: 'parseHeader', file: 'transport/serve.go', line: 40, doc: '', score: 0.02 },
  ],
  dense: true,
};

async function mount(reply: { hits: unknown[]; dense: boolean } = answer) {
  const env = await mountAppDom(graph, { search: () => reply });
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

    expect(await ask.hits()).toEqual(['Serve', 'parseHeader', 'Put']);
    expect(await ask.groups()).toEqual(['transport', 'store']);
    expect(await ask.meta()).toContain('semantic');
  });

  it('marks what the graph added, so it is not read as a match', async () => {
    const { app } = await mount();
    const ask = app.ask();
    await ask.open();
    await ask.ask('where are connections accepted');
    await app.env.waitUntil(async () => (await ask.hits()).length > 0, { message: 'no hits rendered' });

    expect(await ask.relatedHits()).toEqual(['parseHeader']);
    expect(await ask.meta()).toContain('2 hits');
    expect(await ask.meta()).toContain('1 related');
  });

  it('draws the whole answer — the matched packages and the region around them', async () => {
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
    // The card carries the hit and the neighbour the diffusion reached, and
    // nothing else in the package.
    expect(names).toEqual(['Serve', 'parseHeader']);
  });

  it('draws a method hit on the card of the type that declares it', async () => {
    const { app } = await mount({
      hits: [
        {
          id: 'store.Cache.Get',
          kind: 'method',
          package: 'store',
          name: 'Cache.Get',
          file: 'store/cache.go',
          line: 20,
          doc: 'Get reads a cached entry.',
          seed: true,
          text_score: 0.4,
          score: 0.04,
        },
      ],
      dense: true,
    });
    const ask = app.ask();
    await ask.open();
    await ask.ask('where are cached entries read');
    await app.env.waitUntil(async () => (await ask.hits()).length > 0, { message: 'no hits rendered' });

    expect(await ask.hits()).toEqual(['Cache.Get']);
    const diagram = await app.diagram();
    await app.env.waitUntil(async () => (await diagram.componentCount()) === 1, {
      message: 'canvas never narrowed to the answer',
    });
    const card = await diagram.component('store');
    const blocks = await card.blocks();
    // The canvas draws internals, not members: the hit lands on its receiver.
    expect(await Promise.all(blocks.map((block) => block.name()))).toEqual(['Cache']);
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
