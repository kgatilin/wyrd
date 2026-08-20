import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { ArchReport } from '../../domain/archReport';
import type { GitDiff } from '../../domain/gitDiff';
import type { UIGraph } from '../../types';

/**
 * Two packages that depend on each other — the shape a group cycle is read
 * off — plus the symbol an inversion row points at.
 */
const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'internal/serve',
      name: 'serve',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        {
          id: 'internal/serve.Warm',
          kind: 'func',
          name: 'Warm',
          exported: true,
          sourceFile: 'warm.go',
          members: [],
        },
      ],
      ports: [{ id: 'internal/serve.out', side: 'right', y: 58, name: 'use http', kind: 'out' }],
    },
    {
      id: 'internal/adapter/http',
      name: 'http',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        {
          id: 'internal/adapter/http.Server',
          kind: 'class',
          name: 'Server',
          exported: true,
          sourceFile: 'server.go',
          members: [],
        },
      ],
      ports: [{ id: 'internal/adapter/http.in', side: 'left', y: 58, name: 'serve', kind: 'in' }],
    },
  ],
  edges: [
    {
      id: 'e-serve-http',
      from: 'internal/serve',
      to: 'internal/adapter/http',
      fromPort: 'internal/serve.out',
      toPort: 'internal/adapter/http.in',
      label: 'uses',
    },
    {
      id: 'e-http-serve',
      from: 'internal/adapter/http',
      to: 'internal/serve',
      fromPort: 'internal/adapter/http.in',
      toPort: 'internal/serve.out',
      label: 'uses',
    },
  ],
  relations: [
    {
      id: 'r1',
      kind: 'uses',
      fromComponentId: 'internal/adapter/http',
      fromInternalId: 'internal/adapter/http.Server',
      toComponentId: 'internal/serve',
      toInternalId: 'internal/serve.Warm',
    },
  ],
  comments: [],
};

const cycleEdges = [
  { from: 'internal/serve', to: 'internal/adapter/http' },
  { from: 'internal/adapter/http', to: 'internal/serve' },
];

/** A repo-mode report: a cycle, a god file, a god package, and two clean sections. */
const repoReport: ArchReport = {
  schema: 'archai.archreview/v1',
  mode: 'repo',
  sections: [
    {
      id: 'group_cycles',
      title: 'Group cycles',
      severity: 60,
      state: 'flag',
      count: 1,
      summary: '1 group cycle',
      items: [
        {
          text: 'internal/adapter ↔ internal/serve',
          detail: 'weakest link internal/serve → internal/adapter/http, 1 dependency — cut there',
          target: {
            edges: cycleEdges,
            edge: cycleEdges[0],
            componentId: 'internal/serve',
          },
        },
      ],
    },
    {
      id: 'inversions',
      title: 'Inversions',
      severity: 50,
      state: 'ok',
      count: 0,
      summary: 'no inversions (F0 0.03, layered)',
      items: [],
    },
    {
      id: 'god_files',
      title: 'God files',
      severity: 40,
      state: 'flag',
      count: 1,
      summary: '1 file is structurally overloaded',
      items: [
        {
          text: 'internal/adapter/http/server.go — 31 declarations',
          detail: 'past the threshold of 20 — split the file',
          target: { file: 'internal/adapter/http/server.go', componentId: 'internal/adapter/http' },
        },
      ],
    },
    {
      id: 'god_packages',
      title: 'God packages',
      severity: 30,
      state: 'flag',
      count: 1,
      summary: '1 package is an outlier for coupling',
      items: [
        {
          text: 'internal/serve — degree 14',
          detail: '9 in, 5 out — ask for its latent domains',
          target: { componentId: 'internal/serve' },
        },
      ],
    },
    {
      id: 'unused_exports',
      title: 'Unused exports',
      severity: 10,
      state: 'flag',
      count: 3,
      summary: '3 exports have no caller outside their package',
      more: 2,
      items: [
        {
          text: 'internal/serve.Warm',
          detail: '0 callers (tests not in graph) outside its package — unexport it',
          tag: 'dead',
          target: { componentId: 'internal/serve', internalId: 'internal/serve.Warm' },
        },
      ],
    },
  ],
  totals: { packages: 2, edges: 2, components: 1 },
};

/** A review-mode report that also has something to say about its own limits. */
const reviewReport: ArchReport = {
  schema: 'archai.archreview/v1',
  mode: 'review',
  base: { ref: 'main', rev: 'cc451e9d0f' },
  sections: [
    {
      id: 'edges_new',
      title: 'New cross-package edges',
      severity: 60,
      state: 'flag',
      count: 1,
      summary: '1 new package dependency',
      items: [
        {
          text: 'internal/serve → internal/adapter/http',
          detail: 'infrastructure sits below adapters — invert it through an interface',
          tag: 'backward',
          target: { componentId: 'internal/serve', edge: cycleEdges[0] },
        },
      ],
    },
    {
      id: 'hotspot_growth',
      title: 'Hotspot growth',
      severity: 20,
      state: 'flag',
      count: 1,
      summary: '1 hotspot grew',
      items: [
        {
          text: 'internal/adapter/http/server.go — 31 declarations (+4)',
          detail: 'already past the god-file threshold — put the new declarations elsewhere',
          target: { file: 'internal/adapter/http/server.go', componentId: 'internal/adapter/http' },
        },
      ],
    },
    {
      id: 'orphans_new',
      title: 'Orphans',
      severity: 10,
      state: 'ok',
      count: 0,
      summary: 'everything this branch added is referenced',
      items: [],
    },
  ],
  totals: { packages: 2, edges: 2, components: 1 },
  index: { ready: false, indexing: true, embedded: 120, embeddable: 430, denseAvailable: true },
  warnings: ['base: merge-base unavailable (falling back to repo mode)'],
};

const gitDiff: GitDiff = {
  schema: 'archai.gitdiff/1',
  branch: 'feature',
  baseRef: 'main',
  baseRev: 'abc1234',
  stats: { files: 1, insertions: 1, deletions: 0 },
  files: [
    {
      path: 'internal/adapter/http/server.go',
      status: 'M',
      insertions: 1,
      deletions: 0,
      patch: ['@@ -1,1 +1,2 @@', ' package http', '+// here'].join('\n'),
    },
  ],
};

const READY_STATUS = {
  ready: true,
  indexing: false,
  dense_available: true,
  embedded: 2,
  embeddable: 2,
  message: 'Ready.',
};

const DOMAINS = {
  node_count: 2,
  structural: { k: 1, cluster_count: 1, dominant_share: 1, modularity: 0.1, clusters: [{ id: 0, size: 2, members: ['fn:internal/serve.Warm', 'type:internal/adapter/http.Server'] }] },
  semantic: { k: 1, cluster_count: 1, dominant_share: 1, modularity: 0.2, clusters: [{ id: 0, size: 2, members: ['fn:internal/serve.Warm', 'type:internal/adapter/http.Server'] }] },
  agreement: { ami: 1, nmi: 1, verdict: 'aligned' },
  glue: { top_fan_in: [], glue_cluster: 0, note: '' },
  dropped_nodes: 0,
};

interface LensCall {
  name: string;
  args: Record<string, unknown>;
}

async function mount(report: ArchReport = repoReport) {
  const asked: string[] = [];
  const lensCalls: LensCall[] = [];
  const env = await mountAppDom(graph, {
    report: (base) => {
      asked.push(base);
      return report;
    },
    gitDiff: () => gitDiff,
    source: () => ({ content: 'package http\n\n// here\n' }),
    lens: (name, args) => {
      lensCalls.push({ name, args });
      return name === 'status' ? READY_STATUS : DOMAINS;
    },
  });
  const app = await env.load(AppHarness);
  await app.waitForLoaded();
  const panel = app.report();
  await panel.open();
  await panel.waitForReport();
  return { env, app, panel, asked, lensCalls };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('the review report', () => {
  it('renders the server’s sections in order and collapses a clean one to a line', async () => {
    const { panel } = await mount();

    expect(await panel.sectionTitles()).toEqual([
      'Group cycles',
      'Inversions',
      'God files',
      'God packages',
      'Unused exports',
    ]);

    const clean = await panel.section('inversions');
    expect(await clean.state()).toBe('ok');
    expect(await clean.summary()).toBe('no inversions (F0 0.03, layered)');
    expect(await clean.rows()).toHaveLength(0);

    const cycles = await panel.section('group_cycles');
    expect(await cycles.state()).toBe('flag');
    expect(await cycles.count()).toBe(1);
    expect(await cycles.rowTexts()).toEqual(['internal/adapter ↔ internal/serve']);
  });

  it('says how many rows a capped section left out, and how big the graph is', async () => {
    const { panel } = await mount();
    expect(await (await panel.section('unused_exports')).more()).toBe('and 2 more');
    expect(await panel.totals()).toBe('2 packages · 2 dependencies');
  });

  it('offers Refresh and nothing else — no Embed, no embedding files', async () => {
    const { panel, asked } = await mount();
    expect(await panel.buttons()).toEqual(['Refresh']);

    await panel.refresh();
    await panel.env.waitUntil(async () => asked.length >= 2, {
      message: 'Refresh never asked the daemon again',
    });
    expect(asked).toEqual(['main', 'main']);
  });

  it('costs nothing to reopen: the daemon is asked once', async () => {
    const { panel, asked } = await mount();
    expect(asked).toHaveLength(1);

    await panel.close();
    await panel.open();
    await panel.waitForReport();
    // The daemon rebuilds both package models and the model diff per request.
    expect(asked).toHaveLength(1);
  });

  it('names the comparison in review mode, and shows what did not run', async () => {
    const { panel } = await mount(reviewReport);

    expect(await panel.mode()).toBe('this branch');
    expect(await panel.base()).toBe('vs main @ cc451e9');
    // A section that could not run must not read as a section that found
    // nothing.
    expect((await panel.warnings())[0]).toContain('merge-base unavailable');
    expect(await panel.indexNote()).toContain('indexing 120/430');
  });

  it('keeps the index line off a report that does not carry one', async () => {
    const { panel } = await mount();
    expect(await panel.mode()).toBe('whole repository');
    expect(await panel.base()).toBeNull();
    expect(await panel.indexNote()).toBeNull();
    expect(await panel.warnings()).toEqual([]);
  });

  it('reports a read failure instead of an empty report', async () => {
    const env = await mountAppDom(graph); // no report responder
    const app = await env.load(AppHarness);
    await app.waitForLoaded();
    const panel = app.report();
    await panel.open();
    await env.waitUntil(async () => (await panel.error()) != null, {
      message: 'the panel never reported the failed read',
    });
    expect(await panel.mode()).toBeNull();
  });
});

describe('a report row is a gesture on the canvas', () => {
  it('accents every edge of a cycle, and puts the accent down when clicked again', async () => {
    const { env, app, panel } = await mount();
    const diagram = await app.diagram();
    expect(await diagram.highlightedEdgeIds()).toEqual([]);

    const row = await (await panel.section('group_cycles')).row('internal/adapter ↔ internal/serve');
    await row.click();

    await env.waitUntil(async () => (await diagram.highlightedEdgeIds()).length === 2, {
      message: 'the cycle’s edges were never accented',
    });
    expect((await diagram.highlightedEdgeIds()).sort()).toEqual(['e-http-serve', 'e-serve-http']);

    await row.click();
    await env.waitUntil(async () => (await diagram.highlightedEdgeIds()).length === 0, {
      message: 'clicking the row again never took the accent off',
    });
  });

  it('accents a single new dependency without touching the rest', async () => {
    const { env, app, panel } = await mount(reviewReport);
    const diagram = await app.diagram();

    await (await (await panel.section('edges_new')).row('internal/serve → internal/adapter/http')).click();

    await env.waitUntil(async () => (await diagram.highlightedEdgeIds()).length === 1, {
      message: 'the new dependency was never accented',
    });
    expect(await diagram.highlightedEdgeIds()).toEqual(['e-serve-http']);
  });

  it('opens a symbol’s wiring from its row', async () => {
    const { env, app, panel } = await mount();
    const wiring = app.symbolWiring();
    expect(await wiring.isPresent()).toBe(false);

    await (await (await panel.section('unused_exports')).row('internal/serve.Warm')).click();

    await env.waitUntil(() => wiring.isPresent(), { message: 'wiring panel never opened' });
    expect(await wiring.anchorName()).toContain('Warm');
  });

  it('reads a repo-mode file, where there is no base and so no patch', async () => {
    const { env, app, panel } = await mount();
    const row = await (await panel.section('god_files')).row(
      'internal/adapter/http/server.go — 31 declarations'
    );
    // The patch stays on the row; it is the reading that comes first here.
    expect(await row.actions()).toEqual(['diff', 'focus']);

    await row.click();
    const drawer = app.sourceDrawer();
    await env.waitUntil(async () => (await drawer.path()) != null, {
      message: 'the source drawer never opened',
    });
    expect(await drawer.path()).toBe('internal/adapter/http/server.go');
  });

  it('opens a changed file at its patch in review mode', async () => {
    const { env, app, panel } = await mount(reviewReport);
    const row = await (await panel.section('hotspot_growth')).row(
      'internal/adapter/http/server.go — 31 declarations (+4)'
    );
    expect(await row.actions()).toEqual(['source', 'focus']);

    await row.click();
    const fileDiff = app.fileDiff();
    await env.waitUntil(async () => (await fileDiff.activePath()) != null, {
      message: 'the diff overlay never showed the patch',
    });
    expect(await fileDiff.activePath()).toBe('internal/adapter/http/server.go');

    await row.clickAction('source');
    const drawer = app.sourceDrawer();
    await env.waitUntil(async () => (await drawer.path()) != null, {
      message: 'the source drawer never opened',
    });
    expect(await drawer.path()).toBe('internal/adapter/http/server.go');
  });

  it('sends a god package to the domains canvas, scoped to that package', async () => {
    const { env, app, panel, lensCalls } = await mount();
    const row = await (await panel.section('god_packages')).row('internal/serve — degree 14');
    expect(await row.actions()).toEqual(['domains']);

    await row.clickAction('domains');

    const domains = app.domains();
    await env.waitUntil(() => domains.isPresent(), { message: 'the domains canvas never opened' });
    // The panel gets out of the way of the grid it just asked for.
    expect(await panel.isPresent()).toBe(false);

    await env.waitUntil(async () => lensCalls.some((call) => call.name === 'latent_domains'), {
      message: 'the canvas never asked for the clustering',
    });
    const call = lensCalls.find((entry) => entry.name === 'latent_domains');
    expect(call?.args.selector).toEqual({ package: 'internal/serve', include_subpackages: true });
  });

  it('lands on the package a row names', async () => {
    const { env, app, panel } = await mount();
    await (await (await panel.section('god_packages')).row('internal/serve — degree 14')).click();

    const diagram = await app.diagram();
    await env.waitUntil(async () => (await (await diagram.component('serve')).isFocused()), {
      message: 'the package was never focused on the canvas',
    });
  });
});
