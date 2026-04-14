//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
)

// detachProcess places the spawned daemon in its own session so it survives the
// launcher exiting (which happens immediately after this client call returns).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
