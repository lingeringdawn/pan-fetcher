package server

import (
	"strings"
	"testing"
)

// TestManagedJackettStartsOnBoot pins the recovery strategy.
//
// Jackett shares pan-fetcher's cgroup, so a restart stops it. Escaping the
// cgroup from inside the unit is not reliable across systemd versions, so the
// accepted trade-off is that Jackett dies with the service and is started again
// on boot. Without this, every self-update left the panel pointing at a dead
// instance until someone pressed Start by hand.
func TestManagedJackettStartsOnBoot(t *testing.T) {
	src := readSource(t, "server.go")

	// The auto-start must be wired into the startup discovery path.
	i := strings.Index(src, "func (s *Server) AutoDiscoverJackett()")
	if i < 0 {
		t.Fatal("AutoDiscoverJackett not found")
	}
	end := strings.Index(src[i:], "\n}\n")
	if end < 0 {
		end = len(src) - i
	}
	body := src[i : i+end]
	if !strings.Contains(body, "startManagedJackettOnBoot()") {
		t.Error("AutoDiscoverJackett should start the managed Jackett when it is not running")
	}
	// It must only act when Jackett is actually down.
	if !strings.Contains(body, "d.Running") {
		t.Error("the auto-start should be conditional on Jackett not already running")
	}
}

// TestAutoStartRespectsExternalMode verifies an operator-managed instance is
// never started by us.
//
// External mode means the instance belongs to the operator; starting one here
// would be wrong even when a local install happens to exist.
func TestAutoStartRespectsExternalMode(t *testing.T) {
	src := readSource(t, "server.go")

	i := strings.Index(src, "func (s *Server) startManagedJackettOnBoot()")
	if i < 0 {
		t.Fatal("startManagedJackettOnBoot not found")
	}
	end := strings.Index(src[i:], "\n}\n")
	if end < 0 {
		end = len(src) - i
	}
	body := src[i : i+end]

	if !strings.Contains(body, "JackettModeManaged") {
		t.Error("the auto-start must be gated on managed mode")
	}
	// The mode check has to come before the start, otherwise external mode would
	// be started anyway.
	mode := strings.Index(body, "JackettModeManaged")
	start := strings.Index(body, "jackett.Start(")
	if mode < 0 || start < 0 {
		t.Fatalf("expected both the mode gate (%d) and the start call (%d)", mode, start)
	}
	if mode > start {
		t.Error("the managed-mode check must run before starting Jackett")
	}
}

// TestNoCgroupEscapeAttempt verifies the abandoned approach is gone.
//
// Attaching a running process to a transient scope needs `systemd-run --pid`,
// which the systemd shipped on Debian 13 does not have; the attempt silently
// failed and Jackett was still killed. Keeping dead code around would suggest it
// works.
func TestNoCgroupEscapeAttempt(t *testing.T) {
	for _, name := range []string{"server.go", "../jackett/install.go"} {
		src := readSource(t, name)
		for _, stale := range []string{"detachToOwnScope", "wantsScopeDetach", "jackett-detached"} {
			if strings.Contains(src, stale) {
				t.Errorf("%s still references %q, but the approach does not work on the target systemd", name, stale)
			}
		}
	}
}

// TestUnitHasNoKillModeOverride verifies the unit leaves KillMode alone.
//
// Every override tried here broke something:
//   - process: left pan-fetcher's own replacement process alive, so a
//     self-update restart produced two servers racing for the port and the unit
//     looped on "address already in use".
//   - mixed / Delegate=yes: swept the cgroup anyway and killed Jackett, so it
//     bought nothing while adding a setting to explain.
//
// The default (control-group) stops everything and pan-fetcher starts Jackett
// again on boot, which is the behaviour that was verified in production before
// any override existed.
//
// Only active directives are inspected: the comments explain what was tried and
// why it was removed, and naming those settings there is deliberate.
func TestUnitHasNoKillModeOverride(t *testing.T) {
	for _, line := range strings.Split(readSource(t, "../scripts/install-release.sh"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "KillMode=") {
			t.Errorf("the unit should not override KillMode; the default is what makes a restart clean: %s", trimmed)
		}
		if strings.HasPrefix(trimmed, "Delegate=yes") {
			t.Errorf("Delegate=yes swept the cgroup and killed Jackett anyway: %s", trimmed)
		}
	}
}
