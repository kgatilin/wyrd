import { useEffect, useState, useMemo, useRef } from 'react';
import type { UIGraph, Component as ComponentType } from './types';
import { loadGraph } from './data/load';
import { layout } from './layout/layout';
import { useExpansion, useFocus } from './state/hooks';
import { AppBar } from './components/AppBar';
import { PrHeader } from './components/PrHeader';
import { BCGroups } from './components/BCGroups';
import { Component } from './components/Component';
import { EdgeLayer } from './components/EdgeLayer';
import { Legend } from './components/Legend';
import { CanvasToolbar } from './components/CanvasToolbar';
import { Tree } from './components/Tree';
import { ChangesPanel, deriveChanges, ChangeEntry } from './components/ChangesPanel';
import { InlinePopover, PendingComment } from './components/InlinePopover';
import { PinnedMarker, Marker } from './components/PinnedMarker';

/**
 * Main application component - V4 layout shell.
 * Loads the graph, applies layout, and renders the 3-pane stage with canvas.
 */
export default function App() {
  const [graph, setGraph] = useState<UIGraph | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [theme, setTheme] = useState<'dark' | 'light'>('dark');
  const [level, setLevel] = useState(2); // Default to L3/Component

  // Load and layout the graph
  useEffect(() => {
    loadGraph()
      .then((g) => layout(g))
      .then((laid) => setGraph(laid))
      .catch((err) => setError(String(err)));
  }, []);

  const toggleTheme = () => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'));
  };

  if (error) {
    return (
      <div className={`hifi v4 theme-${theme}`} style={{ padding: 20 }}>
        <p style={{ color: 'var(--rem-fg)' }}>Error: {error}</p>
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

  return (
    <AppContent
      graph={graph}
      theme={theme}
      level={level}
      onLevelChange={setLevel}
      onThemeToggle={toggleTheme}
    />
  );
}

interface AppContentProps {
  graph: UIGraph;
  theme: 'dark' | 'light';
  level: number;
  onLevelChange: (level: number) => void;
  onThemeToggle: () => void;
}

/**
 * Inner component that renders the app once graph is loaded.
 * Separated to ensure hooks can reference graph.
 */
function AppContent({ graph, theme, level, onLevelChange, onThemeToggle }: AppContentProps) {
  // Determine if diff mode is active
  const showDiff = graph.pr != null;

  // Expansion hooks - initialize with first component expanded (or 'orders' if present)
  const initialExpanded = useMemo(() => {
    const orders = graph.components.find((c) => c.id === 'orders');
    if (orders) return ['orders'];
    if (graph.components.length > 0) return [graph.components[0].id];
    return [];
  }, [graph.components]);

  const { expanded, toggle, internalExpanded, toggleInternal } = useExpansion(
    graph,
    initialExpanded
  );

  // Focus mode
  const [focusId, setFocusId, related] = useFocus(graph);

  // Left panel state: tab selection and collapse
  const [leftTab, setLeftTab] = useState<'changes' | 'tree'>(showDiff ? 'changes' : 'tree');
  const [leftCollapsed, setLeftCollapsed] = useState(false);

  // Right panel state: collapse
  const [rightCollapsed, setRightCollapsed] = useState(false);

  // Active change (highlighted in list)
  const [activeChangeId, setActiveChangeId] = useState<string | null>(null);

  // Derive changes from graph
  const changes = useMemo(() => deriveChanges(graph), [graph]);

  // Canvas wrap ref for scroll operations
  const canvasWrapRef = useRef<HTMLDivElement>(null);

  // Comment state
  const [pendingComment, setPendingComment] = useState<PendingComment | null>(null);
  const [activeMarkerId, setActiveMarkerId] = useState<string | null>(null);

  // Seed markers from graph.comments, placing them near their targets
  const seedMarkers = useMemo((): Marker[] => {
    return graph.comments.map((cm, i) => {
      // Find component containing this target
      let host: ComponentType | undefined = graph.components.find(
        (c) => c.id === cm.target.id
      );
      if (!host) {
        host = graph.components.find(
          (c) =>
            c.internals.some(
              (it) =>
                it.id === cm.target.id ||
                (it.members ?? []).some((mm) => mm.id === cm.target.id)
            ) || c.ports.some((p) => p.id === cm.target.id)
        );
      }
      if (!host && cm.target.type === 'edge') {
        const edge = graph.edges.find((e) => e.id === cm.target.id);
        if (edge) {
          host = graph.components.find((c) => c.id === edge.from);
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
  }, [graph.comments, graph.components, graph.edges]);

  const [markers, setMarkers] = useState<Marker[]>(seedMarkers);

  // Update markers when seed markers change (e.g., different graph loaded)
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

      const currentTarget = evt.currentTarget as HTMLElement;
      if (currentTarget && currentTarget.getBoundingClientRect) {
        const rect = currentTarget.getBoundingClientRect();
        x = rect.left - wrap.left + sx + rect.width / 2;
        y = rect.bottom - wrap.top + sy + 8;
      } else if (evt.clientX != null) {
        x = evt.clientX - wrap.left + sx;
        y = evt.clientY - wrap.top + sy + 8;
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

  // Go to a change: focus + expand + scroll
  const goToChange = (ch: ChangeEntry) => {
    setActiveChangeId(ch.id);
    setFocusId(ch.cmp);

    // Expand component if navigating to internal/member/port
    if (ch.cmp && (ch.internal || ch.member || ch.port) && !expanded.has(ch.cmp)) {
      toggle(ch.cmp);
    }

    // Smooth scroll to component
    setTimeout(() => {
      const component = graph.components.find((c) => c.id === ch.cmp);
      if (component && canvasWrapRef.current) {
        const x = component.x ?? 0;
        const y = component.y ?? 0;
        const w = component.w ?? 220;
        const h = component.h ?? 86;

        canvasWrapRef.current.scrollTo({
          left: x + w / 2 - canvasWrapRef.current.clientWidth / 2,
          top: y + h / 2 - canvasWrapRef.current.clientHeight / 2,
          behavior: 'smooth',
        });
      }
    }, 150);
  };

  // Handle component selection (for focus mode)
  const handleSelectComponent = (cmp: ComponentType) => {
    // Toggle: if already focused, clear; otherwise set focus
    setFocusId(focusId === cmp.id ? null : cmp.id);
  };

  // Handle canvas background click (clear focus + pending comment)
  const handleCanvasClick = () => {
    setFocusId(null);
    setPendingComment(null);
    setActiveMarkerId(null);
  };

  // Handle marker click in right panel - scroll to marker
  const handleMarkerCardClick = (marker: Marker) => {
    setActiveMarkerId(marker.id);
    if (canvasWrapRef.current) {
      canvasWrapRef.current.scrollTo({
        left: marker.x - canvasWrapRef.current.clientWidth / 2,
        top: marker.y - canvasWrapRef.current.clientHeight / 2,
        behavior: 'smooth',
      });
    }
  };

  // Calculate canvas dimensions based on content
  const canvasDimensions = useMemo(() => {
    let maxX = 1000;
    let maxY = 600;

    for (const bc of graph.boundedContexts) {
      if (bc.x != null && bc.w != null) {
        maxX = Math.max(maxX, bc.x + bc.w + 50);
      }
      if (bc.y != null && bc.h != null) {
        maxY = Math.max(maxY, bc.y + bc.h + 50);
      }
    }

    for (const c of graph.components) {
      if (c.x != null && c.wx != null) {
        maxX = Math.max(maxX, c.x + c.wx + 50);
      }
      if (c.y != null && c.hx != null) {
        maxY = Math.max(maxY, c.y + c.hx + 100);
      }
    }

    return { width: maxX, height: maxY };
  }, [graph.boundedContexts, graph.components]);

  return (
    <div
      className={`hifi v4 theme-${theme}`}
      style={{ width: '100%', height: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      <AppBar
        level={level}
        onLevelChange={onLevelChange}
        theme={theme}
        onThemeToggle={onThemeToggle}
        commentCount={markers.length}
        pr={graph.pr}
      />

      {graph.pr && <PrHeader pr={graph.pr} />}

      <div className="hf-stage">
        {/* LEFT PANE - collapsible, 2 modes (CHANGES | CONTEXTS) */}
        <div className={`hf-side hf-collapsible ${leftCollapsed ? 'collapsed' : ''}`}>
          <button
            className="hf-side-toggle left"
            onClick={() => setLeftCollapsed(!leftCollapsed)}
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
                    onClick={() => setLeftTab('changes')}
                  >
                    CHANGES<span className="count">{changes.length}</span>
                  </button>
                )}
                <button
                  className={leftTab === 'tree' ? 'on' : ''}
                  onClick={() => setLeftTab('tree')}
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
                    onComponentClick={(id) => {
                      const cmp = graph.components.find((c) => c.id === id);
                      if (cmp) {
                        handleSelectComponent(cmp);
                      }
                    }}
                  />
                </div>
              )}
            </>
          )}
        </div>

        {/* CENTER PANE - canvas */}
        <div
          ref={canvasWrapRef}
          className="hf-canvas-wrap"
          style={{ flex: 1 }}
          onClick={handleCanvasClick}
        >
          <div
            className="hf-canvas"
            style={{
              minWidth: canvasDimensions.width,
              minHeight: canvasDimensions.height,
            }}
          >
            {/* Bounded context groups */}
            <BCGroups boundedContexts={graph.boundedContexts} show={true} />

            {/* Edge layer (SVG) */}
            <EdgeLayer
              edges={graph.edges}
              components={graph.components}
              expandedSet={expanded}
              expandedInternals={internalExpanded}
              showDiff={showDiff}
              focusId={focusId}
              flow={true}
              commentTargets={commentTargets}
              onAddComment={startComment}
            />

            {/* Components */}
            {graph.components.map((c) => (
              <Component
                key={c.id}
                cmp={c}
                expanded={expanded.has(c.id)}
                onToggleExpand={toggle}
                expandedInternals={internalExpanded}
                onToggleInternal={toggleInternal}
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

          {/* Canvas toolbar */}
          <CanvasToolbar />

          {/* Legend */}
          <Legend showDiff={showDiff} />
        </div>

        {/* RIGHT PANE - comments reference (collapsible) */}
        <div
          className={`hf-side right hf-collapsible ${rightCollapsed ? 'collapsed' : ''}`}
        >
          <button
            className="hf-side-toggle right"
            onClick={() => setRightCollapsed(!rightCollapsed)}
          >
            {rightCollapsed ? '‹' : '›'}
          </button>

          {rightCollapsed ? (
            <span className="hf-side-vlabel">COMMENTS - {markers.length}</span>
          ) : (
            <>
              <div
                className="hf-side-title"
                style={{ display: 'flex', alignItems: 'center' }}
              >
                Comments
                <span style={{ flex: 1 }} />
                <span
                  style={{
                    fontSize: 10,
                    color: 'var(--fg-3)',
                    textTransform: 'none',
                    letterSpacing: 0,
                  }}
                >
                  {markers.length} thread{markers.length !== 1 ? 's' : ''}
                </span>
              </div>
              <div className="hf-list" style={{ paddingTop: 4 }}>
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
