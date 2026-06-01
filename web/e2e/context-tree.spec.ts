import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

async function loadTree(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  await app.openContextsTab();
  return app;
}

test('CONTEXTS tab shows bounded-context rows; drilling reveals cmp → internal → member', async ({ page }) => {
  const app = await loadTree(page);
  const tree = app.contextTree();
  expect(await tree.boundedContextRowCount()).toBe(3);
  // Bounded contexts start open → components visible.
  expect(await tree.componentRowCount()).toBeGreaterThanOrEqual(5);

  // Expand a component → its internals appear.
  await tree.expand('PaymentService');
  await app.env.waitUntil(async () => (await tree.internalRowCount()) >= 1, {
    message: 'internal rows never appeared',
  });

  // Expand an internal → its members appear.
  await tree.expand('IGateway');
  await app.env.waitUntil(async () => (await tree.memberRowCount()) >= 1, {
    message: 'member rows never appeared',
  });
});

test('clicking a component row focuses that card on the canvas', async ({ page }) => {
  const app = await loadTree(page);
  await app.contextTree().clickRow('Notifier');
  const notifier = await (await app.diagram()).component('Notifier');
  await app.env.waitUntil(async () => await notifier.isFocused(), {
    message: 'clicking the tree row did not focus the card',
  });
  expect(await notifier.isFocused()).toBe(true);
});

test('diff badges render on changed rows', async ({ page }) => {
  const app = await loadTree(page);
  const tree = app.contextTree();
  expect(await tree.badge('OrderService')).toBe('~'); // changed
  expect(await tree.badge('OrderEvents')).toBe('+'); // added
});
