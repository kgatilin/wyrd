package serve

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

// DiscoverDaemon looks up the running daemon record for the worktree
// containing projectRoot. Returns the parsed record (nil when no live
// daemon is registered) and the worktree name used for disk lookups.
// A stale record (process no longer alive) is treated the same as
// "no record" — the caller can then auto-start a fresh daemon.
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

// AutoStartOptions configures how a background daemon is launched by
// AutoStartDaemon.
type AutoStartOptions struct {
	// ExePath is the path to the archai binary. Defaults to os.Args[0]
	// (callers can override for tests).
	ExePath string

	// Root is the project root the daemon should serve. Required.
	Root string

	// HTTPAddr is the listen address passed to `archai serve --http`.
	// Empty falls back to "127.0.0.1:0" so the kernel picks a free
	// port and the auto-started daemon stays on the loopback interface.
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
	cmd := exec.Command(exePath, args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = io.Discard
	}
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
