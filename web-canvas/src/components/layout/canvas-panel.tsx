"use client";

import dynamic from 'next/dynamic';
import { fixtureGraph } from '@/lib/graph/fixture';
import type { Component } from '@/lib/graph/types';
import { useCallback } from 'react';

// Dynamic import with SSR disabled - elkjs requires browser APIs
const Graph = dynamic(
  () => import('@/components/graph/Graph').then(mod => mod.Graph),
  { ssr: false, loading: () => <GraphLoading /> }
);

function GraphLoading() {
  return (
    <div className="flex h-full items-center justify-center bg-background">
      <div className="text-center">
        <p className="text-lg text-muted-foreground">Loading graph renderer...</p>
      </div>
    </div>
  );
}

export function CanvasPanel() {
  const handleSelectNode = useCallback((component: Component) => {
    console.log('Selected component:', component.id);
  }, []);

  return (
    <div className="h-full w-full">
      <Graph
        graph={fixtureGraph}
        onSelectNode={handleSelectNode}
        showDiff={true}
        cardDensity="detailed"
        showInlineSignatures={true}
      />
    </div>
  );
}
