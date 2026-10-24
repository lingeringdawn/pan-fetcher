package server

import (
	"os"
	"strings"
	"testing"
)

// TestDoRestartDelegatesUnderSystemd pins the fix for a restart loop.
//
// The self-update used to always spawn a detached child and exit. Under systemd
// that child and the process the service manager starts after the exit both try
// to bind the same port; with KillMode=process systemd does not clean up the
// unit's processes, so neither is stopped and the unit restarts forever with
// "address already in use".
//
// The unit therefore has to be able to hand the restart to systemd, which is
// what runningUnderSystemd gates.
func TestDoRestartDelegatesUnderSystemd(t *testing.T) {
	src := readSource(t, "server.go")

	i := strings.Index(src, "func doRestart()")
	if i < 0 {
		t.Fatal("doRestart not found")
	}
	end := strings.Index(src[i:], "\n}\n")
	if end < 0 {
		end = len(src) - i
	}
	body := src[i : i+end]

	if !strings.Contains(body, "runningUnderSystemd()") {
		t.Error("doRestart must consult runningUnderSystemd before spawning a detached child")
	}
	// The delegation has to happen before the spawn, not after.
	guard := strings.Index(body, "runningUnderSystemd()")
	spawn := strings.Index(body, "cmd.Start()")
	if guard < 0 || spawn < 0 {
		t.Fatalf("expected both the guard (%d) and the spawn (%d) in doRestart", guard, spawn)
	}
	if guard > spawn {
		t.Error("the systemd guard must run before the detached spawn")
	}
	// Exiting non-zero is what makes systemd treat it as a restart rather than a
	// clean completion, which would not honour Restart=always the same way.
	if !strings.Contains(body[guard:spawn], "os.Exit(1)") {
		t.Error("the systemd path should exit non-zero so systemd restarts the unit")
	}
}

// TestRunningUnderSystemdDetectsUnit verifies the detection logic reads the
// signals systemd actually leaves behind.
func TestRunningUnderSystemdDetectsUnit(t *testing.T) {
	// INVOCATION_ID is the cheapest reliable signal.
	t.Setenv("INVOCATION_ID", "abc123")
	if !runningUnderSystemd() {
		t.Error("INVOCATION_ID should be recognised as systemd")
	}

	// Without it the cgroup path decides; in this test process the cgroup is not
	// a .service, so the answer must be "not systemd" rather than a panic.
	t.Setenv("INVOCATION_ID", "")
	// Read the real file so the assertion matches the environment the test runs
	// in instead of assuming one.
	data, err := os.ReadFile("/proc/self/cgroup")
	want := err == nil && strings.Contains(string(data), ".service")
	if got := runningUnderSystemd(); got != want {
		t.Errorf("runningUnderSystemd() = %v, want %v (cgroup says %q)", got, want, strings.TrimSpace(string(data)))
	}
}

// TestRestartHandlersShareOnePath verifies there is a single restart
// implementation.
//
// handleRestart used to duplicate doRestart's spawn logic. Two copies drift: the
// systemd fix has to be applied to both, and missing one reintroduces the loop.
func TestRestartHandlersShareOnePath(t *testing.T) {
	src := readSource(t, "server.go")

	// Only doRestart may spawn a replacement process.
	if n := strings.Count(src, "cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}"); n != 1 {
		t.Errorf("expected exactly 1 detached spawn site, found %d", n)
	}

	i := strings.Index(src, "func (s *Server) handleRestart")
	if i < 0 {
		t.Fatal("handleRestart not found")
	}
	end := strings.Index(src[i:], "\n}\n")
	if end < 0 {
		end = len(src) - i
	}
	body := src[i : i+end]
	if !strings.Contains(body, "doRestart()") {
		t.Error("handleRestart must call doRestart rather than spawning on its own")
	}
}
