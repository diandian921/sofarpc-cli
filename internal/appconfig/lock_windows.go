//go:build windows

package appconfig

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const lockfileExclusiveLock = 0x00000002

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
	// A clear message: the raw syscall.Errno(1) would render as the misleading
	// "Incorrect function." Used only when LockFileEx fails without setting an
	// error code (the callErr branch below reports the real OS error otherwise).
	errLockFileExFailed = errors.New("LockFileEx reported failure without an error code")
)

func lockConfig(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped syscall.Overlapped
	r1, _, callErr := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		_ = f.Close()
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, errLockFileExFailed
	}
	return func() {
		_, _, _ = procUnlockFileEx.Call(
			f.Fd(),
			0,
			1,
			0,
			uintptr(unsafe.Pointer(&overlapped)),
		)
		_ = f.Close()
	}, nil
}
