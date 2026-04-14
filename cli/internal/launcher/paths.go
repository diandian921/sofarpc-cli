// Package launcher discovers, probes, and (when needed) spawns the daemon. It is the
// single place that touches state.json and daemon.lock.
package launcher

import (
	"os"
	"path/filepath"
)

// Paths bundles the on-disk locations the launcher manages.
type Paths struct {
	BaseDir   string
	StateFile string
	LockFile  string
	LogFile   string
}

// DefaultPaths resolves to ~/.sofarpc/daemon/.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	base := filepath.Join(home, ".sofarpc", "daemon")
	return Paths{
		BaseDir:   base,
		StateFile: filepath.Join(base, "state.json"),
		LockFile:  filepath.Join(base, "daemon.lock"),
		LogFile:   filepath.Join(base, "daemon.log"),
	}, nil
}

// EnsureBaseDir creates the base directory if missing.
func (p Paths) EnsureBaseDir() error {
	return os.MkdirAll(p.BaseDir, 0o755)
}
