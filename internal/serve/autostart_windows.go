//go:build windows

package serve

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

// detachProcess is a no-op on Windows — exec.Cmd inherits enough of
// its environment that the daemon survives the parent's exit in
// practice. We don't use DETACHED_PROCESS here because it would cost
// us the stderr pipe used during diagnostics.
func detachProcess(cmd *exec.Cmd) {}

// acquireAutoStartLock takes an exclusive lock on lockPath using
// LockFileEx. Returns an unlock func that releases the lock and
// closes the file. Mirrors the POSIX flock-based implementation in
// autostart_unix.go.
func acquireAutoStartLock(lockPath string, waitTimeout time.Duration) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", lockPath, err)
	}

	handle := windows.Handle(f.Fd())
	// Lock bytes 0..1 of the file; the range is arbitrary but must be
	// the same across callers. LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	// makes LockFileEx non-blocking so we can drive our own timeout loop.
	var overlapped windows.Overlapped
	deadline := time.Now().Add(waitTimeout)
	for {
		err := windows.LockFileEx(
			handle,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &overlapped,
		)
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: timed out after %s: %w", lockPath, waitTimeout, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
