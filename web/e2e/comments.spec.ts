import { test, expect } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { diffGraph } from '../testing/fixtures';

// Run serially so a single browser context is shared across the cold-start
// window. Without this, all 4 workers spin up Chrome simultaneously and hit
// the 30 s test-setup timeout on a cold Vite server.
test.describe.configure({ mode: 'serial' });

async function loadDiff(page: import('@playwright/test').Page) {
  await routeGraph(page, diffGraph);
  await page.goto('/');
  const app = await new PlaywrightEnvironment(page).load(AppHarness);
  await app.waitForLoaded();
  return app;
}

// ── Trigger: port ────────────────────────────────────────────────────────────

test('port click opens popover with tag "port"', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.clickPort('authorize()');
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open' });
  expect(await popover.tag()).toBe('port');
  await popover.cancel();
  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
});

// ── Trigger: component header dblclick ───────────────────────────────────────

test('component header dblclick opens popover with tag "cmp"', async ({ page }) => {
  const app = await loadDiff(page);
  const cmp = await (await app.diagram()).component('OrderService');
  await cmp.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open after dblclick' });
  expect(await popover.tag()).toBe('cmp');
  await popover.cancel();
  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
});

// ── Trigger: internal header click ───────────────────────────────────────────

test('internal header click opens popover with tag "internal"', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.toggleExpand();
  await app.env.waitUntil(async () => pay.isExpanded(), { message: 'PaymentService did not expand' });
  const gateway = await pay.block('IGateway');
  await gateway.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open on internal header' });
  expect(await popover.tag()).toBe('internal');
  await popover.cancel();
  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
});

// ── Trigger: member row click ─────────────────────────────────────────────────

test('member row click opens popover with tag "member"', async ({ page }) => {
  const app = await loadDiff(page);
  const pay = await (await app.diagram()).component('PaymentService');
  await pay.toggleExpand();
  await app.env.waitUntil(async () => pay.isExpanded(), { message: 'PaymentService did not expand' });
  // Class bodies render with the card — no per-symbol expansion step.
  const gateway = await pay.block('IGateway');
  await app.env.waitUntil(async () => (await gateway.rows()).length > 0, { message: 'IGateway rows never appeared' });
  const member = await gateway.row('refund');
  await member.comment();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open on member click' });
  expect(await popover.tag()).toBe('member');
  await popover.cancel();
  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
});

// ── Trigger: edge click ───────────────────────────────────────────────────────

test('edge click opens popover with tag "edge"', async ({ page }) => {
  const app = await loadDiff(page);
  await app.commentOnFirstEdge();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open on edge click' });
  expect(await popover.tag()).toBe('edge');
  await popover.cancel();
  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
});

// ── Comment button disabled until text typed ─────────────────────────────────

test('comment button is disabled until text is typed', async ({ page }) => {
  const app = await loadDiff(page);
  const cmp = await (await app.diagram()).component('OrderService');
  await cmp.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open' });
  expect(await popover.isCommentDisabled()).toBe(true);
  await popover.type('looks good');
  expect(await popover.isCommentDisabled()).toBe(false);
  await popover.cancel();
  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
});

// ── Submit creates a marker, and makes it the active one ─────────────────────

test('submit creates a marker and makes it active', async ({ page }) => {
  const app = await loadDiff(page);

  const baselineMarkers = await app.markerCount();

  const cmp = await (await app.diagram()).component('OrderService');
  await cmp.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open' });
  await popover.type('LGTM on this design');
  await popover.submit();

  await app.env.waitUntil(async () => (await app.markerCount()) === baselineMarkers + 1, {
    message: 'marker count did not increment',
  });

  const newMarker = await app.markerByNumber(String(baselineMarkers + 1));
  expect(await newMarker.isActive()).toBe(true);
});

// ── Cancel closes popover, no marker ─────────────────────────────────────────

test('cancel closes popover and adds no marker', async ({ page }) => {
  const app = await loadDiff(page);
  const baseline = await app.markerCount();

  const cmp = await (await app.diagram()).component('CheckoutAPI');
  await cmp.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open' });
  await popover.type('nope');
  await popover.cancel();

  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close' });
  expect(await app.markerCount()).toBe(baseline);
});

// ── Escape closes popover, no marker ─────────────────────────────────────────

test('Escape closes popover and adds no marker', async ({ page }) => {
  const app = await loadDiff(page);
  const baseline = await app.markerCount();

  const cmp = await (await app.diagram()).component('CheckoutAPI');
  await cmp.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open' });
  await popover.pressEscape();

  await app.env.waitUntil(async () => !(await popover.isOpen()), { message: 'popover did not close after Escape' });
  expect(await app.markerCount()).toBe(baseline);
});

// ── ⌘/Ctrl+Enter submits ──────────────────────────────────────────────────────

test('Meta+Enter submits the comment', async ({ page }) => {
  const app = await loadDiff(page);
  const baseline = await app.markerCount();

  const cmp = await (await app.diagram()).component('OrderService');
  await cmp.commentOnHeader();
  const popover = app.commentPopover();
  await app.env.waitUntil(async () => popover.isOpen(), { message: 'popover did not open' });
  await popover.type('via keyboard');
  await popover.submitWithKeyboard();

  await app.env.waitUntil(async () => (await app.markerCount()) === baseline + 1, {
    message: 'marker count did not increment after Meta+Enter',
  });
});

// ── Click marker pill → becomes active ───────────────────────────────────────

test('clicking a marker pill on canvas makes it active', async ({ page }) => {
  const app = await loadDiff(page);
  // diffGraph seeds 3 markers; pick marker 1
  const marker1 = await app.markerByNumber('1');
  await marker1.click();
  await app.env.waitUntil(async () => marker1.isActive(), { message: 'marker 1 did not become active' });
  expect(await marker1.isActive()).toBe(true);
});
