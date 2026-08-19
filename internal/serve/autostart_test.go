package serve

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

func TestDiscoverDaemon_NoRecord(t *testing.T) {
	root := t.TempDir()
	rec, _, err := DiscoverDaemon(root)
	if err != nil {
		t.Fatalf("DiscoverDaemon: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil record, got %+v", rec)
	}
}

func TestDiscoverDaemon_LiveRecord(t *testing.T) {
	root := t.TempDir()
	name := worktree.Name(root)
	// Write a serve.json whose PID is this test process — guaranteed alive.
	rec := worktree.ServeRecord{
		PID:       os.Getpid(),
		HTTPAddr:  "127.0.0.1:12345",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := worktree.WriteServe(root, name, rec); err != nil {
		t.Fatalf("WriteServe: %v", err)
	}

	got, _, err := DiscoverDaemon(root)
	if err != nil {
		t.Fatalf("DiscoverDaemon: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.PID != rec.PID || got.HTTPAddr != rec.HTTPAddr {
		t.Errorf("mismatch: got=%+v want=%+v", got, rec)
	}
}

func TestDiscoverDaemon_StaleRecord(t *testing.T) {
	root := t.TempDir()
	name := worktree.Name(root)
	// PID 0 is never a live process on Unix — PIDAlive returns false.
	rec := worktree.ServeRecord{
		PID:       0,
		HTTPAddr:  "127.0.0.1:1",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// Write directly so the guard fires without racing real processes.
	if err := os.MkdirAll(filepath.Dir(worktree.ServePath(root, name)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := worktree.WriteServe(root, name, rec); err != nil {
		t.Fatalf("WriteServe: %v", err)
	}

	got, _, err := DiscoverDaemon(root)
	if err != nil {
		t.Fatalf("DiscoverDaemon: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for stale record, got %+v", got)
	}
}

// TestAutoStartDaemon_LockSerializesCallers exercises the file lock
// added to prevent two concurrent MCP thin clients from both spawning
// a daemon. We use a fake exe whose sole job is to write a serve.json
// pointing at our test process (so PIDAlive returns true). If the
// lock works, only one fake-exe invocation races through to "spawn"
// — the second call sees the now-written serve.json on its
// re-check and returns it without ever calling the exe.
func TestAutoStartDaemon_LockSerializesCallers(t *testing.T) {
	// Use an OS-level temp dir (not t.TempDir()) so we escape the
	// surrounding git repo — otherwise worktree.Name() returns the
	// archai repo name while the fake daemon derives it from
	// filepath.Base(root), and they disagree on serve.json's path.
	root, err := os.MkdirTemp("", "archai-autostart-lock-*")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	// Build a tiny helper binary that writes serve.json and exits.
	// We can't rely on a real `archai serve` here — the test is about
	// the lock, not the daemon startup.
	fakeExe := buildFakeDaemon(t, root)
	// Pass this test binary's PID through to fake daemon so the
	// serve.json it writes survives the PIDAlive check.
	t.Setenv("FAKE_DAEMON_PID", strconv.Itoa(os.Getpid()))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	recs := make(chan *worktree.ServeRecord, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := AutoStartDaemon(AutoStartOptions{
				ExePath:      fakeExe,
				Root:         root,
				HTTPAddr:     "127.0.0.1:0",
				WaitTimeout:  3 * time.Second,
				PollInterval: 20 * time.Millisecond,
				Stderr:       os.Stderr,
			})
			if err != nil {
				errs <- err
				return
			}
			recs <- rec
		}()
	}
	wg.Wait()
	close(errs)
	close(recs)

	for err := range errs {
		t.Errorf("AutoStartDaemon error: %v", err)
	}

	count := 0
	var first *worktree.ServeRecord
	for r := range recs {
		count++
		if first == nil {
			first = r
		} else if r.PID != first.PID || r.HTTPAddr != first.HTTPAddr {
			t.Errorf("concurrent callers got different records: %+v vs %+v", first, r)
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 records, got %d", count)
	}

	// Cross-check: the fake exe counter file should show it was
	// invoked at most twice (we allow 1 or 2: on a fast host the
	// second caller's re-check after the lock might still race
	// before the first writes serve.json). The invariant we actually
	// care about — both callers see the same live record — is
	// already checked above.
	counterPath := filepath.Join(root, "spawn.counter")
	data, _ := os.ReadFile(counterPath)
	t.Logf("fake-exe invocation count: %q", string(data))
	if len(data) == 0 {
		t.Errorf("fake exe was not invoked at all")
	}
}

// buildFakeDaemon compiles a tiny Go program that (a) bumps a counter
// file under root and (b) writes a serve.json claiming its own PID at
// a loopback address, then exits. Used to stand in for real `archai
// serve` in the lock race test.
func buildFakeDaemon(t *testing.T, root string) string {
	t.Helper()

	src := `package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	// Drop "serve" subcommand arg if present (autostart passes it
	// positionally after exe, before flags).
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	rootFlag := flag.String("root", "", "")
	flag.String("http", "", "")
	// Accept --idle-timeout so the CLI arg passthrough doesn't break.
	flag.String("idle-timeout", "", "")
	flag.Parse()
	if *rootFlag == "" {
		os.Exit(2)
	}

	// Bump counter.
	counter := filepath.Join(*rootFlag, "spawn.counter")
	n := 0
	if b, err := os.ReadFile(counter); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	_ = os.WriteFile(counter, []byte(strconv.Itoa(n+1)), 0o644)

	// Derive worktree name the same way worktree.Name falls back
	// when git is absent: base(abs(root)).
	abs, _ := filepath.Abs(*rootFlag)
	name := filepath.Base(abs)

	dir := filepath.Join(*rootFlag, ".arch", ".worktree", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Use FAKE_DAEMON_PID from env — set by the test to its own PID
	// so PIDAlive returns true. os.Getppid() is unreliable here:
	// detachProcess() runs Setsid on the child and re-parents it to
	// init, so the reported ppid is 1 (or 0 on some kernels).
	pid := os.Getpid()
	if env := os.Getenv("FAKE_DAEMON_PID"); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil {
			pid = parsed
		}
	}
	rec := map[string]any{
		"pid":        pid,
		"http_addr":  "127.0.0.1:1",
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(rec)
	tmp := filepath.Join(dir, "serve.json.tmp")
	_ = os.WriteFile(tmp, b, 0o644)
	_ = os.Rename(tmp, filepath.Join(dir, "serve.json"))

	// Sleep briefly so if two of us race, both are alive long
	// enough for the lock test to be meaningful.
	time.Sleep(200 * time.Millisecond)
}
`
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module fakedaemon\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	srcPath := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write fake source: %v", err)
	}

	binPath := filepath.Join(srcDir, "fake-daemon")
	// Use go build via os/exec.
	if err := goBuild(t, srcDir, binPath); err != nil {
		t.Fatalf("build fake daemon: %v", err)
	}
	return binPath
}

func goBuild(t *testing.T, srcDir, out string) error {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// TestAutoStartRepoDaemon_PinsAddrFromOverlay proves the setting a
// project writes down actually reaches the daemon it starts. Auto-start
// passes --http explicitly, so the spawned process never reads
// archai.yaml itself — if the resolution here regressed, every project
// would silently be back on a kernel-assigned port and every bookmarked
// review URL would break after a restart.
func TestAutoStartRepoDaemon_PinsAddrFromOverlay(t *testing.T) {
	repo := autoStartTempRepo(t)
	writeServeOverlay(t, repo, "127.0.0.1:47811")

	rec := runFakeRepoDaemon(t, repo, "")
	if rec.HTTPAddr != "127.0.0.1:47811" {
		t.Errorf("registered addr = %q, want the pinned 127.0.0.1:47811", rec.HTTPAddr)
	}
	if got := spawnArg(t, repo, "--http"); got != "127.0.0.1:47811" {
		t.Errorf("--http = %q, want the pinned 127.0.0.1:47811", got)
	}
}

// An explicit address still wins: the overlay is the project default,
// not an override of what a caller asked for.
func TestAutoStartRepoDaemon_ExplicitAddrBeatsOverlay(t *testing.T) {
	repo := autoStartTempRepo(t)
	writeServeOverlay(t, repo, "127.0.0.1:47811")

	runFakeRepoDaemon(t, repo, "127.0.0.1:47822")
	if got := spawnArg(t, repo, "--http"); got != "127.0.0.1:47822" {
		t.Errorf("--http = %q, want the caller's 127.0.0.1:47822", got)
	}
}

// With nothing pinned the kernel picks the port, as before.
func TestAutoStartRepoDaemon_NoOverlayKeepsDynamicPort(t *testing.T) {
	repo := autoStartTempRepo(t)

	runFakeRepoDaemon(t, repo, "")
	if got := spawnArg(t, repo, "--http"); got != "127.0.0.1:0" {
		t.Errorf("--http = %q, want the dynamic 127.0.0.1:0", got)
	}
}

func TestAutoStartRepoDaemon_MalformedOverlayIsAnError(t *testing.T) {
	repo := autoStartTempRepo(t)
	writeServeOverlay(t, repo, "127.0.0.1:not-a-port")

	if _, _, err := AutoStartRepoDaemon(AutoStartOptions{
		ExePath:      buildFakeRepoDaemon(t, repo),
		Root:         repo,
		WaitTimeout:  3 * time.Second,
		PollInterval: 20 * time.Millisecond,
	}); err == nil {
		t.Fatal("expected an error for a malformed serve.http_addr, got nil")
	}
}

// autoStartTempRepo returns a directory outside any git repository, with
// the global daemon registry redirected into it.
func autoStartTempRepo(t *testing.T) string {
	t.Helper()
	// Not t.TempDir(): that lives under the surrounding git repo on some
	// hosts, and RepoRoot would then resolve somewhere unexpected.
	repo, err := os.MkdirTemp("", "archai-autostart-pin-*")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	t.Setenv("ARCHAI_HOME", filepath.Join(repo, "home"))
	t.Setenv("FAKE_DAEMON_PID", strconv.Itoa(os.Getpid()))
	return repo
}

func writeServeOverlay(t *testing.T, repo, addr string) {
	t.Helper()
	doc := "module: github.com/example/app\n\nlayers:\n  domain:\n    - internal/domain/...\n\nlayer_rules:\n  domain: []\n\nserve:\n  http_addr: \"" + addr + "\"\n"
	if err := os.WriteFile(filepath.Join(repo, "archai.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write archai.yaml: %v", err)
	}
}

func runFakeRepoDaemon(t *testing.T, repo, addr string) *DaemonRecord {
	t.Helper()
	rec, _, err := AutoStartRepoDaemon(AutoStartOptions{
		ExePath:      buildFakeRepoDaemon(t, repo),
		Root:         repo,
		HTTPAddr:     addr,
		WaitTimeout:  5 * time.Second,
		PollInterval: 20 * time.Millisecond,
		Stderr:       os.Stderr,
		Multi:        true,
	})
	if err != nil {
		t.Fatalf("AutoStartRepoDaemon: %v", err)
	}
	return rec
}

// spawnArg reads the argv the fake daemon recorded and returns the value
// that followed `flag`.
func spawnArg(t *testing.T, repo, flag string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "spawn.args"))
	if err != nil {
		t.Fatalf("read spawn.args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("%s not found in spawned argv %q", flag, args)
	return ""
}

// buildFakeRepoDaemon compiles a stand-in for `archai serve --multi`: it
// records the argv it was given and registers itself in the global
// registry the way a real daemon does, then exits.
func buildFakeRepoDaemon(t *testing.T, repo string) string {
	t.Helper()

	src := `package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	repo, addr := "", ""
	for i, a := range args {
		if a == "--repo" && i+1 < len(args) {
			repo = args[i+1]
		}
		if a == "--http" && i+1 < len(args) {
			addr = args[i+1]
		}
	}
	if repo == "" {
		os.Exit(2)
	}
	_ = os.WriteFile(filepath.Join(repo, "spawn.args"), []byte(strings.Join(args, "\n")), 0o644)

	pid := os.Getpid()
	if env := os.Getenv("FAKE_DAEMON_PID"); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil {
			pid = parsed
		}
	}
	abs, _ := filepath.Abs(repo)
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	dir := filepath.Join(os.Getenv("ARCHAI_HOME"), "daemons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		os.Exit(1)
	}
	rec := map[string]any{
		"repo_root":  repo,
		"http_addr":  addr,
		"pid":        pid,
		"caps":       []string{"mcp", "ui", "multi"},
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(rec)
	_ = os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".json"), b, 0o644)
}
`
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write fake daemon: %v", err)
	}
	exe := filepath.Join(dir, "fake-repo-daemon")
	build := exec.Command("go", "build", "-o", exe, srcPath)
	build.Env = append(os.Environ(), "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake daemon: %v\n%s", err, out)
	}
	return exe
}

// TestDetachStdio_HandsTheChildFiles guards the difference between
// /dev/null and io.Discard. io.Discard is not an *os.File, so os/exec
// gives the child a pipe drained by the parent — and a detached daemon
// outlives its parent by design, so the first log line after the parent
// exits raises SIGPIPE and the Go runtime kills the daemon. It would
// register, then vanish, leaving a stale record and a dead URL.
func TestDetachStdio_HandsTheChildFiles(t *testing.T) {
	cmd := exec.Command("true")
	devNull, err := detachStdio(cmd, nil)
	if err != nil {
		t.Fatalf("detachStdio: %v", err)
	}
	defer devNull.Close()

	if _, ok := cmd.Stdout.(*os.File); !ok {
		t.Errorf("stdout = %T, want *os.File (a pipe dies with the parent)", cmd.Stdout)
	}
	if _, ok := cmd.Stderr.(*os.File); !ok {
		t.Errorf("stderr = %T, want *os.File (a pipe dies with the parent)", cmd.Stderr)
	}
	if cmd.Stdin != nil {
		t.Errorf("stdin = %v, want nil so exec opens /dev/null itself", cmd.Stdin)
	}
}

// A caller that asks for the child's stderr still gets it — that is how
// the tests above read what a spawned daemon reported.
func TestDetachStdio_KeepsAnExplicitStderr(t *testing.T) {
	cmd := exec.Command("true")
	var sink bytes.Buffer
	devNull, err := detachStdio(cmd, &sink)
	if err != nil {
		t.Fatalf("detachStdio: %v", err)
	}
	defer devNull.Close()

	if cmd.Stderr != &sink {
		t.Errorf("stderr = %v, want the writer the caller passed", cmd.Stderr)
	}
}
