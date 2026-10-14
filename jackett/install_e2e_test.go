package jackett

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests exercise the real download/extract/launch pipeline against the
// actual Jackett release. They are skipped unless JACKETT_E2E=1, because they
// need network access and roughly 55 MB of disk.
//
//	JACKETT_E2E=1 go test ./jackett/ -run TestE2E -v -timeout 20m
func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("JACKETT_E2E") == "" {
		t.Skip("set JACKETT_E2E=1 to run the end-to-end install test")
	}
}

// TestE2EInstallExtractAndLocate is the load-bearing test for the one-click
// install feature: it performs a genuine download and extraction, then asserts
// the executable landed exactly where Start expects to find it.
func TestE2EInstallExtractAndLocate(t *testing.T) {
	requireE2E(t)

	dir := filepath.Join(t.TempDir(), "jackett")
	var steps []string
	res, err := Install(context.Background(), dir, false, func(format string, args ...interface{}) {
		steps = append(steps, fmt.Sprintf(format, args...))
		t.Logf("[install] "+format, args...)
	})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if !res.Installed {
		t.Error("Installed should be true")
	}
	if res.Version == "" {
		t.Error("Version should be reported")
	}
	if len(steps) == 0 {
		t.Error("expected a progress log trail")
	}

	// The executable must sit directly in dir, which is what Start relies on.
	exe := filepath.Join(dir, executableName())
	st, err := os.Stat(exe)
	if err != nil {
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("executable not at %s: %v (dir contains: %v)", exe, err, names)
	}
	if st.IsDir() {
		t.Fatalf("%s is a directory, not a file", exe)
	}
	if st.Size() == 0 {
		t.Errorf("%s is empty", exe)
	}

	// No stray download temp files may survive a successful install.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read install dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "jackett-download-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}

	// Jackett needs its content directory alongside the binary.
	if _, err := os.Stat(filepath.Join(dir, "Content")); err != nil {
		t.Errorf("Content directory missing after extract: %v", err)
	}
}

// TestE2EInstallIsIdempotent verifies a second install reuses the first result
// instead of re-downloading.
func TestE2EInstallIsIdempotent(t *testing.T) {
	requireE2E(t)

	dir := filepath.Join(t.TempDir(), "jackett")
	if _, err := Install(context.Background(), dir, false, nil); err != nil {
		t.Fatalf("first Install failed: %v", err)
	}

	// Replace the binary with a sentinel: a real re-install would overwrite it,
	// while the reuse path must leave it untouched.
	exe := filepath.Join(dir, executableName())
	if err := os.WriteFile(exe, []byte("sentinel"), 0o755); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	res, err := Install(context.Background(), dir, false, nil)
	if err != nil {
		t.Fatalf("second Install failed: %v", err)
	}
	if !res.Installed {
		t.Error("Installed should be true")
	}

	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "sentinel" {
		t.Error("second Install with force=false re-downloaded instead of reusing")
	}
}

// TestE2EStartAndProbe verifies the launched process actually serves HTTP and is
// recognised by the same probe used during discovery.
func TestE2EStartAndProbe(t *testing.T) {
	requireE2E(t)

	dir := filepath.Join(t.TempDir(), "jackett")
	if _, err := Install(context.Background(), dir, false, nil); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Jackett reads its data folder from ~/.config/Jackett, so point HOME at a
	// scratch directory: this keeps the test instance off the developer's real
	// configuration and off the default port.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	writeJackettPort(t, filepath.Join(home, ".config", "Jackett"), freePort(t))
	url := buildBaseURL("127.0.0.1", readPortFrom(t, filepath.Join(home, ".config", "Jackett")))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	actualURL, err := Start(ctx, dir, url, t.Logf)
	if err != nil {
		if data, rerr := os.ReadFile(filepath.Join(dir, "jackett.log")); rerr == nil {
			t.Logf("jackett.log tail:\n%s", tailOf(string(data), 3000))
		}
		t.Fatalf("Start failed: %v", err)
	}
	// Ensure the child is cleaned up even if an assertion fails.
	t.Cleanup(func() { _ = Stop(dir) })
	t.Logf("requested %s, actually listening on %s", url, actualURL)

	if _, ok := probeJackett(actualURL); !ok {
		t.Errorf("probeJackett did not recognise the started instance at %s", actualURL)
	}
	if err := HealthCheck(actualURL); err != nil {
		t.Errorf("HealthCheck failed: %v", err)
	}

	// Discovery must now find the running instance.
	d := Discover()
	if !d.Running {
		t.Errorf("Discover did not report a running instance (url=%q port=%d)", d.URL, d.Port)
	}
	if d.URL != "" && d.URL != actualURL {
		t.Logf("Discover reported %s, Start reported %s", d.URL, actualURL)
	}

	if err := Stop(dir); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// readPortFrom reads back the port recorded in a Jackett config directory.
func readPortFrom(t *testing.T, dir string) int {
	t.Helper()
	_, cfg := readServerConfig()
	if cfg == nil || cfg.Port == nil {
		t.Fatalf("no ServerConfig.json with a Port found in %s", dir)
	}
	return *cfg.Port
}

// freePort asks the OS for an unused TCP port and releases it immediately.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	port := addr.Port
	if err := l.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

// writeJackettPort writes a ServerConfig.json binding Jackett to port on
// loopback only, keeping the test instance off the network.
func writeJackettPort(t *testing.T, dir string, port int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	content := `{"Port": ` + strconv.Itoa(port) + `, "LocalBindAddress": "127.0.0.1"}`
	if err := os.WriteFile(filepath.Join(dir, "ServerConfig.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write ServerConfig.json: %v", err)
	}
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
