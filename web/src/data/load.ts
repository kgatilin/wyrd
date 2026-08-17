import type { UIGraph } from '../types';
import { fixture } from './fixture';

const SCHEMA_PREFIX = 'archai.uigraph/';

/**
 * Load the UIGraph data with a fallback chain:
 * 1. /api/uigraph (live archai serve daemon)
 * 2. /archgraph.sample.json (committed sample, for standalone `vite dev`)
 * 3. fixture (hardcoded rich sample)
 */
export async function loadGraph(worktree?: string): Promise<UIGraph> {
  for (const url of liveGraphURLs(worktree)) {
    const graph = await tryFetch(url);
    if (graph) return graph;
  }

  if (worktree) {
    throw new Error(`could not load worktree ${worktree}`);
  }

  // Fall back to committed sample
  const graph = await tryFetch('/archgraph.sample.json');
  if (graph) return graph;

  // Fall back to fixture
  console.info('[loadGraph] Using built-in fixture as fallback');
  return fixture;
}

function liveGraphURLs(worktree?: string): string[] {
  if (worktree) {
    return [`/w/${encodeURIComponent(worktree)}/api/uigraph`];
  }
  const urls: string[] = [];
  const prefix = currentWorktreePrefix();
  if (prefix) urls.push(`${prefix}/api/uigraph`);
  urls.push('/api/uigraph');
  return urls;
}

function currentWorktreePrefix(): string | null {
  if (typeof window === 'undefined') return null;
  const match = window.location.pathname.match(/^\/w\/([^/]+)(?:\/|$)/);
  if (!match) return null;
  return `/w/${match[1]}`;
}

async function tryFetch(url: string): Promise<UIGraph | null> {
  try {
    const res = await fetch(url);
    if (!res.ok) return null;

    const data = await res.json();
    if (!validateSchema(data)) {
      console.warn(`[loadGraph] Invalid schema in ${url}:`, data.schema);
      return null;
    }
    console.info(`[loadGraph] Loaded from ${url}`);
    return data as UIGraph;
  } catch (err) {
    console.debug(`[loadGraph] Failed to fetch ${url}:`, err);
    return null;
  }
}

function validateSchema(data: unknown): data is UIGraph {
  if (typeof data !== 'object' || data === null) return false;
  const schema = (data as Record<string, unknown>).schema;
  return typeof schema === 'string' && schema.startsWith(SCHEMA_PREFIX);
}
