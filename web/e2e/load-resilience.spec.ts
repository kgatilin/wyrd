import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraphFailure } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';

test.describe('load resilience (built-in fixture fallback)', () => {
  test('app falls back to built-in fixture when both graph endpoints fail', async ({ page }) => {
    await routeGraphFailure(page);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();

    const diagram = await app.diagram();
    expect(await diagram.componentCount()).toBeGreaterThanOrEqual(1);
    expect(await app.hasError()).toBe(false);
  });

  test('built-in fixture contains bounded contexts', async ({ page }) => {
    await routeGraphFailure(page);
    await page.goto('/');
    const app = await new PlaywrightEnvironment(page).load(AppHarness);
    await app.waitForLoaded();

    const diagram = await app.diagram();
    expect((await diagram.boundedContextNames()).length).toBeGreaterThanOrEqual(1);
  });
});
