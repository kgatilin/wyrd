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
  'always pull it from a data-source (e.g. `<Graph source="component" />`). For',
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
    signature: '<Graph source={string} height?={number} title?={string} caption?={string} />',
    doc: 'Bounded architecture-graph widget (pan/zoom, expand components, focus). Pulls a subgraph from the graph data-source by `source`. height defaults to 520.',
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
    signature: 'useGraph(query: string): UIGraph | null',
    doc: 'Graph data-source (pull). Returns null while loading. Usually consumed via <Graph source=.../>.',
    queries: ['component', 'retrieval'],
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
