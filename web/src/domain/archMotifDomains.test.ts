import { describe, expect, it } from 'vitest';
import {
  buildDomainGrid,
  domainsQueryForScope,
  type RawDomainPartition,
  type RawLatentDomains,
} from './archMotifDomains';
import type { Diff, Internal, UIGraph, SymbolRelation } from '../types';

/**
 * The specs describe a partition the readable way — a cluster and its members —
 * and `payload` encodes it the way the daemon does: one shared node list, and
 * an integer label per node per side.
 */
interface ClusterSpec {
  id: number;
  members: string[];
}
interface SideSpec {
  clusters: ClusterSpec[];
  extra: Partial<RawDomainPartition>;
}

function cluster(id: number, members: string[]): ClusterSpec {
  return { id, members };
}

function partition(clusters: ClusterSpec[], extra: Partial<RawDomainPartition> = {}): SideSpec {
  return { clusters, extra };
}

/** The node list is the union of both sides, in first-seen order. */
function nodeList(sides: SideSpec[]): string[] {
  const nodes: string[] = [];
  const seen = new Set<string>();
  for (const side of sides) {
    for (const spec of side.clusters) {
      for (const member of spec.members) {
        if (seen.has(member)) continue;
        seen.add(member);
        nodes.push(member);
      }
    }
  }
  return nodes;
}

/** One side as label-per-node; -1 for a node this side did not place. */
function labelsOf(side: SideSpec, nodes: string[]): RawDomainPartition {
  const labelOf = new Map<string, number>();
  for (const spec of side.clusters) {
    for (const member of spec.members) labelOf.set(member, spec.id);
  }
  return {
    k: side.clusters.length,
    cluster_count: side.clusters.length,
    dominant_share: 0,
    modularity: 0,
    labels: nodes.map((node) => labelOf.get(node) ?? -1),
    ...side.extra,
  };
}

function payload(
  structural: SideSpec,
  semantic: SideSpec,
  extra: Partial<RawLatentDomains> = {}
): RawLatentDomains {
  const nodes = nodeList([structural, semantic]);
  return {
    nodes,
    node_count: nodes.length,
    structural: labelsOf(structural, nodes),
    semantic: labelsOf(semantic, nodes),
    agreement: { ami: 0.2, nmi: 0.5, verdict: 'latent_domains_glued' },
    glue: { top_fan_in: [], glue_cluster: -1, note: '' },
    dropped_nodes: 0,
    ...extra,
  };
}

/** A graph whose components hold exactly the given symbol ids. */
function graphOf(
  packages: Record<string, { id: string; kind?: Internal['kind']; diff?: Diff }[]>,
  relations: SymbolRelation[] = []
): UIGraph {
  return {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'root', name: 'Root' }],
    components: Object.entries(packages).map(([id, internals]) => ({
      id,
      name: id.split('/').pop() ?? id,
      tech: 'Go',
      desc: '',
      bc: 'root',
      internals: internals.map((internal) => ({
        id: internal.id,
        kind: internal.kind ?? 'func',
        name: internal.id.slice(id.length + 1),
        exported: true,
        diff: internal.diff,
        members: [],
      })),
      ports: [],
    })),
    edges: [],
    relations,
    comments: [],
  };
}

const labels = (axis: { label: string }[]) => axis.map((band) => band.label);

describe('domains grid — contingency assembly', () => {
  it('puts the best-matching cluster pairs on the diagonal', () => {
    // Three groups that both partitions agree about, but the semantic side
    // numbers them in a different order. Ordering by cluster id would scatter
    // them; the greedy max-overlap pass must line them up.
    const raw = payload(
      partition([
        cluster(0, ['type:pkg/a.A1', 'type:pkg/a.A2', 'type:pkg/a.A3', 'type:pkg/a.A4']),
        cluster(1, ['fn:pkg/b.B1', 'fn:pkg/b.B2', 'fn:pkg/b.B3']),
        cluster(2, ['fn:pkg/c.C1', 'fn:pkg/c.C2']),
      ]),
      partition([
        cluster(7, ['fn:pkg/b.B1', 'fn:pkg/b.B2', 'fn:pkg/b.B3']),
        cluster(8, ['type:pkg/a.A1', 'type:pkg/a.A2', 'type:pkg/a.A3', 'type:pkg/a.A4']),
        cluster(9, ['fn:pkg/c.C1', 'fn:pkg/c.C2']),
      ])
    );

    const grid = buildDomainGrid(raw, null);

    expect(labels(grid.rows)).toEqual(['S0', 'S1', 'S2']);
    expect(labels(grid.cols)).toEqual(['M8', 'M7', 'M9']);
    expect(grid.cells.map((cell) => [cell.row, cell.col])).toEqual([
      [0, 0],
      [1, 1],
      [2, 2],
    ]);
    expect(grid.cells.every((cell) => cell.onDiagonal)).toBe(true);
  });

  it('collapses the empty intersections instead of drawing a K×K grid', () => {
    const raw = payload(
      partition([cluster(0, ['fn:pkg/a.A']), cluster(1, ['fn:pkg/b.B'])]),
      partition([cluster(0, ['fn:pkg/a.A']), cluster(1, ['fn:pkg/b.B'])])
    );

    const grid = buildDomainGrid(raw, null);

    // 2×2 axes, but only the two occupied intersections become cells.
    expect(grid.rows).toHaveLength(2);
    expect(grid.cols).toHaveLength(2);
    expect(grid.cells).toHaveLength(2);
  });

  it('spreads one structural cluster across columns when semantics disagrees', () => {
    // The glued shape: everything is one structural blob, semantics splits it.
    const raw = payload(
      partition([
        cluster(0, ['fn:pkg/a.A1', 'fn:pkg/a.A2', 'fn:pkg/b.B1', 'fn:pkg/b.B2', 'fn:pkg/c.C1']),
        cluster(1, ['fn:pkg/d.D1']),
      ]),
      partition([
        cluster(0, ['fn:pkg/a.A1', 'fn:pkg/a.A2']),
        cluster(1, ['fn:pkg/b.B1', 'fn:pkg/b.B2']),
        cluster(2, ['fn:pkg/c.C1', 'fn:pkg/d.D1']),
      ])
    );

    const grid = buildDomainGrid(raw, null);

    const topRow = grid.cells.filter((cell) => cell.row === 0);
    expect(topRow).toHaveLength(3);
    expect(topRow.filter((cell) => cell.onDiagonal)).toHaveLength(1);
    expect(grid.cols).toHaveLength(3);
  });

  it('counts symbols one partition placed and the other did not', () => {
    const raw = payload(
      partition([cluster(0, ['fn:pkg/a.A', 'fn:pkg/a.Orphan'])]),
      partition([cluster(0, ['fn:pkg/a.A'])])
    );

    const grid = buildDomainGrid(raw, null);

    expect(grid.unplaced).toBe(1);
    expect(grid.cells[0].size).toBe(1);
  });
});

describe('domains grid — the label encoding', () => {
  it('reads each side by node position, not by looking an id up', () => {
    // Hand-written rather than built by the helper: this is the wire contract.
    // Labels are positional, so a partition is one integer per node per side
    // and the ids are carried once however many sides read them.
    const raw: RawLatentDomains = {
      nodes: ['fn:pkg/a.A', 'fn:pkg/b.B', 'fn:pkg/c.C'],
      node_count: 3,
      structural: { k: 2, cluster_count: 2, dominant_share: 0.67, modularity: 0.1, labels: [0, 0, 1] },
      semantic: { k: 2, cluster_count: 2, dominant_share: 0.67, modularity: 0.4, labels: [0, 1, 1] },
      agreement: { ami: 0.1, nmi: 0.3, verdict: 'diverging' },
      glue: { top_fan_in: [], glue_cluster: -1, note: '' },
      dropped_nodes: 0,
    };

    const grid = buildDomainGrid(raw, null);

    expect(grid.cells.map((cell) => cell.id).sort()).toEqual(['s0-m0', 's0-m1', 's1-m1']);
    const shared = grid.cells.find((cell) => cell.id === 's0-m0')!;
    expect(shared.packages.flatMap((block) => block.members.map((m) => m.name))).toEqual(['A']);
    expect(grid.unplaced).toBe(0);
  });

  it('treats -1 as unplaced rather than as cluster zero', () => {
    const raw: RawLatentDomains = {
      nodes: ['fn:pkg/a.A', 'fn:pkg/a.Loose'],
      node_count: 2,
      structural: { k: 1, cluster_count: 1, dominant_share: 1, modularity: 0, labels: [0, -1] },
      semantic: { k: 1, cluster_count: 1, dominant_share: 1, modularity: 0, labels: [0, 0] },
      agreement: { ami: 1, nmi: 1, verdict: 'aligned' },
      glue: { top_fan_in: [], glue_cluster: -1, note: '' },
      dropped_nodes: 0,
    };

    const grid = buildDomainGrid(raw, null);

    expect(grid.unplaced).toBe(1);
    expect(grid.cells).toHaveLength(1);
    expect(grid.cells[0].size).toBe(1);
  });

  it('counts a symbol neither side placed once, not twice', () => {
    const raw: RawLatentDomains = {
      nodes: ['fn:pkg/a.A', 'fn:pkg/a.Nowhere'],
      node_count: 2,
      structural: { k: 1, cluster_count: 1, dominant_share: 1, modularity: 0, labels: [0, -1] },
      semantic: { k: 1, cluster_count: 1, dominant_share: 1, modularity: 0, labels: [0, -1] },
      agreement: { ami: 1, nmi: 1, verdict: 'aligned' },
      glue: { top_fan_in: [], glue_cluster: -1, note: '' },
      dropped_nodes: 0,
    };

    expect(buildDomainGrid(raw, null).unplaced).toBe(1);
  });
});

describe('domains grid — cell size', () => {
  it('grows a cell with its member count and floors the small ones', () => {
    const many = Array.from({ length: 12 }, (_, i) => `fn:pkg/a.A${i}`);
    const raw = payload(
      partition([cluster(0, many), cluster(1, ['fn:pkg/b.B'])]),
      partition([cluster(0, many), cluster(1, ['fn:pkg/b.B'])])
    );

    const grid = buildDomainGrid(raw, null);
    const big = grid.cells.find((cell) => cell.size === 12)!;
    const small = grid.cells.find((cell) => cell.size === 1)!;

    expect(big.height).toBeGreaterThan(small.height);
    expect(big.width).toBeGreaterThan(small.width);
    // The floor: a one-symbol card is still a card.
    expect(small.height).toBe(84);
    expect(small.width).toBeGreaterThanOrEqual(190);
  });

  it('lists what fits and reports the rest as overflow', () => {
    const many = Array.from({ length: 20 }, (_, i) => `fn:pkg/a.A${String(i).padStart(2, '0')}`);
    const raw = payload(partition([cluster(0, many)]), partition([cluster(0, many)]));

    const cell = buildDomainGrid(raw, null).cells[0];
    const listed = cell.packages.reduce((sum, block) => sum + block.members.length, 0);

    expect(cell.size).toBe(20);
    expect(listed).toBe(12);
    expect(cell.overflow).toBe(8);
  });

  it('sizes a band by its biggest cell, so the grid stays rectangular', () => {
    const many = Array.from({ length: 10 }, (_, i) => `fn:pkg/a.A${i}`);
    const raw = payload(
      partition([cluster(0, [...many, 'fn:pkg/b.B'])]),
      partition([cluster(0, many), cluster(1, ['fn:pkg/b.B'])])
    );

    const grid = buildDomainGrid(raw, null);
    const [wide, narrow] = grid.cells;

    expect(wide.height).toBe(narrow.height);
    expect(grid.rows[0].extent).toBe(wide.height);
    expect(grid.cols[0].extent).toBe(wide.width);
  });
});

describe('domains grid — cross-cell flow', () => {
  it('aggregates flow per cell pair and ignores flow inside a cell', () => {
    const raw = payload(
      partition([cluster(0, ['fn:pkg/a.A1', 'fn:pkg/a.A2']), cluster(1, ['fn:pkg/b.B1'])]),
      partition([cluster(0, ['fn:pkg/a.A1', 'fn:pkg/a.A2']), cluster(1, ['fn:pkg/b.B1'])])
    );
    const relation = (from: string, to: string, id: string): SymbolRelation => ({
      id,
      kind: 'uses',
      fromComponentId: from.split('.')[0],
      fromInternalId: from,
      toComponentId: to.split('.')[0],
      toInternalId: to,
    });
    const graph = graphOf(
      {
        'pkg/a': [{ id: 'pkg/a.A1' }, { id: 'pkg/a.A2' }],
        'pkg/b': [{ id: 'pkg/b.B1' }],
      },
      [
        relation('pkg/a.A1', 'pkg/b.B1', 'r1'),
        relation('pkg/a.A2', 'pkg/b.B1', 'r2'),
        // Inside one cell — the card already shows it.
        relation('pkg/a.A1', 'pkg/a.A2', 'r3'),
      ]
    );

    const grid = buildDomainGrid(raw, graph);

    expect(grid.edges).toHaveLength(1);
    expect(grid.edges[0]).toMatchObject({ from: 's0-m0', to: 's1-m1', weight: 2 });
    // Endpoints are the two cell centres, so the view only paints them.
    const source = grid.cells.find((cell) => cell.id === 's0-m0')!;
    expect(grid.edges[0].x1).toBe(source.x + source.width / 2);
  });

  it('weighs flow from every member, not only the ones a card lists', () => {
    // The source cell overflows its card; a relation from a member past the
    // cut still pulls on the other cell.
    const many = Array.from({ length: 20 }, (_, i) => `fn:pkg/a.A${String(i).padStart(2, '0')}`);
    const raw = payload(
      partition([cluster(0, many), cluster(1, ['fn:pkg/b.B'])]),
      partition([cluster(0, many), cluster(1, ['fn:pkg/b.B'])])
    );
    const graph = graphOf(
      {
        'pkg/a': many.map((id) => ({ id: id.slice('fn:'.length) })),
        'pkg/b': [{ id: 'pkg/b.B' }],
      },
      [
        {
          id: 'r1',
          kind: 'uses',
          fromComponentId: 'pkg/a',
          fromInternalId: 'pkg/a.A19',
          toComponentId: 'pkg/b',
          toInternalId: 'pkg/b.B',
        },
      ]
    );

    const grid = buildDomainGrid(raw, graph);
    const source = grid.cells.find((cell) => cell.id === 's0-m0')!;

    expect(source.overflow).toBeGreaterThan(0);
    expect(grid.edges).toHaveLength(1);
    expect(grid.edges[0]).toMatchObject({ from: 's0-m0', to: 's1-m1', weight: 1 });
  });

  it('has no edges without a graph to read the relations from', () => {
    const raw = payload(
      partition([cluster(0, ['fn:pkg/a.A']), cluster(1, ['fn:pkg/b.B'])]),
      partition([cluster(0, ['fn:pkg/a.A']), cluster(1, ['fn:pkg/b.B'])])
    );

    expect(buildDomainGrid(raw, null).edges).toEqual([]);
  });
});

describe('domains grid — members and glue', () => {
  it('reads kind and package off the loaded graph, and flags what it cannot', () => {
    const raw = payload(
      partition([cluster(0, ['type:pkg/a.Reader', 'fn:pkg/gone.Vanished'])]),
      partition([cluster(0, ['type:pkg/a.Reader', 'fn:pkg/gone.Vanished'])])
    );
    const graph = graphOf({ 'pkg/a': [{ id: 'pkg/a.Reader', kind: 'iface' }] });

    const members = buildDomainGrid(raw, graph).cells[0].packages.flatMap((block) => block.members);
    const reader = members.find((member) => member.name === 'Reader')!;
    const vanished = members.find((member) => member.name === 'Vanished')!;

    expect(reader).toMatchObject({
      internalId: 'pkg/a.Reader',
      componentId: 'pkg/a',
      kind: 'iface',
      inGraph: true,
    });
    // Not in the graph, so not clickable — but still drawn, under the package
    // its id names.
    expect(vanished).toMatchObject({ componentId: 'pkg/gone', kind: 'func', inGraph: false });
  });

  it('badges the glue nodes with their fan-in and sorts them first', () => {
    const raw = payload(
      partition([cluster(0, ['fn:pkg/a.helper', 'fn:pkg/a.Alpha', 'fn:pkg/a.Beta'])]),
      partition([cluster(0, ['fn:pkg/a.helper', 'fn:pkg/a.Alpha', 'fn:pkg/a.Beta'])]),
      {
        glue: {
          top_fan_in: [{ node: 'fn:pkg/a.helper', fan_in: 31, semantic_cluster: 0 }],
          glue_cluster: 0,
          note: 'pull them to a thin boundary',
        },
      }
    );

    const grid = buildDomainGrid(raw, null);
    const cell = grid.cells[0];

    expect(cell.glueCount).toBe(1);
    expect(cell.packages[0].members[0]).toMatchObject({ name: 'helper', glue: true, fanIn: 31 });
    expect(grid.header.glue.map((member) => member.name)).toEqual(['helper']);
    expect(grid.header.glueNote).toContain('thin boundary');
  });

  it('carries the verdict figures through to the header', () => {
    const raw = payload(
      partition([cluster(0, ['fn:pkg/a.A'])], { k: 4, modularity: 0.11, dominant_share: 0.62 }),
      partition([cluster(0, ['fn:pkg/a.A'])], { k: 4, modularity: 0.48, dominant_share: 0.3 }),
      { node_count: 210, dropped_nodes: 7 }
    );

    expect(buildDomainGrid(raw, null).header).toMatchObject({
      verdict: 'latent_domains_glued',
      ami: 0.2,
      structuralK: 4,
      semanticK: 4,
      structuralModularity: 0.11,
      semanticModularity: 0.48,
      structuralDominantShare: 0.62,
      nodeCount: 210,
      droppedNodes: 7,
    });
  });
});

describe('domains grid — diff overlay', () => {
  it('counts how many cells, rows and columns the change lands in', () => {
    const raw = payload(
      partition([
        cluster(0, ['fn:pkg/a.A1', 'fn:pkg/a.A2']),
        cluster(1, ['fn:pkg/b.B1', 'fn:pkg/c.C1']),
      ]),
      partition([
        cluster(0, ['fn:pkg/a.A1', 'fn:pkg/a.A2', 'fn:pkg/b.B1']),
        cluster(1, ['fn:pkg/c.C1']),
      ])
    );
    const graph = graphOf({
      'pkg/a': [{ id: 'pkg/a.A1', diff: 'changed' }, { id: 'pkg/a.A2' }],
      'pkg/b': [{ id: 'pkg/b.B1', diff: 'added' }],
      'pkg/c': [{ id: 'pkg/c.C1' }],
    });

    const grid = buildDomainGrid(raw, graph);

    // A1 sits in (S0,M0); B1 in (S1,M0) — two cells, two structural clusters,
    // one semantic cluster: the change crosses a module boundary but stays
    // within one subject.
    expect(grid.diff).toEqual({
      changedMembers: 2,
      cells: 2,
      structuralClusters: 2,
      semanticClusters: 1,
    });
  });

  it('reports nothing to overlay on an unchanged tree', () => {
    const raw = payload(partition([cluster(0, ['fn:pkg/a.A'])]), partition([cluster(0, ['fn:pkg/a.A'])]));
    const graph = graphOf({ 'pkg/a': [{ id: 'pkg/a.A' }] });

    expect(buildDomainGrid(raw, graph).diff.changedMembers).toBe(0);
  });
});

describe('domains query', () => {
  it('asks the daemon for the change region in diff scope', () => {
    expect(domainsQueryForScope({ kind: 'diff' })).toEqual({ scope: 'diff' });
  });

  it('asks for the whole repository in repo scope', () => {
    expect(domainsQueryForScope({ kind: 'repo' })).toEqual({ scope: 'repo' });
  });

  it('scopes to one package', () => {
    expect(domainsQueryForScope({ kind: 'package', package: 'internal/adapter/mcp' })).toEqual({
      scope: 'package',
      package: 'internal/adapter/mcp',
    });
  });
});
