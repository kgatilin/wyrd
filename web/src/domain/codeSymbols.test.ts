import { describe, expect, it } from 'vitest';
import type { Component, UIGraph } from '../types';
import { buildSymbolLookup, markSymbols } from './codeSymbols';

function component(over: Partial<Component> & { id: string }): Component {
  return {
    name: over.id.split('/').pop() ?? over.id,
    tech: 'go',
    desc: '',
    bc: 'root',
    ports: [],
    internals: [],
    ...over,
  };
}

const graph: UIGraph = {
  schema: 'test',
  boundedContexts: [],
  comments: [],
  edges: [],
  components: [
    component({
      id: 'internal/serve',
      internals: [
        {
          id: 'internal/serve.State',
          kind: 'class',
          name: 'State',
          sourceFile: 'state.go',
          members: [
            { id: 'internal/serve.State.Load', kind: 'method', name: 'Load', sourceFile: 'state.go' },
            { id: 'internal/serve.State.mu', kind: 'prop', name: 'mu', sourceFile: 'state.go' },
            { id: 'internal/serve.State.Load.return', kind: 'return', name: 'error', sourceFile: 'state.go' },
          ],
        },
        { id: 'internal/serve.Serve', kind: 'func', name: 'Serve', sourceFile: 'serve.go', members: [] },
        { id: 'internal/serve.Store[T any]', kind: 'iface', name: 'Store[T any]', sourceFile: 'serve.go', members: [] },
      ],
    }),
    component({
      id: 'internal/repo',
      internals: [
        {
          id: 'internal/repo.SQL',
          kind: 'class',
          name: 'SQL',
          sourceFile: 'sql.go',
          members: [{ id: 'internal/repo.SQL.Load', kind: 'method', name: 'Load', sourceFile: 'sql.go' }],
        },
      ],
    }),
  ],
};

describe('buildSymbolLookup', () => {
  const lookup = buildSymbolLookup(graph);

  it('resolves a declaration to its own node', () => {
    expect(lookup.resolve('Serve', 'internal/serve/serve.go')).toEqual({
      componentId: 'internal/serve',
      internalId: 'internal/serve.Serve',
    });
  });

  it('prefers the symbol declared in the file being read', () => {
    // `Load` is a method on two different types in two packages.
    expect(lookup.resolve('Load', 'internal/repo/sql.go')).toEqual({
      componentId: 'internal/repo',
      internalId: 'internal/repo.SQL',
      memberId: 'internal/repo.SQL.Load',
    });
    expect(lookup.resolve('Load', 'internal/serve/state.go')).toEqual({
      componentId: 'internal/serve',
      internalId: 'internal/serve.State',
      memberId: 'internal/serve.State.Load',
    });
  });

  it('falls back to the package, then to a graph-wide unique name', () => {
    // Same package, other file.
    expect(lookup.resolve('State', 'internal/serve/serve.go')).toMatchObject({
      internalId: 'internal/serve.State',
    });
    // Different package entirely, but only one `Serve` exists.
    expect(lookup.resolve('Serve', 'web/src/App.tsx')).toMatchObject({
      internalId: 'internal/serve.Serve',
    });
  });

  it('refuses to guess when a name stays ambiguous', () => {
    expect(lookup.resolve('Load', 'web/src/App.tsx')).toBeNull();
  });

  it('indexes a generic type under its bare name', () => {
    expect(lookup.resolve('Store', 'internal/serve/serve.go')).toMatchObject({
      internalId: 'internal/serve.Store[T any]',
    });
  });

  it('ignores param and return rows', () => {
    expect(lookup.resolve('error', 'internal/serve/state.go')).toBeNull();
  });

  it('resolves unexported members too', () => {
    expect(lookup.resolve('mu', 'internal/serve/state.go')).toMatchObject({
      memberId: 'internal/serve.State.mu',
    });
  });

  it('reports an empty index for a graph with no symbols', () => {
    expect(buildSymbolLookup({ ...graph, components: [] }).size).toBe(0);
  });
});

describe('markSymbols', () => {
  const known = (name: string) => name === 'State' || name === 'Load';

  it('wraps known identifiers in the text between tags', () => {
    expect(markSymbols('<span class="hljs-keyword">func</span> Load() {', known)).toBe(
      '<span class="hljs-keyword">func</span> <span class="hf-code-sym" data-sym="Load">Load</span>() {'
    );
  });

  it('never touches the highlighter’s own markup', () => {
    // A class name containing a known identifier must not be rewritten.
    expect(markSymbols('<span class="hljs-Load">x</span>', known)).toBe('<span class="hljs-Load">x</span>');
  });

  it('leaves HTML entities alone', () => {
    // "amp" inside &amp; is not an identifier in the source.
    expect(markSymbols('a &amp;&amp; State', (n) => n === 'amp' || n === 'State')).toBe(
      'a &amp;&amp; <span class="hf-code-sym" data-sym="State">State</span>'
    );
  });

  it('marks each occurrence, including qualified ones', () => {
    expect(markSymbols('s.Load(); t.Load()', known)).toBe(
      's.<span class="hf-code-sym" data-sym="Load">Load</span>(); t.<span class="hf-code-sym" data-sym="Load">Load</span>()'
    );
  });

  it('does not mark identifiers that merely contain a known name', () => {
    expect(markSymbols('Loader preload', known)).toBe('Loader preload');
  });

  it('passes an empty line straight through', () => {
    expect(markSymbols('', known)).toBe('');
  });
});
