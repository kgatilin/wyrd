import { test, expect, type Page } from '@playwright/test';
import { PlaywrightEnvironment, routeGraph } from '../testing/harness/playwright-env';
import { AppHarness } from '../testing/harness/app.harness';
import { EventCanvasHarness } from '../testing/harness/event-canvas.harness';
import { diffGraph } from '../testing/fixtures';

/**
 * The event model the daemon serves, in the shape the endpoint answers with.
 * Two imported AsyncAPI documents and one native declaration, which is the
 * mixed case the canvas exists to draw: `router` calls into `connectors`, and
 * `audit` folds what `connectors` appends.
 */
const eventModel = {
  components: [
    {
      id: 'audit',
      owns: 'audit',
      description: 'Read models over the event log',
      source_file: '/repo/audit/.arch/events.yaml',
      partition_key: ['scope'],
      has_state: true,
      state_events: [{ kind: 'connectors.event.call.completed', pattern: 'svc.connectors.{scope}.completed' }],
    },
    {
      id: 'connectors',
      owns: 'connectors',
      description: 'Event-log port connectors; one instance per configured backend.',
      source: 'asyncapi',
      source_file: '/repo/connectors/.arch/asyncapi.yaml',
      partition_key: ['scope'],
      has_state: false,
      instances: ['alpha', 'beta'],
      inputs: [
        {
          kind: 'connectors.command.call',
          pattern: 'svc.connectors.{group}.{scope}.call',
          description: 'Request one operation from the configured backend',
          role: 'command',
        },
      ],
      outputs: [
        {
          kind: 'connectors.event.call.completed',
          pattern: 'svc.connectors.{group}.{scope}.completed',
          role: 'event',
        },
      ],
    },
    {
      id: 'router',
      owns: 'router',
      source: 'asyncapi',
      source_file: '/repo/router/.arch/asyncapi.yaml',
      partition_key: ['session'],
      has_state: false,
      outputs: [
        {
          kind: 'connectors.command.call',
          pattern: 'svc.connectors.alpha.{session}.call',
          role: 'call',
          instances: ['north'],
        },
      ],
    },
  ],
  flows: [
    { from: 'connectors', to: 'audit', kind: 'connectors.event.call.completed', trigger: false, health: 'ok' },
    { from: 'router', to: 'connectors', kind: 'connectors.command.call', trigger: true, health: 'ok' },
  ],
  kinds: [
    {
      name: 'connectors.command.call',
      pattern: 'svc.connectors.{group}.{scope}.call',
      description: 'Request one operation from the configured backend',
      partition_key: ['scope'],
      delivery: 'broadcast',
      health: 'ok',
      class: 'command',
      owner: 'connectors',
      producers: ['router'],
      triggers: ['connectors'],
      schema: { type: 'object', properties: { op: { type: 'string' }, tries: { type: 'integer' } } },
      example: { op: 'string', tries: 0 },
    },
    {
      name: 'connectors.event.call.completed',
      pattern: 'svc.connectors.{group}.{scope}.completed',
      partition_key: ['scope'],
      delivery: 'broadcast',
      health: 'ok',
      class: 'event',
      owner: 'connectors',
      producers: ['connectors'],
      folders: ['audit'],
    },
  ],
};

async function openCanvas(page: Page, model: unknown = eventModel) {
  await routeGraph(page, diffGraph);
  await page.route('**/api/plugins/events/model', (route) => route.fulfill({ json: model as object }));
  await page.goto('/');

  const env = new PlaywrightEnvironment(page);
  const app = await env.load(AppHarness);
  await app.waitForLoaded();

  const canvas = await env.load(EventCanvasHarness);
  await canvas.open();
  return canvas;
}

test('the canvas draws a node per component and one edge per pair', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  expect((await canvas.nodeNames()).sort()).toEqual(['audit', 'connectors', 'router']);
  // Edge labels are the last two segments of the kind — the leading ones
  // repeat the component the edge starts at.
  expect((await canvas.linkLabels()).sort()).toEqual(['call.completed', 'command.call']);
});

test('the legend counts what was read, and says how much of it is imported', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  const stats = await canvas.stats();
  expect(stats).toContain('3 components');
  expect(stats).toContain('2 kinds');
  expect(stats).toContain('2 imported');
});

test('selecting a component accents what it touches and opens its three lists', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  await canvas.clickNode('connectors');

  expect(await canvas.isDetailOpen()).toBe(true);
  expect(await canvas.detailTitle()).toBe('connectors');
  // connectors, plus router upstream and audit downstream.
  expect(await canvas.accentedNodeCount()).toBe(3);

  const sections = await canvas.detailSections();
  expect(sections.some((s) => s.startsWith('Inputs'))).toBe(true);
  expect(sections.some((s) => s.startsWith('Outputs'))).toBe(true);

  const facts = await canvas.facts();
  expect(facts.instances).toBe('alpha, beta');
  expect(facts.source).toContain('AsyncAPI');
});

test('a native declaration is not labelled as imported', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  await canvas.clickNode('audit');

  const facts = await canvas.facts();
  expect(facts.source).toBeUndefined();
  expect(facts.file).toContain('events.yaml');
});

test('a kind opens from a component, and names everyone it reaches', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  await canvas.clickNode('connectors');
  await canvas.clickDetailKind('connectors.command.call');

  expect(await canvas.detailTitle()).toBe('connectors.command.call');
  const facts = await canvas.facts();
  expect(facts.subject).toBe('svc.connectors.{group}.{scope}.call');
  expect(facts.subjects).toBeUndefined(); // one address, so no list
  expect(facts.class).toBe('command');
  // The coordinates the document declared, not every {slot} of the address:
  // {group} selects the port instance and keys nothing.
  expect(facts.partition).toBe('scope');
});

test('a kind shows the payload as an object before it shows the schema', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  await canvas.clickNode('connectors');
  await canvas.clickDetailKind('connectors.command.call');

  // The example comes first: a schema states what may be on the wire, and a
  // reader opening a kind wants to see what is on it.
  const sections = await canvas.detailSections();
  expect(sections).toContain('Payload example');
  expect(sections).toContain('Payload schema');
  expect(sections.indexOf('Payload example')).toBeLessThan(sections.indexOf('Payload schema'));

  const [example, schema] = await canvas.payloads();
  expect(JSON.parse(example)).toEqual({ op: 'string', tries: 0 });
  expect(JSON.parse(schema).properties.op.type).toBe('string');
});

test('the legend names each unhealthy state rather than totalling them', async ({ page }) => {
  const kinds = [
    { ...eventModel.kinds[0], health: 'starved' },
    { ...eventModel.kinds[1], health: 'orphan' },
    // Appended by audit and observed by nobody, so it travels no edge and the
    // diagram has nothing to draw for it.
    { name: 'audit.event.rolled', pattern: 'svc.audit.{scope}.rolled', health: 'orphan', producers: ['audit'] },
  ];
  const canvas = await openCanvas(page, { ...eventModel, kinds });
  await canvas.waitForDiagram();

  // "3 unreached" would name neither finding: kinds nobody appends and kinds
  // nobody observes are opposite problems.
  const stats = await canvas.stats();
  expect(stats).toContain('2 orphan');
  expect(stats).toContain('1 starved');

  // The chip is the way in: an orphan sits on no edge, so the diagram cannot
  // point at it and the list has to.
  await canvas.clickStat('2 orphan');
  expect(await canvas.detailTitle()).toBe('2 orphan');
  expect(await canvas.detailKinds()).toEqual(['connectors.event.call.completed', 'audit.event.rolled']);

  // Opening one accents the component that declared it, which is the whole
  // answer to "where do I look" for a kind with no edge of its own.
  await canvas.clickDetailKind('audit.event.rolled');
  expect(await canvas.detailTitle()).toBe('audit.event.rolled');
  expect(await canvas.accentedNodeCount()).toBe(1);
});

test('Esc puts the detail down first and the canvas second', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  await canvas.clickNode('router');
  expect(await canvas.isDetailOpen()).toBe(true);

  await page.keyboard.press('Escape');
  expect(await canvas.isDetailOpen()).toBe(false);
  expect(await canvas.isPresent()).toBe(true);

  await page.keyboard.press('Escape');
  await canvas.env.waitUntil(async () => !(await canvas.isPresent()), {
    message: 'the canvas stayed up after a second Escape',
  });
});

test('the diagram opens fitted to the pane, and the zoom is adjustable', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  // Whatever the pane's size, the diagram opens inside it and never above 1:1
  // — scaling a small model up to fill a screen makes it look like a large one.
  const fitted = parseInt(await canvas.zoom(), 10);
  expect(fitted).toBeGreaterThan(0);
  expect(fitted).toBeLessThanOrEqual(100);

  // The readout is a rounded view of the real scale, so this asserts the
  // direction and the round trip rather than arithmetic on a rounded number.
  await canvas.zoomIn();
  expect(parseInt(await canvas.zoom(), 10)).toBeGreaterThan(fitted);

  await canvas.fit();
  expect(parseInt(await canvas.zoom(), 10)).toBe(fitted);
});

test('the flow runs downwards, and the toggle turns it left to right', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  // Which direction reads better is a property of the model — a chain reads
  // across, a hub reads down — so the canvas offers both rather than picking.
  const down = await canvas.nodeBoxes();
  expect(down.router.y).toBeLessThan(down.connectors.y);

  await canvas.toggleDirection();
  await canvas.env.waitUntil(
    async () => {
      const boxes = await canvas.nodeBoxes();
      return boxes.router != null && boxes.connectors != null && boxes.router.x < boxes.connectors.x;
    },
    { message: 'the toggle did not re-lay the flow left to right' }
  );

  // Across, the two sit on one line rather than one above the other.
  const across = await canvas.nodeBoxes();
  expect(Math.abs(across.router.y - across.connectors.y)).toBeLessThan(40);
});

test('a plain wheel scrolls the pane and Cmd/Ctrl+wheel zooms it', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();

  // The review canvas's division of labour: the wheel moves the diagram, the
  // modifier is what makes it a zoom. A reader who learned one should not find
  // the other scrolling where it zoomed.
  const zoomBefore = await canvas.zoom();
  const before = await canvas.scrollPosition();
  await canvas.wheel(160);
  await canvas.env.waitUntil(async () => (await canvas.scrollPosition()).top > before.top, {
    message: 'a plain wheel did not scroll the pane',
  });
  expect(await canvas.zoom()).toBe(zoomBefore);

  await canvas.ctrlWheelZoom(-120);
  await canvas.env.waitUntil(async () => (await canvas.zoom()) !== zoomBefore, {
    message: 'ctrl+wheel did not change the zoom',
  });
  // The gesture is the canvas's, not the page's.
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
});

test('the background pans under a drag, and the drag does not clear the selection', async ({ page }) => {
  const canvas = await openCanvas(page);
  await canvas.waitForDiagram();
  expect(await canvas.cursor()).toBe('grab');

  await canvas.clickNode('router');
  expect(await canvas.isDetailOpen()).toBe(true);

  const before = await canvas.scrollPosition();
  await canvas.pan(-160, -120);
  await canvas.env.waitUntil(
    async () => {
      const after = await canvas.scrollPosition();
      return Math.abs(after.left - before.left) > 20 || Math.abs(after.top - before.top) > 20;
    },
    { message: 'dragging the background did not pan the diagram' }
  );
  // The click that ends a pan is part of the pan, not a click on the backdrop.
  expect(await canvas.isDetailOpen()).toBe(true);
});

test('a kind on one address per instance lists them all', async ({ page }) => {
  // A port family is one kind travelling one address per instance. Naming the
  // first of them as "the" subject is how an edge into one instance reads as
  // an edge into every sibling.
  const kinds = [
    {
      ...eventModel.kinds[0],
      subjects: [
        'svc.connectors.alpha.{scope}.call',
        'svc.connectors.beta.{scope}.call',
      ],
    },
    eventModel.kinds[1],
  ];
  const canvas = await openCanvas(page, { ...eventModel, kinds });
  await canvas.waitForDiagram();

  await canvas.clickNode('connectors');
  await canvas.clickDetailKind('connectors.command.call');

  const facts = await canvas.facts();
  expect(facts.subjects).toContain('svc.connectors.alpha.{scope}.call');
  expect(facts.subjects).toContain('svc.connectors.beta.{scope}.call');
});

test('an empty model says where declarations go instead of drawing nothing', async ({ page }) => {
  const canvas = await openCanvas(page, { components: [], flows: [], kinds: [] });

  await canvas.env.waitUntil(async () => (await canvas.notice()) !== null, {
    message: 'no notice for an empty model',
  });
  expect(await canvas.notice()).toContain('.arch/asyncapi.yaml');
});
