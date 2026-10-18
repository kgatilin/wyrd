package daemonmodel

import (
	"bytes"
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	wyrdhttp "github.com/kgatilin/wyrd/internal/adapter/http"
	"github.com/kgatilin/wyrd/internal/check"
	"github.com/kgatilin/wyrd/internal/serve"
)

const overlayYAML = `module: example.com/app
layers:
  domain:
    - internal/domain/...
  adapter:
    - internal/adapter/...
layer_rules:
  adapter:
    - domain
  domain: []
policy:
  allow:
    - "internal/adapter/... -> internal/domain/..."
`

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newProject writes a module whose domain package reaches the adapter only
// through an inferred generic argument, inside a git repository, and points
// the client configuration at a daemon serving it. It returns the root and
// the number of models the daemon has answered.
func newProject(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.21\n")
	write(t, filepath.Join(root, "wyrd.yaml"), overlayYAML)
	write(t, filepath.Join(root, "internal", "gen", "gen.go"), `package gen

func First[T any](xs []T) T { return xs[0] }
`)
	write(t, filepath.Join(root, "internal", "adapter", "adapter.go"), `package adapter

type Conn struct{}
`)
	write(t, filepath.Join(root, "internal", "domain", "lookup", "lookup.go"), `package lookup

type Item struct{}

func All() []Item { return nil }
`)
	write(t, filepath.Join(root, "internal", "domain", "order.go"), `package domain

import (
	"example.com/app/internal/adapter"
	"example.com/app/internal/domain/lookup"
	"example.com/app/internal/gen"
)

var _ adapter.Conn

func Pick() { _ = gen.First(lookup.All()) }
`)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	state := serve.NewState(root)
	if err := state.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv, err := wyrdhttp.NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	bound := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx, "127.0.0.1:0", func(addr string) { bound <- addr })
	}()
	t.Cleanup(func() { cancel(); <-done })
	daemon, err := url.Parse("http://" + <-bound)
	if err != nil {
		t.Fatal(err)
	}

	// The configured endpoint addresses the worktree as /w/app, the way a
	// repo-level daemon serves it; this single-worktree one serves it at /.
	var answered atomic.Int32
	proxy := httputil.NewSingleHostReverseProxy(daemon)
	proxy.ModifyResponse = func(resp *nethttp.Response) error {
		if resp.Request.URL.Path == "/api/model" && resp.StatusCode == nethttp.StatusOK {
			answered.Add(1)
		}
		return nil
	}
	ts := httptest.NewServer(nethttp.StripPrefix("/w/app", proxy))
	t.Cleanup(ts.Close)

	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	write(t, filepath.Join(configDir, "wyrd", "config.yaml"), "repos:\n  "+root+":\n    endpoint: "+ts.URL+"\n")
	t.Chdir(root)
	return root, &answered
}

// runAll runs every gate wyrd-check all runs and returns its report and
// whether it failed.
func runAll(t *testing.T, root string, c *check.Checker) (string, bool) {
	t.Helper()
	overlayPath, goModPath := check.ResolveOverlay("")
	var out, errOut bytes.Buffer
	err := c.RunAll(context.Background(), check.AllOptions{OverlayPath: overlayPath, GoModPath: goModPath}, &out, &errOut)
	return out.String(), err != nil
}

// TestReaderGivesTheGatesWhatALocalParseGives: the gates report the same
// through the daemon as over a local parse — after the load, and after an
// edit to a package an untouched one infers its dependencies from.
func TestReaderGivesTheGatesWhatALocalParseGives(t *testing.T) {
	root, answered := newProject(t)
	var warn bytes.Buffer
	daemon := check.New(NewReader(golang.NewReader(), &warn), nil)
	local := check.New(golang.NewReader(), nil)

	compare := func(step string) {
		t.Helper()
		before := answered.Load()
		got, gotFailed := runAll(t, root, daemon)
		want, wantFailed := runAll(t, root, local)
		if answered.Load() == before {
			t.Fatalf("%s: the daemon answered no model (warnings: %s)", step, warn.String())
		}
		if got != want || gotFailed != wantFailed {
			t.Fatalf("%s: through the daemon:\n%s(failed %v)\nlocal parse:\n%s(failed %v)", step, got, gotFailed, want, wantFailed)
		}
		if !gotFailed {
			t.Fatalf("%s: the fixture's violation was not reported:\n%s", step, got)
		}
	}
	compare("load")

	// lookup.All now returns adapter values: domain's inferred argument
	// moves to the adapter package without domain's source changing.
	write(t, filepath.Join(root, "internal", "domain", "lookup", "lookup.go"), `package lookup

import "example.com/app/internal/adapter"

type Item struct{}

func All() []adapter.Conn { return nil }
`)
	compare("API edit")
}

// TestReaderParsesLocallyWhenTheDaemonDoesNotAnswer: a configured daemon
// that refuses is reported, and the read is a local parse.
func TestReaderParsesLocallyWhenTheDaemonDoesNotAnswer(t *testing.T) {
	ts := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		nethttp.Error(w, "the daemon's model is of another source tree", nethttp.StatusConflict)
	}))
	defer ts.Close()
	root, _ := newProject(t)
	write(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wyrd", "config.yaml"), "repos:\n  "+root+":\n    endpoint: "+ts.URL+"\n")

	var warn bytes.Buffer
	got, gotFailed := runAll(t, root, check.New(NewReader(golang.NewReader(), &warn), nil))
	want, wantFailed := runAll(t, root, check.New(golang.NewReader(), nil))
	if got != want || gotFailed != wantFailed {
		t.Fatalf("fallback report differs from a local parse:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(warn.String(), "another source tree") || !strings.Contains(warn.String(), "parsing locally") {
		t.Fatalf("warning = %q, want the daemon's reason and the fallback", warn.String())
	}
}
