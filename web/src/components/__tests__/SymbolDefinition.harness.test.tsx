import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom, type MountOptions } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { UIGraph } from '../../types';

/**
 * One interface with a method, and a function that calls it — enough wiring
 * for the panel to open, with one symbol the graph indexes as a node of its
 * own and one (the method) it does not.
 */
const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'api',
      name: 'api',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        {
          id: 'api.Store',
          kind: 'iface',
          name: 'Store',
          exported: true,
          sourceFile: 'store.go',
          members: [
            {
              id: 'api.Store.Save',
              kind: 'method',
              name: 'Save',
              params: '(ctx context.Context)',
              type: 'error',
              exported: true,
            },
          ],
        },
      ],
      ports: [],
    },
    {
      id: 'app',
      name: 'app',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        {
          id: 'app.Run',
          kind: 'func',
          name: 'Run',
          exported: true,
          sourceFile: 'run.go',
          members: [],
        },
      ],
      ports: [],
    },
  ],
  edges: [],
  relations: [
    {
      id: 'r:calls:app.Run->api.Store.Save',
      kind: 'calls',
      fromComponentId: 'app',
      fromInternalId: 'app.Run',
      fromLabel: 'Run(ctx context.Context) error',
      toComponentId: 'api',
      toInternalId: 'api.Store',
      toMemberId: 'api.Store.Save',
      toLabel: 'Save(ctx context.Context) error',
    },
  ],
  comments: [],
};

/**
 * What the daemon's retrieval graph has. `api.Store.Save` is deliberately
 * absent: an interface method is a span inside its interface, not a node.
 */
const nodes: Record<string, unknown> = {
  'app.Run': {
    node_id: 'app.Run',
    kind: 'func',
    package: 'app',
    name: 'Run',
    file: 'app/run.go',
    line: 12,
    signature: 'func Run(ctx context.Context) error',
    doc: 'Run drives one pass over the store.\n',
    body: 'func Run(ctx context.Context) error {\n\treturn store.Save(ctx)\n}',
  },
  'api.Store': {
    node_id: 'api.Store',
    kind: 'iface',
    package: 'api',
    name: 'Store',
    file: 'api/store.go',
    line: 7,
    signature: 'type Store interface',
    body: 'type Store interface {\n\tSave(ctx context.Context) error\n}',
  },
};

const sources: Record<string, string> = {
  'app/run.go': 'package app\n\nfunc Run(ctx context.Context) error {\n\treturn store.Save(ctx)\n}\n',
};

async function mount(options?: MountOptions) {
  const env = await mountAppDom(graph, {
    node: (id) => nodes[id],
    source: (path) => ({ content: sources[path] ?? '' }),
    ...options,
  });
  const app = await env.load(AppHarness);
  await app.waitForLoaded();
  return { env, app };
}

/** Open the wiring panel on the package-level function. */
async function focusRun(app: AppHarness) {
  const card = await (await app.diagram()).component('app');
  if (!(await card.isExpanded())) await card.toggleExpand();
  await app.env.waitUntil(async () => (await card.blockCount()) >= 1, {
    message: 'the app card never drew its symbols',
  });
  const block = await card.block('Run');
  await block.focusSymbol();
  const wiring = app.symbolWiring();
  await app.env.waitUntil(() => wiring.isPresent(), { message: 'symbol wiring overlay never opened' });
  return wiring;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('the declaration in the wiring panel', () => {
  it('shows where the symbol is written, its signature, doc and source', async () => {
    const { app } = await mount();
    const wiring = await focusRun(app);
    const definition = wiring.definition();

    await app.env.waitUntil(async () => (await definition.location()) != null, {
      message: 'the declaration was never read',
    });
    expect(await definition.location()).toContain('app/run.go:12');
    expect(await definition.signature()).toBe('func Run(ctx context.Context) error');
    // The indexer keeps the doc's trailing newline; the panel must not show it
    // as a blank line under the text.
    expect(await definition.doc()).toBe('Run drives one pass over the store.');
    expect(await definition.sourceLines()).toEqual([
      'func Run(ctx context.Context) error {',
      'return store.Save(ctx)',
      '}',
    ]);
    // Numbered by the file, not by the block: 12 is where the declaration is.
    expect(await definition.lineNumbers()).toEqual(['12', '13', '14']);
    // The declaration is the anchor's own, so nothing stands in for it.
    expect(await definition.fallbackNote()).toBeNull();
  });

  it('answers with the declaring type when a member is no node of its own', async () => {
    const { app } = await mount();
    const store = await (await (await app.diagram()).component('api')).block('Store');
    await (await store.row('Save')).focusSymbol();

    const definition = app.symbolWiring().definition();
    await app.env.waitUntil(async () => (await definition.location()) != null, {
      message: 'the declaring type was never read',
    });
    expect(await definition.location()).toContain('api/store.go:7');
    expect(await definition.fallbackNote()).toBe('declared in Store');
  });

  it('says so when the graph records no declaration', async () => {
    const { app } = await mount({ node: () => undefined });
    const wiring = await focusRun(app);
    const definition = wiring.definition();

    await app.env.waitUntil(async () => (await definition.state()) === 'No declaration recorded for this symbol.', {
      message: 'the panel never reported the missing declaration',
    });
    expect(await definition.location()).toBeNull();
  });

  it('folds the source away but keeps the signature', async () => {
    const { app } = await mount();
    const wiring = await focusRun(app);
    const definition = wiring.definition();
    await app.env.waitUntil(async () => (await definition.sourceLines()).length === 3, {
      message: 'the source was never drawn',
    });

    await definition.toggleSource();
    await app.env.waitUntil(async () => (await definition.sourceLines()).length === 0, {
      message: 'the source never folded away',
    });
    expect(await definition.signature()).toBe('func Run(ctx context.Context) error');
    expect(await definition.location()).toContain('app/run.go:12');
  });
});

describe('opening the whole file from the wiring panel', () => {
  it('opens the source viewer at the declaring file', async () => {
    const { app } = await mount();
    const wiring = await focusRun(app);
    const definition = wiring.definition();
    await app.env.waitUntil(async () => (await definition.location()) != null, {
      message: 'the declaration was never read',
    });

    await definition.openFile();
    const drawer = app.sourceDrawer();
    await app.env.waitUntil(async () => (await drawer.path()) != null, {
      message: 'source drawer never opened',
    });
    expect(await drawer.path()).toBe('app/run.go');
    expect((await drawer.lines()).join('\n')).toContain('func Run(ctx context.Context) error {');
    // The file takes over: the panel that named it has said all it can.
    expect(await wiring.isPresent()).toBe(false);
  });
});
