package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2E_MCPStdio spawns `archai serve --mcp-stdio` as a subprocess in
// a throwaway Go module and drives it through initialize → tools/list →
// tools/call for each of the three tools, asserting the responses.
func TestE2E_MCPStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess E2E in -short mode")
	}

	// Build the CLI binary from the repo root (two levels up from cwd).
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "archai")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build archai: %v", err)
	}

	// Create a minimal Go module with two packages so the daemon has
	// something to return.
	projectDir := t.TempDir()
	mustWriteE2E(t, filepath.Join(projectDir, "go.mod"), "module mcp.test\n\ngo 1.21\n")
	mustWriteE2E(t, filepath.Join(projectDir, "alpha", "alpha.go"), `package alpha

type Service interface{ Do() }
type Impl struct{}
func New() *Impl { return &Impl{} }
`)
	mustWriteE2E(t, filepath.Join(projectDir, "beta", "beta.go"), `package beta

func Hello() string { return "hi" }
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use --no-daemon so this test exercises the in-process one-shot
	// mode (previously the default shape of `serve --mcp-stdio`).
	// Thin-client mode has its own dedicated coverage in
	// TestE2E_MCPStdio_ThinClient.
	cmd := exec.CommandContext(ctx, binPath, "serve", "--mcp-stdio", "--no-daemon", "--root", projectDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	reader := bufio.NewReader(stdout)

	sendRequest := func(method string, id int, params any) Response {
		t.Helper()
		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
		}
		if params != nil {
			req["params"] = params
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if _, err := stdin.Write(append(data, '\n')); err != nil {
			t.Fatalf("write request: %v", err)
		}
		line, err := readLineWithTimeout(reader, 5*time.Second)
		if err != nil {
			t.Fatalf("read response (%s): %v (stderr=%s)", method, err, stderr.String())
		}
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("unmarshal response (%s): %v — line=%s", method, err, line)
		}
		return resp
	}

	// 1) initialize
	resp := sendRequest("initialize", 1, nil)
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}

	// 2) tools/list — confirm the three tool names
	resp = sendRequest("tools/list", 2, nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	toolsPayload, _ := json.Marshal(resp.Result)
	var toolsWrapper struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(toolsPayload, &toolsWrapper); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range toolsWrapper.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"extract", "list_packages", "get_package"} {
		if !names[want] {
			t.Errorf("missing tool %q in tools/list: %+v", want, toolsWrapper.Tools)
		}
	}

	// 3) tools/call list_packages — expect both packages.
	resp = sendRequest("tools/call", 3, map[string]any{
		"name":      "list_packages",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("list_packages error: %+v", resp.Error)
	}
	listText := textContent(t, resp)
	if !strings.Contains(listText, `"path": "alpha"`) || !strings.Contains(listText, `"path": "beta"`) {
		t.Errorf("list_packages did not include both packages: %s", listText)
	}

	// 4) tools/call extract with a filter
	resp = sendRequest("tools/call", 4, map[string]any{
		"name": "extract",
		"arguments": map[string]any{
			"paths": []string{"alpha"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("extract error: %+v", resp.Error)
	}
	extractText := textContent(t, resp)
	if !strings.Contains(extractText, `"Path": "alpha"`) {
		t.Errorf("extract did not include alpha (mixed-case Path): %s", extractText)
	}
	if strings.Contains(extractText, `"Path": "beta"`) {
		t.Errorf("extract should not include beta when filtered: %s", extractText)
	}

	// 5) tools/call get_package for beta
	resp = sendRequest("tools/call", 5, map[string]any{
		"name": "get_package",
		"arguments": map[string]any{
			"path": "beta",
		},
	})
	if resp.Error != nil {
		t.Fatalf("get_package error: %+v", resp.Error)
	}
	pkgText := textContent(t, resp)
	if !strings.Contains(pkgText, `"Name": "beta"`) {
		t.Errorf("get_package did not return beta payload: %s", pkgText)
	}

	// 6) tools/call get_package for unknown path → isError=true
	resp = sendRequest("tools/call", 6, map[string]any{
		"name": "get_package",
		"arguments": map[string]any{
			"path": "nope",
		},
	})
	if resp.Error != nil {
		t.Fatalf("get_package (unknown) RPC error: %+v", resp.Error)
	}
	payload, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(payload), `"isError":true`) {
		t.Errorf("expected isError=true for unknown package, got %s", payload)
	}
}

// Response mirrors the adapter/mcp.Response shape for test-local use so
// the e2e test doesn't import an internal package outside its module
// subtree (it's same module, but keeping the boundary clean).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func textContent(t *testing.T, resp Response) string {
	t.Helper()
	payload, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var tr struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &tr); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if len(tr.Content) == 0 {
		t.Fatalf("tool result has no content: %s", payload)
	}
	return tr.Content[0].Text
}

func mustWriteE2E(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readLineWithTimeout reads until '\n' or the timeout expires. A helper
// so a hung daemon surfaces as a test failure rather than a deadlock.
func readLineWithTimeout(r *bufio.Reader, d time.Duration) ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadBytes('\n')
		ch <- result{line: line, err: err}
	}()
	select {
	case res := <-ch:
		if res.err != nil && res.err != io.EOF {
			return nil, res.err
		}
		return res.line, nil
	case <-time.After(d):
		return nil, context.DeadlineExceeded
	}
}
