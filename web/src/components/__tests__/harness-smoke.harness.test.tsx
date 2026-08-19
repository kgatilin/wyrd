import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import { diffGraph, nonDiffGraph } from '../../../testing/fixtures';
import type { UIGraph } from '../../types';

/** One function whose dependencies straddle a package boundary. */
const crossPackageGraph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'svc',
      name: 'svc',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        { id: 'svc.Handle', kind: 'func', name: 'Handle', exported: true, members: [] },
        { id: 'svc.helper', kind: 'func', name: 'helper', exported: false, members: [] },
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
  ],
  edges: [],
  relations: [
    {
      id: 'r:calls:svc.Handle->svc.helper',
      kind: 'calls',
      fromComponentId: 'svc',
      fromInternalId: 'svc.Handle',
      toComponentId: 'svc',
      toInternalId: 'svc.helper',
      toLabel: 'helper',
    },
    {
      id: 'r:calls:svc.Handle->store.Put',
      kind: 'calls',
      fromComponentId: 'svc',
      fromInternalId: 'svc.Handle',
      toComponentId: 'store',
      toInternalId: 'store.Put',
      toLabel: 'Put',
    },
  ],
  comments: [],
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('harness smoke (jsdom) — diffGraph', () => {
  it('loads the diagram with 5 components', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();
    expect(await diagram.componentCount()).toBe(5);
  });

  it('OrderService is auto-expanded and carries parentInitial "O"', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const svc = await (await app.diagram()).component('OrderService');
    expect(await svc.isExpanded()).toBe(true);
    expect(await svc.parentInitial()).toBe('O');
  });

  it('groups a package card into source-file containers', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    // OrderService is auto-expanded on load.
    const svc = await (await app.diagram()).component('OrderService');
    await app.env.waitUntil(async () => (await svc.fileCount()) >= 1, {
      message: 'OrderService file containers never rendered',
    });
    // Every class shape lives inside a file container, never loose on the card.
    const inFiles = (await Promise.all((await svc.files()).map(async (f) => (await f.blocks()).length)))
      .reduce((a, b) => a + b, 0);
    expect(inFiles).toBe(await svc.blockCount());
  });

  it('canvas toolbar expands and collapses all package cards', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const canvas = await app.canvas();
    const diagram = await app.diagram();

    await canvas.expandAllPackages();
    await app.env.waitUntil(async () => {
      const components = await diagram.components();
      return (await Promise.all(components.map((component) => component.isExpanded()))).every(Boolean);
    }, {
      message: 'not every package card expanded',
    });

    await canvas.collapseAllPackages();
    await app.env.waitUntil(async () => {
      const components = await diagram.components();
      return (await Promise.all(components.map((component) => component.isExpanded()))).every((expanded) => !expanded);
    }, {
      message: 'not every package card collapsed',
    });
  });

  it('derives changed: PaymentService/IGateway and CheckoutAPI', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();

    // CheckoutAPI has no own diff but an added child internal + added port.
    expect(await (await diagram.component('CheckoutAPI')).diffState()).toBe('changed');

    // Expand PaymentService and assert IGateway (no own diff, members add/add/remove).
    const pay = await diagram.component('PaymentService');
    await pay.toggleExpand();
    await app.env.waitUntil(async () => (await pay.blockCount()) >= 1, {
      message: 'PaymentService class shapes never rendered',
    });
    expect(await (await pay.block('IGateway')).diffState()).toBe('changed');
  });

  it('emits no in-card diff tags', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    for (const c of await (await app.diagram()).components()) {
      expect(await c.inCardTagCount()).toBe(0);
    }
  });

  it('review tree shows the changed-details projection and no duplicated PR summary', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    expect(await app.hasReviewTab()).toBe(true);
    await app.openReviewTree();
    const tree = app.contextTree();
    expect(await tree.componentRowCount()).toBe(5);
    expect(await tree.fileRowCount()).toBeGreaterThan(0);
    expect(await tree.memberRowCount()).toBeGreaterThan(0);
    expect(await app.changesPanel().hasPrSummary()).toBe(false);
  });

  it('PR header stats reflect the active review projection', async () => {
    const graph: UIGraph = {
      schema: 'archai.uigraph/v0',
      pr: {
        title: 'Projection stats',
        branch: 'feature',
        agent: 'archai',
        summary: '',
        stats: { added: 99, removed: 99, changed: 99, comments: 7 },
      },
      reviewScopes: [{ id: 'everything', title: 'Everything' }],
      reviewViews: [
        {
          id: 'framework',
          title: 'Framework',
          defaultScope: 'everything',
          componentIds: ['api'],
          componentCount: 1,
        },
      ],
      defaultReviewView: 'framework',
      defaultReviewScope: 'everything',
      boundedContexts: [{ id: 'root', name: 'Root' }],
      components: [
        { id: 'api', name: 'API', tech: 'Go', desc: '', bc: 'root', diff: 'added', internals: [], ports: [] },
        { id: 'internal/runtime', name: 'runtime', tech: 'Go', desc: '', bc: 'root', diff: 'removed', internals: [], ports: [] },
      ],
      edges: [],
      comments: [],
    };

    const env = await mountAppDom(graph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();

    expect(await (await env.rootLocator('.hf-stat.add').first()).text()).toBe('+1');
    expect(await (await env.rootLocator('.hf-stat.rem').first()).text()).toContain('0');
    expect(await (await env.rootLocator('.hf-stat.chg').first()).text()).toBe('~0');
    expect(await (await env.rootLocator('.hf-stat.com').first()).text()).toContain('7');
  });

  it('review tree is package -> file -> type/member, independent from graph contexts', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    await app.openReviewTree();
    const tree = app.contextTree();
    expect(await tree.isPresent()).toBe(true);
    expect(await tree.boundedContextRowCount()).toBe(0);
    expect(await tree.componentRowCount()).toBe(5);
    expect(await tree.fileRowCount()).toBeGreaterThan(0);
  });

  it('review tree renders package paths as an alphabetic directory tree', async () => {
    const graph: UIGraph = {
      schema: 'archai.uigraph/v0',
      boundedContexts: [{ id: 'root', name: 'Root' }],
      components: [
        { id: 'zeta', name: 'zeta', tech: 'Go', desc: '', bc: 'root', internals: [], ports: [] },
        { id: 'internal/eventstore', name: 'eventstore', tech: 'Go', desc: '', bc: 'root', internals: [], ports: [] },
        { id: 'app/eventstore', name: 'eventstore', tech: 'Go', desc: '', bc: 'root', internals: [], ports: [] },
      ],
      edges: [],
      comments: [],
    };

    const env = await mountAppDom(graph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    await app.openReviewTree();

    const tree = app.contextTree();
    expect(await tree.packageDirectoryNames()).toEqual(['app', 'internal']);
    expect(await tree.componentRowNames()).toEqual(['eventstore', 'eventstore', 'zeta']);
  });

  it('diagram cards show whether their package layer is public or internal', async () => {
    const graph: UIGraph = {
      schema: 'archai.uigraph/v0',
      boundedContexts: [{ id: 'root', name: 'Root' }],
      components: [
        { id: 'eventstore', name: 'PublicStore', tech: 'Go', desc: '', bc: 'root', internals: [], ports: [] },
        { id: 'internal/eventstore', name: 'InternalStore', tech: 'Go', desc: '', bc: 'root', internals: [], ports: [] },
      ],
      edges: [],
      comments: [],
    };

    const env = await mountAppDom(graph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();

    expect(await (await diagram.component('PublicStore')).packageLayer()).toBe('public');
    expect(await (await diagram.component('InternalStore')).packageLayer()).toBe('internal');
  });

  it('clicking a package enters focused package view with public/internal symbols highlighted', async () => {
    const graph: UIGraph = {
      schema: 'archai.uigraph/v0',
      boundedContexts: [{ id: 'root', name: 'Root' }],
      components: [
        {
          id: 'a',
          name: 'A',
          tech: 'Go',
          desc: '',
          bc: 'root',
          internals: [
            {
              id: 'a.PublicType',
              kind: 'class',
              name: 'PublicType',
              exported: true,
              members: [{ id: 'a.PublicType.Do', kind: 'method', name: 'Do()', exported: true }],
            },
            {
              id: 'a.privateType',
              kind: 'class',
              name: 'privateType',
              exported: false,
              members: [{ id: 'a.privateType.help', kind: 'method', name: 'help()', exported: false }],
            },
          ],
          ports: [],
        },
        {
          id: 'b',
          name: 'B',
          tech: 'Go',
          desc: '',
          bc: 'root',
          internals: [{ id: 'b.Worker', kind: 'class', name: 'Worker', exported: true, members: [] }],
          ports: [],
        },
      ],
      edges: [{ id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: 'uses' }],
      comments: [],
    };

    const env = await mountAppDom(graph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();

    await (await (await app.diagram()).component('B')).toggleExpand();
    await app.env.waitUntil(async () => await (await (await app.diagram()).component('B')).isExpanded(), {
      message: 'B never expanded before package focus',
    });

    await (await (await app.diagram()).component('A')).focus();
    await app.env.waitUntil(async () => {
      const diagram = await app.diagram();
      return (
        await (await diagram.component('A')).isExpanded()
      ) && !(await (await diagram.component('B')).isExpanded());
    }, {
      message: 'focused package view never collapsed non-focused package',
    });

    // Class bodies are always open — a symbol's structure needs no extra click.
    const a = await (await app.diagram()).component('A');
    const publicType = await a.block('PublicType');
    const privateType = await a.block('privateType');
    expect(await publicType.hasBody()).toBe(true);
    expect(await publicType.symbolVisibility()).toBe('public');
    expect(await privateType.symbolVisibility()).toBe('internal');
    expect(await (await publicType.row('Do')).symbolVisibility()).toBe('public');
    expect(await (await privateType.row('help')).symbolVisibility()).toBe('internal');
    expect(await (await app.diagram()).edgeCount()).toBe(1);
  });

  it('clicking a symbol opens its first-level wiring grouped by package', async () => {
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
              members: [{ id: 'api.Store.Save', kind: 'method', name: 'Save(ctx context.Context) error', exported: true }],
            },
          ],
          ports: [],
        },
        {
          id: 'internal/repo',
          name: 'repo',
          tech: 'Go',
          desc: '',
          bc: 'root',
          internals: [
            {
              id: 'internal/repo.SQLStore',
              kind: 'class',
              name: 'SQLStore',
              exported: false,
              members: [{ id: 'internal/repo.SQLStore.Save', kind: 'method', name: 'Save(ctx context.Context) error', exported: false }],
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
          internals: [{ id: 'app.Run', kind: 'func', name: 'Run(ctx context.Context) error', exported: true, members: [] }],
          ports: [],
        },
      ],
      edges: [],
      relations: [
        {
          id: 'r:implements:internal/repo.SQLStore->api.Store',
          kind: 'implements',
          fromComponentId: 'internal/repo',
          fromInternalId: 'internal/repo.SQLStore',
          fromLabel: 'SQLStore',
          toComponentId: 'api',
          toInternalId: 'api.Store',
          toLabel: 'Store',
        },
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

    const env = await mountAppDom(graph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const store = await (await (await app.diagram()).component('api')).block('Store');
    await (await store.row('Save')).focusSymbol();

    const wiring = app.symbolWiring();
    await app.env.waitUntil(() => wiring.isPresent(), {
      message: 'symbol wiring overlay never opened',
    });

    expect(await wiring.anchorName()).toContain('Save(ctx context.Context) error');
    expect(await wiring.anchorPackage()).toBe('api');
    expect(await wiring.stats()).toEqual(['2 in', '0 out', '2 cross-package']);

    // Both neighbours live outside `api`, so both blocks are flagged as crossing
    // the package boundary — the caller and the implementation.
    const incoming = wiring.incoming();
    // Blocks carry the full package path — basenames collide across roots.
    expect(await incoming.packages()).toEqual(['app', 'internal/repo']);
    expect(await incoming.crossPackages()).toEqual(['app', 'internal/repo']);
    expect(await incoming.linkNames()).toEqual([
      'Run(ctx context.Context) error',
      'Save(ctx context.Context) error',
    ]);

    const caller = await incoming.link('Run(ctx context.Context) error');
    expect(await caller.relations()).toEqual(['calls']);
    expect(await caller.symbolVisibility()).toBe('public');
    const impl = await incoming.link('Save(ctx context.Context) error');
    expect(await impl.relations()).toEqual(['implements']);
    expect(await impl.symbolVisibility()).toBe('internal');

    // Nothing flows out of an interface method declaration.
    expect(await wiring.outgoing().isEmpty()).toBe(true);

    // Only first-level relations are shown; depth comes from walking one hop.
    expect(await wiring.hasBack()).toBe(false);
    await caller.walk();
    await app.env.waitUntil(async () => (await wiring.anchorName()).includes('Run(ctx'), {
      message: 'walking to a neighbour never re-anchored the panel',
    });
    expect(await wiring.anchorPackage()).toBe('app');
    expect(await wiring.outgoing().linkNames()).toEqual(['Save(ctx context.Context) error']);

    await wiring.back();
    await app.env.waitUntil(async () => (await wiring.anchorPackage()) === 'api', {
      message: 'back never returned to the previous symbol',
    });
  });

  it('hides same-package neighbours when cross-package only is on', async () => {
    const env = await mountAppDom(crossPackageGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const svc = await (await (await app.diagram()).component('svc')).block('Handle');
    await svc.focusSymbol();

    const wiring = app.symbolWiring();
    await app.env.waitUntil(() => wiring.isPresent(), {
      message: 'symbol wiring overlay never opened',
    });
    expect(await wiring.outgoing().packages()).toEqual(['store', 'svc']);

    await wiring.toggleCrossPackageOnly();
    await app.env.waitUntil(async () => (await wiring.outgoing().packages()).length === 1, {
      message: 'cross-package filter never applied',
    });
    expect(await wiring.outgoing().packages()).toEqual(['store']);
    expect(await wiring.outgoing().count()).toBe('1 / 2');
  });
});

describe('harness smoke (jsdom) — nonDiffGraph', () => {
  it('has no PR header, no CHANGES tab, and no legend', async () => {
    const env = await mountAppDom(nonDiffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    expect(await app.hasPrHeader()).toBe(false);
    expect(await app.hasChangesTab()).toBe(false);
    expect(await app.legend().isPresent()).toBe(false);
    expect(await app.branchCrumb()).toBeNull();
  });
});
