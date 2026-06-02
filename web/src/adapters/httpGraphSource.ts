import { loadGraph } from '../data/load';
import type { GraphSourcePort } from '../domain/ports';

/** GraphSourcePort backed by the existing fetch-with-fallback loader. */
export function createHttpGraphSource(): GraphSourcePort {
  return { load: () => loadGraph() };
}
