package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

func TestStopDaemon_SignalsNamedDaemon(t *testing.T) {
	root := t.TempDir()
	const name = "feature"
	const pid = 4242
	if err := worktree.WriteServe(root, name, worktree.ServeRecord{
		PID:       pid,
		HTTPAddr:  "127.0.0.1:1234",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteServe: %v", err)
	}

	origAlive := daemonPIDAlive
	origSignal := daemonSignal
	defer func() {
		daemonPIDAlive = origAlive
		daemonSignal = origSignal
	}()

	alive := true
	daemonPIDAlive = func(got int) bool {
		if got != pid {
			t.Fatalf("PIDAlive pid = %d, want %d", got, pid)
		}
		return alive
	}
	signaled := false
	daemonSignal = func(got int) error {
		if got != pid {
			t.Fatalf("signal pid = %d, want %d", got, pid)
		}
		signaled = true
		alive = false
		return nil
	}

	t.Chdir(root)
	cmd := newStopDaemonCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runStopDaemon(cmd, []string{name}); err != nil {
		t.Fatalf("runStopDaemon: %v", err)
	}
	if !signaled {
		t.Fatal("daemon was not signaled")
	}
	if rec, err := worktree.ReadServe(root, name); err != nil {
		t.Fatalf("ReadServe: %v", err)
	} else if rec != nil {
		t.Fatalf("serve.json still present after stop: %+v", *rec)
	}
	if !strings.Contains(out.String(), `Stopped daemon "feature"`) {
		t.Fatalf("output = %q, want stopped message", out.String())
	}
}

func TestStopDaemon_RemovesStaleRecord(t *testing.T) {
	root := t.TempDir()
	const name = "stale"
	if err := worktree.WriteServe(root, name, worktree.ServeRecord{
		PID:       0x7ffffffe,
		HTTPAddr:  "127.0.0.1:1234",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteServe: %v", err)
	}

	origAlive := daemonPIDAlive
	defer func() { daemonPIDAlive = origAlive }()
	daemonPIDAlive = func(int) bool { return false }

	t.Chdir(root)
	cmd := newStopDaemonCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runStopDaemon(cmd, []string{name}); err != nil {
		t.Fatalf("runStopDaemon: %v", err)
	}
	if rec, err := worktree.ReadServe(root, name); err != nil {
		t.Fatalf("ReadServe: %v", err)
	} else if rec != nil {
		t.Fatalf("serve.json still present after stale cleanup: %+v", *rec)
	}
	if !strings.Contains(out.String(), "Removed stale daemon record") {
		t.Fatalf("output = %q, want stale cleanup message", out.String())
	}
}

func TestStopDaemon_NoRecordErrors(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	cmd := newStopDaemonCmd()
	err := runStopDaemon(cmd, []string{"missing"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `no daemon running in worktree "missing"`) {
		t.Fatalf("error = %q", err)
	}
}
