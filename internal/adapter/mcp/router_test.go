package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// initializeVersion drives one initialize through the router and returns the
// protocolVersion it answered with.
func initializeVersion(t *testing.T, rawParams string) string {
	t.Helper()
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"}
	if rawParams != "" {
		req.Params = json.RawMessage(rawParams)
	}
	resp := HandleRequest(context.Background(), nil, req, nil)
	if resp == nil {
		t.Fatal("initialize got no response")
	}
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	result, ok := resp.Result.(initializeResult)
	if !ok {
		t.Fatalf("result is %T, want initializeResult", resp.Result)
	}
	return result.ProtocolVersion
}

// A client is entitled to keep speaking the revision it asked for, so a
// version we know is echoed rather than replaced with ours.
func TestHandleRequest_InitializeEchoesAKnownClientVersion(t *testing.T) {
	for _, want := range protocolVersions {
		got := initializeVersion(t, `{"protocolVersion":"`+want+`"}`)
		if got != want {
			t.Errorf("initialize(%s) answered %q, want it echoed", want, got)
		}
	}
}

// An unknown or absent version has to be answered with one we actually speak:
// clients validate the answer against their own supported set and fail the
// handshake on anything outside it.
func TestHandleRequest_InitializeFallsBackToTheLatestVersion(t *testing.T) {
	cases := map[string]string{
		"absent params":  "",
		"empty params":   `{}`,
		"unknown future": `{"protocolVersion":"2099-01-01"}`,
		"malformed":      `{"protocolVersion":42}`,
	}
	for name, params := range cases {
		if got := initializeVersion(t, params); got != latestProtocolVersion {
			t.Errorf("%s: initialize answered %q, want %q", name, got, latestProtocolVersion)
		}
	}
}

// Streamable HTTP arrived in the 2025-03-26 revision, so a server that offers
// it must not advertise an older one as its newest.
func TestLatestProtocolVersion_PostdatesStreamableHTTP(t *testing.T) {
	if latestProtocolVersion < "2025-03-26" {
		t.Errorf("latestProtocolVersion = %q, older than the revision that introduced Streamable HTTP", latestProtocolVersion)
	}
}

// A notification carries no id, and JSON-RPC 2.0 forbids answering one. The
// HTTP transport reads the nil as "202 Accepted, empty body".
func TestHandleRequest_NotificationGetsNoResponse(t *testing.T) {
	for _, method := range []string{"notifications/initialized", "tools/list", "ping", "nonexistent"} {
		req := Request{JSONRPC: "2.0", Method: method}
		if resp := HandleRequest(context.Background(), nil, req, nil); resp != nil {
			t.Errorf("notification %q answered with %+v, want no response", method, resp)
		}
	}
}

// The initialized acknowledgement is answered with nothing even when a client
// mistakenly sends it as a call.
func TestHandleRequest_InitializedWithAnIDGetsNoResponse(t *testing.T) {
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`7`), Method: "notifications/initialized"}
	if resp := HandleRequest(context.Background(), nil, req, nil); resp != nil {
		t.Errorf("notifications/initialized answered with %+v, want no response", resp)
	}
}

func TestHandleRequest_UnknownMethodIsAJSONRPCError(t *testing.T) {
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "resources/list"}
	resp := HandleRequest(context.Background(), nil, req, nil)
	if resp == nil || resp.Error == nil {
		t.Fatalf("resources/list answered %+v, want a JSON-RPC error", resp)
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrMethodNotFound)
	}
}

func TestHandleRequest_ToolsCallWithoutANameIsInvalidParams(t *testing.T) {
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "tools/call", Params: json.RawMessage(`{"arguments":{}}`)}
	resp := HandleRequest(context.Background(), nil, req, nil)
	if resp == nil || resp.Error == nil {
		t.Fatalf("nameless tools/call answered %+v, want a JSON-RPC error", resp)
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrInvalidParams)
	}
}
