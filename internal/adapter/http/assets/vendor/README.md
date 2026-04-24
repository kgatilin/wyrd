# Vendored JS assets

Client-side graph rendering libraries used by the type-detail and
sequence views (M7d). Per M8 (#46) graphs are rendered in the browser
with Cytoscape.js instead of server-side D2 → SVG.

All files are the upstream minified/UMD builds — no Node tooling is
required to produce them. They are embedded into the archai binary via
`//go:embed` in `internal/adapter/http/server.go` (same `assets/`
filesystem as `htmx.min.js` and `styles.css`).

| File | Upstream | Version |
| --- | --- | --- |
| `cytoscape.min.js` | <https://unpkg.com/cytoscape@3.30.2/dist/cytoscape.min.js> | 3.30.2 |
| `dagre.min.js` | <https://unpkg.com/dagre@0.8.5/dist/dagre.min.js> | 0.8.5 |
| `cytoscape-dagre.js` | <https://unpkg.com/cytoscape-dagre@2.5.0/cytoscape-dagre.js> | 2.5.0 |
| `elk.bundled.js` | <https://unpkg.com/elkjs@0.9.3/lib/elk.bundled.js> | 0.9.3 |
| `cytoscape-elk.js` | <https://unpkg.com/cytoscape-elk@2.2.0/dist/cytoscape-elk.js> | 2.2.0 |

The ELK layout is used by the Layers view (compound nodes per layer,
layered placement of cross-layer edges). Dagre is used for the Package
Overview, Type Detail and Diff views (directed acyclic graphs).

To refresh, fetch the URLs above and drop the files into this
directory. Update the version column accordingly.
