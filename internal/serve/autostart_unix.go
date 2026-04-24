//go:build !windows

package serve

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// detachProcess configures the child to run in a new session/process
// group so it survives when the parent exits. Without Setsid the child
// would share the parent's controlling terminal and receive SIGHUP
// when the MCP client closes its stdio.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// acquireAutoStartLock takes an exclusive advisory lock on lockPath
// using flock(2). It retries for up to waitTimeout before failing so
// callers never block forever when another process has crashed while
// holding the lock (stale locks are released automatically by the
// kernel on fd close/process exit, but a ctrl-C'd shell may leave
// one for a moment). Returns an unlock func that closes the file.
func acquireAutoStartLock(lockPath string, waitTimeout time.Duration) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", lockPath, err)
	}

	deadline := time.Now().Add(waitTimeout)
	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		} else if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = f.Close()
			return nil, fmt.Errorf("flock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("flock %s: timed out after %s", lockPath, waitTimeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
