import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

/**
 * The REVIEW rail's package tree.
 *
 * A row is a package path segment, not a display name: an id is an import path
 * for real data, and the tree is that path split into directories. So the row
 * for the notifier reads `notif` while its card on the canvas reads `Notifier`.
 */
async function loadTree(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  await app.openReviewTree();
  return app;
}

test('the tree is one row per package, drilling into files, classes and members', async ({ page }) => {
  const app = await loadTree(page);
  const tree = app.contextTree();

  expect((await tree.componentRowNames()).sort()).toEqual(['api', 'events', 'notif', 'orders', 'pay']);
  expect(await tree.fileRowCount()).toBeGreaterThanOrEqual(1);
  expect(await tree.internalRowCount()).toBeGreaterThanOrEqual(1);

  // Opening a closed class row reveals its members.
  const before = await tree.memberRowCount();
  await tree.toggle('IEventBus');
  await app.env.waitUntil(async () => (await tree.memberRowCount()) > before, {
    message: 'member rows never appeared after opening a class row',
  });
});

test('clicking a package row focuses that card on the canvas', async ({ page }) => {
  const app = await loadTree(page);
  await app.contextTree().clickRow('notif');
  const notifier = await (await app.diagram()).component('Notifier');
  await app.env.waitUntil(async () => await notifier.isFocused(), {
    message: 'clicking the tree row did not focus the card',
  });
  expect(await notifier.isFocused()).toBe(true);
});

test('diff badges render on changed rows', async ({ page }) => {
  const app = await loadTree(page);
  const tree = app.contextTree();
  expect(await tree.badge('orders')).toBe('~'); // changed
  expect(await tree.badge('events')).toBe('+'); // added
});
