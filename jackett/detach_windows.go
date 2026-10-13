//go:build windows

package jackett

import (
	"os/exec"
	"syscall"
)

// detachProcess configures cmd so the spawned Jackett process survives the
// parent exiting and does not pop up a console window.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW
		CreationFlags: 0x00000008 | 0x00000200 | 0x08000000,
	}
}
