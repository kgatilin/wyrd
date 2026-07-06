package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Response-size budgets.
//
// Every tools/call result flows through Dispatch, which enforces
// maxResultBytes as a hard ceiling: any larger payload is replaced with a
// structured oversize envelope so the MCP transport — and a downstream
// 1 MiB NATS bridge — can never be blown by a single tool response. Shaped
// handlers (get_package, search, expand, ...) pack toward softResultBytes
// and paginate/truncate before they reach the ceiling; the hard clamp is a
// backstop, not the primary UX.
const (
	// maxResultBytes is the hard ceiling on a single tool result's text
	// payload. Comfortably under NATS's default 1 MiB max_payload, leaving
	// room for the JSON-RPC envelope and any bridge overhead.
	maxResultBytes = 256 * 1024

	// softResultBytes is the target size shaped handlers pack toward before
	// paginating or truncating (~12k tokens — heavy but workable for one
	// tool call).
	softResultBytes = 48 * 1024

	// maxReadFileBytes caps read_file's raw-text output. Kept just under
	// maxResultBytes so a truncated file plus its trailing notice still
	// clears the hard ceiling without tripping the Dispatch clamp.
	maxReadFileBytes = maxResultBytes - 4*1024
)

// oversizeEnvelope is the payload substituted for any tool result that
// exceeds maxResultBytes. It names the tool, the measured size, the limit,
// and how to narrow the request so the model gets an actionable message
// instead of a truncated blob or a dropped transport frame.
type oversizeEnvelope struct {
	Error   string `json:"error"`
	Tool    string `json:"tool"`
	Bytes   int    `json:"bytes"`
	Limit   int    `json:"limit"`
	Message string `json:"message"`
}

// clampToolResult enforces the maxResultBytes hard ceiling on a tool result.
// Under the ceiling the result passes through untouched; over it, the result
// is replaced with an oversize envelope (IsError=true so the model knows it
// did not receive the data it asked for).
func clampToolResult(tool string, res ToolResult) ToolResult {
	total := 0
	for _, c := range res.Content {
		total += len(c.Text)
	}
	if total <= maxResultBytes {
		return res
	}
	payload, err := json.Marshal(oversizeEnvelope{
		Error:   "result_too_large",
		Tool:    tool,
		Bytes:   total,
		Limit:   maxResultBytes,
		Message: oversizeHint(tool, total),
	})
	if err != nil {
		// Marshalling a fixed-shape struct cannot realistically fail; fall
		// back to a plain-text notice rather than emit the oversized blob.
		payload = fmt.Appendf(nil, "result too large: %d bytes exceeds the %d-byte limit", total, maxResultBytes)
	}
	return ToolResult{
		Content: []ToolResultContent{{Type: "text", Text: string(payload)}},
		IsError: true,
	}
}

// oversizeHint returns tool-specific guidance for narrowing an over-budget
// request. Shaped tools rarely reach here, so the hints target the levers a
// caller actually has (pagination, filters, k/hops).
func oversizeHint(tool string, bytes int) string {
	base := fmt.Sprintf("Tool %q produced a %d-byte result, over the %d-byte response limit; it was suppressed so the transport isn't blown. ", tool, bytes, maxResultBytes)
	switch tool {
	case "get_package", "extract":
		return base + "Page it with a smaller `limit` (and advance `offset`), filter to one `kinds` group, or read individual symbols with get_node."
	case "search", "search_graph":
		return base + "Lower `k` (and `hops` for search_graph), or add filters to narrow the result set."
	case "expand":
		return base + "Expand from fewer node_ids, reduce `hops`, or restrict `edges` to specific kinds."
	default:
		return base + "Narrow the request (fewer items, tighter scope) and retry."
	}
}

// clip truncates s to at most max bytes on a rune boundary, appending a
// marker with the elided byte count so the model can tell content was cut.
// Used for per-field bulk (doc synopses, snippets, symbol bodies), never for
// whole payloads — the marker may push the returned string a few bytes past
// max, which is fine at field granularity.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " \t\n") + fmt.Sprintf(" … [+%d bytes]", len(s)-cut)
}

// firstLine returns the first non-empty line of a doc comment (the Go
// synopsis convention), clipped to max bytes. Multi-paragraph docs are
// exactly the kind of bulk that inflates a package digest.
func firstLine(doc string, max int) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	if i := strings.IndexByte(doc, '\n'); i >= 0 {
		doc = strings.TrimSpace(doc[:i])
	}
	return clip(doc, max)
}

// capMembers caps a string slice to max elements, appending a "… +N more"
// sentinel when items were dropped. Keeps a single symbol's method/field
// list from ballooning a digest.
func capMembers(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	out := make([]string, 0, max+1)
	out = append(out, items[:max]...)
	out = append(out, fmt.Sprintf("… +%d more", len(items)-max))
	return out
}
