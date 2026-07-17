//go:build unix

package appconfig

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockConfig(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	// flock is not restarted by SA_RESTART, so a signal (SIGCHLD from the host CLI
	// subprocesses this package spawns, SIGURG from the runtime) can interrupt the
	// blocking acquire with EINTR; retry rather than fail the lock spuriously.
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
