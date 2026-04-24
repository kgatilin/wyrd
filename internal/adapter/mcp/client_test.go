package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDaemon is a minimal httptest.Server that answers /api/mcp/tools/call
// with a canned ToolResult. Used to exercise the thin-client wrapper
// without spinning up the real serve package.
func fakeDaemon(t *testing.T, handler func(name string, args json.RawMessage) (ToolResult, int)) *httptest.Server {
	t.Helper()
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/api/mcp/tools/call", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			nethttp.Error(w, "method", nethttp.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				nethttp.Error(w, err.Error(), nethttp.StatusBadRequest)
				return
			}
		}
		res, status := handler(req.Name, req.Arguments)
		if status == 0 {
			status = 200
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(res)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// runClientSession feeds requestLines (already newline-terminated) into
// the thin client, collects stdout, and returns the written lines.
func runClientSession(t *testing.T, endpoint string, requestLines []string) []string {
	t.Helper()
	var in bytes.Buffer
	for _, line := range requestLines {
		in.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			in.WriteByte('\n')
		}
	}
	var out, errOut bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := serveClientIO(ctx, ClientOptions{Endpoint: endpoint}, &in, &out, &errOut); err != nil {
		t.Fatalf("serveClientIO: %v (stderr=%s)", err, errOut.String())
	}

	raw := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(raw) == 1 && raw[0] == "" {
		return nil
	}
	return raw
}

func TestClient_InitializeAndToolsList_AreHandledLocally(t *testing.T) {
	// Endpoint never gets hit — initialize and tools/list are answered
	// without an HTTP round-trip.
	lines := runClientSession(t, "http://127.0.0.1:1", []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	})
	if len(lines) != 2 {
		t.Fatalf("want 2 responses, got %d — lines=%v", len(lines), lines)
	}

	var initResp Response
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if initResp.Error != nil {
		t.Errorf("initialize.error=%+v", initResp.Error)
	}

	var listResp Response
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	payload, _ := json.Marshal(listResp.Result)
	if !strings.Contains(string(payload), `"extract"`) ||
		!strings.Contains(string(payload), `"validate"`) {
		t.Errorf("tools/list missing tools: %s", payload)
	}
}

func TestClient_ToolsCall_ForwardsToDaemon(t *testing.T) {
	var called struct {
		Name string
		Args string
	}
	ts := fakeDaemon(t, func(name string, args json.RawMessage) (ToolResult, int) {
		called.Name = name
		called.Args = string(args)
		return ToolResult{
			Content: []ToolResultContent{{Type: "text", Text: `[{"path":"alpha"}]`}},
		}, 200
	})

	lines := runClientSession(t, ts.URL, []string{
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_packages","arguments":{}}}`,
	})
	if len(lines) != 1 {
		t.Fatalf("want 1 response, got %d — %v", len(lines), lines)
	}

	if called.Name != "list_packages" {
		t.Errorf("daemon got name=%q, want list_packages", called.Name)
	}

	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	payload, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(payload), "alpha") {
		t.Errorf("response missing alpha payload: %s", payload)
	}
}

func TestClient_ToolsCall_DaemonError_Surfaces(t *testing.T) {
	ts := fakeDaemon(t, func(name string, args json.RawMessage) (ToolResult, int) {
		// Write a JSON-error envelope like writeJSONError does.
		return ToolResult{}, 0 // fallthrough; we override below
	})
	ts.Close()

	// Point the client at a closed server so the request fails.
	lines := runClientSession(t, ts.URL, []string{
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_packages"}}`,
	})
	if len(lines) != 1 {
		t.Fatalf("want 1 response, got %d", len(lines))
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected JSON-RPC error, got %+v", resp)
	}
	if !strings.Contains(resp.Error.Message, "unreachable") &&
		!strings.Contains(resp.Error.Message, "connection") {
		// Accept either wording — the Go net error text varies by OS.
		t.Logf("note: error message=%q", resp.Error.Message)
	}
}

func TestClient_EmptyEndpoint(t *testing.T) {
	err := serveClientIO(context.Background(), ClientOptions{Endpoint: ""}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}
