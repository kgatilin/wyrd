package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// runServeLines feeds input to serveIO and returns the response lines
// written to the fake stdout. serveIO exits when the reader reaches
// io.EOF; if it hasn't returned within timeout we fail the test to
// avoid hangs.
func runServeLines(t *testing.T, input string) []string {
	t.Helper()
	in := strings.NewReader(input)
	var out bytes.Buffer
	var errOut bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serveIO(ctx, nil, in, &out, &errOut) }()

	select {
	case err := <-done:
		if err != nil && err != io.EOF {
			t.Fatalf("serveIO: %v (stderr=%s)", err, errOut.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("serveIO did not return in time")
	}

	raw := strings.TrimRight(out.String(), "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func TestStdio_Initialize(t *testing.T) {
	lines := runServeLines(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d: %v", len(lines), lines)
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	// Result is returned as map[string]any since Response.Result is any.
	resultJSON, _ := json.Marshal(resp.Result)
	var result initializeResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ServerInfo.Name != "archai" {
		t.Errorf("wrong server name: %q", result.ServerInfo.Name)
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		t.Errorf("missing tools capability: %+v", result.Capabilities)
	}
}

func TestStdio_ToolsList(t *testing.T) {
	lines := runServeLines(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	payload, _ := json.Marshal(resp.Result)
	var wrapper struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	if len(wrapper.Tools) != 9 {
		t.Fatalf("expected 9 tools, got %d", len(wrapper.Tools))
	}
}

func TestStdio_ToolsCall_ListPackages_EmptyState(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_packages","arguments":{}}}` + "\n"
	lines := runServeLines(t, body)
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	payload, _ := json.Marshal(resp.Result)
	var tr ToolResult
	if err := json.Unmarshal(payload, &tr); err != nil {
		t.Fatalf("unmarshal toolResult: %v", err)
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", tr.Content)
	}
	text := strings.TrimSpace(tr.Content[0].Text)
	if text != "[]" {
		t.Errorf("expected empty array, got %q", text)
	}
}

func TestStdio_UnknownMethod(t *testing.T) {
	lines := runServeLines(t, `{"jsonrpc":"2.0","id":4,"method":"no/such"}`+"\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response")
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrMethodNotFound {
		t.Fatalf("expected method-not-found, got %+v", resp.Error)
	}
}

func TestStdio_ParseError(t *testing.T) {
	lines := runServeLines(t, "not-json\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrParseError {
		t.Fatalf("expected parse error, got %+v", resp.Error)
	}
}

func TestStdio_Notification_NoResponse(t *testing.T) {
	// "notifications/initialized" is a no-id client notification; we
	// must not write anything back.
	lines := runServeLines(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	if len(lines) != 0 {
		t.Fatalf("expected 0 responses for notification, got %d: %v", len(lines), lines)
	}
}

func TestStdio_MultipleRequestsInOneSession(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":"a","method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":"b","method":"tools/list"}` + "\n"
	lines := runServeLines(t, input)
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %v", len(lines), lines)
	}
}
