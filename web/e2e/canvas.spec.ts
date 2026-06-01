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

test('canvas shows a grab cursor and an oversized sizer', async ({ page }) => {
  const app = await loadDiff(page);
  const canvas = await app.canvas();
  expect(await canvas.cursor()).toBe('grab');
  expect(await canvas.sizerExceedsContent()).toBe(true);
});

test('dragging the background pans the canvas and clears .panning', async ({ page }) => {
  const app = await loadDiff(page);
  const canvas = await app.canvas();
  const before = await canvas.scrollPosition();
  await canvas.pan(-160, -120); // drag up-left → scroll increases
  await app.env.waitUntil(
    async () => {
      const p = await canvas.scrollPosition();
      return Math.abs(p.left - before.left) > 20 || Math.abs(p.top - before.top) > 20;
    },
    { message: 'pan did not move the scroll position' }
  );
  expect(await canvas.isPanning()).toBe(false);
});

test('a pan-drag does not clear an existing focus', async ({ page }) => {
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService');
  await svc.focus();
  await app.env.waitUntil(async () => await svc.isFocused(), { message: 'card did not focus' });
  const canvas = await app.canvas();
  await canvas.pan(-140, -100);
  expect(await svc.isFocused()).toBe(true);
});

test('zoom buttons and ctrl+wheel change the zoom label/scale', async ({ page }) => {
  const app = await loadDiff(page);
  const canvas = await app.canvas();
  const initial = await canvas.zoomLabel();

  await canvas.zoomIn();
  await app.env.waitUntil(async () => (await canvas.zoomLabel()) !== initial, {
    message: 'zoom in did not change label',
  });
  const afterIn = await canvas.zoomLabel();

  await canvas.zoomOut();
  await app.env.waitUntil(async () => (await canvas.zoomLabel()) !== afterIn, {
    message: 'zoom out did not change label',
  });

  await canvas.fit();
  const beforeWheel = await canvas.canvasTransform();
  await canvas.ctrlWheelZoom(-120); // zoom in
  await app.env.waitUntil(async () => (await canvas.canvasTransform()) !== beforeWheel, {
    message: 'ctrl+wheel did not change transform',
  });
  // Page itself must not scroll (ctrl+wheel is intercepted).
  const pageScrollY = await page.evaluate(() => window.scrollY);
  expect(pageScrollY).toBe(0);
});

test('port labels reveal on card hover', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  const label = await pay.portLabel('refund()');
  expect(await label.computedStyleProp('opacity')).toBe('0');
  await pay.hoverCard();
  await app.env.waitUntil(async () => (await label.computedStyleProp('opacity')) === '1', {
    message: 'port label did not reveal on hover',
  });
});
