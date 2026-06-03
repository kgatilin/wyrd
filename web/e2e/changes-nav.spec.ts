import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  await app.openChangesTab();
  return app;
}

test('click a component-level change focuses that card and marks the entry active', async ({ page }) => {
  const app = await loadDiff(page);
  const changes = app.changesPanel();
  const diagram = await app.diagram();

  // OrderEvents has diff:'added' on the component itself → component-level entry.
  await changes.clickEntry('OrderEvents');

  const orderEvents = await diagram.component('OrderEvents');
  await app.env.waitUntil(async () => await orderEvents.isFocused(), {
    message: 'OrderEvents card never became focused after clicking component-level change',
  });

  expect(await orderEvents.isFocused()).toBe(true);
  expect(await changes.activeCount()).toBe(1);
  expect(await changes.isEntryActive('OrderEvents')).toBe(true);
});

test('click a drill-in change auto-expands its collapsed component and focuses it', async ({ page }) => {
  const app = await loadDiff(page);
  const changes = app.changesPanel();
  const diagram = await app.diagram();

  // PaymentService starts collapsed (only 'orders'/OrderService is auto-expanded).
  const pay = await diagram.component('PaymentService');
  expect(await pay.isExpanded()).toBe(false);

  // authorize(amt) is a member of IGateway (internal of PaymentService) with diff:'added'.
  // drillIn = true because change.internal AND change.member are set → expands PaymentService.
  await changes.clickEntry('authorize(amt)');

  await app.env.waitUntil(async () => await pay.isExpanded(), {
    message: 'PaymentService never expanded after clicking drill-in member change',
  });

  expect(await pay.isExpanded()).toBe(true);
  expect(await pay.isFocused()).toBe(true);
  expect(await changes.isEntryActive('authorize(amt)')).toBe(true);
});

test('active highlight is single and moves when switching between entries', async ({ page }) => {
  const app = await loadDiff(page);
  const changes = app.changesPanel();

  // Click entry A: OrderEvents (component-level, unambiguous).
  await changes.clickEntry('OrderEvents');
  await app.env.waitUntil(async () => await changes.isEntryActive('OrderEvents'), {
    message: 'entry A (OrderEvents) never became active',
  });
  expect(await changes.activeEntryName()).toContain('OrderEvents');
  expect(await changes.activeCount()).toBe(1);

  // Click entry B: OrderService (component-level, different component).
  await changes.clickEntry('OrderService');
  await app.env.waitUntil(async () => await changes.isEntryActive('OrderService'), {
    message: 'entry B (OrderService) never became active after switching',
  });
  expect(await changes.activeEntryName()).toContain('OrderService');
  expect(await changes.activeCount()).toBe(1);
});
