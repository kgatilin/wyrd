package http

// graphPayload is the structured graph document returned by every
// /api/.../graph endpoint. The browser-side graph.js reads payload.meta
// to pick a layout + style, then maps nodes/edges into Cytoscape
// elements. Each node's Parent points at a compound parent (e.g. layer
// group); root-level nodes leave it empty.
type graphPayload struct {
	Meta  graphMeta   `json:"meta"`
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

// graphMeta carries view-specific rendering hints for the client. View
// ("layer-map" / "layer-map-mini" / "package-overview" / "diff-overlay")
// triggers the right Cytoscape style preset in graph.js. Layout names
// match cytoscape layout ids ("elk", "dagre").
type graphMeta struct {
	View   string `json:"view"`
	Layout string `json:"layout"`
	Title  string `json:"title,omitempty"`
	// Mode is set on overview-style payloads ("public" / "full") so the
	// browser can render the correct toggle state without a round-trip.
	Mode string `json:"mode,omitempty"`
}

// graphEndpointDoc describes where graph.js should refresh the data
// from, used by .cy-graph divs that carry a data-api attribute rather
// than an inline data-graph JSON payload.
//
// The payload itself is self-contained — graph.js consumes the same
// shape whether it comes from an inline attribute or an API fetch.
