//go:build !windows

package launcher

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// FileLock wraps an advisory flock on a regular file. Used as the spawn election:
// only the launcher that grabs the lock spawns the daemon; the rest poll state.json.
type FileLock struct {
	path string
	file *os.File
}

// TryLock attempts a non-blocking exclusive lock. Returns (lock, true) if acquired,
// (nil, false) if another process holds it.
func TryLock(path string) (*FileLock, bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("flock %s: %w", path, err)
	}
	return &FileLock{path: path, file: f}, true, nil
}

// Release drops the lock. Always safe to call.
func (l *FileLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}
