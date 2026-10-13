//go:build !windows

package jackett

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the spawned Jackett process into its own session so it is
// not terminated when pan-fetcher exits or its terminal closes.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Setsid:  true,
	}
}
