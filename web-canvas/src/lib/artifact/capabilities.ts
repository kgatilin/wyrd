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
  'An artifact is a single file authored with write_file. It MUST define a',
  'function `Artifact()` that returns JSX. Do NOT use imports — the capabilities',
  'below are already in scope; reference them directly. Wrap the document in',
  '`<article className="artifact-doc">`; use `<div className="prose-block">` for',
  'prose. NEVER bake graph data into the file — always pull it from a',
  'data-source (e.g. `<Graph source="component" />`).',
].join(' ');

export const CAPABILITIES: CapabilityDef[] = [
  {
    name: 'Graph',
    kind: 'component',
    signature: '<Graph source={string} height?={number} title?={string} caption?={string} />',
    doc: 'Bounded architecture-graph widget (pan/zoom, expand components, focus). Pulls a subgraph from the graph data-source by `source`. height defaults to 520.',
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
