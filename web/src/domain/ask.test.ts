import { describe, expect, it } from 'vitest';
import { buildAskProjection, groupAskHits, resolveAskHits, reresolveAskHits, type RawAskHit } from './ask';
import type { UIGraph } from '../types';

const graph: UIGraph = {
  schema: 'wyrd.uigraph/v0',
  boundedContexts: [{ id: 'root', name: 'Root' }],
  components: [
    {
      id: 'internal/adapter/http',
      name: 'http',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        { id: 'internal/adapter/http.Server', kind: 'class', name: 'Server', members: [] },
        { id: 'internal/adapter/http.handleAPISearch', kind: 'func', name: 'handleAPISearch', members: [] },
      ],
      ports: [],
    },
    {
      id: 'internal/retrieval',
      name: 'retrieval',
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: [
        {
          id: 'internal/retrieval.Service',
          kind: 'class',
          name: 'Service',
          members: [{ id: 'internal/retrieval.Service.Search', kind: 'method', name: 'Search' }],
        },
      ],
      ports: [],
    },
  ],
  edges: [],
  comments: [],
};

const hit = (over: Partial<RawAskHit> & { node_id: string }): RawAskHit => ({
  kind: 'func',
  package: '',
  name: '',
  file: '',
  line: 0,
  doc: '',
  score: 0.03,
  ...over,
});

describe('resolveAskHits', () => {
  it('maps a hit onto the card row that carries the same id', () => {
    const [resolved] = resolveAskHits(graph, [
      hit({ node_id: 'internal/retrieval.Service', package: 'internal/retrieval', name: 'Service', kind: 'class' }),
    ]);
    expect(resolved.packageId).toBe('internal/retrieval');
    expect(resolved.inGraph).toBe(true);
    expect(resolved.symbolInGraph).toBe(true);
  });

  it('recovers the package from the node id when the daemon did not send one', () => {
    const [resolved] = resolveAskHits(graph, [hit({ node_id: 'internal/adapter/http.Server' })]);
    // Splitting on the last dot would have yielded "internal/adapter/http.Server"
    // minus its symbol only by luck; the component index makes it exact.
    expect(resolved.packageId).toBe('internal/adapter/http');
    expect(resolved.name).toBe('Server');
    expect(resolved.symbolInGraph).toBe(true);
  });

  it('lands a method hit on the row of the type that declares it', () => {
    const [resolved] = resolveAskHits(graph, [
      hit({
        node_id: 'internal/retrieval.Service.Search',
        package: 'internal/retrieval',
        name: 'Service.Search',
        kind: 'method',
      }),
    ]);
    expect(resolved.symbolInGraph).toBe(true);
    // The canvas draws internals; selecting on the member id alone would narrow
    // the card to a row it does not have and draw it empty.
    expect(resolved.internalId).toBe('internal/retrieval.Service');
  });

  it('keeps a hit whose package is not in the graph, flagged as undrawable', () => {
    const [resolved] = resolveAskHits(graph, [
      hit({ node_id: 'internal/elsewhere.Thing', package: 'internal/elsewhere', name: 'Thing' }),
    ]);
    expect(resolved.inGraph).toBe(false);
    expect(resolved.symbolInGraph).toBe(false);
  });

  it('flags a known package whose symbol is filtered out of the current projection', () => {
    const [resolved] = resolveAskHits(graph, [
      hit({ node_id: 'internal/retrieval.rrfFuse', package: 'internal/retrieval', name: 'rrfFuse' }),
    ]);
    expect(resolved.inGraph).toBe(true);
    expect(resolved.symbolInGraph).toBe(false);
  });
});

describe('reresolveAskHits', () => {
  it('re-checks the answer against a reloaded graph', () => {
    const hits = resolveAskHits(graph, [
      hit({ node_id: 'internal/retrieval.Service', package: 'internal/retrieval', name: 'Service' }),
    ]);
    const withoutRetrieval: UIGraph = { ...graph, components: [graph.components[0]] };
    const [after] = reresolveAskHits(withoutRetrieval, hits);
    expect(after.inGraph).toBe(false);
    expect(after.symbolInGraph).toBe(false);
  });
});

describe('buildAskProjection', () => {
  it('selects the matched packages and symbols', () => {
    const hits = resolveAskHits(graph, [
      hit({ node_id: 'internal/adapter/http.handleAPISearch', package: 'internal/adapter/http' }),
      hit({ node_id: 'internal/retrieval.Service', package: 'internal/retrieval' }),
    ]);
    const projection = buildAskProjection(hits, true)!;
    expect([...projection.componentIds].sort()).toEqual(['internal/adapter/http', 'internal/retrieval']);
    expect(projection.internalIds.size).toBe(2);
  });

  it('selects the declaring type when the answer is a method', () => {
    const hits = resolveAskHits(graph, [
      hit({ node_id: 'internal/retrieval.Service.Search', package: 'internal/retrieval', kind: 'method' }),
    ]);
    const projection = buildAskProjection(hits, true)!;
    expect([...projection.internalIds]).toEqual(['internal/retrieval.Service']);
  });

  it('is null when nothing landed in the graph, so the review stays up', () => {
    const hits = resolveAskHits(graph, [hit({ node_id: 'other.Thing', package: 'other' })]);
    expect(buildAskProjection(hits, true)).toBeNull();
  });

  it('drops undrawable hits from the selection', () => {
    const hits = resolveAskHits(graph, [
      hit({ node_id: 'other.Thing', package: 'other' }),
      hit({ node_id: 'internal/retrieval.Service', package: 'internal/retrieval' }),
    ]);
    expect([...buildAskProjection(hits, true)!.componentIds]).toEqual(['internal/retrieval']);
  });
});

describe('groupAskHits', () => {
  it('groups by package, keeping packages in best-hit order', () => {
    const hits = resolveAskHits(graph, [
      hit({ node_id: 'internal/retrieval.Service', package: 'internal/retrieval' }),
      hit({ node_id: 'internal/adapter/http.Server', package: 'internal/adapter/http' }),
      hit({ node_id: 'internal/retrieval.rrfFuse', package: 'internal/retrieval' }),
    ]);
    const groups = groupAskHits(hits);
    expect(groups.map((group) => group.packageId)).toEqual(['internal/retrieval', 'internal/adapter/http']);
    expect(groups[0].hits).toHaveLength(2);
  });
});
