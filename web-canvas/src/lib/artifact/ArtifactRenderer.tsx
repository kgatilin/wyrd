"use client";

import * as React from 'react';
import { useEffect, useState } from 'react';
import { compileArtifact } from './compile';

/**
 * Renders a single artifact FILE: agent-authored JSX that composes a document
 * using host components (`Graph`, `dataSource`). The file is compiled in the
 * browser via {@link compileArtifact}; a compile/eval failure is shown inline
 * (the same result the agent gets back after write_file).
 */
export function ArtifactRenderer({ code }: { code: string }) {
  const [node, setNode] = useState<React.ReactNode>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const result = await compileArtifact(code);
      if (cancelled) return;
      if (result.ok) {
        setError(null);
        setNode(React.createElement(result.Component));
      } else {
        setError(result.error);
        setNode(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [code]);

  if (error) return <ArtifactError error={error} />;
  return <ArtifactBoundary>{node}</ArtifactBoundary>;
}

function ArtifactError({ error }: { error: string }) {
  return (
    <div className="artifact-error">
      <strong>Artifact failed to render</strong>
      <pre>{error}</pre>
    </div>
  );
}

/** Catches render-time errors thrown by agent-authored artifact code. */
class ArtifactBoundary extends React.Component<
  { children: React.ReactNode },
  { error: string | null }
> {
  state = { error: null as string | null };

  static getDerivedStateFromError(err: unknown) {
    return { error: err instanceof Error ? err.message : String(err) };
  }

  componentDidUpdate(prev: { children: React.ReactNode }) {
    if (prev.children !== this.props.children && this.state.error) {
      this.setState({ error: null });
    }
  }

  render() {
    if (this.state.error) return <ArtifactError error={this.state.error} />;
    return this.props.children;
  }
}
