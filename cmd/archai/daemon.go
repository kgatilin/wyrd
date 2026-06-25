package main

import (
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/worktree"
	"github.com/spf13/cobra"
)

// newDaemonCmd builds the `archai daemon` command group for managing serve
// daemons via the global registry (~/.arch/daemons): the repo-level daemons
// that MCP and UI clients auto-start.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage archai serve daemons",
		Long: `Inspect and control archai serve daemons recorded in the global
registry (~/.arch/daemons) — the repo-level daemons that MCP and UI clients
auto-start. Subcommands: list, stop, restart.`,
	}
	cmd.AddCommand(newDaemonStartCmd(), newDaemonListCmd(), newDaemonStatusCmd(), newDaemonStopCmd(), newDaemonRestartCmd())
	return cmd
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [name|pid]",
		Short: "Show index readiness/progress for a daemon (default: current repo's)",
		Long: `Query a daemon for its model and retrieval-index readiness: whether it
is still indexing, dense-embedding progress (embedded/embeddable), and whether
embedding-backed lenses are ready. On a large repo the dense pass runs for a
while after startup or a refresh — use this to tell "still indexing" from
"ready". Default target is the current repo's daemon; pass [name|pid] for
another.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDaemonStatus,
	}
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	rec, err := resolveDaemonTarget(arg)
	if err != nil {
		return err
	}

	// Ask the daemon's MCP `status` tool over the same HTTP tools/call endpoint
	// the MCP thin client uses, so the CLI and MCP report identical readiness.
	// A multi daemon serves only under /w/<worktree>/ — pick the worktree
	// matching cwd, falling back to the first the daemon knows.
	prefix := ""
	if rec.HasCap("multi") {
		prefix = "/w/" + daemonWorktreeName(rec)
	}
	url := "http://" + rec.HTTPAddr + prefix + "/api/mcp/tools/call"
	httpReq, err := nethttp.NewRequest(nethttp.MethodPost, url, strings.NewReader(`{"name":"status","arguments":{}}`))
	if err != nil {
		return fmt.Errorf("build status request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &nethttp.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("querying daemon for %s at %s: %w", filepath.Base(rec.RepoRoot), rec.HTTPAddr, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var tr struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &tr); err != nil || len(tr.Content) == 0 {
		return fmt.Errorf("unexpected status response: %s", strings.TrimSpace(string(data)))
	}
	// The tool's text payload is the statusResult JSON; pretty-print it.
	out := tr.Content[0].Text
	var pretty map[string]any
	if json.Unmarshal([]byte(out), &pretty) == nil {
		if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			out = string(b)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s  (pid %d)\n%s\n", filepath.Base(rec.RepoRoot), rec.PID, out)
	return nil
}

// daemonWorktreeName picks the worktree to scope a multi daemon request to:
// the one matching cwd if the daemon serves it, otherwise the first worktree
// the daemon knows (a stable, valid route for cross-repo targeting).
func daemonWorktreeName(rec *serve.DaemonRecord) string {
	if cwd, err := os.Getwd(); err == nil {
		name := worktree.Name(cwd)
		for _, w := range rec.Worktrees {
			if w == name {
				return name
			}
		}
	}
	if len(rec.Worktrees) > 0 {
		return rec.Worktrees[0]
	}
	return ""
}

func newDaemonStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a repo-level daemon for the current repository",
		Long: `Start a multi-worktree daemon for the repository containing the
current directory. The repo root is resolved from cwd, so this works from any
worktree — the daemon is started at the repo root and serves every worktree.
Idempotent: if a daemon is already serving this repo, it is printed instead of
starting a second one.`,
		Args: cobra.NoArgs,
		RunE: runDaemonStart,
	}
	cmd.Flags().Duration("idle-timeout", 15*time.Minute, "Idle timeout for the daemon (0 = never exit)")
	return cmd
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	idle, _ := cmd.Flags().GetDuration("idle-timeout")
	rec, _, err := serve.AutoStartRepoDaemon(serve.AutoStartOptions{
		Root:        cwd,
		IdleTimeout: idle,
	})
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Daemon for %s: http://%s (pid %d).\n",
		filepath.Base(rec.RepoRoot), rec.HTTPAddr, rec.PID)
	return nil
}

func newDaemonListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List live daemons",
		Args:  cobra.NoArgs,
		RunE:  runListDaemons,
	}
}

func newDaemonStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop [name|pid]",
		Short: "Stop a daemon (default: current repo's)",
		Long: `Stop a daemon from the global registry.

The target is the daemon serving the current repository by default, or the one
named by [name|pid]: a numeric argument matches by PID, anything else matches a
repo-root basename (e.g. "archai") as shown by ` + "`archai daemon list`" + `.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDaemonStop,
	}
	cmd.Flags().Duration("timeout", 20*time.Second, "How long to wait for graceful shutdown after SIGTERM before giving up")
	cmd.Flags().Bool("force", false, "Escalate to SIGKILL if the daemon does not exit within timeout")
	return cmd
}

func newDaemonRestartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart [name|pid]",
		Short: "Restart a daemon, picking up the current binary (default: current repo's)",
		Long: `Stop the target daemon and start a fresh repo-level daemon in its place.

Use this after installing a new archai binary so the running daemon picks up
the new code (the replacement is launched from the archai binary now on PATH).
The target defaults to the current repo's daemon; pass [name|pid] to target
another, where a numeric argument is a PID and anything else is a repo-root
basename.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDaemonRestart,
	}
	cmd.Flags().Duration("timeout", 20*time.Second, "How long to wait for the old daemon to exit before escalating to SIGKILL")
	cmd.Flags().Duration("idle-timeout", 15*time.Minute, "Idle timeout for the restarted daemon (0 = never exit)")
	return cmd
}

// resolveDaemonTarget finds the daemon a [name|pid] argument refers to. Empty
// arg → the daemon serving the current repo (via the global registry keyed on
// cwd). A numeric arg → match by PID. Otherwise → match by repo-root basename
// across the registry, erroring on an ambiguous basename with the PIDs to
// disambiguate.
func resolveDaemonTarget(arg string) (*serve.DaemonRecord, error) {
	if arg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolving cwd: %w", err)
		}
		rec, repoRoot, err := serve.DiscoverGlobalDaemon(cwd)
		if err != nil {
			return nil, err
		}
		if rec == nil {
			return nil, fmt.Errorf("no daemon running for repo %s (start one with `archai serve --repo .`)", repoRoot)
		}
		return rec, nil
	}

	daemons, err := serve.ListGlobalDaemons()
	if err != nil {
		return nil, fmt.Errorf("reading global registry: %w", err)
	}

	if pid, perr := strconv.Atoi(arg); perr == nil {
		for _, d := range daemons {
			if d.Record.PID == pid {
				rec := d.Record
				return &rec, nil
			}
		}
		return nil, fmt.Errorf("no daemon with pid %d (see `archai daemon list`)", pid)
	}

	var matches []serve.DaemonRecord
	for _, d := range daemons {
		if filepath.Base(d.Record.RepoRoot) == arg {
			matches = append(matches, d.Record)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no daemon for repo named %q (see `archai daemon list`)", arg)
	case 1:
		rec := matches[0]
		return &rec, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q is ambiguous — %d daemons share that name; target by PID:", arg, len(matches))
		for _, m := range matches {
			fmt.Fprintf(&b, "\n  pid %d  %s", m.PID, m.RepoRoot)
		}
		return nil, fmt.Errorf("%s", b.String())
	}
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	rec, err := resolveDaemonTarget(arg)
	if err != nil {
		return err
	}
	timeout, _ := cmd.Flags().GetDuration("timeout")
	force, _ := cmd.Flags().GetBool("force")
	return stopDaemonRecord(cmd, rec, timeout, force)
}

// stopDaemonRecord SIGTERMs the daemon's PID, waits up to timeout for it to
// stop running, and clears its global record. A multi-worktree daemon can take
// ~10s+ to shut down gracefully (draining the HTTP server, cancelling in-flight
// indexing/embedding, closing watchers), so the timeout is generous. When force
// is set and graceful shutdown overruns, it escalates to SIGKILL rather than
// failing — callers that must end in a known state (restart) pass force.
//
// Liveness is zombie-aware (see pidRunning): a process that has exited but not
// yet been reaped by its parent — the case for daemons auto-started by a still
// running MCP thin client — counts as stopped, so a clean SIGTERM shutdown is
// not mistaken for a hang.
//
// A daemon that exits gracefully removes its own global record; the explicit
// RemoveGlobalRecord is a defensive cleanup for the SIGKILL path (whose deferred
// cleanup never runs) and for stale records.
func stopDaemonRecord(cmd *cobra.Command, rec *serve.DaemonRecord, timeout time.Duration, force bool) error {
	name := filepath.Base(rec.RepoRoot)
	if !pidRunning(rec.PID) {
		_ = serve.RemoveGlobalRecord(rec.RepoRoot)
		fmt.Fprintf(cmd.OutOrStdout(), "Removed stale record for %s (pid %d not running).\n", name, rec.PID)
		return nil
	}
	if err := daemonSignal(rec.PID); err != nil {
		return fmt.Errorf("stop daemon for %s (pid %d): %w", name, rec.PID, err)
	}
	if timeout > 0 && waitForPIDStop(rec.PID, timeout) {
		_ = serve.RemoveGlobalRecord(rec.RepoRoot)
		fmt.Fprintf(cmd.OutOrStdout(), "Stopped daemon for %s (pid %d).\n", name, rec.PID)
		return nil
	}
	// Still running after the graceful window.
	if !force {
		return fmt.Errorf("sent SIGTERM to %s (pid %d), but it is still running after %s (use --force to SIGKILL)", name, rec.PID, timeout)
	}
	if err := forceKillPID(rec.PID); err != nil {
		return fmt.Errorf("SIGKILL %s (pid %d): %w", name, rec.PID, err)
	}
	waitForPIDStop(rec.PID, 3*time.Second)
	_ = serve.RemoveGlobalRecord(rec.RepoRoot)
	fmt.Fprintf(cmd.OutOrStdout(), "Force-killed daemon for %s (pid %d).\n", name, rec.PID)
	return nil
}

// waitForPIDStop polls until the pid is no longer running (exited or zombie),
// returning true once stopped and false if still running at the deadline.
func waitForPIDStop(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !pidRunning(pid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// pidRunning reports whether pid is a live, schedulable process — NOT counting
// zombies. daemonPIDAlive (kill -0) returns true for a zombie because the PID
// still exists in the table until the parent reaps it; daemons auto-started by
// a long-lived MCP thin client are never reaped while that client runs, so a
// SIGKILLed or cleanly-exited daemon would otherwise look "alive" forever. A
// `ps` state query distinguishes the zombie ('Z') state from running.
func pidRunning(pid int) bool {
	if !daemonPIDAlive(pid) {
		return false // fully gone (ESRCH)
	}
	out, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false // ps failed or no such process
	}
	st := strings.TrimSpace(string(out))
	return st != "" && !strings.HasPrefix(st, "Z")
}

func forceKillPID(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGKILL)
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	rec, err := resolveDaemonTarget(arg)
	if err != nil {
		return err
	}
	timeout, _ := cmd.Flags().GetDuration("timeout")
	idle, _ := cmd.Flags().GetDuration("idle-timeout")
	repoRoot := rec.RepoRoot
	name := filepath.Base(repoRoot)

	// force=true: a restart must end with the old daemon down and a new one up,
	// so if graceful shutdown overruns we SIGKILL rather than abort and leave
	// the repo with no daemon.
	if err := stopDaemonRecord(cmd, rec, timeout, true); err != nil {
		return err
	}

	newRec, _, err := serve.AutoStartRepoDaemon(serve.AutoStartOptions{
		Root:        repoRoot,
		IdleTimeout: idle,
	})
	if err != nil {
		return fmt.Errorf("restart: starting new daemon for %s: %w", name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Restarted daemon for %s: http://%s (pid %d).\n", name, newRec.HTTPAddr, newRec.PID)
	return nil
}
