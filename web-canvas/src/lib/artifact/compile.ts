import * as React from 'react';
import { GraphView } from './host-scope';
import { useGraph } from '@/lib/data/graph';
import { useEvents } from '@/lib/data/events';

export type CompileResult =
  | { ok: true; Component: React.ComponentType }
  | { ok: false; error: string };

/**
 * Compile a single artifact FILE (agent-authored JSX) into a renderable
 * component. This is the validation the agent gets back after write_file:
 * a transpile/eval failure, or a missing `Artifact()` entry point, becomes a
 * clear error string; success yields the component.
 *
 * The file is evaluated with a fixed host scope and must NOT use imports — it
 * references those identifiers directly:
 *   - React                (JSX runtime)
 *   - Graph                (bounded graph widget; pulls data via `source`)
 *   - useGraph(query)      (graph data-source hook)
 *   - useEvents(type?)     (agent event-stream data-source hook)
 */
export async function compileArtifact(code: string): Promise<CompileResult> {
  let transpiled: string;
  try {
    const Babel = await import('@babel/standalone');
    // classic runtime → React.createElement (no react/jsx-runtime imports,
    // which `new Function` can't resolve).
    transpiled =
      Babel.transform(code, {
        presets: [['react', { runtime: 'classic' }]],
      }).code ?? '';
  } catch (err) {
    return { ok: false, error: `Syntax error: ${msg(err)}` };
  }

  const scope: Record<string, unknown> = {
    React,
    Graph: GraphView,
    useGraph,
    useEvents,
  };
  try {
    const factory = new Function(
      ...Object.keys(scope),
      `${transpiled}\n;return typeof Artifact !== 'undefined' ? Artifact : null;`,
    );
    const Component = factory(...Object.values(scope));
    if (typeof Component !== 'function') {
      return {
        ok: false,
        error: 'Artifact must define a function `Artifact()` that returns JSX.',
      };
    }
    return { ok: true, Component: Component as React.ComponentType };
  } catch (err) {
    return { ok: false, error: `Evaluation error: ${msg(err)}` };
  }
}

function msg(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
