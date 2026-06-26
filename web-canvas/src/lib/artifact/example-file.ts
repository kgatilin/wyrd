/**
 * A single artifact FILE, exactly as the agent will author it via
 * write_file / edit_file. It is JSX (transpiled at runtime) that composes a
 * document from host components and DATA-SOURCES — it bakes no data:
 *   - <Graph source="…"/>  pulls a subgraph from the graph data-source
 *   - useEvents()          folds the agent event-stream data-source
 *
 * This string is the unit the agent reads/writes.
 */
export const exampleArtifactFile = `
function Artifact() {
  const events = useEvents();

  return (
    <article className="artifact-doc">
      <Markdown>{\`
## Architecture overview

This package is organised as **ports & adapters**. The domain types sit at the
centre with no outward dependencies; adapters translate between the domain and
the outside world, and the \\\`service\\\` layer orchestrates the operations.

Pinch / ⌘+scroll to zoom inside a graph; click a component to focus it.
\`}</Markdown>

      <Graph
        source="component"
        height={520}
        title="Component graph"
        caption="from data-source · component"
      />

      <Markdown>{\`
## Retrieval pipeline

A separate region: the **retrieval** service embeds a query via the
\\\`Embedder\\\` port and ranks candidates against the \\\`VectorStore\\\`. Added
wholesale (green); one \\\`Embedder.Batch\\\` method was dropped (red).
\`}</Markdown>

      <Graph
        source="retrieval"
        height={460}
        title="Retrieval pipeline"
        caption="from data-source · retrieval"
      />

      <div className="prose-block">
        <h3>Agent activity</h3>
        <p>Folded live from the event-stream data-source:</p>
        <ul>
          {events.map((e) => (
            <li key={e.id}><code>{e.type}</code> — {e.summary}</li>
          ))}
        </ul>
      </div>
    </article>
  );
}
`;
