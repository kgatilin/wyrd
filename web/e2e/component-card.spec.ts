import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph, longMemberGraph } from '../testing/fixtures';

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  return app;
}

test('OrderService is auto-expanded; parent initials are O/P/N', async ({ page }) => {
  const app = await loadDiff(page);
  const diagram = await app.diagram();
  expect(await (await diagram.component('OrderService')).isExpanded()).toBe(true);
  expect(await (await diagram.component('CheckoutAPI')).parentInitial()).toBe('O');
  expect(await (await diagram.component('PaymentService')).parentInitial()).toBe('P');
  expect(await (await diagram.component('Notifier')).parentInitial()).toBe('N');
});

test('grouped actions (i)/«»/− do not overlap and sit within the card', async ({ page }) => {
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService'); // expanded → all 3 present
  const card = await svc.box();
  const info = await svc.infoIconBox();
  const all = await svc.expandAllBox();
  const exp = await svc.expandBox();
  expect(card && info && all && exp).toBeTruthy();
  if (!card || !info || !all || !exp) return;

  const right = (b: { x: number; width: number }) => b.x + b.width;
  // Horizontal order: info left of expand-all left of expand, no overlap.
  expect(right(info)).toBeLessThanOrEqual(all.x + 1);
  expect(right(all)).toBeLessThanOrEqual(exp.x + 1);
  // All within the card's horizontal extent.
  for (const b of [info, all, exp]) {
    expect(b.x).toBeGreaterThanOrEqual(card.x - 1);
    expect(right(b)).toBeLessThanOrEqual(right(card) + 1);
  }
});

test('(i) popover opens ABOVE the info icon on hover', async ({ page }) => {
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService');
  await svc.hoverInfo();
  const pop = await svc.infoPopover();
  await app.env.waitUntil(async () => (await pop.computedStyleProp('visibility')) === 'visible', {
    message: 'info popover never became visible',
  });
  const popBox = await pop.boundingBox();
  const iconBox = await svc.infoIconBox();
  expect(popBox && iconBox).toBeTruthy();
  if (!popBox || !iconBox) return;
  // Popover bottom is at/above the icon top (CSS bottom: calc(100% + 6px)).
  expect(popBox.y + popBox.height).toBeLessThanOrEqual(iconBox.y + 2);
});

test('collapsed card: single + action, one-line header (tech not wrapped), width > 220', async ({ page }) => {
  const app = await loadDiff(page);
  const api = await (await app.diagram()).component('CheckoutAPI'); // collapsed
  expect(await api.isExpanded()).toBe(false);
  expect(await api.actionButtonCount()).toBe(1);
  expect(await api.expandButtonGlyph()).toBe('+');
  expect(await api.hasExpandAllButton()).toBe(false);
  expect(await api.width()).toBeGreaterThan(220);
  expect(await api.height()).toBeGreaterThanOrEqual(120);
  const techBox = await api.techBox();
  expect(techBox).toBeTruthy();
  if (techBox) expect(techBox.height).toBeLessThan(24); // single line
});

test('expand/collapse toggle flips the glyph and internals', async ({ page }) => {
  const app = await loadDiff(page);
  const api = await (await app.diagram()).component('CheckoutAPI');
  expect(await api.expandButtonGlyph()).toBe('+');
  await api.toggleExpand();
  await app.env.waitUntil(async () => await api.isExpanded(), { message: 'did not expand' });
  expect(await api.expandButtonGlyph()).toBe('−');
  await api.toggleExpand();
  await app.env.waitUntil(async () => !(await api.isExpanded()), { message: 'did not collapse' });
  expect(await api.expandButtonGlyph()).toBe('+');
});

test('expand-all glyph round-trips with no console errors', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  const app = await loadDiff(page);
  const svc = await (await app.diagram()).component('OrderService');
  expect(await svc.expandAllGlyph()).toBe('«»');
  await svc.expandAll();
  await app.env.waitUntil(async () => (await svc.expandAllGlyph()) === '»«', { message: 'glyph did not flip' });
  await svc.expandAll();
  await app.env.waitUntil(async () => (await svc.expandAllGlyph()) === '«»', { message: 'glyph did not reset' });
  expect(errors).toEqual([]);
});

test('fit-width widens an internal then restores it (longMemberGraph)', async ({ page }) => {
  await routeGraph(page, longMemberGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  const cmp = await (await app.diagram()).component('Reconciler'); // first cmp → auto-expanded
  const engine = await cmp.internal('Engine');
  const base = await engine.width();
  expect(base).toBeLessThanOrEqual(185); // ~INTERNAL_W 180

  await engine.toggleFitWidth();
  await app.env.waitUntil(async () => (await engine.width()) > base + 50, {
    message: 'internal did not widen on fit-width',
  });
  expect(await engine.fitWidthGlyph()).toBe('−');

  await engine.toggleFitWidth();
  await app.env.waitUntil(async () => (await engine.width()) <= base + 5, {
    message: 'internal width did not restore',
  });
  expect(await engine.fitWidthGlyph()).toBe('+');
});

test('member and internal carry title tooltips', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.toggleExpand();
  await app.env.waitUntil(async () => (await pay.internalCount()) >= 1, { message: 'no internals' });
  const gateway = await pay.internal('IGateway');
  expect(await gateway.nameTitle()).toBe('IGateway');
  const member = await gateway.member('refund');
  expect(await member.rowTitle()).toContain('refund');
});

test('clicking a port opens the comment popover with the right target', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.clickPort('authorize()');
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => await popover.isOpen(), { message: 'popover did not open' });
  expect(await popover.tag()).toBe('port');
  expect(await popover.target()).toBe('pay.auth');
});
