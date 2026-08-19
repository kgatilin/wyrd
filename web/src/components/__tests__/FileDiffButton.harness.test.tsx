import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { GitDiff } from '../../domain/gitDiff';
import type { UIGraph } from '../../types';

/** One package with two files: one the branch touched, one it did not. */
const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'controllers/llm',
      name: 'llm',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        {
          id: 'controllers/llm.New',
          kind: 'func',
          name: 'New',
          exported: true,
          sourceFile: 'controller.go',
          members: [],
        },
        {
          id: 'controllers/llm.Config',
          kind: 'class',
          name: 'Config',
          exported: true,
          sourceFile: 'config.go',
          members: [],
        },
      ],
      ports: [],
    },
  ],
  edges: [],
  comments: [],
};

const diff: GitDiff = {
  schema: 'archai.gitdiff/1',
  branch: 'feature',
  baseRef: 'main',
  baseRev: 'abc1234',
  stats: { files: 1, insertions: 1, deletions: 0 },
  files: [
    {
      path: 'controllers/llm/controller.go',
      status: 'M',
      insertions: 1,
      deletions: 0,
      patch: ['@@ -1,1 +1,2 @@', ' package llm', '+// wired here'].join('\n'),
    },
  ],
};

async function mount() {
  const env = await mountAppDom(graph, { gitDiff: () => diff });
  const app = await env.load(AppHarness);
  await app.waitForLoaded();
  const diagram = await app.diagram();
  const card = await diagram.component('llm');
  if (!(await card.isExpanded())) await card.toggleExpand();
  await env.waitUntil(async () => (await card.fileCount()) === 2, {
    message: 'card never expanded into its file containers',
  });
  return { env, app, card };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('open a card file in the diff', () => {
  it('opens the file diff at the patch of the clicked file', async () => {
    const { env, app, card } = await mount();
    await (await card.file('controller.go')).openDiff();

    const fileDiff = app.fileDiff();
    await env.waitUntil(async () => (await fileDiff.activePath()) != null, {
      message: 'diff overlay never showed a patch',
    });
    expect(await fileDiff.activePath()).toBe('controllers/llm/controller.go');
  });

  it("says a file is unchanged rather than showing another file's patch", async () => {
    const { env, app, card } = await mount();
    await (await card.file('config.go')).openDiff();

    const fileDiff = app.fileDiff();
    await env.waitUntil(async () => (await fileDiff.note()) != null, {
      message: 'diff overlay never answered',
    });
    expect(await fileDiff.note()).toContain('controllers/llm/config.go');
    expect(await fileDiff.note()).toContain('unchanged');
    expect(await fileDiff.activePath()).toBeNull();
  });

  it('offers the button on every file the card can name', async () => {
    const { card } = await mount();
    expect(await (await card.file('controller.go')).hasOpenDiff()).toBe(true);
    expect(await (await card.file('config.go')).hasOpenDiff()).toBe(true);
  });
});
