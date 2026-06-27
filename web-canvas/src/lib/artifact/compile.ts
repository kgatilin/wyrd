import * as React from 'react';
import { buildArtifactScope } from './scope';

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
      Babel.transform(stripModuleSyntax(code), {
        presets: [['react', { runtime: 'classic' }]],
      }).code ?? '';
  } catch (err) {
    return { ok: false, error: `Syntax error: ${msg(err)}` };
  }

  const scope = buildArtifactScope();
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

/**
 * The artifact runs as a plain script inside `new Function`, where module
 * syntax is a hard syntax error. The contract forbids it, but models slip — so
 * defensively drop `import` lines and unwrap `export` / `export default` before
 * transpiling. Anything left referencing a bare module identifier still fails
 * loudly, as it should.
 */
function stripModuleSyntax(code: string): string {
  return code
    .replace(/^\s*import\s[^\n;]*;?\s*$/gm, '')
    .replace(/\bexport\s+default\s+/g, '')
    .replace(/\bexport\s+(?=(?:async\s+)?(?:function|const|let|var|class)\b)/g, '');
}

function msg(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
