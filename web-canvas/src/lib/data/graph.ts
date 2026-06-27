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
  return (await res.json()) as UIGraph;
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
