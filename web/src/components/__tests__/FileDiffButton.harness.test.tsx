import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { mountAppDom } from '../../../testing/harness/dom-env';
import { AppHarness } from '../../../testing/harness/app.harness';
import type { GitDiff } from '../../domain/gitDiff';
import type { UIGraph } from '../../types';

/** One package under review: one file the branch changed, one it did not. */
const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  pr: {
    title: 'Wire the llm controller',
    branch: 'feature',
    agent: 'agent',
    summary: '',
    stats: { added: 0, removed: 0, changed: 1, comments: 0 },
  },
  reviewViews: [
    {
      id: 'changed',
      title: 'Changed packages',
      defaultScope: 'everything',
      componentIds: ['controllers/llm'],
      componentCount: 1,
    },
  ],
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
          diff: 'changed',
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
  await env.waitUntil(async () => (await card.fileCount()) >= 1, {
    message: 'card never expanded into its file containers',
  });
  return { env, app, card };
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('open a card file in the diff', () => {
  it('offers the button only on the files the review marks as changed', async () => {
    const { env, app, card } = await mount();
    // "Full package" draws the untouched files of a changed package too —
    // they are the ones that must NOT offer a patch.
    await app.setViewOption('Details', 'full');
    await env.waitUntil(async () => (await card.fileCount()) === 2, {
      message: 'card never drew its unchanged file',
    });

    expect(await (await card.file('controller.go')).hasOpenDiff()).toBe(true);
    expect(await (await card.file('config.go')).hasOpenDiff()).toBe(false);
  });

  it('opens the file diff at the patch of the clicked file', async () => {
    const { env, app, card } = await mount();
    await (await card.file('controller.go')).openDiff();

    const fileDiff = app.fileDiff();
    await env.waitUntil(async () => (await fileDiff.activePath()) != null, {
      message: 'diff overlay never showed a patch',
    });
    expect(await fileDiff.activePath()).toBe('controllers/llm/controller.go');
  });
});
