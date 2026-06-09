import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import { diffGraph, nonDiffGraph } from '../../../testing/fixtures';
import type { UIGraph } from '../../types';

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

  it('expand-all glyph round-trips «» → »« → «»', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    await (await app.canvas()).toggleInlineSignatures();
    await app.env.waitUntil(async () => (
      await (await (await app.diagram()).component('OrderService')).hasExpandAllButton()
    ), {
      message: 'expand-all button never rendered in short-signature mode',
    });
    const svc = await (await app.diagram()).component('OrderService');
    expect(await svc.expandAllGlyph()).toBe('«»');
    await svc.expandAll();
    expect(await svc.expandAllGlyph()).toBe('»«');
    await svc.expandAll();
    expect(await svc.expandAllGlyph()).toBe('«»');
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
    await app.env.waitUntil(async () => (await pay.internalCount()) >= 1, {
      message: 'PaymentService internals never rendered',
    });
    expect(await (await pay.internal('IGateway')).diffState()).toBe('changed');
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

    const a = await (await app.diagram()).component('A');
    const publicType = await a.internal('PublicType');
    const privateType = await a.internal('privateType');
    expect(await publicType.isExpanded()).toBe(true);
    expect(await privateType.isExpanded()).toBe(true);
    expect(await publicType.symbolVisibility()).toBe('public');
    expect(await privateType.symbolVisibility()).toBe('internal');
    expect(await (await publicType.member('Do')).symbolVisibility()).toBe('public');
    expect(await (await privateType.member('help')).symbolVisibility()).toBe('internal');
    expect(await (await app.diagram()).edgeCount()).toBe(1);
  });

  it('clicking a symbol opens a wiring graph including interface implementations', async () => {
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
    const store = await (await (await app.diagram()).component('api')).internal('Store');
    await (await store.member('Save')).focusSymbol();

    await app.env.waitUntil(async () => (await env.rootLocator('.hf-symbol-overlay').count()) === 1, {
      message: 'symbol wiring overlay never opened',
    });
    const labels = await Promise.all(
      (await env.rootLocator('.hf-symbol-node-label').all()).map((node) => node.text())
    );
    const edgeLabels = await Promise.all(
      (await env.rootLocator('.hf-symbol-edge-label').all()).map((node) => node.text())
    );
    expect(labels).toContain('Save(ctx context.Context) error');
    expect(labels).toContain('Run(ctx context.Context) error');
    expect(edgeLabels).toContain('calls');
    expect(edgeLabels).toContain('implements');
    expect(await env.rootLocator('.hf-symbol-node.symbol-public').count()).toBeGreaterThan(0);
    expect(await env.rootLocator('.hf-symbol-node.symbol-internal').count()).toBeGreaterThan(0);
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
