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
	TokenFile string
}

// DefaultPaths resolves to the MCP-first engine runtime layout under ~/.sofarpc/.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(home, ".sofarpc")
	state := filepath.Join(root, "state")
	return Paths{
		BaseDir:   state,
		StateFile: filepath.Join(state, "engine.json"),
		LockFile:  filepath.Join(state, "engine.lock"),
		LogFile:   filepath.Join(root, "logs", "engine.log"),
		TokenFile: filepath.Join(state, "token"),
	}, nil
}

// EnsureBaseDir creates the state and log directories if missing.
func (p Paths) EnsureBaseDir() error {
	if err := os.MkdirAll(p.BaseDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(p.LogFile), 0o755)
}
