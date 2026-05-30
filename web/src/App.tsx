import { useEffect, useState, useMemo } from 'react';
import type { UIGraph } from './types';
import { loadGraph } from './data/load';
import { layout } from './layout/layout';
import { useExpansion } from './state/hooks';
import { AppBar } from './components/AppBar';
import { PrHeader } from './components/PrHeader';
import { BCGroups } from './components/BCGroups';
import { Component } from './components/Component';
import { EdgeLayer } from './components/EdgeLayer';
import { Legend } from './components/Legend';
import { CanvasToolbar } from './components/CanvasToolbar';

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
      .then((g) => setGraph(layout(g)))
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

  // Comment targets for highlighting (seeded from graph.comments)
  const commentTargets = useMemo(
    () => new Set(graph.comments.map((c) => c.target.id)),
    [graph.comments]
  );

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
        commentCount={graph.comments.length}
        pr={graph.pr}
      />

      {graph.pr && <PrHeader pr={graph.pr} />}

      <div className="hf-stage">
        {/* LEFT PANE - placeholder for Phase D */}
        <div className="hf-side">
          <div className="hf-side-title">CONTEXTS</div>
          <div className="hf-tree">
            {graph.boundedContexts.map((bc) => (
              <div key={bc.id} className="hf-tree-row bc">
                <span className="chev">&#9662;</span>
                <span className="ico">&#9635;</span>
                <span className="name">{bc.name}</span>
              </div>
            ))}
          </div>
        </div>

        {/* CENTER PANE - canvas */}
        <div className="hf-canvas-wrap" style={{ flex: 1 }}>
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
              focusId={null} // Focus mode is Phase D
              flow={true}
              commentTargets={commentTargets}
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
                commentTargets={commentTargets}
              />
            ))}
          </div>

          {/* Canvas toolbar */}
          <CanvasToolbar />

          {/* Legend */}
          <Legend showDiff={showDiff} />
        </div>

        {/* RIGHT PANE - placeholder for Phase D */}
        <div className="hf-side right">
          <div className="hf-side-title">COMMENTS</div>
          <div className="hf-list">
            {graph.comments.length === 0 ? (
              <div
                style={{
                  textAlign: 'center',
                  color: 'var(--fg-3)',
                  fontSize: 11,
                  padding: '12px 0',
                  fontFamily: 'JetBrains Mono, monospace',
                }}
              >
                No comments yet
              </div>
            ) : (
              graph.comments.map((c) => (
                <div key={c.id} className="hf-card">
                  <div className="hf-card-meta">
                    <span className="hf-card-author">@you</span>
                    <span className="hf-card-target">
                      {c.target.type}:{c.target.id}
                    </span>
                  </div>
                  <div className="hf-card-body">{c.body}</div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
