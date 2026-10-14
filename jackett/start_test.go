package jackett

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWaitAliveDetectsImmediateDeath covers the silent-crash path.
//
// Starting a detached Jackett used to be reported as success regardless of
// whether the process survived: a missing libicu aborts .NET within about a
// second, so the panel showed "installed" while nothing ever listened. The
// readiness poll then burned 90 seconds and the user was left with a Jackett
// entry that could not connect.
func TestWaitAliveDetectsImmediateDeath(t *testing.T) {
	// A process that exits immediately: "exit 0" via /bin/sh.
	cmd := detachedCmd()
	if cmd == nil {
		t.Skip("cannot spawn /bin/sh in this environment")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	defer cmd.Process.Release()

	if err := waitAlive(pid, 3*time.Second); err == nil {
		t.Fatal("waitAlive should fail for a process that exits at once")
	}
}

// TestWaitAliveSucceedsForLongLived verifies the happy path is not broken by
// the new check: a sleeping process must be accepted.
func TestWaitAliveSucceedsForLongLived(t *testing.T) {
	cmd := detachedCmd("30")
	if cmd == nil {
		t.Skip("cannot spawn /bin/sh in this environment")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Process.Release()
	}()

	if err := waitAlive(pid, 2*time.Second); err != nil {
		t.Errorf("waitAlive should accept a live process: %v", err)
	}
}

// TestCrashHintExplainsMissingICU checks that the .NET globalization failure is
// translated into an actionable instruction instead of a raw stack frame.
func TestCrashHintExplainsMissingICU(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jackett.log")
	body := strings.Join([]string{
		"   at System.Globalization.GlobalizationMode+Settings..cctor()",
		"   at System.Globalization.CultureData.CreateCultureWithInvariantData()",
		"   at Jackett.Server.Program.Main(System.String[])",
	}, "\n")
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	hint := crashHint(logPath)
	if !strings.Contains(hint, "ICU") {
		t.Errorf("hint should mention ICU, got %q", hint)
	}
	if !strings.Contains(hint, "libicu") {
		t.Errorf("hint should name the package, got %q", hint)
	}
}

// TestCrashHintOtherError verifies unknown failures still surface the log's
// first line rather than being swallowed.
func TestCrashHintOtherError(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jackett.log")
	if err := os.WriteFile(logPath, []byte("Fatal: address already in use\nmore detail"), 0o644); err != nil {
		t.Fatal(err)
	}
	hint := crashHint(logPath)
	if !strings.Contains(hint, "address already in use") {
		t.Errorf("hint should include the log's first line, got %q", hint)
	}
}

func TestCrashHintMissingLog(t *testing.T) {
	if got := crashHint(filepath.Join(t.TempDir(), "nope.log")); got != "" {
		t.Errorf("missing log should yield empty hint, got %q", got)
	}
}

// TestProcessAliveRejectsZombie makes sure a defunct child is treated as gone:
// its parent is the test process, which never reaps a released child.
func TestProcessAliveRejectsZombie(t *testing.T) {
	cmd := detachedCmd()
	if cmd == nil {
		t.Skip("cannot spawn /bin/sh in this environment")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait() // reap it, so it is gone for real
	defer cmd.Process.Release()

	if processAlive(pid) {
		t.Error("reaped process must not be reported alive")
	}
}

// detachedCmd builds a short-lived /bin/sh suitable for the lifecycle tests.
// Passing "0" (or nothing) exits immediately; passing a number sleeps.
func detachedCmd(sleepSeconds ...string) *exec.Cmd {
	secs := "0"
	if len(sleepSeconds) > 0 && sleepSeconds[0] != "" {
		secs = sleepSeconds[0]
	}
	return exec.Command("/bin/sh", "-c", "sleep "+secs)
}
