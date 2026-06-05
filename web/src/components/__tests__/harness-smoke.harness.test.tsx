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
