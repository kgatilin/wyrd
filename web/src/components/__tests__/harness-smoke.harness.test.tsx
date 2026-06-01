import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import { diffGraph, nonDiffGraph } from '../../../testing/fixtures';

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

  it('Changes panel shows 38 entries and no duplicated PR summary', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    expect(await app.hasChangesTab()).toBe(true);
    await app.openChangesTab();
    const changes = app.changesPanel();
    expect(await changes.count()).toBe(38);
    expect(await changes.hasPrSummary()).toBe(false);
  });

  it('CONTEXTS tab switch reveals the tree with 3 bounded contexts', async () => {
    const env = await mountAppDom(diffGraph);
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    await app.openContextsTab();
    const tree = app.contextTree();
    expect(await tree.isPresent()).toBe(true);
    expect(await tree.boundedContextRowCount()).toBe(3);
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
