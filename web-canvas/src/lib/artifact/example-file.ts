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
      <div className="prose-block">
        <h2>Architecture overview</h2>
        <p>
          This package is organised as <strong>ports & adapters</strong>. The
          domain types sit at the centre with no outward dependencies; adapters
          translate between the domain and the outside world, and the
          <code>service</code> layer orchestrates the operations.
        </p>
        <p>Pinch / ⌘+scroll to zoom inside a graph; click a component to focus it.</p>
      </div>

      <Graph
        source="component"
        height={520}
        title="Component graph"
        caption="from data-source · component"
      />

      <div className="prose-block">
        <h2>Retrieval pipeline</h2>
        <p>
          A separate region: the <strong>retrieval</strong> service embeds a
          query via the <code>Embedder</code> port and ranks candidates against
          the <code>VectorStore</code>. Added wholesale (green); one
          <code>Embedder.Batch</code> method was dropped (red).
        </p>
      </div>

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
