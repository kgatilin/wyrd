'use client';

import { useState, useEffect, useLayoutEffect, useRef, useCallback, useMemo } from 'react';
import type { UIGraph, Component, GraphInteraction, CardDensity } from '@/lib/graph/types';
import { layout } from '@/lib/graph/layout';
import { ComponentCard } from './ComponentCard';
import { EdgeLayer } from './EdgeLayer';
import { BCGroups } from './BCGroups';
import { RelationLayer } from './RelationLayer';
import { Legend } from './Legend';

export interface GraphProps {
  /** The graph data to render */
  graph: UIGraph;
  /** Called when a node is selected */
  onSelectNode?: (component: Component) => void;
  /** Whether to show diff styling */
  showDiff?: boolean;
  /** Card display density */
  cardDensity?: CardDensity;
  /** Whether to show full signatures inline */
  showInlineSignatures?: boolean;
  /** Initial zoom level */
  initialZoom?: number;
}

interface ViewportState {
  zoom: number;
  scrollX: number;
  scrollY: number;
}

/**
 * Self-contained architecture graph renderer.
 *
 * Owns its own interaction state:
 * - Pan & zoom viewport
 * - Expanded components and internals
 * - Pinned positions (future)
 *
 * Does NOT depend on any external store or review features.
 */
export function Graph({
  graph,
  onSelectNode,
  showDiff = true,
  cardDensity = 'detailed',
  showInlineSignatures = true,
  initialZoom = 1,
}: GraphProps) {
  // Interaction state (local)
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [internalExpanded, setInternalExpanded] = useState<Set<string>>(new Set());
  const [internalWide, setInternalWide] = useState<Set<string>>(new Set());
  const [focusId, setFocusId] = useState<string | null>(null);

  // Viewport state
  const [viewport, setViewport] = useState<ViewportState>({
    zoom: initialZoom,
    scrollX: 0,
    scrollY: 0,
  });

  // Laid out graph
  const [laidGraph, setLaidGraph] = useState<UIGraph | null>(null);
  const [layoutError, setLayoutError] = useState<string | null>(null);

  // Panning state
  const [panning, setPanning] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const panRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    scrollLeft: number;
    scrollTop: number;
  } | null>(null);
  // When a wheel/pinch zoom happens we record the content point under the
  // cursor so the next layout pass can re-anchor scroll to keep it stationary.
  const zoomAnchorRef = useRef<{
    contentX: number;
    contentY: number;
    offX: number;
    offY: number;
  } | null>(null);

  // Build interaction config for layout
  const interaction: GraphInteraction = useMemo(() => ({
    expanded,
    internalExpanded,
    internalWide,
    cardDensity,
    showInlineSignatures,
  }), [expanded, internalExpanded, internalWide, cardDensity, showInlineSignatures]);

  // Run layout when graph or interaction changes
  useEffect(() => {
    let cancelled = false;

    async function runLayout() {
      try {
        const result = await layout(graph, interaction);
        if (!cancelled) {
          setLaidGraph(result);
          setLayoutError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setLayoutError(err instanceof Error ? err.message : 'Layout failed');
        }
      }
    }

    runLayout();

    return () => {
      cancelled = true;
    };
  }, [graph, interaction]);

  // Compute canvas bounds
  const canvasBounds = useMemo(() => {
    if (!laidGraph) return { width: 1200, height: 700 };

    let maxX = 0;
    let maxY = 0;

    for (const bc of laidGraph.boundedContexts) {
      if (bc.x != null && bc.w != null) maxX = Math.max(maxX, bc.x + bc.w);
      if (bc.y != null && bc.h != null) maxY = Math.max(maxY, bc.y + bc.h);
    }

    for (const c of laidGraph.components) {
      if (c.x != null && c.w != null) maxX = Math.max(maxX, c.x + c.w);
      if (c.y != null && c.h != null) maxY = Math.max(maxY, c.y + c.h);
    }

    return {
      width: Math.max(1200, maxX + 100),
      height: Math.max(700, maxY + 100),
    };
  }, [laidGraph]);

  // Handlers
  const handleToggleExpand = useCallback((id: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const handleToggleInternalExpand = useCallback((id: string) => {
    setInternalExpanded(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const handleToggleInternalWide = useCallback((id: string) => {
    setInternalWide(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const handleSelectComponent = useCallback((cmp: Component) => {
    setFocusId(prev => prev === cmp.id ? null : cmp.id);
    onSelectNode?.(cmp);
  }, [onSelectNode]);

  const handleZoom = useCallback((delta: number) => {
    setViewport(prev => ({
      ...prev,
      zoom: Math.max(0.25, Math.min(2, prev.zoom + delta)),
    }));
  }, []);

  const handleResetZoom = useCallback(() => {
    setViewport(prev => ({ ...prev, zoom: 1 }));
  }, []);

  // Wheel / trackpad-pinch zoom, anchored to the cursor. Only intercepts
  // ctrl/⌘+wheel (and macOS pinch, which fires ctrlKey wheel) so a plain
  // scroll still pans the page over this embedded widget. Attached natively
  // so preventDefault works (React's onWheel is passive).
  useEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap) return;

    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      const rect = wrap.getBoundingClientRect();
      const offX = e.clientX - rect.left;
      const offY = e.clientY - rect.top;
      const scrollLeft = wrap.scrollLeft;
      const scrollTop = wrap.scrollTop;
      setViewport(prev => {
        const factor = Math.exp(-e.deltaY * 0.0015);
        const next = Math.max(0.25, Math.min(2, prev.zoom * factor));
        if (next === prev.zoom) return prev;
        zoomAnchorRef.current = {
          contentX: (scrollLeft + offX) / prev.zoom,
          contentY: (scrollTop + offY) / prev.zoom,
          offX,
          offY,
        };
        return { ...prev, zoom: next };
      });
    };

    wrap.addEventListener('wheel', onWheel, { passive: false });
    return () => wrap.removeEventListener('wheel', onWheel);
  }, [laidGraph]);

  // After a cursor-anchored zoom, re-place scroll so the same content point
  // stays under the cursor. Runs after the sizer resizes, before paint.
  useLayoutEffect(() => {
    const anchor = zoomAnchorRef.current;
    const wrap = wrapRef.current;
    if (!anchor || !wrap) return;
    wrap.scrollLeft = anchor.contentX * viewport.zoom - anchor.offX;
    wrap.scrollTop = anchor.contentY * viewport.zoom - anchor.offY;
    zoomAnchorRef.current = null;
  }, [viewport.zoom]);

  // Pan handling
  const handlePanStart = useCallback((e: React.PointerEvent) => {
    if (e.button !== 0) return;
    // Only start panning if clicking on the canvas background (not a component)
    if ((e.target as HTMLElement).closest('.hf-cmp, .hf-bc-group')) return;

    e.preventDefault();
    panRef.current = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      scrollLeft: wrapRef.current?.scrollLeft ?? 0,
      scrollTop: wrapRef.current?.scrollTop ?? 0,
    };
    wrapRef.current?.setPointerCapture(e.pointerId);
    setPanning(true);
  }, []);

  const handlePanMove = useCallback((e: React.PointerEvent) => {
    const pan = panRef.current;
    if (!pan || pan.pointerId !== e.pointerId || !wrapRef.current) return;

    const dx = e.clientX - pan.startX;
    const dy = e.clientY - pan.startY;
    wrapRef.current.scrollLeft = pan.scrollLeft - dx;
    wrapRef.current.scrollTop = pan.scrollTop - dy;
  }, []);

  const handlePanEnd = useCallback((e: React.PointerEvent) => {
    const pan = panRef.current;
    if (!pan || pan.pointerId !== e.pointerId) return;

    panRef.current = null;
    setPanning(false);
    wrapRef.current?.releasePointerCapture(e.pointerId);
  }, []);

  // Click on canvas background clears focus
  const handleCanvasClick = useCallback((e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest('.hf-cmp, .hf-bc-group')) return;
    setFocusId(null);
  }, []);

  // Loading state
  if (!laidGraph) {
    return (
      <div className="graph-container flex h-full items-center justify-center">
        <div className="text-center">
          {layoutError ? (
            <p className="text-red-500">{layoutError}</p>
          ) : (
            <p className="text-muted-foreground">Laying out graph...</p>
          )}
        </div>
      </div>
    );
  }

  // Get BC name for component parent
  const bcById = new Map(laidGraph.boundedContexts.map(bc => [bc.id, bc]));

  return (
    <div className="graph-container relative flex flex-col h-full w-full">
      {/* Canvas viewport */}
      <div className="hf-canvas-viewport flex flex-col flex-1 relative">
        {/* Scrollable canvas wrapper */}
        <div
          ref={wrapRef}
          className={`hf-canvas-wrap ${panning ? 'panning' : ''}`}
          onPointerDown={handlePanStart}
          onPointerMove={handlePanMove}
          onPointerUp={handlePanEnd}
          onPointerCancel={handlePanEnd}
          onClick={handleCanvasClick}
        >
          {/* Scaled sizer for scrollbar tracking */}
          <div
            className="hf-canvas-sizer"
            style={{
              width: canvasBounds.width * viewport.zoom,
              height: canvasBounds.height * viewport.zoom,
            }}
          >
            {/* Scaled canvas content */}
            <div
              className="hf-canvas"
              style={{
                width: canvasBounds.width,
                height: canvasBounds.height,
                transform: `scale(${viewport.zoom})`,
                transformOrigin: 'top left',
              }}
            >
              {/* BC group containers */}
              <BCGroups
                boundedContexts={laidGraph.boundedContexts}
                show={true}
                showLabels={true}
              />

              {/* Edge layer (below components) */}
              <EdgeLayer
                edges={laidGraph.edges}
                components={laidGraph.components}
                expandedSet={expanded}
                showDiff={showDiff}
                focusId={focusId}
              />

              {/* Symbol relations layer */}
              <RelationLayer
                relations={laidGraph.relations ?? []}
                components={laidGraph.components}
                expandedSet={expanded}
                expandedInternals={internalExpanded}
                showDiff={showDiff}
                focusId={focusId}
              />

              {/* Component cards */}
              {laidGraph.components.map((cmp) => (
                <ComponentCard
                  key={cmp.id}
                  cmp={cmp}
                  expanded={expanded.has(cmp.id)}
                  onToggleExpand={handleToggleExpand}
                  expandedInternals={internalExpanded}
                  wideInternals={internalWide}
                  onToggleWide={handleToggleInternalWide}
                  onToggleInternalExpand={handleToggleInternalExpand}
                  parentName={bcById.get(cmp.bc)?.name}
                  showDiff={showDiff}
                  onSelect={handleSelectComponent}
                  focused={focusId === cmp.id}
                  dimmed={focusId !== null && focusId !== cmp.id}
                  cardDensity={cardDensity}
                  showInlineSignatures={showInlineSignatures}
                  zoom={viewport.zoom}
                  relations={laidGraph.relations?.filter(
                    r => r.fromComponentId === cmp.id && r.toComponentId === cmp.id
                  ) ?? []}
                />
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Legend overlay — pinned to the card frame, not the scroll area */}
      <Legend showDiff={showDiff} />

      {/* Toolbar overlay — pinned to the card frame, not the scroll area */}
      <div className="hf-canvas-toolbar">
        <button onClick={() => handleZoom(-0.1)} title="Zoom out">-</button>
        <button className="zoom" onClick={handleResetZoom} title="Reset zoom">
          {Math.round(viewport.zoom * 100)}%
        </button>
        <button onClick={() => handleZoom(0.1)} title="Zoom in">+</button>
      </div>
    </div>
  );
}
