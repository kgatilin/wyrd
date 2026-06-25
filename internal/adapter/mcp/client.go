package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ClientOptions configures the thin-client MCP stdio wrapper. Endpoint
// is the base URL of the HTTP daemon (e.g. "http://127.0.0.1:47123").
//
// HTTPClient is optional; when nil a package-default with a 30s
// per-request timeout is used.
//
// EndpointResolver, when non-nil, is called to re-resolve the endpoint
// when a connection failure occurs. This allows the client to survive
// daemon restarts by discovering the new daemon address.
type ClientOptions struct {
	Endpoint         string
	HTTPClient       *nethttp.Client
	EndpointResolver func() (string, error)

	// WorktreePrefix, when non-empty, is inserted between Endpoint and
	// the API path so tool calls are routed to a specific worktree of a
	// multi-worktree daemon (e.g. "/w/feature-x" yields
	// "http://addr/w/feature-x/api/mcp/tools/call"). Empty preserves the
	// classic single-worktree path.
	WorktreePrefix string
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

	// Mutable endpoint state for re-resolution.
	var endpointMu sync.RWMutex
	currentEndpoint := opts.Endpoint

	getEndpoint := func() string {
		endpointMu.RLock()
		defer endpointMu.RUnlock()
		return currentEndpoint
	}

	// tryResolve attempts to re-resolve the endpoint on connection failure.
	// Returns the new endpoint if successful, or empty string if resolution
	// is not available or fails.
	tryResolve := func() string {
		if opts.EndpointResolver == nil {
			return ""
		}
		newEndpoint, err := opts.EndpointResolver()
		if err != nil {
			fmt.Fprintf(errOut, "mcp-client: re-resolve endpoint: %v\n", err)
			return ""
		}
		if newEndpoint == "" {
			return ""
		}
		endpointMu.Lock()
		currentEndpoint = newEndpoint
		endpointMu.Unlock()
		fmt.Fprintf(errOut, "mcp-client: re-resolved endpoint to %s\n", newEndpoint)
		return newEndpoint
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
				if err := handleClientLine(ctx, client, getEndpoint, tryResolve, opts.WorktreePrefix, r.line, writeLine, errOut); err != nil {
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
	getEndpoint func() string,
	tryResolve func() string,
	worktreePrefix string,
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
		return forwardToolsCall(ctx, httpClient, getEndpoint, tryResolve, worktreePrefix, req, writeLine, errOut)
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

// isConnectionError returns true if the error indicates a connection
// failure (connection refused, timeout, etc.) that might be resolved
// by re-discovering the daemon.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	// Check for network errors.
	var netErr net.Error
	if ok := isNetError(err, &netErr); ok {
		return true
	}
	// Check error message for common patterns.
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "EOF")
}

// isNetError checks if err is a net.Error (or wraps one).
func isNetError(err error, target *net.Error) bool {
	for err != nil {
		if ne, ok := err.(net.Error); ok {
			*target = ne
			return true
		}
		// Try to unwrap.
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}

// maxRetries is the number of times to retry a request after re-resolution.
const maxRetries = 2

// retryBackoff is the initial backoff between retries.
const retryBackoff = 100 * time.Millisecond

// forwardToolsCall POSTs the raw params to /api/mcp/tools/call and
// writes the daemon's response (a ToolResult) back to the MCP client.
// Daemon-side RPC errors are surfaced as JSON-RPC errors; transport
// failures (connection refused, timeout) trigger re-resolution and
// retry before being reported as internal errors.
func forwardToolsCall(
	ctx context.Context,
	httpClient *nethttp.Client,
	getEndpoint func() string,
	tryResolve func() string,
	worktreePrefix string,
	req Request,
	writeLine func(Response) error,
	errOut io.Writer,
) error {
	var lastErr error
	backoff := retryBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		endpoint := getEndpoint()

		// Build URL with optional worktree prefix for multi-worktree routing.
		url := endpoint + worktreePrefix + "/api/mcp/tools/call"

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
			lastErr = err
			fmt.Fprintf(errOut, "mcp-client: HTTP POST %s: %v (attempt %d/%d)\n", url, err, attempt+1, maxRetries+1)

			// On connection error, try to re-resolve and retry.
			if isConnectionError(err) && attempt < maxRetries {
				newEndpoint := tryResolve()
				if newEndpoint != "" {
					// Sleep briefly before retry.
					select {
					case <-ctx.Done():
						return writeLine(newErrorResponse(req.ID, ErrInternal, "context cancelled"))
					case <-time.After(backoff):
					}
					backoff *= 2 // Exponential backoff.
					continue
				}
			}

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

	// All retries exhausted.
	return writeLine(newErrorResponse(req.ID, ErrInternal, fmt.Sprintf("daemon unreachable after %d attempts: %v", maxRetries+1, lastErr)))
}
