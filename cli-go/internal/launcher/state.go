package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// State mirrors the daemon-side state.json schema.
type State struct {
	PID          int    `json:"pid"`
	Port         int    `json:"port"`
	BuildVersion string `json:"buildVersion"`
	StartedAtMS  int64  `json:"startedAtMs"`
	Status       string `json:"status"`
}

// ReadState loads state.json from disk. Returns (nil, nil) if the file is missing,
// (nil, err) on any other failure (corrupt JSON, IO error, ...).
func ReadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state file %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state file %s: %w", path, err)
	}
	return &s, nil
}

// DeleteState removes state.json, ignoring missing-file errors.
func DeleteState(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// IsPIDAlive reports whether the OS still has a process with this pid. On Unix this is
// kill(pid, 0); the launcher uses it to detect stale state.json after a daemon crash.
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscallZero()); err != nil {
		return false
	}
	return true
}
