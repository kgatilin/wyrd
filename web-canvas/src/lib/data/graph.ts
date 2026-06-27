"use client";

import { useEffect, useState } from 'react';
import type { UIGraph, Component, Internal, SymbolRelation } from '@/lib/graph/types';

/**
 * The graph data-source (pull / request-response) over the real archai daemon
 * graph of this repo, via the same-origin `/api/graph` route (see
 * `src/app/api/graph/route.ts`).
 *
 * A `GraphQuery` selects what to show:
 *   - `query`  — a semantic query; returns the matching subgraph (search_graph).
 *   - `nodes`  — explicit seed node ids; returns their neighborhood (expand).
 *   - `source` — a package path/name to focus the whole-project graph on.
 *   - `hops`   — neighborhood radius for query/nodes (default 1).
 *   - `edges`  — edge kinds to keep (uses|returns|implements|calls).
 * A bare string is treated as `{ source }` for convenience.
 */
export interface GraphQuery {
  query?: string;
  nodes?: string[];
  source?: string;
  hops?: number;
  edges?: string[];
}

function normalize(spec: string | GraphQuery): GraphQuery {
  return typeof spec === 'string' ? { source: spec } : spec;
}

/** Stable key so the hook re-fetches only when the spec actually changes. */
function specKey(s: GraphQuery): string {
  return JSON.stringify([s.query ?? '', s.nodes ?? [], s.source ?? '', s.hops ?? 1, s.edges ?? []]);
}

export async function resolveGraph(spec: string | GraphQuery): Promise<UIGraph> {
  const q = normalize(spec);
  const params = new URLSearchParams();
  if (q.query) params.set('query', q.query);
  if (q.nodes?.length) params.set('nodes', q.nodes.join(','));
  if (q.hops != null) params.set('hops', String(q.hops));
  if (q.edges?.length) params.set('edges', q.edges.join(','));
  // source is applied client-side against the full graph; pass it through too.
  if (q.source) params.set('source', q.source);

  const res = await fetch(`/api/graph?${params.toString()}`, {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    let detail = '';
    try {
      detail = ((await res.json()) as { error?: string }).error ?? '';
    } catch {
      // ignore non-JSON error bodies
    }
    throw new Error(`graph fetch failed (${res.status})${detail ? `: ${detail}` : ''}`);
  }

  const data = (await res.json()) as UIGraph | ApiSubgraph;

  // Queried subgraph (search_graph/expand) → assemble a UIGraph.
  if (isSubgraph(data)) {
    return subgraphToUIGraph(data, q.edges);
  }
  // Whole-project UIGraph → optionally focus on a package.
  return focusSubgraph(data, q.source ?? '');
}

/** Reactive graph fetch: null while loading, then the resolved subgraph. */
export function useGraph(spec: string | GraphQuery): UIGraph | null {
  const [graph, setGraph] = useState<UIGraph | null>(null);
  const key = specKey(normalize(spec));

  useEffect(() => {
    let cancelled = false;
    resolveGraph(spec)
      .then((g) => {
        if (!cancelled) setGraph(g);
      })
      .catch((err) => {
        if (!cancelled) console.error('useGraph:', err);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return graph;
}

// --- archai search_graph / expand result shape ---

interface ApiNode {
  id: string;
  kind: string;
  package: string;
  name: string;
  file?: string;
  line?: number;
}
interface ApiEdge {
  from: string;
  to: string;
  kind: string;
}
interface ApiSubgraph {
  nodes: ApiNode[];
  edges: ApiEdge[];
}

function isSubgraph(d: UIGraph | ApiSubgraph): d is ApiSubgraph {
  return Array.isArray((d as ApiSubgraph).nodes) && !(d as UIGraph).components;
}

const INTERNAL_KIND: Record<string, Internal['kind']> = {
  iface: 'iface',
  class: 'class',
  func: 'func',
  type: 'type',
  const: 'const',
  var: 'var',
  error: 'error',
};

/**
 * Assemble a UIGraph from a queried symbol subgraph: group nodes into package
 * components, carry each symbol as an internal, and map symbol edges to
 * relations. Package-level arrows are derived by the renderer from the
 * relations, so `edges` is left empty here.
 */
function subgraphToUIGraph(sg: ApiSubgraph, edgeKinds?: string[]): UIGraph {
  const allow = edgeKinds && edgeKinds.length ? new Set(edgeKinds) : null;

  const components = new Map<string, Component>();
  const pkgOf = new Map<string, string>();
  for (const n of sg.nodes) {
    pkgOf.set(n.id, n.package);
    let c = components.get(n.package);
    if (!c) {
      c = {
        id: n.package,
        name: n.package.split('/').pop() || n.package,
        tech: 'Go',
        desc: '',
        bc: 'default',
        internals: [],
        ports: [],
      };
      components.set(n.package, c);
    }
    c.internals.push({
      id: n.id,
      kind: INTERNAL_KIND[n.kind] ?? 'type',
      name: n.name,
      sourceFile: n.file,
      members: [],
    });
  }

  const relations: SymbolRelation[] = [];
  for (const e of sg.edges) {
    if (allow && !allow.has(e.kind)) continue;
    const fromComponentId = pkgOf.get(e.from);
    const toComponentId = pkgOf.get(e.to);
    if (!fromComponentId || !toComponentId) continue;
    relations.push({
      id: `${e.from}->${e.to}:${e.kind}`,
      kind: e.kind,
      fromComponentId,
      fromInternalId: e.from,
      toComponentId,
      toInternalId: e.to,
    });
  }

  return {
    schema: 'archai.uigraph/subgraph',
    boundedContexts: [{ id: 'default', name: 'Subgraph' }],
    components: [...components.values()],
    edges: [],
    relations,
  };
}

// --- whole-project focus (package path/name) ---

const WHOLE_PROJECT = new Set(['', 'project', 'all', '*', 'overview']);

/**
 * Scope the whole-project graph to a package focus: the matched package(s) plus
 * their direct dependencies and dependents (1 hop). A sentinel or a no-match
 * selector returns the full graph unchanged.
 */
function focusSubgraph(g: UIGraph, source: string): UIGraph {
  const qy = (source ?? '').trim().toLowerCase();
  if (WHOLE_PROJECT.has(qy)) return g;

  const seeds = new Set(
    g.components
      .filter((c) => c.id.toLowerCase().includes(qy) || c.name.toLowerCase().includes(qy))
      .map((c) => c.id),
  );
  if (seeds.size === 0) return g;

  const keep = new Set(seeds);
  for (const e of g.edges) {
    if (seeds.has(e.from)) keep.add(e.to);
    if (seeds.has(e.to)) keep.add(e.from);
  }
  for (const r of g.relations ?? []) {
    if (seeds.has(r.fromComponentId)) keep.add(r.toComponentId);
    if (seeds.has(r.toComponentId)) keep.add(r.fromComponentId);
  }

  const components = g.components.filter((c) => keep.has(c.id));
  const edges = g.edges.filter((e) => keep.has(e.from) && keep.has(e.to));
  const relations = (g.relations ?? []).filter(
    (r) => keep.has(r.fromComponentId) && keep.has(r.toComponentId),
  );
  const bcIds = new Set(components.map((c) => c.bc));
  const boundedContexts = g.boundedContexts.filter((bc) => bcIds.has(bc.id));

  return { ...g, components, edges, relations, boundedContexts };
}
