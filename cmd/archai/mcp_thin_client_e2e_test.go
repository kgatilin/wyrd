package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2E_MCPStdio_ThinClient exercises the M11 thin-client path end
// to end: `archai serve --mcp-stdio` without --no-daemon auto-starts
// an HTTP daemon, the wrapper proxies tools/call over HTTP, and the
// responses match the one-shot mode for the same Go module. Uses a
// single rebuilt archai binary (~3s) that's shared across the two
// subprocess lifecycles.
func TestE2E_MCPStdio_ThinClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess E2E in -short mode")
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "archai")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build archai: %v", err)
	}

	projectDir := t.TempDir()
	mustWriteE2E(t, filepath.Join(projectDir, "go.mod"), "module thin.test\n\ngo 1.21\n")
	mustWriteE2E(t, filepath.Join(projectDir, "alpha", "alpha.go"), `package alpha

type Service interface{ Do() }
type Impl struct{}
func New() *Impl { return &Impl{} }
`)
	// Mark the project as a git repo so worktree.Name() is stable —
	// without it, both the auto-started daemon and the wrapper fall
	// back to filepath.Base which still works, but a proper git env
	// matches real-world usage.
	gitInit := exec.Command("git", "init", "-q", projectDir)
	_ = gitInit.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "serve", "--mcp-stdio", "--root", projectDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
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
		// Best-effort: kill the auto-started daemon if it's still around.
		killDaemonFromServeJSON(projectDir)
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
		data, _ := json.Marshal(req)
		if _, err := stdin.Write(append(data, '\n')); err != nil {
			t.Fatalf("write %s: %v", method, err)
		}
		line, err := readLineWithTimeout(reader, 15*time.Second)
		if err != nil {
			t.Fatalf("read %s: %v — stderr=%s", method, err, stderr.String())
		}
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("decode %s: %v — line=%s", method, err, line)
		}
		return resp
	}

	// initialize — handled locally by the wrapper.
	resp := sendRequest("initialize", 1, nil)
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}

	// list_packages — forwarded to the auto-started daemon.
	resp = sendRequest("tools/call", 2, map[string]any{
		"name":      "list_packages",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("list_packages error: %+v — stderr=%s", resp.Error, stderr.String())
	}
	text := textContent(t, resp)
	if !strings.Contains(text, `"path": "alpha"`) {
		t.Errorf("list_packages missing alpha: %s", text)
	}

	// lock_target — exercises a write-path HTTP endpoint.
	resp = sendRequest("tools/call", 3, map[string]any{
		"name": "lock_target",
		"arguments": map[string]any{
			"id": "v1",
		},
	})
	if resp.Error != nil {
		t.Fatalf("lock_target error: %+v — stderr=%s", resp.Error, stderr.String())
	}
	lockText := textContent(t, resp)
	// target.TargetMeta has no json tags so the field name surfaces as "ID".
	if !strings.Contains(lockText, `"ID": "v1"`) {
		t.Errorf("lock_target did not return v1 meta: %s", lockText)
	}

	// validate against the freshly-locked target — expect ok=true.
	resp = sendRequest("tools/call", 4, map[string]any{
		"name": "validate",
		"arguments": map[string]any{
			"target": "v1",
		},
	})
	if resp.Error != nil {
		t.Fatalf("validate error: %+v", resp.Error)
	}
	validateText := textContent(t, resp)
	if !strings.Contains(validateText, `"ok": true`) {
		t.Errorf("validate.ok != true: %s", validateText)
	}

	// Verify a serve.json was written for the auto-started daemon.
	glob := filepath.Join(projectDir, ".arch", ".worktree", "*", "serve.json")
	matches, _ := filepath.Glob(glob)
	if len(matches) == 0 {
		t.Errorf("expected serve.json under .arch/.worktree/, got none — stderr=%s", stderr.String())
	}
}

// killDaemonFromServeJSON reads each serve.json under projectDir and
// sends SIGTERM to the recorded PID. Best-effort — missing files and
// already-dead processes are ignored.
func killDaemonFromServeJSON(projectDir string) {
	glob := filepath.Join(projectDir, ".arch", ".worktree", "*", "serve.json")
	matches, _ := filepath.Glob(glob)
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var rec struct {
			PID int `json:"pid"`
		}
		if err := json.Unmarshal(data, &rec); err != nil || rec.PID <= 0 {
			continue
		}
		if proc, err := os.FindProcess(rec.PID); err == nil {
			_ = proc.Kill()
		}
	}
}
