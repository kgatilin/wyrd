"use client";

import { useEffect, useState } from 'react';
import type { UIGraph } from '@/lib/graph/types';

/**
 * The graph data-source (pull / request-response).
 *
 * `resolveGraph(query)` returns the real architecture graph of the repo the
 * archai daemon is serving. It hits the same-origin `/api/graph` route handler,
 * which proxies to the daemon (discovered from the registry) — see
 * `src/app/api/graph/route.ts`. The daemon's project graph is whole-repo, so
 * `query` is currently a label/seed hint forwarded for future scoping rather
 * than a distinct dataset.
 */
export async function resolveGraph(query: string): Promise<UIGraph> {
  const res = await fetch(`/api/graph?source=${encodeURIComponent(query)}`, {
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
  const full = (await res.json()) as UIGraph;
  return focusSubgraph(full, query);
}

/** Whole-project sentinels: `source` values that mean "don't focus". */
const WHOLE_PROJECT = new Set(['', 'project', 'all', '*', 'overview', 'component', 'retrieval']);

/**
 * Scope the project graph to a focus selector. `source` matches package
 * components by path or name (case-insensitive substring); the result is the
 * matched package(s) plus their direct dependencies and dependents (1 hop in
 * both directions) — a real "dependency graph of X". A whole-project sentinel,
 * or a selector that matches nothing, returns the full graph unchanged.
 */
function focusSubgraph(g: UIGraph, source: string): UIGraph {
  const q = (source ?? '').trim().toLowerCase();
  if (WHOLE_PROJECT.has(q)) return g;

  const seeds = new Set(
    g.components
      .filter((c) => c.id.toLowerCase().includes(q) || c.name.toLowerCase().includes(q))
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

/** Reactive graph fetch: null while loading, then the resolved subgraph. */
export function useGraph(query: string): UIGraph | null {
  const [graph, setGraph] = useState<UIGraph | null>(null);

  useEffect(() => {
    let cancelled = false;
    resolveGraph(query)
      .then((g) => {
        if (!cancelled) setGraph(g);
      })
      .catch((err) => {
        if (!cancelled) console.error('useGraph:', err);
      });
    return () => {
      cancelled = true;
    };
  }, [query]);

  return graph;
}
