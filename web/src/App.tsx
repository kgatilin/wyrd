import { useEffect, useState } from 'react';
import type { UIGraph } from './types';
import { loadGraph } from './data/load';
import { layout } from './layout/layout';
import { AppBar } from './components/AppBar';
import { PrHeader } from './components/PrHeader';

/**
 * Main application component - V4 layout shell.
 * Loads the graph, applies layout, and renders the 3-pane stage.
 */
export default function App() {
  const [graph, setGraph] = useState<UIGraph | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [theme, setTheme] = useState<'dark' | 'light'>('dark');
  const [level, setLevel] = useState(2); // Default to L3/Component
  const [commentCount] = useState(0); // Placeholder - will be wired to actual comments

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
    <div
      className={`hifi v4 theme-${theme}`}
      style={{ width: '100%', height: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      <AppBar
        level={level}
        onLevelChange={setLevel}
        theme={theme}
        onThemeToggle={toggleTheme}
        commentCount={graph.comments.length + commentCount}
        pr={graph.pr}
      />

      {graph.pr && <PrHeader pr={graph.pr} />}

      <div className="hf-stage">
        {/* LEFT PANE - placeholder */}
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

        {/* CENTER PANE - canvas placeholder */}
        <div className="hf-canvas-wrap">
          <div className="hf-canvas" style={{ minWidth: 1000, minHeight: 600 }}>
            {/* Canvas content will be added in Phase C */}
            <div
              style={{
                position: 'absolute',
                top: '50%',
                left: '50%',
                transform: 'translate(-50%, -50%)',
                color: 'var(--fg-2)',
                fontFamily: 'var(--mono, monospace)',
              }}
            >
              Canvas: {graph.components.length} components, {graph.edges.length} edges
            </div>
          </div>
        </div>

        {/* RIGHT PANE - placeholder */}
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
