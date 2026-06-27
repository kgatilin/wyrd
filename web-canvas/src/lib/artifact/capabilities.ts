/**
 * The artifact capability manifest — the single source of truth for what an
 * artifact (a single agent-authored file) may use. It drives two things:
 *   - the runtime scope handed to compileArtifact (see scope.ts), and
 *   - the declaration injected into the agent's system prompt
 *     (renderAgentDeclaration), so the agent knows exactly what's available.
 *
 * This module is pure data (no React/client imports) so it can be read on the
 * server (system prompt) and the client (scope) alike.
 */

export interface CapabilityDef {
  /** Identifier as it appears in artifact code scope. */
  name: string;
  kind: 'component' | 'data-source';
  /** How the agent writes it. */
  signature: string;
  doc: string;
  /** For data-sources: the currently valid query ids. */
  queries?: string[];
}

export const ARTIFACT_CONTRACT = [
  'An artifact is a single file authored with write_artifact. It MUST define a bare',
  'top-level `function Artifact() { ... }` that returns JSX. The file runs as a',
  'plain script, so do NOT use `export`, `export default`, `import`, or any',
  'module syntax — the capabilities below are already in scope; reference them',
  'directly. Wrap the document in `<article className="artifact-doc">`. Put ALL',
  'text — headings and prose — inside `<Markdown>` blocks (markdown `#`/`##`',
  'headings, lists, tables); do NOT hand-build tab bars, nav bars, badges, or',
  'other custom chrome, and avoid inline styles. Place each `<Graph>` on its own',
  'line as a block (never side by side). NEVER bake graph data into the file —',
  'always pull it from a data-source (e.g. `<Graph query="hybrid retrieval" />`). For',
  'diagrams (flowcharts, sequence, etc.) use `<Mermaid>` — never a markdown code',
  'block, which only shows the diagram source as text.',
].join(' ');

export const CAPABILITIES: CapabilityDef[] = [
  {
    name: 'Markdown',
    kind: 'component',
    signature: '<Markdown>{string}</Markdown>',
    doc: 'Renders a markdown string as prose (headings, lists, tables via GFM, inline code) and math via KaTeX (inline `$…$`, block `$$…$$`). Use this for all prose instead of raw HTML. Raw HTML is escaped.',
  },
  {
    name: 'Graph',
    kind: 'component',
    signature:
      '<Graph query?={string} source?={string} nodes?={string[]} hops?={number} edges?={string[]} height?={number} title?={string} caption?={string} />',
    doc:
      'Architecture-graph widget (pan/zoom, expand, click-to-focus dependencies, fullscreen) that renders any slice of the real code graph. Choose the slice:\n' +
      '  • query — a natural-language query selecting a subgraph by meaning, e.g. query="hybrid retrieval scoring" or query="BM25 lexical index". This is the main way to show a focused piece.\n' +
      '  • nodes — explicit seed node ids (pkg.Symbol, e.g. ["internal/retrieval.Service"]) to grow a neighborhood from.\n' +
      '  • source — focus the whole-project graph on a package (path or name, e.g. "internal/retrieval"); shows it plus direct deps/dependents.\n' +
      '  • hops — neighborhood radius for query/nodes (default 1; 2 = wider).\n' +
      '  • edges — restrict to these edge kinds: any of "uses","returns","implements","calls".\n' +
      'With none of query/nodes/source it shows the whole project. Prefer `query` for "show me the X subgraph". height defaults to 520.',
  },
  {
    name: 'Mermaid',
    kind: 'component',
    signature: '<Mermaid chart={string} />',
    doc: 'Renders a Mermaid diagram (flowchart, sequenceDiagram, classDiagram, etc.) from its text. Pass the diagram source as `chart` (a template literal). Use this for diagrams instead of a markdown code block. Syntax errors render inline.',
  },
  {
    name: 'useGraph',
    kind: 'data-source',
    signature: 'useGraph(spec: string | { query?, nodes?, source?, hops?, edges? }): UIGraph | null',
    doc: 'Graph data-source (pull) over the real archai daemon graph. Pass a GraphQuery (query=semantic subgraph, nodes=seed ids, source=package focus, hops, edges=kinds) or a bare string (treated as source). Returns null while loading. Usually consumed via <Graph .../>.',
  },
  {
    name: 'useEvents',
    kind: 'data-source',
    signature: 'useEvents(type?: string): AgentEvent[]',
    doc: 'Agent event-stream data-source (live). AgentEvent = { id, ts, type, summary, data? }. Fold it to show agent activity.',
  },
];

/** Markdown declaration of the capability surface, for the agent system prompt. */
export function renderAgentDeclaration(): string {
  const fmt = (c: CapabilityDef) => {
    const q = c.queries?.length
      ? ` Available queries: ${c.queries.map((x) => `\`${x}\``).join(', ')}.`
      : '';
    return `- \`${c.signature}\` — ${c.doc}${q}`;
  };
  const components = CAPABILITIES.filter((c) => c.kind === 'component').map(fmt);
  const sources = CAPABILITIES.filter((c) => c.kind === 'data-source').map(fmt);

  return [
    '## Authoring artifacts',
    ARTIFACT_CONTRACT,
    '',
    '### Components in scope',
    ...components,
    '',
    '### Data-sources in scope',
    ...sources,
  ].join('\n');
}
