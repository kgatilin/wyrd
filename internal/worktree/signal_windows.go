//go:build windows

package worktree

import "syscall"

// syscallZero is unused on Windows (os.Process.Signal is not
// meaningfully implemented there) — PIDAlive always returns true for
// positive pids to avoid false negatives on an untested platform.
var syscallZero syscall.Signal = 0
