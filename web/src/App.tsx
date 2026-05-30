import { useEffect, useState } from 'react';
import type { UIGraph } from './types';
import { loadGraph } from './data/load';

// Placeholder App - will be replaced in B3
export default function App() {
  const [graph, setGraph] = useState<UIGraph | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadGraph()
      .then(setGraph)
      .catch((err) => setError(String(err)));
  }, []);

  if (error) return <div className="hifi v4 theme-dark">Error: {error}</div>;
  if (!graph) return <div className="hifi v4 theme-dark">Loading...</div>;

  return (
    <div className="hifi v4 theme-dark" style={{ width: '100%', height: '100vh' }}>
      <div style={{ padding: 20 }}>
        <p>Loaded graph with {graph.components.length} components</p>
        <p>Schema: {graph.schema}</p>
      </div>
    </div>
  );
}
