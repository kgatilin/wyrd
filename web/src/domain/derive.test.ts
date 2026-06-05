import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { relatedIds, deriveChanges, deriveChangeStats, addInternalsOfExpanded, initialExpanded, seedMarkers, selectReviewGraph } from './derive';

function graph(overrides?: Partial<UIGraph>): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'bc1', name: 'Core' }],
    components: [
      { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] },
      { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
    ],
    edges: [{ id: 'e1', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' }],
    comments: [],
    ...overrides,
  };
}

describe('relatedIds', () => {
  it('returns null when nothing is focused', () => {
    expect(relatedIds(graph(), null)).toBeNull();
  });
  it('returns the focused node plus its edge neighbours', () => {
    expect(relatedIds(graph(), 'a')).toEqual(new Set(['a', 'b']));
  });
});

describe('deriveChanges', () => {
  it('walks the graph for diff-flagged elements', () => {
    const g = graph({
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
      ],
      edges: [],
    });
    const changes = deriveChanges(g);
    expect(changes).toHaveLength(1);
    expect(changes[0]).toMatchObject({ id: 'cmp-a', kind: 'added', name: 'A', cmp: 'a' });
  });

  it('preserves before/after signature strings for changed detail entries', () => {
    const g = graph({
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          internals: [
            {
              id: 'api.Client',
              kind: 'iface',
              name: 'Client',
              diff: 'changed',
              diffBefore: 'type Client interface { Do() string }',
              diffAfter: 'type Client interface { Do() int }',
              members: [
                {
                  id: 'api.Client.Do',
                  kind: 'method',
                  name: 'Do() int',
                  diff: 'changed',
                  diffBefore: 'Do() string',
                  diffAfter: 'Do() int',
                },
              ],
            },
          ],
          ports: [],
        },
      ],
      edges: [],
    });

    expect(deriveChanges(g)).toEqual([
      {
        id: 'int-api.Client',
        kind: 'changed',
        name: 'Client',
        where: 'iface - api',
        cmp: 'api',
        internal: 'api.Client',
        diffBefore: 'type Client interface { Do() string }',
        diffAfter: 'type Client interface { Do() int }',
      },
      {
        id: 'mem-api.Client.Do',
        kind: 'changed',
        name: 'Do() int',
        where: 'method - Client',
        cmp: 'api',
        internal: 'api.Client',
        member: 'api.Client.Do',
        diffBefore: 'Do() string',
        diffAfter: 'Do() int',
      },
    ]);
  });

  it('summarizes projected change entries by kind', () => {
    expect(deriveChangeStats([
      { id: 'add', kind: 'added', name: 'A', where: 'component - Core', cmp: 'a' },
      { id: 'rem', kind: 'removed', name: 'B', where: 'component - Core', cmp: 'b' },
      { id: 'chg', kind: 'changed', name: 'C', where: 'component - Core', cmp: 'c' },
      { id: 'pol', kind: 'policy', name: 'A -> B', where: 'policy', cmp: 'a' },
    ])).toEqual({ added: 1, removed: 1, changed: 1, policy: 1, total: 4 });
  });
});

describe('selectReviewGraph', () => {
  it('filters components by resolved review view component ids', () => {
    const g = graph({
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'everything', componentIds: ['a'], componentCount: 1 },
      ],
    });
    const selected = selectReviewGraph(g, 'api', 'everything');
    expect(selected.components.map((c) => c.id)).toEqual(['a']);
    expect(selected.edges).toHaveLength(0);
  });

  it('projects visible components into the selected server-resolved grouping', () => {
    const g = graph({
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'everything', componentIds: ['a', 'b'], componentCount: 2 },
      ],
      reviewGroupings: [
        {
          id: 'directory',
          title: 'Directory',
          groups: [
            { id: 'directory:root', title: 'Root', componentIds: ['a'], componentCount: 1 },
            { id: 'directory:internal', title: 'Internal', componentIds: ['b'], componentCount: 1 },
          ],
        },
      ],
    });

    const selected = selectReviewGraph(g, 'api', 'everything', 'directory');

    expect(selected.components.map((c) => [c.id, c.bc])).toEqual([
      ['a', 'directory:root'],
      ['b', 'directory:internal'],
    ]);
    expect(selected.boundedContexts.map((bc) => [bc.id, bc.name])).toEqual([
      ['directory:root', 'Root'],
      ['directory:internal', 'Internal'],
    ]);
  });

  it('filters internals and members for public API scopes', () => {
    const g = graph({
      components: [
        {
          id: 'a',
          name: 'A',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [],
          internals: [
            {
              id: 'a.Public',
              kind: 'class',
              name: 'Public',
              exported: true,
              members: [
                { id: 'a.Public.Kept', kind: 'method', name: 'Kept()', exported: true },
                { id: 'a.Public.hidden', kind: 'method', name: 'hidden()' },
              ],
            },
            {
              id: 'a.private',
              kind: 'class',
              name: 'private',
              members: [{ id: 'a.private.Helper', kind: 'method', name: 'Helper()', exported: true }],
            },
          ],
        },
      ],
      edges: [],
    });
    const selected = selectReviewGraph(g, null, 'all_public_api');
    expect(selected.components).toHaveLength(1);
    expect(selected.components[0].internals.map((i) => i.id)).toEqual(['a.Public']);
    expect(selected.components[0].internals[0].members.map((m) => m.id)).toEqual(['a.Public.Kept']);
  });

  it('filters ports and dependency edges to public surface flags in public API scopes', () => {
    const g = graph({
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [
            { id: 'api:out:storage', side: 'right', kind: 'out', name: 'use storage', public: true },
            { id: 'api:out:cache', side: 'right', kind: 'out', name: 'use cache' },
          ],
          internals: [{ id: 'api.NewRepository', kind: 'func', name: 'NewRepository()', exported: true, members: [] }],
        },
        {
          id: 'storage',
          name: 'storage',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [{ id: 'storage:in:Repository', side: 'left', kind: 'in', name: 'Repository', public: true }],
          internals: [{ id: 'storage.Repository', kind: 'iface', name: 'Repository', exported: true, members: [] }],
        },
        {
          id: 'cache',
          name: 'cache',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [{ id: 'cache:in:Cache', side: 'left', kind: 'in', name: 'Cache' }],
          internals: [{ id: 'cache.Cache', kind: 'class', name: 'Cache', exported: true, members: [] }],
        },
      ],
      edges: [
        { id: 'api-storage', from: 'api', to: 'storage', fromPort: 'api:out:storage', toPort: 'storage:in:Repository', label: 'returns', public: true },
        { id: 'api-cache', from: 'api', to: 'cache', fromPort: 'api:out:cache', toPort: 'cache:in:Cache', label: 'uses' },
      ],
    });

    const publicGraph = selectReviewGraph(g, null, 'all_public_api');

    expect(publicGraph.edges.map((e) => e.id)).toEqual(['api-storage']);
    expect(publicGraph.components.find((c) => c.id === 'api')?.ports.map((p) => p.id)).toEqual(['api:out:storage']);
    expect(publicGraph.components.find((c) => c.id === 'cache')?.ports).toEqual([]);

    const everything = selectReviewGraph(g, null, 'everything');
    expect(everything.edges.map((e) => e.id)).toEqual(['api-storage', 'api-cache']);
    expect(everything.components.find((c) => c.id === 'api')?.ports.map((p) => p.id)).toEqual([
      'api:out:storage',
      'api:out:cache',
    ]);
  });

  it('keeps exported package-level symbols in public API scopes', () => {
    const g = graph({
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [],
          internals: [
            { id: 'api.NewClient', kind: 'func', name: 'NewClient() Client', exported: true, members: [] },
            { id: 'api.helper', kind: 'func', name: 'helper()', members: [] },
            {
              id: 'api.Mode',
              kind: 'type',
              name: 'Mode : string',
              exported: true,
              members: [
                { id: 'api.Mode.ModeFast', kind: 'const', name: 'ModeFast', exported: true },
                { id: 'api.Mode.modeSlow', kind: 'const', name: 'modeSlow' },
              ],
            },
            { id: 'api.DefaultPort', kind: 'const', name: 'DefaultPort : int', exported: true, members: [] },
            { id: 'api.DefaultClient', kind: 'var', name: 'DefaultClient : Client', exported: true, members: [] },
            { id: 'api.ErrMissing', kind: 'error', name: 'ErrMissing', exported: true, members: [] },
          ],
        },
      ],
      edges: [],
    });

    const selected = selectReviewGraph(g, null, 'all_public_api');

    expect(selected.components).toHaveLength(1);
    expect(selected.components[0].internals.map((i) => i.id)).toEqual([
      'api.NewClient',
      'api.Mode',
      'api.DefaultPort',
      'api.DefaultClient',
      'api.ErrMissing',
    ]);
    expect(selected.components[0].internals.find((i) => i.id === 'api.Mode')?.members.map((m) => m.id)).toEqual([
      'api.Mode.ModeFast',
    ]);
  });

  it('filters internal implementation scope to private details and private dependencies', () => {
    const g = graph({
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [
            { id: 'api:out:storage', side: 'right', kind: 'out', name: 'use storage', public: true },
            { id: 'api:out:cache', side: 'right', kind: 'out', name: 'use cache' },
          ],
          internals: [
            {
              id: 'api.Client',
              kind: 'class',
              name: 'Client',
              exported: true,
              diff: 'changed',
              members: [
                { id: 'api.Client.Do', kind: 'method', name: 'Do()', exported: true, diff: 'changed' },
                { id: 'api.Client.helper', kind: 'method', name: 'helper()', diff: 'added' },
              ],
            },
            {
              id: 'api.worker',
              kind: 'class',
              name: 'worker',
              members: [{ id: 'api.worker.Run', kind: 'method', name: 'Run()', exported: true }],
            },
          ],
        },
        {
          id: 'storage',
          name: 'storage',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [{ id: 'storage:in:Repository', side: 'left', kind: 'in', name: 'Repository', public: true }],
          internals: [{ id: 'storage.Repository', kind: 'iface', name: 'Repository', exported: true, members: [] }],
        },
        {
          id: 'cache',
          name: 'cache',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [{ id: 'cache:in:Cache', side: 'left', kind: 'in', name: 'Cache' }],
          internals: [{ id: 'cache.cacheStore', kind: 'class', name: 'cacheStore', members: [] }],
        },
      ],
      edges: [
        { id: 'api-storage', from: 'api', to: 'storage', fromPort: 'api:out:storage', toPort: 'storage:in:Repository', label: 'returns', public: true },
        { id: 'api-cache', from: 'api', to: 'cache', fromPort: 'api:out:cache', toPort: 'cache:in:Cache', label: 'uses' },
      ],
    });

    const selected = selectReviewGraph(g, null, 'internal_implementation', null, { impactMode: 'review_view' });

    expect(selected.components.map((c) => c.id)).toEqual(['api', 'cache']);
    expect(selected.components.find((c) => c.id === 'api')?.ports.map((p) => p.id)).toEqual(['api:out:cache']);
    expect(selected.components.find((c) => c.id === 'api')?.internals.map((i) => [i.id, i.diff])).toEqual([
      ['api.Client', undefined],
      ['api.worker', undefined],
    ]);
    expect(selected.components.find((c) => c.id === 'api')?.internals[0].members.map((m) => m.id)).toEqual([
      'api.Client.helper',
    ]);
    expect(selected.components.find((c) => c.id === 'cache')?.internals.map((i) => i.id)).toEqual(['cache.cacheStore']);
    expect(selected.edges.map((e) => e.id)).toEqual(['api-cache']);
  });

  it('can filter visible package details down to changed public API symbols and ports', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 3, removed: 0, changed: 1, comments: 0 } },
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [
            { id: 'api:out:new', side: 'right', kind: 'out', name: 'use new', public: true, diff: 'added' },
            { id: 'api:out:old', side: 'right', kind: 'out', name: 'use old', public: true },
            { id: 'api:out:private', side: 'right', kind: 'out', name: 'use private', diff: 'added' },
          ],
          internals: [
            {
              id: 'api.Application',
              kind: 'iface',
              name: 'Application',
              exported: true,
              diff: 'changed',
              members: [
                { id: 'api.Application.Sessions', kind: 'method', name: 'Sessions()', exported: true },
                { id: 'api.Application.SessionGraphRepository', kind: 'method', name: 'SessionGraphRepository()', exported: true, diff: 'added' },
              ],
            },
            {
              id: 'api.Client',
              kind: 'iface',
              name: 'Client',
              exported: true,
              members: [
                { id: 'api.Client.Old', kind: 'method', name: 'Old()', exported: true },
                { id: 'api.Client.New', kind: 'method', name: 'New()', exported: true, diff: 'added' },
              ],
            },
            {
              id: 'api.Mode',
              kind: 'type',
              name: 'Mode : string',
              exported: true,
              diff: 'added',
              members: [
                { id: 'api.Mode.Fast', kind: 'const', name: 'Fast', exported: true },
                { id: 'api.Mode.Slow', kind: 'const', name: 'Slow', exported: true },
              ],
            },
            {
              id: 'api.Unchanged',
              kind: 'class',
              name: 'Unchanged',
              exported: true,
              members: [{ id: 'api.Unchanged.Kept', kind: 'method', name: 'Kept()', exported: true }],
            },
            {
              id: 'api.privateChanged',
              kind: 'class',
              name: 'privateChanged',
              diff: 'added',
              members: [],
            },
          ],
        },
      ],
      edges: [],
    });

    const selected = selectReviewGraph(g, null, 'all_public_api', null, {
      impactMode: 'review_view',
      changedDetailsOnly: true,
    });

    const api = selected.components[0];
    expect(api.internals.map((i) => i.id)).toEqual(['api.Application', 'api.Client', 'api.Mode']);
    expect(api.internals.find((i) => i.id === 'api.Application')?.members.map((m) => m.id)).toEqual([
      'api.Application.SessionGraphRepository',
    ]);
    expect(api.internals.find((i) => i.id === 'api.Application')?.diff).toBeUndefined();
    expect(api.internals.find((i) => i.id === 'api.Client')?.members.map((m) => m.id)).toEqual(['api.Client.New']);
    expect(api.internals.find((i) => i.id === 'api.Mode')?.members.map((m) => m.id)).toEqual([
      'api.Mode.Fast',
      'api.Mode.Slow',
    ]);
    expect(api.ports.map((p) => p.id)).toEqual(['api:out:new']);
  });

  it('keeps dependency-only packages visible without dumping unchanged symbols in changed-details mode', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [{ id: 'api:out:storage', side: 'right', kind: 'out', name: 'use storage', public: true }],
          internals: [{ id: 'api.Unchanged', kind: 'class', name: 'Unchanged', exported: true, members: [] }],
        },
        {
          id: 'storage',
          name: 'storage',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [{ id: 'storage:in:Repository', side: 'left', kind: 'in', name: 'Repository', public: true }],
          internals: [{ id: 'storage.Repository', kind: 'iface', name: 'Repository', exported: true, members: [] }],
        },
      ],
      edges: [
        { id: 'api-storage', from: 'api', to: 'storage', fromPort: 'api:out:storage', toPort: 'storage:in:Repository', label: 'returns', public: true, diff: 'added' },
      ],
    });

    const selected = selectReviewGraph(g, null, 'all_public_api', null, {
      impactMode: 'changed_neighbors',
      changeFilter: 'dependency',
      changedDetailsOnly: true,
    });

    expect(selected.components.map((c) => c.id)).toEqual(['api', 'storage']);
    expect(selected.components.flatMap((c) => c.internals)).toEqual([]);
    expect(selected.components.flatMap((c) => c.ports)).toEqual([]);
    expect(selected.edges.map((e) => e.id)).toEqual(['api-storage']);
  });

  it('keeps changed containers visible without dumping unchanged members in changed-details mode', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 0, removed: 0, changed: 1, comments: 0 } },
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [],
          internals: [
            {
              id: 'api.Application',
              kind: 'iface',
              name: 'Application',
              exported: true,
              diff: 'changed',
              members: [
                { id: 'api.Application.Sessions', kind: 'method', name: 'Sessions()', exported: true },
                { id: 'api.Application.Tools', kind: 'method', name: 'Tools()', exported: true },
              ],
            },
          ],
        },
      ],
      edges: [],
    });

    const selected = selectReviewGraph(g, null, 'all_public_api', null, {
      impactMode: 'changed_only',
      changedDetailsOnly: true,
    });

    expect(selected.components.map((c) => c.id)).toEqual(['api']);
    expect(selected.components[0].internals.map((i) => [i.id, i.diff])).toEqual([
      ['api.Application', 'changed'],
    ]);
    expect(selected.components[0].internals[0].members).toEqual([]);
  });

  it('filters context edges to changed dependency edges in changed-details mode', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'changed', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [
        { id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: 'returns', diff: 'added' },
        { id: 'ac', from: 'a', to: 'c', fromPort: '', toPort: '', label: 'uses' },
      ],
    });

    const selected = selectReviewGraph(g, null, 'everything', null, {
      impactMode: 'changed_neighbors',
      changedDetailsOnly: true,
    });

    expect(selected.components.map((c) => c.id)).toEqual(['a', 'b', 'c']);
    expect(selected.edges.map((e) => e.id)).toEqual(['ab']);
  });

  it('in diff mode keeps only changed components by default', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'b.Public', kind: 'class', name: 'Public', exported: true, diff: 'added', members: [] }], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'd', name: 'D', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [
        { id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' },
        { id: 'bc', from: 'b', to: 'c', fromPort: '', toPort: '', label: '' },
      ],
    });

    const selected = selectReviewGraph(g, null, 'everything');

    expect(selected.components.map((c) => c.id)).toEqual(['b']);
    expect(selected.edges).toHaveLength(0);
  });

  it('can hide unchanged neighbors while keeping the selected impact mode', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [
        { id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' },
        { id: 'bc', from: 'b', to: 'c', fromPort: '', toPort: '', label: '' },
      ],
    });

    const withNeighbors = selectReviewGraph(g, null, 'everything', null, { impactMode: 'changed_neighbors' });
    const hiddenNeighbors = selectReviewGraph(g, null, 'everything', null, {
      impactMode: 'changed_neighbors',
      hideUnchangedNeighbors: true,
    });

    expect(withNeighbors.components.map((c) => c.id)).toEqual(['a', 'b', 'c']);
    expect(hiddenNeighbors.components.map((c) => c.id)).toEqual(['b']);
    expect(hiddenNeighbors.edges).toHaveLength(0);
  });

  it('can expand diff mode to containing groups', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      reviewGroupings: [
        {
          id: 'directory',
          title: 'Directory',
          groups: [
            { id: 'directory:one', title: 'One', componentIds: ['a', 'b'], componentCount: 2 },
            { id: 'directory:two', title: 'Two', componentIds: ['c'], componentCount: 1 },
          ],
        },
      ],
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'changed', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [],
    });

    const selected = selectReviewGraph(g, null, 'everything', 'directory', { impactMode: 'containing_group' });

    expect(selected.components.map((c) => c.id)).toEqual(['a', 'b']);
    expect(selected.boundedContexts.map((bc) => bc.id)).toEqual(['directory:one']);
  });

  it('filters diff mode by change kind', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 1, changed: 0, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', diff: 'removed', internals: [], ports: [] },
      ],
      edges: [],
    });

    const selected = selectReviewGraph(g, null, 'everything', null, {
      impactMode: 'changed_only',
      changeFilter: 'added',
    });

    expect(selected.components.map((c) => c.id)).toEqual(['a']);
    expect(deriveChanges(selected).map((change) => change.kind)).toEqual(['added']);
  });

  it('can explicitly expand from the selected review view to the whole repository', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'everything', componentIds: ['a', 'b'], componentCount: 2 },
      ],
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [
        { id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' },
        { id: 'bc', from: 'b', to: 'c', fromPort: '', toPort: '', label: '' },
      ],
    });

    const viewOnly = selectReviewGraph(g, 'api', 'everything', null, { impactMode: 'review_view' });
    const wholeRepo = selectReviewGraph(g, 'api', 'everything', null, { impactMode: 'repository' });

    expect(viewOnly.components.map((c) => c.id)).toEqual(['a', 'b']);
    expect(viewOnly.edges.map((e) => e.id)).toEqual(['ab']);
    expect(wholeRepo.components.map((c) => c.id)).toEqual(['a', 'b', 'c']);
    expect(wholeRepo.edges.map((e) => e.id)).toEqual(['ab', 'bc']);
  });

  it('filters dependency changes from edge diff flags', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 0, removed: 0, changed: 1, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [
        { id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: '', diff: 'added' },
        { id: 'bc', from: 'b', to: 'c', fromPort: '', toPort: '', label: '' },
      ],
    });

    const selected = selectReviewGraph(g, null, 'everything', null, {
      impactMode: 'changed_only',
      changeFilter: 'dependency',
    });

    expect(selected.components.map((c) => c.id)).toEqual(['a', 'b']);
    expect(selected.edges.map((e) => e.id)).toEqual(['ab']);
    expect(deriveChanges(selected).map((change) => change.id)).toEqual(['edg-ab']);
  });

  it('filters policy violations from server-resolved violation pairs', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 0, removed: 0, changed: 1, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', diff: 'changed', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'c', name: 'C', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
      ],
      edges: [
        { id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: 'uses' },
        { id: 'bc', from: 'b', to: 'c', fromPort: '', toPort: '', label: 'uses' },
      ],
      policyViolations: [
        {
          id: 'policy:layer-rule:a->b',
          kind: 'layer_rule',
          sourceComponentId: 'a',
          targetComponentId: 'b',
          sourceLayer: 'domain',
          targetLayer: 'service',
          message: 'a (domain) imports b (service), which is not allowed by layer_rules',
        },
      ],
    });

    const selected = selectReviewGraph(g, null, 'everything', null, {
      impactMode: 'changed_only',
      changeFilter: 'policy',
    });

    expect(selected.components.map((c) => c.id)).toEqual(['a', 'b']);
    expect(selected.components.map((c) => c.diff)).toEqual([undefined, undefined]);
    expect(selected.edges.map((e) => e.id)).toEqual(['ab']);
    expect(selected.policyViolations?.map((violation) => violation.id)).toEqual(['policy:layer-rule:a->b']);
    expect(deriveChanges(selected).map((change) => [change.id, change.kind, change.name])).toEqual([
      ['pol-policy:layer-rule:a->b', 'policy', 'A -> B'],
    ]);
  });

  it('filters symbol relations by public API scope', () => {
    const g = graph({
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [],
          internals: [
            {
              id: 'api.Application',
              kind: 'iface',
              name: 'Application',
              exported: true,
              members: [{ id: 'api.Application.Repository', kind: 'method', name: 'Repository()', exported: true }],
            },
            {
              id: 'api.helper',
              kind: 'func',
              name: 'helper()',
              members: [],
            },
          ],
        },
        {
          id: 'repo',
          name: 'repo',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [],
          internals: [{ id: 'repo.Repository', kind: 'iface', name: 'Repository', exported: true, members: [] }],
        },
      ],
      edges: [],
      relations: [
        {
          id: 'r:returns:api.Application.Repository->repo.Repository',
          kind: 'returns',
          fromComponentId: 'api',
          fromInternalId: 'api.Application',
          fromMemberId: 'api.Application.Repository',
          toComponentId: 'repo',
          toInternalId: 'repo.Repository',
          public: true,
        },
        {
          id: 'r:returns:api.helper->repo.Repository',
          kind: 'returns',
          fromComponentId: 'api',
          fromInternalId: 'api.helper',
          toComponentId: 'repo',
          toInternalId: 'repo.Repository',
        },
      ],
    });

    const selected = selectReviewGraph(g, null, 'all_public_api');

    expect(selected.relations?.map((relation) => relation.id)).toEqual([
      'r:returns:api.Application.Repository->repo.Repository',
    ]);
  });

  it('keeps only changed-source symbol relations in changed-details mode', () => {
    const g = graph({
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        {
          id: 'api',
          name: 'api',
          tech: '',
          desc: '',
          bc: 'bc1',
          ports: [],
          internals: [
            {
              id: 'api.Application',
              kind: 'iface',
              name: 'Application',
              exported: true,
              members: [
                { id: 'api.Application.Repository', kind: 'method', name: 'Repository()', exported: true, diff: 'added' },
                { id: 'api.Application.Other', kind: 'method', name: 'Other()', exported: true },
              ],
            },
          ],
        },
        {
          id: 'repo',
          name: 'repo',
          tech: '',
          desc: '',
          bc: 'bc1',
          diff: 'added',
          ports: [],
          internals: [{ id: 'repo.Repository', kind: 'iface', name: 'Repository', exported: true, diff: 'added', members: [] }],
        },
      ],
      edges: [],
      relations: [
        {
          id: 'r:returns:api.Application.Repository->repo.Repository',
          kind: 'returns',
          fromComponentId: 'api',
          fromInternalId: 'api.Application',
          fromMemberId: 'api.Application.Repository',
          toComponentId: 'repo',
          toInternalId: 'repo.Repository',
          public: true,
        },
        {
          id: 'r:returns:api.Application.Other->repo.Repository',
          kind: 'returns',
          fromComponentId: 'api',
          fromInternalId: 'api.Application',
          fromMemberId: 'api.Application.Other',
          toComponentId: 'repo',
          toInternalId: 'repo.Repository',
          public: true,
        },
      ],
    });

    const selected = selectReviewGraph(g, null, 'all_public_api', null, {
      impactMode: 'changed_only',
      changeFilter: 'all',
      changedDetailsOnly: true,
    });

    expect(selected.components.find((component) => component.id === 'api')?.internals[0].members.map((member) => member.id)).toEqual([
      'api.Application.Repository',
    ]);
    expect(selected.relations?.map((relation) => relation.id)).toEqual([
      'r:returns:api.Application.Repository->repo.Repository',
    ]);
  });
});

describe('addInternalsOfExpanded', () => {
  it('adds internals of expanded components (add-only, preserves prior)', () => {
    const result = addInternalsOfExpanded(graph(), new Set(['a']), new Set(['old']));
    expect(result).toEqual(new Set(['old', 'a.i']));
  });
});

describe('initialExpanded', () => {
  it('falls back to the first component when no "orders" exists', () => {
    expect(initialExpanded(graph())).toEqual(['a']);
  });

  it('honors server-provided review view expansion policies', () => {
    const g = graph({
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', diff: 'changed', internals: [], ports: [] },
      ],
    });

    expect(initialExpanded(g, 'collapsed')).toEqual([]);
    expect(initialExpanded(g, 'changed')).toEqual(['b']);
    expect(initialExpanded(g, 'expanded')).toEqual(['a', 'b']);
  });
});

describe('seedMarkers', () => {
  it('positions a comment marker beside its host component using laid geometry', () => {
    const g = graph({
      comments: [{ id: 'cm1', target: { type: 'component', id: 'a' }, body: 'hi' }],
    });
    const laid = { ...g, components: g.components.map((c) => (c.id === 'a' ? { ...c, x: 100, y: 200, w: 220 } : c)) };
    const markers = seedMarkers(g, laid);
    expect(markers).toHaveLength(1);
    expect(markers[0]).toMatchObject({ id: 'seed-0', n: 1, target: { type: 'component', id: 'a' }, body: 'hi' });
    expect(markers[0].x).toBe(100 + 220 + 8);
    expect(markers[0].y).toBe(200 - 10);
  });

  it('falls back to a default offset when the target/host is not laid out', () => {
    const g = graph({ comments: [{ id: 'cm1', target: { type: 'component', id: 'ghost' }, body: 'x' }] });
    const markers = seedMarkers(g, null);
    expect(markers).toHaveLength(1);
    expect(markers[0].x).toBe(80);
    expect(markers[0].y).toBe(30);
  });
});
