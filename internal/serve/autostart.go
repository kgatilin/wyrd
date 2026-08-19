package serve

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/worktree"
)

// DiscoverDaemon looks up the running daemon record for the worktree
// containing projectRoot. Returns the parsed record (nil when no live
// daemon is registered) and the worktree name used for disk lookups.
// A stale record (process no longer alive) is treated the same as
// "no record" — the caller can then auto-start a fresh daemon.
//
// Deprecated: Use DiscoverRepoDaemon for the new global registry model.
// This function is retained for backward compatibility with existing
// per-worktree serve.json records.
func DiscoverDaemon(projectRoot string) (*worktree.ServeRecord, string, error) {
	name := worktree.Name(projectRoot)
	rec, err := worktree.ReadServe(projectRoot, name)
	if err != nil {
		return nil, name, err
	}
	if rec == nil {
		return nil, name, nil
	}
	if !worktree.PIDAlive(rec.PID) {
		return nil, name, nil
	}
	return rec, name, nil
}

// DiscoverRepoDaemon looks up the running multi-worktree daemon for the
// repo containing cwd via the global registry under $HOME/.arch/daemons.
// Returns the daemon record (nil when no live daemon is registered),
// the repo root, and the current worktree name.
//
// Discovery is deliberately global-registry-only. The legacy per-worktree
// serve.json lives inside the repo tree, so when the repo is bind-mounted
// into a container at an identical path (as a containerized dev setup that
// mounts the repo does), the container's own archai overwrites it with a PID
// and address
// that are only valid inside the container's namespace — poisoning host
// discovery. The global registry lives under $HOME, which differs between
// host ($HOME/.arch) and container (/root/.arch) and is never mounted
// across, so it stays isolated. serve.json is still written for backward
// compatibility but is no longer trusted for repo-level discovery.
func DiscoverRepoDaemon(cwd string) (*DaemonRecord, string, string, error) {
	repoRoot, ok := worktree.RepoRoot(cwd)
	if !ok {
		// Not in a git repo — fall back to cwd as both repo and worktree.
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return nil, "", "", fmt.Errorf("discover: resolve %s: %w", cwd, err)
		}
		repoRoot = abs
	}
	wtName := worktree.Name(cwd)

	globalRec, err := ReadGlobalRecord(repoRoot)
	if err != nil {
		return nil, repoRoot, wtName, err
	}
	return globalRec, repoRoot, wtName, nil
}

// AutoStartOptions configures how a background daemon is launched by
// AutoStartDaemon.
type AutoStartOptions struct {
	// ExePath is the path to the archai binary. Defaults to os.Args[0]
	// (callers can override for tests).
	ExePath string

	// Root is the project root the daemon should serve. Required.
	Root string

	// HTTPAddr is the listen address passed to `archai serve --http`.
	// Empty means "ask the repo": the project's archai.yaml
	// serve.http_addr if it pins one, else "127.0.0.1:0" so the kernel
	// picks a free port and the daemon stays on the loopback interface.
	HTTPAddr string

	// WaitTimeout is the maximum time to wait for serve.json to appear
	// and the PID to become alive. Zero uses 5s.
	WaitTimeout time.Duration

	// PollInterval is the polling cadence for serve.json. Zero uses
	// 50ms.
	PollInterval time.Duration

	// Stderr, when non-nil, receives the child's stderr. Defaults to
	// os.DevNull so the parent process (e.g. the MCP stdio wrapper)
	// keeps stderr free of daemon noise.
	Stderr io.Writer

	// IdleTimeout, when non-zero, is passed as `--idle-timeout` to the
	// spawned daemon so it exits after that long without HTTP traffic.
	// Used by the MCP thin client to avoid leaking orphan daemons.
	IdleTimeout time.Duration

	// Multi, when true, starts a multi-worktree daemon (--multi flag).
	// This is the new default for MCP clients.
	Multi bool
}

// AutoStartDaemon spawns `archai serve --http <addr>` as a detached
// child process and waits until its serve.json record appears on disk
// and the PID is alive. Returns the discovered record on success. The
// child is intentionally NOT attached to the parent's process group so
// it survives when the MCP stdio wrapper exits — callers who want to
// tear the daemon down should signal it by PID.
//
// A file lock at .arch/.worktree/<name>/autostart.lock serializes
// concurrent callers. The sequence under the lock is:
//
//  1. re-check serve.json (another process may have won the race)
//  2. if still no live daemon, spawn one
//  3. poll for serve.json to confirm the child registered
//
// This prevents two simultaneous MCP clients in the same worktree from
// spawning two daemons where the second overwrites the first's
// serve.json and leaves the first daemon as an orphaned listener.
func AutoStartDaemon(opts AutoStartOptions) (*worktree.ServeRecord, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("autostart: empty root")
	}
	exePath := opts.ExePath
	if exePath == "" {
		exe, err := os.Executable()
		if err != nil || exe == "" {
			exePath = os.Args[0]
		} else {
			exePath = exe
		}
	}
	httpAddr := opts.HTTPAddr
	if httpAddr == "" {
		httpAddr = "127.0.0.1:0"
	}
	waitTimeout := opts.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 5 * time.Second
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 50 * time.Millisecond
	}

	name := worktree.Name(opts.Root)

	// Acquire the per-worktree autostart lock so two MCP clients
	// racing to auto-start don't both spawn a daemon. The lock file
	// lives alongside serve.json under .arch/.worktree/<name>/.
	lockDir := filepath.Join(opts.Root, ".arch", ".worktree", name)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("autostart: create %s: %w", lockDir, err)
	}
	lockPath := filepath.Join(lockDir, "autostart.lock")
	unlock, err := acquireAutoStartLock(lockPath, waitTimeout)
	if err != nil {
		return nil, fmt.Errorf("autostart: acquire lock: %w", err)
	}
	defer unlock()

	// Re-check: another caller may have started a daemon while we
	// were waiting for the lock.
	if rec, rerr := worktree.ReadServe(opts.Root, name); rerr == nil && rec != nil && worktree.PIDAlive(rec.PID) {
		return rec, nil
	}

	args := []string{"serve", "--root", opts.Root, "--http", httpAddr}
	if opts.IdleTimeout > 0 {
		args = append(args, "--idle-timeout", opts.IdleTimeout.String())
	}
	if opts.Multi {
		args = append(args, "--multi")
	}
	cmd := exec.Command(exePath, args...)
	devNull, err := detachStdio(cmd, opts.Stderr)
	if err != nil {
		return nil, err
	}
	defer devNull.Close()
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("autostart: start daemon: %w", err)
	}
	// Release the process so we don't accumulate zombies if the wait
	// below times out — the daemon continues running independently.
	childPID := cmd.Process.Pid
	_ = cmd.Process.Release()

	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		rec, err := worktree.ReadServe(opts.Root, name)
		if err == nil && rec != nil && worktree.PIDAlive(rec.PID) {
			return rec, nil
		}
		time.Sleep(pollInterval)
	}
	return nil, fmt.Errorf("autostart: daemon (pid %d) did not register within %s", childPID, waitTimeout)
}

// AutoStartRepoDaemon spawns a multi-worktree daemon for the repo
// containing cwd. It discovers the repo root, acquires a repo-level
// lock, and starts the daemon with --multi --ui flags. The daemon
// is discovered via the global registry.
//
// Returns the daemon record and the current worktree name.
func AutoStartRepoDaemon(opts AutoStartOptions) (*DaemonRecord, string, error) {
	if opts.Root == "" {
		return nil, "", fmt.Errorf("autostart: empty root")
	}

	// Resolve repo root.
	repoRoot, ok := worktree.RepoRoot(opts.Root)
	if !ok {
		abs, err := filepath.Abs(opts.Root)
		if err != nil {
			return nil, "", fmt.Errorf("autostart: resolve %s: %w", opts.Root, err)
		}
		repoRoot = abs
	}
	wtName := worktree.Name(opts.Root)

	exePath := opts.ExePath
	if exePath == "" {
		exe, err := os.Executable()
		if err != nil || exe == "" {
			exePath = os.Args[0]
		} else {
			exePath = exe
		}
	}
	// A repo may pin its daemon to a fixed address so the review URL is
	// stable enough to bookmark. Auto-start is the path that would
	// otherwise always hand out a fresh kernel-assigned port, since it
	// passes --http explicitly and the spawned daemon therefore never
	// consults the overlay itself.
	httpAddr := opts.HTTPAddr
	pinned := false
	if httpAddr == "" {
		configured, cErr := overlay.ServeHTTPAddr(repoRoot)
		if cErr != nil {
			return nil, "", fmt.Errorf("autostart: %w", cErr)
		}
		httpAddr = configured
		pinned = configured != ""
	}
	if httpAddr == "" {
		httpAddr = "127.0.0.1:0"
	}
	waitTimeout := opts.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 5 * time.Second
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 50 * time.Millisecond
	}

	// Acquire a repo-level lock so concurrent MCP clients from different
	// worktrees don't both spawn daemons.
	lockDir := filepath.Join(repoRoot, ".arch")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("autostart: create %s: %w", lockDir, err)
	}
	lockPath := filepath.Join(lockDir, "autostart-repo.lock")
	unlock, err := acquireAutoStartLock(lockPath, waitTimeout)
	if err != nil {
		return nil, "", fmt.Errorf("autostart: acquire lock: %w", err)
	}
	defer unlock()

	// Re-check the global registry: another caller may have started a
	// daemon while we were waiting for the lock. Discovery is
	// global-registry-only — the legacy per-worktree serve.json is not
	// consulted here because it leaks across an identical-path container
	// mount and would resurrect a container-local PID/address (see
	// DiscoverRepoDaemon).
	if rec, rerr := ReadGlobalRecord(repoRoot); rerr == nil && rec != nil {
		return rec, wtName, nil
	}

	// Start a multi-worktree daemon at the repo root.
	args := []string{"serve", "--repo", repoRoot, "--http", httpAddr, "--multi"}
	if opts.IdleTimeout > 0 {
		args = append(args, "--idle-timeout", opts.IdleTimeout.String())
	}
	cmd := exec.Command(exePath, args...)
	devNull, err := detachStdio(cmd, opts.Stderr)
	if err != nil {
		return nil, "", err
	}
	defer devNull.Close()
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("autostart: start daemon: %w", err)
	}
	childPID := cmd.Process.Pid
	_ = cmd.Process.Release()

	// Poll global registry for the daemon to register.
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		rec, err := ReadGlobalRecord(repoRoot)
		if err == nil && rec != nil {
			return rec, wtName, nil
		}
		time.Sleep(pollInterval)
	}
	if pinned {
		// The overwhelmingly likely cause: something else already holds
		// the configured port, so the daemon died on bind instead of
		// registering. Say so — the fallback is silence.
		return nil, "", fmt.Errorf(
			"autostart: daemon (pid %d) did not register within %s; it was pinned to %s by serve.http_addr in %s/archai.yaml — is that address already in use?",
			childPID, waitTimeout, httpAddr, repoRoot)
	}
	return nil, "", fmt.Errorf("autostart: daemon (pid %d) did not register within %s", childPID, waitTimeout)
}

// detachStdio points a spawned daemon's stdout (and stderr, unless the
// caller wants it) at /dev/null — a real file, not io.Discard.
//
// This distinction is the whole function. io.Discard is not an *os.File,
// so os/exec hands the child a pipe and copies it in the parent; when the
// parent exits — which is the entire point of a detached daemon, and what
// `archai daemon start` does immediately — the read end closes and the
// daemon's next log line raises SIGPIPE on fd 2, which the Go runtime
// turns into process death. The daemon would come up, register, answer
// nothing, and leave a stale record behind.
//
// Returns the file so the caller can close its own copy after Start; the
// child holds a dup of the descriptor.
func detachStdio(cmd *exec.Cmd, stderr io.Writer) (*os.File, error) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("autostart: open %s: %w", os.DevNull, err)
	}
	cmd.Stdin = nil
	cmd.Stdout = devNull
	if stderr != nil {
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = devNull
	}
	return devNull, nil
}
