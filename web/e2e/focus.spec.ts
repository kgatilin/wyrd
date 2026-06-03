import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  return app;
}

test('focus dims unrelated components; related stay lit', async ({ page }) => {
  const app = await loadDiff(page);
  const diagram = await app.diagram();
  const svc = await diagram.component('OrderService');
  await svc.focus();
  await app.env.waitUntil(async () => svc.isFocused(), { message: 'OrderService card did not focus' });

  // Focused component is never dimmed.
  expect(await svc.isDimmed()).toBe(false);

  // Direct edge neighbours of OrderService: CheckoutAPI (e1: api→orders),
  // OrderEvents (e4: orders→events), Notifier (e6: orders→notif) — all lit.
  expect(await (await diagram.component('CheckoutAPI')).isDimmed()).toBe(false);
  expect(await (await diagram.component('OrderEvents')).isDimmed()).toBe(false);
  expect(await (await diagram.component('Notifier')).isDimmed()).toBe(false);

  // PaymentService has no edge to/from OrderService → dimmed.
  expect(await (await diagram.component('PaymentService')).isDimmed()).toBe(true);

  // Belt-and-suspenders: at least one component is dimmed and the focused one is not.
  const allCards = await diagram.components();
  const dimmedCards = await Promise.all(allCards.map((c) => c.isDimmed()));
  expect(dimmedCards.some(Boolean)).toBe(true);
  expect(await svc.isDimmed()).toBe(false);
});

test('re-clicking the focused card clears focus (toggle-off)', async ({ page }) => {
  const app = await loadDiff(page);
  const diagram = await app.diagram();
  const svc = await diagram.component('OrderService');

  await svc.focus();
  await app.env.waitUntil(async () => svc.isFocused(), { message: 'OrderService did not focus' });

  await svc.focus();
  await app.env.waitUntil(async () => !(await svc.isFocused()), { message: 'second click did not clear focus' });

  // No component should be dimmed when focus is cleared.
  const allCards = await diagram.components();
  for (const card of allCards) {
    expect(await card.isDimmed()).toBe(false);
  }
});

test('clicking the empty canvas background clears focus', async ({ page }) => {
  const app = await loadDiff(page);
  const diagram = await app.diagram();
  const svc = await diagram.component('OrderService');

  await svc.focus();
  await app.env.waitUntil(async () => svc.isFocused(), { message: 'OrderService did not focus' });

  await (await app.canvas()).clickBackground();
  await app.env.waitUntil(async () => !(await svc.isFocused()), { message: 'canvas background click did not clear focus' });

  expect(await svc.isFocused()).toBe(false);
  const allCards = await diagram.components();
  for (const card of allCards) {
    expect(await card.isDimmed()).toBe(false);
  }
});

test('clicking a tree internal row expands + focuses the owning component on the canvas', async ({ page }) => {
  const app = await loadDiff(page);
  const tree = app.contextTree();

  await app.openContextsTab();

  // Expand PaymentService so its IGateway internal row appears.
  await tree.expand('PaymentService');
  await app.env.waitUntil(async () => (await tree.internalRowCount()) >= 1, {
    message: 'internal rows never appeared after expanding PaymentService',
  });

  // Click the IGateway internal row → should expand and focus the PaymentService card.
  await tree.clickRow('IGateway');

  const diagram = await app.diagram();
  const pay = await diagram.component('PaymentService');
  await app.env.waitUntil(async () => pay.isExpanded(), {
    message: 'PaymentService card did not expand after clicking IGateway tree row',
  });
  expect(await pay.isFocused()).toBe(true);
});
