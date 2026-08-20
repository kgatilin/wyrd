import { afterEach, describe, expect, it } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { Diff, UIGraph } from '../../types';

/**
 * Three packages. `shared.bind` is called from both of the others — the glue
 * shape the grid exists to surface.
 */
function graphOf(options: { pr?: boolean; diffs?: Record<string, Diff> } = {}): UIGraph {
  const diff = (id: string) => options.diffs?.[id];
  return {
    schema: 'archai.uigraph/v0',
    ...(options.pr
      ? {
          pr: {
            title: 'Pull the glue out',
            branch: 'topic',
            agent: 'agent',
            summary: '',
            stats: { added: 1, removed: 0, changed: 1, comments: 0 },
          },
        }
      : {}),
    boundedContexts: [{ id: 'root', name: 'Root' }],
    components: [
      {
        id: 'transport',
        name: 'transport',
        tech: 'Go',
        desc: '',
        bc: 'root',
        internals: [
          { id: 'transport.Serve', kind: 'func', name: 'Serve', exported: true, diff: diff('transport.Serve'), members: [] },
          { id: 'transport.parseHeader', kind: 'func', name: 'parseHeader', exported: false, members: [] },
        ],
        ports: [],
      },
      {
        id: 'store',
        name: 'store',
        tech: 'Go',
        desc: '',
        bc: 'root',
        internals: [
          { id: 'store.Put', kind: 'func', name: 'Put', exported: true, diff: diff('store.Put'), members: [] },
          { id: 'store.Row', kind: 'class', name: 'Row', exported: true, members: [] },
        ],
        ports: [],
      },
      {
        id: 'shared',
        name: 'shared',
        tech: 'Go',
        desc: '',
        bc: 'root',
        internals: [{ id: 'shared.bind', kind: 'func', name: 'bind', exported: false, members: [] }],
        ports: [],
      },
    ],
    edges: [],
    relations: [
      rel('r1', 'transport', 'transport.Serve', 'shared', 'shared.bind'),
      rel('r2', 'transport', 'transport.parseHeader', 'shared', 'shared.bind'),
      rel('r3', 'store', 'store.Put', 'shared', 'shared.bind'),
      // Inside one cell — the card already shows it, so it is not a grid edge.
      rel('r4', 'transport', 'transport.Serve', 'transport', 'transport.parseHeader'),
    ],
    comments: [],
  };
}

function rel(id: string, fromPkg: string, from: string, toPkg: string, to: string) {
  return {
    id,
    kind: 'uses',
    fromComponentId: fromPkg,
    fromInternalId: from,
    toComponentId: toPkg,
    toInternalId: to,
  };
}

const READY_STATUS = {
  ready: true,
  indexing: false,
  dense_available: true,
  embedded: 5,
  embeddable: 5,
  message: 'Ready. 5 nodes indexed, 5 embedded.',
};

/**
 * Structure puts the shared helper with transport; semantics puts it with the
 * store. That disagreement is the off-diagonal cell.
 */
const DOMAINS = {
  node_count: 5,
  structural: {
    k: 2,
    cluster_count: 2,
    dominant_share: 0.6,
    modularity: 0.12,
    clusters: [
      { id: 0, size: 3, members: ['fn:transport.Serve', 'fn:transport.parseHeader', 'fn:shared.bind'] },
      { id: 1, size: 2, members: ['fn:store.Put', 'type:store.Row'] },
    ],
  },
  semantic: {
    k: 2,
    cluster_count: 2,
    dominant_share: 0.6,
    modularity: 0.47,
    clusters: [
      { id: 0, size: 2, members: ['fn:transport.Serve', 'fn:transport.parseHeader'] },
      { id: 1, size: 3, members: ['fn:store.Put', 'type:store.Row', 'fn:shared.bind'] },
    ],
  },
  agreement: { ami: 0.31, nmi: 0.58, verdict: 'latent_domains_glued' },
  glue: {
    top_fan_in: [{ node: 'fn:shared.bind', fan_in: 12, semantic_cluster: 1 }],
    glue_cluster: 1,
    note: 'Semantics splits into 2 balanced domains but structure collapses into one blob.',
  },
  dropped_nodes: 2,
};

interface LensCall {
  name: string;
  args: Record<string, unknown>;
}

/** Mount with a lens that records what was asked and answers with `answers`. */
async function mount(
  answers: { status?: unknown; domains?: unknown } = {},
  graph: UIGraph = graphOf()
) {
  const calls: LensCall[] = [];
  const env = await mountAppDom(graph, {
    lens: (name, args) => {
      calls.push({ name, args });
      if (name === 'status') return answers.status ?? READY_STATUS;
      return answers.domains ?? DOMAINS;
    },
  });
  const app = await env.load(AppHarness);
  await app.waitForLoaded();
  const domains = app.domains();
  await domains.open();
  return { app, domains, calls };
}

afterEach(cleanup);

describe('domains canvas', () => {
  it('draws the contingency grid with the agreeing clusters on the diagonal', async () => {
    const { domains } = await mount();
    await domains.waitForGrid();

    expect(await domains.rows()).toEqual(['S0', 'S1']);
    expect(await domains.columns()).toEqual(['M0', 'M1']);
    // Three occupied intersections out of four — the empty one collapses.
    expect(await domains.cellIds()).toEqual(['S0·M0', 'S0·M1', 'S1·M1']);
    expect(await (await domains.cell('S0·M0')).onDiagonal()).toBe(true);
    // The disagreement: one structural cluster reaching into a second column.
    expect(await (await domains.cell('S0·M1')).onDiagonal()).toBe(false);
  });

  it('draws each cell as a card of package headers and symbol rows', async () => {
    const { domains } = await mount();
    await domains.waitForGrid();

    const cell = await domains.cell('S0·M0');
    expect(await cell.packages()).toEqual(['transport']);
    // Alphabetical within a package, case-insensitively.
    expect(await cell.symbols()).toEqual(['parseHeader', 'Serve']);
    expect(await cell.size()).toBe('2');
  });

  it('opens a symbol’s wiring panel from its row', async () => {
    const { app, domains } = await mount();
    await domains.waitForGrid();

    const wiring = app.symbolWiring();
    expect(await wiring.isPresent()).toBe(false);

    await (await domains.cell('S0·M0')).clickSymbol('Serve');

    await app.env.waitUntil(() => wiring.isPresent(), { message: 'wiring panel never opened' });
    expect(await wiring.anchorName()).toContain('Serve');
    // The click belonged to the symbol, not to the cell behind it.
    expect(await (await domains.cell('S0·M0')).isSelected()).toBe(false);
  });

  it('names the glue in the header and badges it in its cell', async () => {
    const { domains } = await mount();
    await domains.waitForGrid();

    expect(await domains.glue()).toEqual(['bind×12']);
    const glueCell = await domains.cell('S0·M1');
    expect(await glueCell.glueSymbols()).toEqual(['bind']);
    expect(await glueCell.glueFanIn()).toEqual(['×12']);
  });

  it('reports the verdict and the two modularities in the header', async () => {
    const { domains } = await mount();
    await domains.waitForGrid();

    expect(await domains.verdict()).toBe('latent domains glued');
    const stats = (await domains.stats()).join(' | ');
    expect(stats).toContain('AMI 0.31');
    expect(stats).toContain('Q 0.12 struct / 0.47 sem');
    expect(stats).toContain('blob 60%');
    expect(stats).toContain('5 nodes · 2 dropped');
  });

  it('draws flow only for the selected cell, aggregated per cell pair', async () => {
    const { app, domains } = await mount();
    await domains.waitForGrid();

    // Nothing selected: no lines. All of them at once is the hairball.
    expect(await domains.edgeCount()).toBe(0);

    await (await domains.cell('S0·M0')).select();
    await app.env.waitUntil(async () => (await domains.edgeCount()) > 0, {
      message: 'selecting a cell drew no flow',
    });
    // Serve and parseHeader both call bind: one line, weight 2.
    expect(await domains.edgeLabels()).toEqual(['s0-m0 → s0-m1: 2']);

    await (await domains.cell('S0·M1')).select();
    await app.env.waitUntil(async () => (await domains.edgeCount()) === 2, {
      message: 'the glue cell should carry flow from both sides',
    });
  });
});

describe('domains canvas — readiness', () => {
  it('offers nothing but the diagnosis when there is no embedder', async () => {
    const { domains } = await mount({
      status: { ready: true, indexing: false, dense_available: false, message: 'Ready (lexical only).' },
    });

    expect(await domains.readinessKind()).toBe('no-embedder');
    expect(await domains.readiness()).toContain('needs an embedder');
    // Half a grid answers the wrong question, so no grid is drawn.
    expect(await domains.cellIds()).toEqual([]);
  });

  it('shows indexing progress instead of a partial grid', async () => {
    const { domains } = await mount({
      status: {
        ready: false,
        indexing: true,
        dense_available: true,
        embedded: 40,
        embeddable: 120,
        message: 'Indexing in progress: 40/120 nodes embedded.',
      },
    });

    expect(await domains.readinessKind()).toBe('indexing');
    expect(await domains.readiness()).toContain('40/120');
    expect(await domains.cellIds()).toEqual([]);
  });

  it('never asks the lens before the daemon says it is ready', async () => {
    const { calls } = await mount({
      status: { ready: true, indexing: false, dense_available: false, message: '' },
    });

    expect(calls.map((call) => call.name)).toEqual(['status']);
  });
});

describe('domains canvas — scope', () => {
  it('asks the whole repository when the worktree has no review base', async () => {
    const { domains, calls } = await mount();
    await domains.waitForGrid();

    expect(await domains.scope()).toBe('repo');
    expect(await domains.isScopeEnabled('diff region')).toBe(false);
    const lensCall = calls.find((call) => call.name === 'latent_domains');
    expect(lensCall?.args).toEqual({
      selector: { node_kinds: ['type', 'fn'] },
      include_members: true,
    });
  });

  it('asks for the change region on a branch, and re-asks when the scope changes', async () => {
    const { app, domains, calls } = await mount({}, graphOf({ pr: true }));
    await domains.waitForGrid();

    expect(await domains.scope()).toBe('diff region');
    expect(calls.find((call) => call.name === 'latent_domains')?.args).toEqual({
      selector: { diff: true },
      include_members: true,
    });

    await domains.setScope('repo');
    await app.env.waitUntil(
      async () => calls.filter((call) => call.name === 'latent_domains').length === 2,
      { message: 'the scope switch did not re-ask the lens' }
    );
    expect(calls.filter((call) => call.name === 'latent_domains')[1].args).toEqual({
      selector: { node_kinds: ['type', 'fn'] },
      include_members: true,
    });
  });

  it('scopes to one package', async () => {
    const { app, domains, calls } = await mount();
    await domains.waitForGrid();

    await domains.setPackageScope('store');
    await app.env.waitUntil(
      async () => calls.filter((call) => call.name === 'latent_domains').length === 2,
      { message: 'the package scope did not re-ask the lens' }
    );
    expect(calls.filter((call) => call.name === 'latent_domains')[1].args).toEqual({
      selector: { package: 'store', include_subpackages: true },
      include_members: true,
    });
  });
});

describe('domains canvas — diff overlay', () => {
  it('says how many cells the branch’s changes land in', async () => {
    const graph = graphOf({
      pr: true,
      diffs: { 'transport.Serve': 'changed', 'store.Put': 'added' },
    });
    const { domains } = await mount({}, graph);
    await domains.waitForGrid();

    // Two cells, two structural clusters, two semantic ones: the change cuts
    // across domains rather than staying local.
    expect((await domains.stats()).join(' | ')).toContain('2 changed in 2 cells (2×2)');
    expect(await (await domains.cell('S0·M0')).changedSymbols()).toEqual(['Serve']);
    expect(await (await domains.cell('S0·M1')).changedSymbols()).toEqual([]);
  });
});

describe('domains canvas — chrome', () => {
  it('closes back to the review canvas', async () => {
    const { app, domains } = await mount();
    await domains.waitForGrid();

    await domains.close();

    await app.env.waitUntil(async () => !(await domains.isPresent()), {
      message: 'the domains canvas never closed',
    });
    const diagram = await app.diagram();
    expect(await diagram.componentCount()).toBeGreaterThan(0);
  });

  it('reports a lens failure instead of an empty grid', async () => {
    const env = await mountAppDom(graphOf());
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const domains = app.domains();
    await domains.open();

    await app.env.waitUntil(async () => (await domains.readinessKind()) === 'error', {
      message: 'the canvas never reported the failed lens call',
    });
    expect(await domains.readiness()).toContain('failed');
  });
});
