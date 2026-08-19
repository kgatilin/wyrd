import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

// Run serially so a single browser context is shared and the cold-start window
// does not cause spurious timeouts.
test.describe.configure({ mode: 'serial' });

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  return app;
}

// ── 1. Theme toggle ───────────────────────────────────────────────────────────

test('theme toggle flips the root theme class', async ({ page }) => {
  const app = await loadDiff(page);

  const before = await app.themeName();
  await app.toggleTheme();
  await app.env.waitUntil(async () => (await app.themeName()) !== before, {
    message: 'theme did not change after first toggle',
  });
  const after = await app.themeName();
  expect(after).toBe(before === 'dark' ? 'light' : 'dark');

  await app.toggleTheme();
  await app.env.waitUntil(async () => (await app.themeName()) === before, {
    message: 'theme did not return to original after second toggle',
  });
  expect(await app.themeName()).toBe(before);
});

// ── 3. Left panel collapse ────────────────────────────────────────────────────

test('left panel collapse shows vertical label; expand restores', async ({ page }) => {
  const app = await loadDiff(page);

  expect(await app.isLeftCollapsed()).toBe(false);

  await app.toggleLeftPanel();
  await app.env.waitUntil(async () => app.isLeftCollapsed(), {
    message: 'left panel did not collapse',
  });
  expect(await app.isLeftCollapsed()).toBe(true);

  // The rail's two tabs are REVIEW and ASK; collapsed, it names the active one.
  expect(await app.leftCollapsedLabel()).toBe('REVIEW');

  await app.toggleLeftPanel();
  await app.env.waitUntil(async () => !(await app.isLeftCollapsed()), {
    message: 'left panel did not expand',
  });
  expect(await app.isLeftCollapsed()).toBe(false);
});
