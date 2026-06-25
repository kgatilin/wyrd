package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/serve"
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
	cmd.AddCommand(newDaemonListCmd(), newDaemonStopCmd(), newDaemonRestartCmd())
	return cmd
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
	cmd.Flags().Duration("timeout", 5*time.Second, "How long to wait for graceful shutdown after SIGTERM")
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
	cmd.Flags().Duration("timeout", 5*time.Second, "How long to wait for the old daemon to exit")
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
	return stopDaemonRecord(cmd, rec, timeout)
}

// stopDaemonRecord SIGTERMs the daemon's PID, waits for it to exit, and clears
// its global record. A daemon that exits gracefully removes its own record;
// the explicit RemoveGlobalRecord is a defensive cleanup for the case where it
// did not (or was already dead).
func stopDaemonRecord(cmd *cobra.Command, rec *serve.DaemonRecord, timeout time.Duration) error {
	name := filepath.Base(rec.RepoRoot)
	if !daemonPIDAlive(rec.PID) {
		_ = serve.RemoveGlobalRecord(rec.RepoRoot)
		fmt.Fprintf(cmd.OutOrStdout(), "Removed stale record for %s (pid %d not alive).\n", name, rec.PID)
		return nil
	}
	if err := daemonSignal(rec.PID); err != nil {
		return fmt.Errorf("stop daemon for %s (pid %d): %w", name, rec.PID, err)
	}
	if timeout > 0 {
		if err := waitForPIDExit(rec.PID, timeout); err != nil {
			return err
		}
	}
	_ = serve.RemoveGlobalRecord(rec.RepoRoot)
	fmt.Fprintf(cmd.OutOrStdout(), "Stopped daemon for %s (pid %d).\n", name, rec.PID)
	return nil
}

func waitForPIDExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !daemonPIDAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("sent SIGTERM to pid %d, but it is still alive after %s", pid, timeout)
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

	if err := stopDaemonRecord(cmd, rec, timeout); err != nil {
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
