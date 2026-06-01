import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph, nonDiffGraph } from '../testing/fixtures';

test.describe('diff mode (diffGraph)', () => {
  test('PR header, tabs, legend, diff colors, edges, no in-card tags', async ({ page }) => {
    await routeGraph(page, diffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();

    // PR header + branch crumb
    expect(await app.hasPrHeader()).toBe(true);
    expect(await app.prTitle()).toContain('OrderEvents');
    expect(await app.branchCrumb()).toBe('agent/order-events-2026-04-30');

    // Tabs
    expect(await app.hasChangesTab()).toBe(true);
    expect(await app.changesTabCount()).toBe(38);
    expect(await app.contextsTabCount()).toBe(3);

    // Legend: 3 items, diff-only
    const legend = app.legend();
    expect(await legend.isPresent()).toBe(true);
    expect(await legend.itemTexts()).toEqual(['added', 'removed', 'changed']);

    const diagram = await app.diagram();
    expect(await diagram.componentCount()).toBe(5);

    // Component diff colors (explicit + derived)
    expect(await (await diagram.component('OrderEvents')).diffState()).toBe('added');
    expect(await (await diagram.component('OrderService')).diffState()).toBe('changed');
    expect(await (await diagram.component('PaymentService')).diffState()).toBe('changed');
    expect(await (await diagram.component('Notifier')).diffState()).toBe('changed');
    expect(await (await diagram.component('CheckoutAPI')).diffState()).toBe('changed'); // derived

    // Edges
    expect(await diagram.edgeCount()).toBe(6);
    expect(await diagram.diffEdgeCount()).toBe(5);

    // No in-card NEW/MOD tags
    for (const c of await diagram.components()) {
      expect(await c.inCardTagCount()).toBe(0);
    }
  });

  test('IGateway derives changed; removed member + removed port are NOT struck through', async ({ page }) => {
    await routeGraph(page, diffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();
    const diagram = await app.diagram();

    const pay = await diagram.component('PaymentService');
    await pay.toggleExpand();
    await app.env.waitUntil(async () => (await pay.internalCount()) >= 1, {
      message: 'PaymentService internals never rendered',
    });

    const gateway = await pay.internal('IGateway');
    expect(await gateway.diffState()).toBe('changed'); // derived from members

    // Removed member charge(amt): colored but NOT line-through.
    const charge = await gateway.member('charge');
    expect(await charge.diffState()).toBe('removed');
    expect(await charge.textDecoration()).not.toContain('line-through');

    // Removed port label: NOT line-through.
    const removedPort = await pay.removedPortLabel();
    const portDecoration = await removedPort.computedStyleProp('text-decoration');
    expect(portDecoration).not.toContain('line-through');
  });

  test('Changes panel lists entries without a duplicated PR summary', async ({ page }) => {
    await routeGraph(page, diffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();
    await app.openChangesTab();
    const changes = app.changesPanel();
    expect(await changes.count()).toBe(38);
    expect(await changes.hasPrSummary()).toBe(false);
  });
});

test.describe('non-diff mode (nonDiffGraph)', () => {
  test('no PR header, no CHANGES tab, no legend, zero diff classes', async ({ page }) => {
    await routeGraph(page, nonDiffGraph);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();

    expect(await app.hasPrHeader()).toBe(false);
    expect(await app.hasChangesTab()).toBe(false);
    expect(await app.legend().isPresent()).toBe(false);

    const diagram = await app.diagram();
    for (const c of await diagram.components()) {
      expect(await c.diffState()).toBeNull();
    }
    expect(await diagram.diffEdgeCount()).toBe(0);
  });
});
