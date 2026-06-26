/**
 * A single artifact FILE, exactly as the agent will author it via
 * write_file / edit_file. It is JSX (transpiled at runtime) that composes a
 * document from host components. Note it pulls graph data from `dataSource` —
 * it never bakes the data in. This string is the unit the agent reads/writes.
 */
export const exampleArtifactFile = `
function Artifact() {
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
        <p>
          Expand a component to see its members, or click one to focus its
          relationships. Pinch / ⌘+scroll to zoom inside a graph.
        </p>
      </div>

      <Graph
        graph={dataSource.graph('component')}
        height={520}
        title="Component graph"
        caption="internal/… — 5 components across 2 packages"
      />

      <div className="prose-block">
        <h2>Retrieval pipeline</h2>
        <p>
          A separate region: the <strong>retrieval</strong> service embeds a
          query via the <code>Embedder</code> port and ranks candidates against
          the <code>VectorStore</code>. It was added wholesale (green); one
          <code>Embedder.Batch</code> method was dropped (red).
        </p>
      </div>

      <Graph
        graph={dataSource.graph('retrieval')}
        height={460}
        title="Retrieval pipeline"
        caption="internal/retrieval — embed → index → service"
      />
    </article>
  );
}
`;
