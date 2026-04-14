//go:build !windows

package launcher

import (
	"os"
	"syscall"
)

// syscallZero returns the signal used by IsPIDAlive. On Unix, signal 0 performs the
// permission/existence check without actually delivering a signal — exactly what we want.
func syscallZero() os.Signal {
	return syscall.Signal(0)
}
