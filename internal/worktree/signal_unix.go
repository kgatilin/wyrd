//go:build !windows

package worktree

import "syscall"

// syscallZero is the "probe" signal used by PIDAlive. Sending signal 0
// does not deliver anything but returns the same errors a real signal
// would (ESRCH for a non-existent pid, EPERM for a pid owned by
// another user). On Windows this file is replaced by a stub.
var syscallZero syscall.Signal = 0
