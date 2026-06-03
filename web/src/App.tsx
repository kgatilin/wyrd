import { useEffect, useLayoutEffect, useState, useMemo, useRef } from 'react';
import type { UIGraph, Component as ComponentType } from './types';
import { createAppStore } from './runtime/createAppStore';
import { StoreProvider, useStore, useDispatch } from './runtime/react';
import { relatedIds, deriveChanges } from './domain/derive';
import { AppBar } from './components/AppBar';
import { PrHeader } from './components/PrHeader';
import { BCGroups } from './components/BCGroups';
import { Component } from './components/Component';
import { EdgeLayer } from './components/EdgeLayer';
import { Legend } from './components/Legend';
import { CanvasToolbar } from './components/CanvasToolbar';
import { Tree, TreeFocusTarget } from './components/Tree';
import { ChangesPanel, type ChangeEntry } from './components/ChangesPanel';
import { InlinePopover, PendingComment } from './components/InlinePopover';
import { PinnedMarker, Marker } from './components/PinnedMarker';

/**
 * Main application component - V4 layout shell.
 * Owns the store (composition root) and bootstraps the graph load; the store
 * now drives data/layout/semantic state. AppContent renders once a graph exists.
 */
export default function App() {
  const [store] = useState(() => createAppStore());
  useEffect(() => {
    store.dispatch({ type: 'GraphRequested' });
  }, [store]);
  return (
    <StoreProvider store={store}>
      <AppRoot />
    </StoreProvider>
  );
}

function AppRoot() {
  const theme = useStore((s) => s.ui.theme);
  const load = useStore((s) => s.load);
  const graph = useStore((s) => s.graph);

  if (load.status === 'error') {
    return (
      <div className={`hifi v4 theme-${theme}`} style={{ padding: 20 }}>
        <p style={{ color: 'var(--rem-fg)' }}>Error: {load.error}</p>
      </div>
    );
  }
  if (!graph) {
    return (
      <div className={`hifi v4 theme-${theme}`} style={{ padding: 20 }}>
        <p>Loading...</p>
      </div>
    );
  }
  return <AppContent graph={graph} />;
}

/**
 * Inner component that renders the app once graph is loaded.
 * Separated to ensure hooks can reference graph.
 *
 * Data-flow:
 *   graph (raw, from App) ──► semantic consumers (expansion, focus, diff, tree, changes)
 *                         └──► layout effect ──► laid (UIGraph with ELK geometry)
 *                                                  └──► geometry consumers (BCGroups, EdgeLayer,
 *                                                       Component map, seedMarkers, canvasDimensions,
 *                                                       goToChange scroll)
 */
function AppContent({ graph }: { graph: UIGraph }) {
  // Store-owned data/layout/semantic state (Plan 2a). The viewport (zoom/pan/
  // scroll) and comments (markers/popover) remain local islands for now.
  const dispatch = useDispatch();
  const theme = useStore((s) => s.ui.theme);
  const level = useStore((s) => s.ui.level);
  const expanded = useStore((s) => s.ui.expanded);
  const internalExpanded = useStore((s) => s.ui.internalExpanded);
  const internalWide = useStore((s) => s.ui.internalWide);
  const focusId = useStore((s) => s.ui.focusId);
  const leftTab = useStore((s) => s.ui.leftTab);
  const leftCollapsed = useStore((s) => s.ui.leftCollapsed);
  const rightCollapsed = useStore((s) => s.ui.rightCollapsed);
  const activeChangeId = useStore((s) => s.ui.activeChangeId);
  const laid = useStore((s) => s.geometry.laid);
  const layoutError = useStore((s) => (s.geometry.status === 'error' ? s.geometry.error : null));
  const related = useMemo(() => relatedIds(graph, focusId), [graph, focusId]);

  // Determine if diff mode is active (semantic — raw graph)
  const showDiff = graph.pr != null;

  // Map bounded-context id → display name, so each component's header icon can
  // show its parent (BC) initial instead of its own.
  const bcNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const bc of graph.boundedContexts) m.set(bc.id, bc.name);
    return m;
  }, [graph.boundedContexts]);

  // Derive changes from graph (semantic — raw graph)
  const changes = useMemo(() => deriveChanges(graph), [graph]);

  // Canvas wrap ref for scroll operations
  const canvasWrapRef = useRef<HTMLDivElement>(null);

  // Empty slack (unscaled px) reserved around the diagram on every side so the
  // canvas feels "borderless": you can drag/scroll well past the diagram in any
  // direction (≈ one screen). The diagram content is shifted right/down by this
  // much, so all viewport↔content scroll math below adds PAN_MARGIN.
  const PAN_MARGIN = 1200;
  // True when the last pointer interaction was a pan-drag, so the click that
  // follows mouseup doesn't also clear focus/selection.
  const didPanRef = useRef(false);

  // ── Zoom ────────────────────────────────────────────────────────────────
  // Applied as a CSS transform on .hf-canvas; a sizer reserves the scaled space
  // so scrollbars track. Default 1 (100%).
  const ZOOM_MIN = 0.4;
  const ZOOM_MAX = 2;
  const ZOOM_STEP = 0.1;
  const [zoom, setZoom] = useState(1);
  const zoomBy = (delta: number) =>
    setZoom((z) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round((z + delta) * 100) / 100)));
  const fitZoom = () => {
    const wrap = canvasWrapRef.current;
    if (!wrap) {
      setZoom(1);
      return;
    }
    const fit = Math.min(
      wrap.clientWidth / canvasDimensions.width,
      wrap.clientHeight / canvasDimensions.height,
      1
    );
    const z = Math.max(ZOOM_MIN, Math.round(fit * 100) / 100);
    // Land the diagram's top-left near the viewport corner (past the margin).
    // Applied via pendingScrollRef so it runs after the sizer resizes for `z`.
    pendingScrollRef.current = { left: PAN_MARGIN * z - 40, top: PAN_MARGIN * z - 40 };
    setZoom(z);
  };

  // After a wheel-zoom changes `zoom`, re-anchor the scroll so the content point
  // under the cursor stays put. Applied in a layout effect so the sizer has
  // already grown/shrunk (otherwise scroll would clamp to the stale size).
  const pendingScrollRef = useRef<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const wrap = canvasWrapRef.current;
    if (wrap && pendingScrollRef.current) {
      wrap.scrollLeft = pendingScrollRef.current.left;
      wrap.scrollTop = pendingScrollRef.current.top;
      pendingScrollRef.current = null;
    }
  }, [zoom]);

  // Ctrl/Cmd + wheel (and trackpad pinch, which the browser reports as
  // ctrl+wheel) zooms the diagram instead of the whole page. Attached natively
  // with { passive: false } because React's onWheel is passive — preventDefault
  // there is ignored. Plain wheel keeps scrolling the canvas as usual.
  useEffect(() => {
    const wrap = canvasWrapRef.current;
    if (!wrap) return;
    const onWheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      const rect = wrap.getBoundingClientRect();
      const cx = e.clientX - rect.left;
      const cy = e.clientY - rect.top;
      const step = e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP;
      setZoom((z) => {
        const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round((z + step) * 100) / 100));
        if (next === z) return z;
        // Content point currently under the cursor (unscaled canvas coords);
        // the diagram is shifted by PAN_MARGIN, so subtract it here…
        const contentX = (wrap.scrollLeft + cx) / z - PAN_MARGIN;
        const contentY = (wrap.scrollTop + cy) / z - PAN_MARGIN;
        // …and add it back so that point remains under the cursor at `next`.
        pendingScrollRef.current = {
          left: (contentX + PAN_MARGIN) * next - cx,
          top: (contentY + PAN_MARGIN) * next - cy,
        };
        return next;
      });
    };
    wrap.addEventListener('wheel', onWheel, { passive: false });
    return () => wrap.removeEventListener('wheel', onWheel);
  }, []);

  // Drag-to-pan: grab empty canvas background and drag to scroll the diagram in
  // any direction. Drags that start on an interactive node (component, port,
  // edge, marker, chrome) are ignored so clicks/selection still work there.
  useEffect(() => {
    const wrap = canvasWrapRef.current;
    if (!wrap) return;
    let pan: { x: number; y: number; left: number; top: number; moved: boolean } | null = null;
    const INTERACTIVE = '.hf-cmp, .hf-port, .hf-pin-marker, .hf-popover, .edges-svg g, .hf-canvas-toolbar, .hf-canvas-legend';
    const onDown = (e: MouseEvent) => {
      if (e.button !== 0) return;
      const t = e.target as HTMLElement;
      if (t.closest && t.closest(INTERACTIVE)) return;
      pan = { x: e.clientX, y: e.clientY, left: wrap.scrollLeft, top: wrap.scrollTop, moved: false };
      wrap.classList.add('panning');
    };
    const onMove = (e: MouseEvent) => {
      if (!pan) return;
      const dx = e.clientX - pan.x;
      const dy = e.clientY - pan.y;
      if (Math.abs(dx) > 3 || Math.abs(dy) > 3) pan.moved = true;
      wrap.scrollLeft = pan.left - dx;
      wrap.scrollTop = pan.top - dy;
    };
    const onUp = () => {
      if (pan && pan.moved) didPanRef.current = true;
      pan = null;
      wrap.classList.remove('panning');
    };
    wrap.addEventListener('mousedown', onDown);
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      wrap.removeEventListener('mousedown', onDown);
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, []);

  // After the first layout resolves, scroll the diagram into view (it sits one
  // PAN_MARGIN in from the top-left of the oversized scroll area).
  const didInitScrollRef = useRef(false);
  useLayoutEffect(() => {
    if (laid && !didInitScrollRef.current && canvasWrapRef.current) {
      didInitScrollRef.current = true;
      canvasWrapRef.current.scrollLeft = PAN_MARGIN * zoom - 40;
      canvasWrapRef.current.scrollTop = PAN_MARGIN * zoom - 40;
    }
  }, [laid, zoom]);

  // Comment state
  const [pendingComment, setPendingComment] = useState<PendingComment | null>(null);
  const [activeMarkerId, setActiveMarkerId] = useState<string | null>(null);

  // Seed markers from graph.comments, placing them near their laid-out targets.
  // Uses `laid` for x/y/w so marker positions are correct after layout.
  const seedMarkers = useMemo((): Marker[] => {
    // Fall back to raw graph components for id-matching; use laid components
    // for geometry so positions are accurate once layout is done.
    const laidComponents = laid?.components ?? graph.components;
    const laidEdges = laid?.edges ?? graph.edges;

    return graph.comments.map((cm, i) => {
      // Find component containing this target (id-match against raw graph is equivalent)
      let host: ComponentType | undefined = laidComponents.find(
        (c) => c.id === cm.target.id
      );
      if (!host) {
        host = laidComponents.find(
          (c) =>
            c.internals.some(
              (it) =>
                it.id === cm.target.id ||
                (it.members ?? []).some((mm) => mm.id === cm.target.id)
            ) || c.ports.some((p) => p.id === cm.target.id)
        );
      }
      if (!host && cm.target.type === 'edge') {
        const edge = laidEdges.find((e) => e.id === cm.target.id);
        if (edge) {
          host = laidComponents.find((c) => c.id === edge.from);
        }
      }

      // Calculate position near the host component
      let x = 80 + i * 130;
      let y = 30 + (i % 2) * 40;
      if (host && host.x != null && host.y != null && host.w != null) {
        x = host.x + host.w + 8;
        y = host.y - 10;
      }

      return {
        id: `seed-${i}`,
        n: i + 1,
        x,
        y,
        target: cm.target,
        body: cm.body,
        author: '@you',
        when: '2m',
      };
    });
  }, [graph.comments, laid]);

  const [markers, setMarkers] = useState<Marker[]>(seedMarkers);

  // Update markers when seed markers change (e.g., different graph loaded or re-laid-out)
  useEffect(() => {
    setMarkers(seedMarkers);
  }, [seedMarkers]);

  // Comment targets for highlighting (from current markers)
  const commentTargets = useMemo(
    () => new Set(markers.map((m) => m.target.id)),
    [markers]
  );

  // Start a comment on a target element
  const startComment = (
    target: { type: string; id: string },
    evt: React.MouseEvent
  ) => {
    let x = 300;
    let y = 300;

    if (canvasWrapRef.current) {
      const wrap = canvasWrapRef.current.getBoundingClientRect();
      const sx = canvasWrapRef.current.scrollLeft;
      const sy = canvasWrapRef.current.scrollTop;

      // getBoundingClientRect + scrollLeft give coords in the SCALED content
      // space; divide by zoom to convert to unscaled canvas coords (the space
      // the popover and markers are positioned in, inside the scaled .hf-canvas).
      const currentTarget = evt.currentTarget as HTMLElement;
      if (currentTarget && currentTarget.getBoundingClientRect) {
        const rect = currentTarget.getBoundingClientRect();
        x = (rect.left - wrap.left + sx + rect.width / 2) / zoom - PAN_MARGIN;
        y = (rect.bottom - wrap.top + sy) / zoom - PAN_MARGIN + 8;
      } else if (evt.clientX != null) {
        x = (evt.clientX - wrap.left + sx) / zoom - PAN_MARGIN;
        y = (evt.clientY - wrap.top + sy) / zoom - PAN_MARGIN + 8;
      }
    }

    setPendingComment({ target, x, y });
  };

  // Submit a comment
  const submitComment = (text: string) => {
    if (!pendingComment) return;

    const n = markers.length + 1;
    const marker: Marker = {
      id: `m-${Date.now()}`,
      n,
      x: pendingComment.x,
      y: pendingComment.y - 8,
      target: pendingComment.target,
      body: text,
      author: '@you',
      when: 'just now',
    };

    setMarkers((prev) => [...prev, marker]);
    setPendingComment(null);
    setActiveMarkerId(marker.id);
  };

  // Smooth-scroll a component to the center of the viewport.
  // Uses `laid` geometry so coordinates match what's on screen; scroll positions
  // are in scaled content space → multiply by zoom.
  const scrollToComponent = (componentId: string) => {
    const laidComponents = laid?.components ?? graph.components;
    const component = laidComponents.find((c) => c.id === componentId);
    if (component && canvasWrapRef.current) {
      const x = component.x ?? 0;
      const y = component.y ?? 0;
      const w = component.w ?? 220;
      const h = component.h ?? 86;
      canvasWrapRef.current.scrollTo({
        left: (PAN_MARGIN + x + w / 2) * zoom - canvasWrapRef.current.clientWidth / 2,
        top: (PAN_MARGIN + y + h / 2) * zoom - canvasWrapRef.current.clientHeight / 2,
        behavior: 'smooth',
      });
    }
  };

  // Go to a change: focus + expand (store) + scroll (local viewport glue).
  const goToChange = (ch: ChangeEntry) => {
    dispatch({ type: 'ChangeActivated', change: ch });
    setTimeout(() => scrollToComponent(ch.cmp), 150);
  };

  // Focus an object from the context tree: focus + expand (store), then scroll
  // (local). 200ms only when this actually triggers an expand re-layout; otherwise scroll now.
  const focusFromTree = (t: TreeFocusTarget) => {
    const willExpand = !!(t.internalId || t.memberId) && !expanded.has(t.componentId);
    dispatch({ type: 'TreeFocusRequested', target: t });
    setTimeout(() => scrollToComponent(t.componentId), willExpand ? 200 : 0);
  };

  // Handle component selection (for focus mode) — store toggles focus.
  const handleSelectComponent = (cmp: ComponentType) => {
    dispatch({ type: 'ComponentSelected', id: cmp.id });
  };

  // Handle canvas background click (clear focus + pending comment). Skipped right
  // after a pan-drag so dragging the canvas doesn't also clear the selection.
  // Focus is cleared via the store; comments stay a local island for now.
  const handleCanvasClick = () => {
    if (didPanRef.current) {
      didPanRef.current = false;
      return;
    }
    dispatch({ type: 'FocusCleared' });
    setPendingComment(null);
    setActiveMarkerId(null);
  };

  // Handle marker click in right panel - scroll to marker
  const handleMarkerCardClick = (marker: Marker) => {
    setActiveMarkerId(marker.id);
    if (canvasWrapRef.current) {
      canvasWrapRef.current.scrollTo({
        left: (PAN_MARGIN + marker.x) * zoom - canvasWrapRef.current.clientWidth / 2,
        top: (PAN_MARGIN + marker.y) * zoom - canvasWrapRef.current.clientHeight / 2,
        behavior: 'smooth',
      });
    }
  };

  // Calculate canvas dimensions based on laid-out content (geometry)
  const canvasDimensions = useMemo(() => {
    let maxX = 1000;
    let maxY = 600;

    // Use laid geometry; fall back to raw (pre-layout) values so canvas
    // is not zero-sized on the initial render before ELK resolves.
    const bcs = laid?.boundedContexts ?? graph.boundedContexts;
    const cmps = laid?.components ?? graph.components;

    for (const bc of bcs) {
      if (bc.x != null && bc.w != null) {
        maxX = Math.max(maxX, bc.x + bc.w + 50);
      }
      if (bc.y != null && bc.h != null) {
        maxY = Math.max(maxY, bc.y + bc.h + 50);
      }
    }

    for (const c of cmps) {
      if (c.x != null && c.wx != null) {
        maxX = Math.max(maxX, c.x + c.wx + 50);
      }
      if (c.y != null && c.hx != null) {
        maxY = Math.max(maxY, c.y + c.hx + 100);
      }
    }

    return { width: maxX, height: maxY };
  }, [laid, graph.boundedContexts, graph.components]);

  return (
    <div
      className={`hifi v4 theme-${theme}`}
      style={{ width: '100%', height: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      {/* AppBar / PrHeader use semantic data from raw graph */}
      <AppBar
        level={level}
        onLevelChange={(l) => dispatch({ type: 'LevelChanged', level: l })}
        theme={theme}
        onThemeToggle={() => dispatch({ type: 'ThemeToggled' })}
        commentCount={markers.length}
        pr={graph.pr}
      />

      {graph.pr && <PrHeader pr={graph.pr} />}

      <div className="hf-stage">
        {/* LEFT PANE - collapsible, 2 modes (CHANGES | CONTEXTS) */}
        {/* Tree and ChangesPanel are semantic — raw graph */}
        <div className={`hf-side hf-collapsible ${leftCollapsed ? 'collapsed' : ''}`}>
          <button
            className="hf-side-toggle left"
            onClick={() => dispatch({ type: 'LeftCollapsedToggled' })}
          >
            {leftCollapsed ? '›' : '‹'}
          </button>

          {leftCollapsed ? (
            <span className="hf-side-vlabel">
              {leftTab === 'changes' ? 'CHANGES' : 'CONTEXTS'}
            </span>
          ) : (
            <>
              <div className="hf-tabs" style={{ flexShrink: 0 }}>
                {showDiff && (
                  <button
                    className={leftTab === 'changes' ? 'on' : ''}
                    onClick={() => dispatch({ type: 'LeftTabChanged', tab: 'changes' })}
                  >
                    CHANGES<span className="count">{changes.length}</span>
                  </button>
                )}
                <button
                  className={leftTab === 'tree' ? 'on' : ''}
                  onClick={() => dispatch({ type: 'LeftTabChanged', tab: 'tree' })}
                >
                  CONTEXTS<span className="count">{graph.boundedContexts.length}</span>
                </button>
              </div>

              {leftTab === 'changes' && showDiff && (
                <ChangesPanel
                  graph={graph}
                  changes={changes}
                  activeChangeId={activeChangeId}
                  onChangeClick={goToChange}
                />
              )}

              {leftTab === 'tree' && (
                <div className="hf-list" style={{ paddingTop: 6 }}>
                  <Tree
                    boundedContexts={graph.boundedContexts}
                    components={graph.components}
                    showDiff={showDiff}
                    activeId={focusId}
                    onFocus={focusFromTree}
                  />
                </div>
              )}
            </>
          )}
        </div>

        {/* CENTER PANE - canvas viewport (does not scroll; pins toolbar/legend) */}
        <div className="hf-canvas-viewport">
        <div
          ref={canvasWrapRef}
          className="hf-canvas-wrap"
          onClick={handleCanvasClick}
        >
          {/* Show loading placeholder until first ELK layout resolves.
              We keep the previous laid graph visible during re-layout so
              the canvas never flashes empty. On an unrecoverable ELK error
              we surface a message instead of hanging indefinitely. */}
          {laid == null ? (
            <div style={{ padding: 20 }}>
              {layoutError != null
                ? <p style={{ color: 'var(--rem-fg)' }}>Layout failed: {layoutError}</p>
                : <p>Laying out…</p>
              }
            </div>
          ) : (
          // Sizer reserves the scaled footprint so scrollbars track the zoom;
          // the inner .hf-canvas is scaled from its top-left corner.
          <div
            className="hf-canvas-sizer"
            style={{
              width: (canvasDimensions.width + 2 * PAN_MARGIN) * zoom,
              height: (canvasDimensions.height + 2 * PAN_MARGIN) * zoom,
            }}
          >
          <div
            className="hf-canvas"
            style={{
              width: canvasDimensions.width,
              height: canvasDimensions.height,
              transform: `translate(${PAN_MARGIN * zoom}px, ${PAN_MARGIN * zoom}px) scale(${zoom})`,
              transformOrigin: '0 0',
            }}
          >
            {/* Bounded context groups — geometry from laid */}
            <BCGroups boundedContexts={laid.boundedContexts} show={true} />

            {/* Edge layer (SVG) — geometry from laid */}
            <EdgeLayer
              edges={laid.edges}
              components={laid.components}
              expandedSet={expanded}
              expandedInternals={internalExpanded}
              showDiff={showDiff}
              focusId={focusId}
              flow={true}
              commentTargets={commentTargets}
              onAddComment={startComment}
            />

            {/* Components — geometry from laid, expansion/focus from raw state */}
            {laid.components.map((c) => (
              <Component
                key={c.id}
                cmp={c}
                expanded={expanded.has(c.id)}
                onToggleExpand={(id) => dispatch({ type: 'ComponentToggled', id })}
                expandedInternals={internalExpanded}
                wideInternals={internalWide}
                onToggleWide={(id) => dispatch({ type: 'InternalWideToggled', id })}
                onSetAllWide={(componentId, wide) => dispatch({ type: 'ComponentAllWideSet', id: componentId, wide })}
                parentName={bcNameById.get(c.bc)}
                showDiff={showDiff}
                focused={focusId === c.id}
                dimmed={!!(focusId && related && !related.has(c.id))}
                onSelect={handleSelectComponent}
                onAddComment={startComment}
                commentTargets={commentTargets}
              />
            ))}

            {/* Pinned numbered comment markers */}
            {markers.map((m) => (
              <PinnedMarker
                key={m.id}
                marker={m}
                active={activeMarkerId === m.id}
                onClick={(mm) => setActiveMarkerId(mm.id)}
              />
            ))}

            {/* Inline comment popover */}
            <InlinePopover
              pending={pendingComment}
              onCancel={() => setPendingComment(null)}
              onSubmit={submitComment}
            />
          </div>
          </div>
          )}
        </div>

          {/* Canvas toolbar — pinned to the viewport, not the scroller */}
          <CanvasToolbar
            zoom={zoom}
            onZoomOut={() => zoomBy(-ZOOM_STEP)}
            onZoomIn={() => zoomBy(ZOOM_STEP)}
            onFit={fitZoom}
          />

          {/* Legend */}
          <Legend showDiff={showDiff} />
        </div>

        {/* RIGHT PANE - comments reference (collapsible) */}
        <div
          className={`hf-side right hf-collapsible ${rightCollapsed ? 'collapsed' : ''}`}
        >
          <button
            className="hf-side-toggle right"
            onClick={() => dispatch({ type: 'RightCollapsedToggled' })}
          >
            {rightCollapsed ? '‹' : '›'}
          </button>

          {rightCollapsed ? (
            <span className="hf-side-vlabel">COMMENTS - {markers.length}</span>
          ) : (
            <>
              {/* Header styled identically to the left panel's CONTEXTS tab. */}
              <div className="hf-tabs" style={{ flexShrink: 0 }}>
                <button className="on">
                  COMMENTS<span className="count">{markers.length}</span>
                </button>
              </div>
              <div className="hf-list" style={{ paddingTop: 6 }}>
                {markers.map((m) => (
                  <div
                    key={m.id}
                    className={`hf-card ${activeMarkerId === m.id ? 'active' : ''}`}
                    onClick={() => handleMarkerCardClick(m)}
                  >
                    <div className="hf-card-meta">
                      <span className="hf-pin-marker-mini">{m.n}</span>
                      <span className="hf-card-author">{m.author}</span>
                      <span>- {m.when}</span>
                      <span className="hf-card-target">
                        {m.target.type}:{m.target.id}
                      </span>
                    </div>
                    <div className="hf-card-body">{m.body}</div>
                  </div>
                ))}
                <div
                  style={{
                    textAlign: 'center',
                    color: 'var(--fg-3)',
                    fontSize: 11,
                    padding: '12px 0',
                    fontFamily: 'JetBrains Mono, monospace',
                  }}
                >
                  click any element on canvas - comment
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
