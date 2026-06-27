/**
 * The default landing dashboard — a SAVED artifact seeded when the canvas has
 * no saved dashboards yet (see seed.ts). It is an ordinary artifact file (the
 * same unit the agent authors via write_artifact): a script defining
 * `function Artifact()` that composes host components and pulls everything from
 * data-sources — it bakes no data. It doubles as a how-to for the canvas.
 */
export const welcomeDashboardFile = `
function Artifact() {
  const events = useEvents();

  return (
    <article className="artifact-doc">
      <Markdown>{\`
# archai canvas

A live view of the **archai** Go codebase. Ask the agent to explore it — it
reads the real source through archai's graph tools and renders the answer here
as an interactive artifact.

Try: *"show me the files in package internal/retrieval"*, *"open
internal/adapter/d2/sequence.go"*, or *"diagram the retrieval pipeline"*.
\`}</Markdown>

      <Graph
        source="internal"
        height={520}
        title="archai — internal packages"
        caption="click a node to focus its deps · flip to Sequence for a package's call flow"
      />

      <Markdown>{\`
## What you can put on this canvas

- **Graph** — any slice of the code graph; its header has a **Graph / Sequence**
  toggle that draws a package's call flow as a type-interaction diagram.
- **File** — a single source file with syntax highlighting and line numbers;
  collapsed by default, with an inline diff when it differs from main.
- **FileTree** — a mini file browser over a chosen subtree; click a file to open
  it. Expand to fullscreen with the ⛶ button.
- **Markdown · Mermaid · math** — prose, diagrams, and KaTeX.
\`}</Markdown>

      <FileTree
        root="internal/sequence"
        height={420}
        title="Browse a package"
        caption="internal/sequence — click a file to open it"
      />

      <div className="prose-block">
        <h2>Agent activity</h2>
        <p>The raw backend event log, live (each event verbatim — fold it however you like):</p>
        {events.length === 0 ? (
          <p><em>No activity yet — start chatting.</em></p>
        ) : (
          <ul>
            {events.slice(-15).map((e) => (
              <li key={e.seq}><code>{e.type}</code>{e.source ? \` · \${e.source}\` : ''}</li>
            ))}
          </ul>
        )}
      </div>
    </article>
  );
}
`;
