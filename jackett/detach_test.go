package jackett

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
)

// TestDetachedSysProcAttrIsValid guards the child-process detachment flags.
//
// Setsid and Setpgid are mutually exclusive: setsid(2) makes the child a session
// leader, and setpgid(2) then refuses it with EPERM. Combining them makes every
// launch fail at fork/exec with "operation not permitted" — which is exactly
// what happened to Start, and is invisible to a cross-compile check because it
// only fails at runtime.
//
// This test actually spawns a process, so it fails loudly if the flags regress.
func TestDetachedSysProcAttrIsValid(t *testing.T) {
	cmd := exec.Command("/bin/true")
	cmd.SysProcAttr = detachedSysProcAttr()
	if err := cmd.Start(); err != nil {
		t.Fatalf("detached SysProcAttr rejected by the kernel: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("detached child did not run: %v", err)
	}
}

// TestDetachedSysProcAttrCreatesOwnSession verifies the flag actually detaches:
// the child must not share our process group.
func TestDetachedSysProcAttrCreatesOwnSession(t *testing.T) {
	// /bin/sh reports its own pid, pgid and session id.
	cmd := exec.Command("/bin/sh", "-c", "ps -o pid=,pgid=,sid= -p $$")
	cmd.SysProcAttr = detachedSysProcAttr()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("spawn detached shell: %v", err)
	}

	var pid, pgid, sid int
	if _, err := fmt.Sscan(string(out), &pid, &pgid, &sid); err != nil {
		t.Fatalf("parse ps output %q: %v", string(out), err)
	}
	if sid != pid {
		t.Errorf("child is not a session leader: pid=%d sid=%d", pid, sid)
	}
	if pgid != pid {
		t.Errorf("child is not in its own process group: pid=%d pgid=%d", pid, pgid)
	}
	if pgid == syscall.Getpgrp() {
		t.Errorf("child shares our process group (%d); it was not detached", pgid)
	}
}
