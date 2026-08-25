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

	"github.com/kgatilin/wyrd/internal/serve"
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
				if err := handleLine(ctx, state, r.line, writeLine, errOut); err != nil {
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
// response, unless the message is a notification (has no id) — then
// nothing is written. Every method is answered by HandleRequest, which
// the Streamable HTTP endpoint calls too.
func handleLine(ctx context.Context, state *serve.State, line []byte, writeLine func(Response) error, errOut io.Writer) error {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		// Unparseable input: respond with ParseError and a null id.
		return writeLine(NewErrorResponse(nil, ErrParseError, fmt.Sprintf("parse error: %v", err)))
	}

	resp := HandleRequest(ctx, state, req, errOut)
	if resp == nil {
		return nil
	}
	return writeLine(*resp)
}
