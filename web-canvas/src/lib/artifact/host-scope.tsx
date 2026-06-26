"use client";

import dynamic from 'next/dynamic';
import type { UIGraph } from '@/lib/graph/types';

const GraphRenderer = dynamic(
  () => import('@/components/graph/Graph').then((m) => m.Graph),
  {
    ssr: false,
    loading: () => <div className="graph-block-loading">Loading graph renderer…</div>,
  },
);

/**
 * `Graph` is the host component exposed to artifact code. The agent's file
 * writes `<Graph graph={dataSource.graph('component')} height={520} />` — a
 * bounded, embeddable widget (card + internal pan/zoom), not a full-bleed canvas.
 */
export function GraphView({
  graph,
  height = 520,
  title,
  caption,
}: {
  graph: UIGraph;
  height?: number;
  title?: string;
  caption?: string;
}) {
  return (
    <figure className="graph-block">
      {(title || caption) && (
        <figcaption className="graph-block-header">
          {title && <span className="graph-block-title">{title}</span>}
          {caption && <span className="graph-block-caption">{caption}</span>}
        </figcaption>
      )}
      <div className="graph-block-body" style={{ height }}>
        <GraphRenderer graph={graph} showDiff cardDensity="detailed" showInlineSignatures />
      </div>
    </figure>
  );
}
