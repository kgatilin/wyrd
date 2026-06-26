import { fixtureGraph } from '@/lib/graph/fixture';
import { fixtureGraphAlt } from '@/lib/graph/fixture-alt';
import type { UIGraph } from '@/lib/graph/types';

/**
 * The data-source exposed to artifact code. An artifact (a single agent-authored
 * file) never bakes graph data — it asks the data-source for a subgraph. This is
 * the seam the live archai daemon (`/api/uigraph`) plugs into later; for now it
 * is a mock backed by fixtures (the daemon stand-in).
 */
export interface DataSource {
  /** Return a graph by id (a stand-in for a seed/grow query). */
  graph(id: string): UIGraph;
}

const GRAPHS: Record<string, UIGraph> = {
  component: fixtureGraph,
  retrieval: fixtureGraphAlt,
};

export const dataSource: DataSource = {
  graph(id) {
    return GRAPHS[id] ?? fixtureGraph;
  },
};
