"use client";

import dynamic from 'next/dynamic';
import { useGraph } from '@/lib/data/graph';

const GraphRenderer = dynamic(
  () => import('@/components/graph/Graph').then((m) => m.Graph),
  {
    ssr: false,
    loading: () => <div className="graph-block-loading">Loading graph renderer…</div>,
  },
);

/**
 * `Graph` is the host component exposed to artifact code. The agent writes
 * `<Graph source="component" height={520} />` — it names a data-source query and
 * the widget pulls the subgraph itself (via {@link useGraph}). The agent never
 * sees or bakes graph data; it only references a source.
 */
export function GraphView({
  source,
  height = 520,
  title,
  caption,
}: {
  source: string;
  height?: number;
  title?: string;
  caption?: string;
}) {
  const graph = useGraph(source);

  return (
    <figure className="graph-block">
      {(title || caption) && (
        <figcaption className="graph-block-header">
          {title && <span className="graph-block-title">{title}</span>}
          {caption && <span className="graph-block-caption">{caption}</span>}
        </figcaption>
      )}
      <div className="graph-block-body" style={{ height }}>
        {graph ? (
          <GraphRenderer graph={graph} showDiff cardDensity="detailed" showInlineSignatures />
        ) : (
          <div className="graph-block-loading">Loading “{source}” from data-source…</div>
        )}
      </div>
    </figure>
  );
}
