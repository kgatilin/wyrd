"use client";

import dynamic from 'next/dynamic';
import { useCallback } from 'react';
import type { GraphBlockData } from '@/lib/artifact/types';
import type { Component } from '@/lib/graph/types';

const Graph = dynamic(
  () => import('@/components/graph/Graph').then((mod) => mod.Graph),
  {
    ssr: false,
    loading: () => (
      <div className="graph-block-loading">Loading graph renderer…</div>
    ),
  },
);

/**
 * An embedded, bounded graph widget. The graph is sized to the card body, so
 * its pan/zoom and toolbar stay contained — it is a block within the document,
 * not a full-bleed canvas.
 */
export function GraphBlock({ block }: { block: GraphBlockData }) {
  const handleSelectNode = useCallback((component: Component) => {
    // Drill-in wiring lands later; for now just surface the selection.
    console.log('Selected component:', component.id);
  }, []);

  const height = block.height ?? 520;

  return (
    <figure className="graph-block">
      {(block.title || block.caption) && (
        <figcaption className="graph-block-header">
          {block.title && <span className="graph-block-title">{block.title}</span>}
          {block.caption && <span className="graph-block-caption">{block.caption}</span>}
        </figcaption>
      )}
      <div className="graph-block-body" style={{ height }}>
        <Graph
          graph={block.graph}
          onSelectNode={handleSelectNode}
          showDiff={block.options?.showDiff ?? true}
          cardDensity={block.options?.cardDensity ?? 'detailed'}
          showInlineSignatures={block.options?.showInlineSignatures ?? true}
        />
      </div>
    </figure>
  );
}
