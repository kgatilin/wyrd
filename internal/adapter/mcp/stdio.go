package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/kgatilin/archai/internal/serve"
)

// Serve runs the MCP stdio transport: it reads line-delimited JSON-RPC
// 2.0 messages from stdin, dispatches them against state, and writes
// responses back to stdout. Diagnostic logs go to stderr.
//
// Serve returns when stdin is closed (io.EOF) or ctx is cancelled.
// It is safe to call concurrently with state mutations — all reads go
// through serve.State.Snapshot().
func Serve(ctx context.Context, state *serve.State) error {
	return serveIO(ctx, state, os.Stdin, os.Stdout, os.Stderr)
}

// serveIO is the testable form of Serve with explicit streams.
func serveIO(ctx context.Context, state *serve.State, in io.Reader, out io.Writer, errOut io.Writer) error {
	// bufio.Scanner has a default 64KiB line cap which is too small for
	// packages-worth of JSON. Use a buffered reader with ReadBytes('\n')
	// so messages of any length are supported.
	reader := bufio.NewReader(in)

	// Stdout writes must not interleave: dispatch is synchronous here,
	// but we keep a mutex so future async extensions don't regress.
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

	// readCh decouples the blocking ReadBytes call from ctx so we can
	// exit promptly when the caller cancels.
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
			// ReadBytes may return a partial final line with io.EOF.
			if len(r.line) > 0 {
				if err := handleLine(state, r.line, writeLine, errOut); err != nil {
					fmt.Fprintf(errOut, "mcp: write response: %v\n", err)
				}
			}
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					return nil
				}
				return r.err
			}
		}
	}
}

// handleLine parses a single incoming line and emits exactly one
// response (unless the message is a notification — has no id — in
// which case nothing is written).
func handleLine(state *serve.State, line []byte, writeLine func(Response) error, errOut io.Writer) error {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		// Unparseable input: respond with ParseError and a null id.
		return writeLine(newErrorResponse(nil, ErrParseError, fmt.Sprintf("parse error: %v", err)))
	}

	// Notifications (no id) require no response per JSON-RPC 2.0.
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		if isNotification {
			return nil
		}
		return writeLine(newResponse(req.ID, newInitializeResult()))

	case "notifications/initialized", "initialized":
		// Client acknowledgement — no response required.
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
		return dispatchToolsCall(state, req, writeLine, errOut)

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

// toolsCallParams is the shape of tools/call params per the MCP spec.
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// dispatchToolsCall extracts name+arguments from the request, invokes
// the tool, and writes the response.
func dispatchToolsCall(state *serve.State, req Request, writeLine func(Response) error, errOut io.Writer) error {
	var params toolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return writeLine(newErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("invalid params: %v", err)))
		}
	}
	if params.Name == "" {
		return writeLine(newErrorResponse(req.ID, ErrInvalidParams, "missing tool name"))
	}

	result, rpcErr := Dispatch(state, params.Name, params.Arguments)
	if rpcErr != nil {
		if errOut != nil {
			fmt.Fprintf(errOut, "mcp: tool %q error: %s\n", params.Name, rpcErr.Message)
		}
		return writeLine(Response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcErr})
	}
	return writeLine(newResponse(req.ID, result))
}
