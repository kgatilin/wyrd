/**
 * Data source for fetching UIGraph from archai daemon.
 */
import type { UIGraph } from './types';
import { fixtureGraph } from './fixture';

/**
 * Get the archai daemon base URL.
 * Uses NEXT_PUBLIC_ARCHAI_URL env var if set, otherwise defaults to fixture mode.
 */
function getArchaiBaseUrl(): string | null {
  if (typeof window === 'undefined') return null;
  return process.env.NEXT_PUBLIC_ARCHAI_URL || null;
}

/**
 * Fetch a UIGraph from the archai daemon.
 *
 * @param packagePath - Optional package path to fetch (e.g., "./internal/...")
 * @returns The UIGraph from the daemon
 * @throws Error if the daemon is unreachable or returns an error
 */
export async function fetchGraph(packagePath?: string): Promise<UIGraph> {
  const baseUrl = getArchaiBaseUrl();
  if (!baseUrl) {
    throw new Error('NEXT_PUBLIC_ARCHAI_URL not configured');
  }

  const url = new URL('/api/uigraph', baseUrl);
  if (packagePath) {
    url.searchParams.set('package', packagePath);
  }

  const response = await fetch(url.toString(), {
    method: 'GET',
    headers: {
      'Accept': 'application/json',
    },
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch graph: ${response.status} ${response.statusText}`);
  }

  const data = await response.json();
  return data as UIGraph;
}

/**
 * Get a UIGraph, either from the daemon (if configured) or the fixture.
 *
 * @param options.live - If true, fetch from daemon (requires NEXT_PUBLIC_ARCHAI_URL)
 * @param options.packagePath - Optional package path for live fetch
 * @returns The UIGraph
 */
export async function getGraph(options?: {
  live?: boolean;
  packagePath?: string;
}): Promise<UIGraph> {
  const { live = false, packagePath } = options ?? {};

  if (live) {
    return fetchGraph(packagePath);
  }

  return fixtureGraph;
}

/**
 * Check if live mode is available (archai daemon URL configured).
 */
export function isLiveAvailable(): boolean {
  return getArchaiBaseUrl() !== null;
}
