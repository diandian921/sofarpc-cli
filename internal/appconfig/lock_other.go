//go:build !unix && !windows

package appconfig

import (
	"os"
	"path/filepath"
)

// lockConfig on platforms that are neither unix nor windows is a DEGRADED no-op:
// it opens (and later closes) the lock file but acquires no OS-level lock, so
// concurrent Update calls across processes are NOT mutually excluded here. This
// is an accepted fallback for exotic GOOS targets; the real MCP/CLI hosts run on
// unix or windows, which have true flock/LockFileEx implementations.
func lockConfig(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	return func() {
		_ = f.Close()
	}, nil
}
