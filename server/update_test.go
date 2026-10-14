package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lingeringdawn/pan-fetcher/notify"
)

// TestCompareVersion pins the ordering rules, including pre-release suffixes.
//
// The original implementation parsed each dot-separated segment with
// strconv.Atoi and ignored the error, so "1-rc1" became 0 and
// compareVersion("v2.0.1-rc1", "v2.0.0") returned 0 — a pre-release was
// reported as equal to an older release and the update was hidden.
func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.0.1", "v2.0.0", 1},
		{"v2.0.0", "v2.0.1", -1},
		{"v2.0.0", "v2.0.0", 0},
		{"v2.1.0", "v2.0.9", 1},
		{"v1.4.1", "v2.0.0", -1},
		{"v10.0.0", "v9.0.0", 1},

		// Pre-releases sort below their own release, but above the previous one.
		{"v2.0.1-rc1", "v2.0.0", 1},
		{"v2.0.1", "v2.0.1-rc1", 1},
		{"v2.0.1-rc2", "v2.0.1-rc1", 1},
		{"v2.0.1-beta", "v2.0.1-alpha", 1},
		{"v2.0.1-rc1", "v2.0.1-rc1", 0},

		// Version is "" until ldflags inject it, which must not break ordering.
		{"v2.0.1", "", 1},
		{"", "v2.0.1", -1},
		{"", "", 0},

		// Unparsable input must not silently masquerade as a valid version.
		{"v2.0.1", "garbage", 1},
		{"garbage", "garbage", 0},
	}
	for _, c := range cases {
		got := compareVersion(c.a, c.b)
		gotSign := 0
		if got > 0 {
			gotSign = 1
		} else if got < 0 {
			gotSign = -1
		}
		if gotSign != c.want {
			t.Errorf("compareVersion(%q, %q) = %d (sign %d), want sign %d",
				c.a, c.b, got, gotSign, c.want)
		}
	}
}

// TestUpdateWriteErrorPermission checks that a permission failure becomes an
// actionable message instead of a raw os error string.
func TestUpdateWriteErrorPermission(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "pan-fetcher")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	msg := updateWriteError("zh", target, os.ErrPermission)
	if msg == "" {
		t.Fatal("empty message")
	}
	// Must name the directory so the user knows where the problem is.
	if !strings.Contains(msg, dir) {
		t.Errorf("message should mention the directory %q: %q", dir, msg)
	}
	// Must not leak the raw os error text.
	if strings.Contains(msg, "permission denied") {
		t.Errorf("message should not dump the raw error: %q", msg)
	}
}

// TestUpdateWriteErrorOther verifies non-permission errors still surface the
// underlying cause, which is the only useful detail for those cases.
func TestUpdateWriteErrorOther(t *testing.T) {
	sentinel := os.ErrNotExist
	msg := updateWriteError("zh", "/opt/pan-fetcher/bin/pan-fetcher", sentinel)
	if !strings.Contains(msg, sentinel.Error()) {
		t.Errorf("non-permission error should be included: %q", msg)
	}
}

// TestResolveDataFileIgnoresWorkingDir is the regression test for the lost
// Jackett configuration after a self-update.
//
// Self-update restarts the server with its working directory set to the
// binary's folder (doRestart sets cmd.Dir). Every runtime file used to be a
// bare relative name, so afterwards they resolved against the new directory:
// web-settings.json was not found and the Jackett URL and API key came back
// empty, which looked like the connection had broken.
//
// The deeper cause was the working directory switch itself: with cwd moved to
// the binary folder, config.toml was no longer found, so the database path fell
// back to a default beside the binary and the data directory moved with it.
// doRestart now keeps the original working directory, and runtime files are
// additionally anchored to the data directory so a cwd change cannot silently
// relocate them again.
func TestResolveDataFileIgnoresWorkingDir(t *testing.T) {
	stateDir := t.TempDir()
	exeDir := t.TempDir()

	// Simulate the post-restart working directory: somewhere unrelated to the
	// data directory.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(exeDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	savedDataDir := dataDir
	dataDir = stateDir
	t.Cleanup(func() { dataDir = savedDataDir })

	got := resolveDataFile("web-settings.json")
	want := filepath.Join(stateDir, "web-settings.json")
	if got != want {
		t.Errorf("resolveDataFile(web-settings.json) = %q, want %q", got, want)
	}

	// The resolved path must actually be writable at the data directory, i.e.
	// the file is not silently redirected into the working directory.
	if err := os.WriteFile(got, []byte("{}"), 0o644); err != nil {
		t.Fatalf("cannot write resolved path %q: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(exeDir, "web-settings.json")); !os.IsNotExist(err) {
		t.Errorf("file should not appear in the working directory %q", exeDir)
	}
}

// TestResolveDataFileKeepsAbsolute verifies an explicitly configured path is
// left alone, so operators can still point state files elsewhere.
func TestResolveDataFileKeepsAbsolute(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "web-settings.json")
	if got := resolveDataFile(abs); got != abs {
		t.Errorf("resolveDataFile(%q) = %q, want unchanged", abs, got)
	}
}

// TestNotifySetDataDir covers the same anchor for the notify package, which has
// no Server receiver and therefore takes the directory through a setter.
func TestNotifySetDataDir(t *testing.T) {
	dir := t.TempDir()
	notify.SetDataDir(dir)

	// RecordItems writes through to the anchored path; a nil-free call proves
	// the file is addressed inside dir rather than the working directory.
	notify.RecordItems("sub", []string{"item"})

	if _, err := os.Stat(filepath.Join(dir, "recent-items.json")); err != nil {
		t.Fatalf("recent-items.json not written under data dir: %v", err)
	}
}
