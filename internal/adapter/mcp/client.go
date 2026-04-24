package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"os"
	"sync"
	"time"
)

// ClientOptions configures the thin-client MCP stdio wrapper. Endpoint
// is the base URL of the HTTP daemon (e.g. "http://127.0.0.1:47123").
//
// HTTPClient is optional; when nil a package-default with a 30s
// per-request timeout is used.
type ClientOptions struct {
	Endpoint   string
	HTTPClient *nethttp.Client
}

// ServeClient runs the MCP stdio transport in thin-client mode. It
// reads JSON-RPC messages from stdin, forwards tools/call invocations
// to the HTTP daemon at opts.Endpoint, and writes responses back to
// stdout. initialize, notifications/initialized and tools/list are
// handled locally (ToolDefinitions matches the daemon's set by
// construction).
//
// ServeClient returns when stdin is closed (io.EOF) or ctx is
// cancelled.
func ServeClient(ctx context.Context, opts ClientOptions) error {
	return serveClientIO(ctx, opts, os.Stdin, os.Stdout, os.Stderr)
}

// serveClientIO is the testable form of ServeClient with explicit
// streams. All reads are serialized against a goroutine reading from in
// so ctx cancellation exits promptly even on a blocked stdin.
func serveClientIO(ctx context.Context, opts ClientOptions, in io.Reader, out io.Writer, errOut io.Writer) error {
	if opts.Endpoint == "" {
		return fmt.Errorf("mcp: empty endpoint")
	}
	client := opts.HTTPClient
	if client == nil {
		client = &nethttp.Client{Timeout: 30 * time.Second}
	}

	reader := bufio.NewReader(in)
	var writeMu sync.Mutex
	writeLine := func(resp Response) error {
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := out.Write(data); err != nil {
			return err
		}
		_, err = out.Write([]byte{'\n'})
		return err
	}

	type readResult struct {
		line []byte
		err  error
	}
	readCh := make(chan readResult, 1)
	go func() {
		for {
			line, err := reader.ReadBytes('\n')
			readCh <- readResult{line: line, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-readCh:
			if len(r.line) > 0 {
				if err := handleClientLine(ctx, client, opts.Endpoint, r.line, writeLine, errOut); err != nil {
					fmt.Fprintf(errOut, "mcp-client: write response: %v\n", err)
				}
			}
			if r.err != nil {
				if r.err == io.EOF {
					return nil
				}
				return r.err
			}
		}
	}
}

// handleClientLine dispatches a single incoming line. initialize,
// tools/list and ping are answered locally; tools/call is forwarded
// via forwardToolsCall.
func handleClientLine(
	ctx context.Context,
	httpClient *nethttp.Client,
	endpoint string,
	line []byte,
	writeLine func(Response) error,
	errOut io.Writer,
) error {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return writeLine(newErrorResponse(nil, ErrParseError, fmt.Sprintf("parse error: %v", err)))
	}
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		if isNotification {
			return nil
		}
		return writeLine(newResponse(req.ID, newInitializeResult()))
	case "notifications/initialized", "initialized":
		return nil
	case "tools/list":
		if isNotification {
			return nil
		}
		return writeLine(newResponse(req.ID, map[string]any{
			"tools": ToolDefinitions(),
		}))
	case "tools/call":
		if isNotification {
			return nil
		}
		return forwardToolsCall(ctx, httpClient, endpoint, req, writeLine, errOut)
	case "ping":
		if isNotification {
			return nil
		}
		return writeLine(newResponse(req.ID, map[string]any{}))
	default:
		if isNotification {
			return nil
		}
		return writeLine(newErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method %q not supported", req.Method)))
	}
}

// forwardToolsCall POSTs the raw params to /api/mcp/tools/call and
// writes the daemon's response (a ToolResult) back to the MCP client.
// Daemon-side RPC errors are surfaced as JSON-RPC errors; transport
// failures (connection refused, timeout) are reported as internal
// errors so the MCP client can retry.
func forwardToolsCall(
	ctx context.Context,
	httpClient *nethttp.Client,
	endpoint string,
	req Request,
	writeLine func(Response) error,
	errOut io.Writer,
) error {
	// Forward params verbatim — they are already {name, arguments}.
	url := endpoint + "/api/mcp/tools/call"
	var body io.Reader
	if len(req.Params) > 0 {
		body = bytes.NewReader(req.Params)
	}
	httpReq, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, url, body)
	if err != nil {
		return writeLine(newErrorResponse(req.ID, ErrInternal, fmt.Sprintf("build request: %v", err)))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		fmt.Fprintf(errOut, "mcp-client: HTTP POST %s: %v\n", url, err)
		return writeLine(newErrorResponse(req.ID, ErrInternal, fmt.Sprintf("daemon unreachable: %v", err)))
	}
	defer resp.Body.Close()

	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return writeLine(newErrorResponse(req.ID, ErrInternal, fmt.Sprintf("read response: %v", readErr)))
	}

	// On 4xx/5xx with a JSON error envelope, translate to a JSON-RPC
	// error so the client sees a structured failure.
	if resp.StatusCode >= 400 {
		var errEnv struct {
			Error string `json:"error"`
			Code  int    `json:"code"`
		}
		if jsonErr := json.Unmarshal(data, &errEnv); jsonErr == nil && errEnv.Error != "" {
			code := errEnv.Code
			if code == 0 {
				code = ErrInternal
			}
			return writeLine(newErrorResponse(req.ID, code, errEnv.Error))
		}
		return writeLine(newErrorResponse(req.ID, ErrInternal,
			fmt.Sprintf("daemon returned %d: %s", resp.StatusCode, string(data))))
	}

	// Happy path: body is a ToolResult JSON document. Forward verbatim
	// as the result field.
	var tr ToolResult
	if err := json.Unmarshal(data, &tr); err != nil {
		return writeLine(newErrorResponse(req.ID, ErrInternal, fmt.Sprintf("decode tool result: %v", err)))
	}
	return writeLine(newResponse(req.ID, tr))
}
