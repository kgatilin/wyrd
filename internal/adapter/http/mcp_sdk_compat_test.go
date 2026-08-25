package http

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPHTTP_OfficialSDKClient is the compatibility gate for the endpoint.
// Hand-written checks pin what we believe the Streamable HTTP contract to be;
// this one pins what a real client does with it — the same
// StreamableClientTransport that Go MCP client plugins connect with,
// driven through the whole handshake against a live listener. It is the only
// check here that fails when our reading of the transport is wrong rather
// than merely inconsistent.
//
// The client does more than POST: after initialize it opens the standalone
// SSE stream with a GET (which must 405), validates the protocol version we
// answered against its own supported set, and refuses any response whose
// Content-Type it does not recognize.
func TestMCPHTTP_OfficialSDKClient(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := sdk.NewClient(&sdk.Implementation{Name: "wyrd-compat-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Close()

	// A session id would oblige the client to echo one back on every later
	// request; a stateless server must not hand one out.
	if got := session.ID(); got != "" {
		t.Errorf("session ID = %q, want none", got)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"search", "list_packages", "get_package"} {
		if !names[want] {
			t.Errorf("tools/list is missing %q; got %v", want, names)
		}
	}

	packages, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "list_packages",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(list_packages): %v", err)
	}
	if packages.IsError {
		t.Fatalf("list_packages reported an error: %+v", packages.Content)
	}
	text, ok := packages.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want a text block", packages.Content[0])
	}
	if !strings.Contains(text.Text, "alpha") {
		t.Errorf("list_packages text = %q, want the fixture's alpha package", text.Text)
	}

	// `search` is the tool an agent reaches for first, and the one with
	// arguments, so it is worth driving end to end. Retrieval is switched off
	// in this package's tests (see TestMain), so the answer is a tool-level
	// error rather than hits — what is under test here is that the call
	// round-trips as a tool call at all.
	found, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "Service", "k": 3},
	})
	if err != nil {
		t.Fatalf("CallTool(search): %v", err)
	}
	if len(found.Content) == 0 {
		t.Errorf("search returned no content blocks")
	}
}
