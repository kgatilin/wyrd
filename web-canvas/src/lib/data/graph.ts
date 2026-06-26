"use client";

import { useEffect, useState } from 'react';
import { fixtureGraph } from '@/lib/graph/fixture';
import { fixtureGraphAlt } from '@/lib/graph/fixture-alt';
import type { UIGraph } from '@/lib/graph/types';

/**
 * The graph data-source (pull / request-response).
 *
 * `resolveGraph(query)` returns a subgraph for a query id. Today it is a mock
 * backed by fixtures (the daemon stand-in); the live archai daemon plugs in here
 * (`fetch('/api/uigraph?...')`) and `query` becomes a real seed/grow spec. The
 * async shape is deliberate so swapping in the network call changes nothing
 * downstream.
 */
const GRAPHS: Record<string, UIGraph> = {
  component: fixtureGraph,
  retrieval: fixtureGraphAlt,
};

export async function resolveGraph(query: string): Promise<UIGraph> {
  // Simulate the latency/async of a real data-source fetch.
  await new Promise((r) => setTimeout(r, 0));
  return GRAPHS[query] ?? fixtureGraph;
}

/** Reactive graph fetch: null while loading, then the resolved subgraph. */
export function useGraph(query: string): UIGraph | null {
  const [graph, setGraph] = useState<UIGraph | null>(null);

  useEffect(() => {
    let cancelled = false;
    resolveGraph(query).then((g) => {
      if (!cancelled) setGraph(g);
    });
    return () => {
      cancelled = true;
    };
  }, [query]);

  return graph;
}
