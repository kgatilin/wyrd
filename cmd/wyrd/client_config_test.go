package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/wyrd/internal/serve"
)

func writeClientConfig(t *testing.T, data string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wyrd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClientConfigWorktrees(t *testing.T) {
	repo, linked := newRepoWithWorktree(t)
	t.Setenv("HOME", filepath.Dir(repo))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeClientConfig(t, "repos:\n  ~/proj:\n    endpoint: https://wyrd.example/\n")
	for _, root := range []string{repo, linked} {
		got, err := configuredClientURL(root)
		want := "https://wyrd.example/w/" + filepath.Base(root)
		if err != nil || got != want {
			t.Fatalf("root %s: got %q, %v; want %q", root, got, err, want)
		}
	}
	t.Setenv("WYRD_HOME", t.TempDir())
	t.Chdir(linked)
	if err := serve.WriteGlobalRecord(serve.DaemonRecord{
		RepoRoot: repo, PID: os.Getpid(), HTTPAddr: "127.0.0.1:9999",
		Caps: []string{"multi"}, Worktrees: []string{"proj", "feature"},
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveClientURL(""); err != nil || got != "https://wyrd.example/w/feature" {
		t.Fatalf("config must win over a running local daemon: %q, %v", got, err)
	}
	if got, err := resolveClientURL("proj"); err != nil || got != "http://127.0.0.1:9999/w/feature" {
		t.Fatalf("explicit daemon must win over config: %q, %v", got, err)
	}
	subdir := filepath.Join(linked, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredClientURL(subdir); err != nil || got != "https://wyrd.example/w/feature" {
		t.Fatalf("subdirectory: %q, %v", got, err)
	}
	other, _ := newRepoWithWorktree(t)
	if got, err := configuredClientURL(other); err != nil || got != "" {
		t.Fatalf("unconfigured repo: %q, %v", got, err)
	}
	// The default location deliberately uses .config on macOS too.
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "wyrd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".config", "wyrd", "config.yaml"),
		[]byte("repos:\n  ~/proj:\n    endpoint: http://default.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredClientURL(linked); err != nil || got != "http://default.example/w/feature" {
		t.Fatalf("home config: %q, %v", got, err)
	}
}

func TestClientConfigErrors(t *testing.T) {
	repo, _ := newRepoWithWorktree(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got, err := configuredClientURL(repo); err != nil || got != "" {
		t.Fatalf("missing config: %q, %v", got, err)
	}
	for _, endpoint := range []string{"", "file:///tmp/server", "http://", "http://server/w/code", "http://server?x=1", "http://user:pass@server"} {
		writeClientConfig(t, "repos:\n  "+repo+":\n    endpoint: "+endpoint+"\n")
		if _, err := configuredClientURL(repo); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	writeClientConfig(t, "repoz: {}\n")
	if _, err := configuredClientURL(repo); err == nil {
		t.Fatal("accepted misspelled config field")
	}
}

// Exercise the actual CLI and both stdio modes against a remote server with
// no daemon registry. Also prove connection failure never starts a local daemon.
func TestE2E_ConfiguredClient(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess E2E")
	}
	repo, linked := newRepoWithWorktree(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stateDir := t.TempDir()
	t.Setenv("WYRD_HOME", stateDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/w/feature/api/mcp/tools/call" {
			t.Errorf("wrong worktree route: %s", r.URL.Path)
			http.Error(w, "wrong route", 404)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name != "status" {
			t.Errorf("unexpected tool request: %s, %v", req.Name, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"remote ready"}]}`))
	}))
	defer server.Close()
	writeClientConfig(t, "repos:\n  "+repo+":\n    endpoint: "+server.URL+"\n")
	bin := filepath.Join(t.TempDir(), "wyrd")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	run := func(input string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = linked
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if strings.Contains(stderr.String(), "auto-start") {
			t.Errorf("configured client attempted autostart: %s", stderr.String())
		}
		return stdout.String() + stderr.String(), err
	}
	for _, args := range [][]string{{"graph", "status"}, {"daemon", "status"}} {
		out, err := run("", args...)
		if err != nil || !strings.Contains(out, "remote ready") {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if out, err := run("", "where"); err != nil || !strings.Contains(out, server.URL+"/w/feature") {
		t.Fatalf("where: %v\n%s", err, out)
	}
	request := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"status\",\"arguments\":{}}}\n"
	for _, args := range [][]string{{"serve", "--mcp-stdio"}, {"serve", "--mcp-stdio", "--shared"}} {
		out, err := run(request, args...)
		if err != nil || !strings.Contains(out, "remote ready") {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	server.Close()
	if out, err := run("", "graph", "status"); err == nil {
		t.Fatalf("unreachable server succeeded: %s", out)
	}
	if out, _ := run(request, "serve", "--mcp-stdio"); !strings.Contains(out, "error") && !strings.Contains(out, "isError") {
		t.Fatalf("unreachable MCP server did not report failure: %s", out)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("configured client wrote local daemon state: %v, %v", entries, err)
	}
}
