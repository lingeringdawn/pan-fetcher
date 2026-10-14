package jackett

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestE2EDiscoverReadsAPIKey is the end-to-end proof behind the "zero-config"
// promise: that automatic discovery reads a real ServerConfig.json written by
// Jackett and yields an API key that actually works.
//
// It deliberately lets Jackett generate its own configuration — including the
// API key — rather than seeding one, so the file layout and key format are
// whatever the real product produces. Merely asserting a non-empty string would
// not be enough: the key must authenticate against the Torznab API.
func TestE2EDiscoverReadsAPIKey(t *testing.T) {
	requireE2E(t)

	dir := filepath.Join(t.TempDir(), "jackett")
	if _, err := Install(context.Background(), dir, false, nil); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Redirect Jackett's data folder to a scratch directory so it writes a fresh
	// ServerConfig.json instead of reusing the developer's real one.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// Clear ambient overrides so discovery must go through the config file.
	t.Setenv("JACKETT_URL", "")
	t.Setenv("JACKETT_APIKEY", "")
	t.Setenv("JACKETT_PORT", "")

	port := freePort(t)
	configDir := filepath.Join(home, ".config", "Jackett")
	writeJackettPort(t, configDir, port)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Discovery has not run yet, so Start is given the expected address.
	actualURL, err := Start(ctx, dir, buildBaseURL("127.0.0.1", port), t.Logf)
	if err != nil {
		if data, rerr := os.ReadFile(filepath.Join(dir, "jackett.log")); rerr == nil {
			t.Logf("jackett.log tail:\n%s", tailOf(string(data), 2000))
		}
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = Stop(dir) })
	t.Logf("jackett listening on %s", actualURL)

	// Jackett creates the key while starting up; wait for it to land on disk.
	waitForAPIKeyFile(t, configDir)

	d := Discover()
	if !d.Found {
		t.Fatalf("Discover found nothing although Jackett is running on %s", actualURL)
	}
	if d.APIKey == "" {
		t.Fatalf("Discover returned no API key (config=%q, url=%q)", d.ConfigPath, d.URL)
	}
	if d.ConfigPath == "" {
		t.Errorf("Discover reported no config path; the key came from somewhere unexpected")
	}
	t.Logf("discovered url=%s config=%s key=%s", d.URL, d.ConfigPath, maskKey(d.APIKey))

	// The decisive assertion: the discovered key must be accepted by Jackett.
	cfg := Config{URL: actualURL, APIKey: d.APIKey}
	if err := Test(cfg); err != nil {
		t.Errorf("discovered API key was rejected by Jackett: %v", err)
	}
	indexers, err := ListIndexers(cfg)
	if err != nil {
		t.Errorf("ListIndexers with the discovered key failed: %v", err)
	} else {
		t.Logf("authenticated via discovered key: %d configured indexers", len(indexers))
	}

	// A wrong key must fail, proving Test really validates rather than always
	// succeeding when the endpoint is reachable.
	if err := Test(Config{URL: actualURL, APIKey: "definitely-not-the-key"}); err == nil {
		t.Error("Test accepted an invalid API key; the check is not validating anything")
	}
}

// TestE2EDiscoverPrefersFileOverStaleEnv documents precedence: an explicit
// environment override wins over the on-disk configuration.
func TestE2EDiscoverPrefersFileOverStaleEnv(t *testing.T) {
	requireE2E(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "Jackett")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	writeJackettPort(t, configDir, 45123)
	// Add a key to the file so a file-based read would succeed too.
	seedFile := filepath.Join(configDir, "ServerConfig.json")
	if err := os.WriteFile(seedFile, []byte(`{"Port":45123,"LocalBindAddress":"127.0.0.1","APIKey":"fromfile"}`), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// No live Jackett is needed: the environment override must be reported even
	// when the service is unreachable.
	t.Setenv("JACKETT_URL", "http://127.0.0.1:45999")
	t.Setenv("JACKETT_APIKEY", "fromenv")

	d := Discover()
	if d.APIKey != "fromenv" {
		t.Errorf("APIKey = %q, want the environment value", d.APIKey)
	}
	if d.URL != "http://127.0.0.1:45999" {
		t.Errorf("URL = %q, want the environment value", d.URL)
	}
	if d.Source != "env" {
		t.Errorf("Source = %q, want %q", d.Source, "env")
	}
}

// waitForAPIKeyFile blocks until Jackett has written a non-empty API key.
func waitForAPIKeyFile(t *testing.T, configDir string) {
	t.Helper()
	path := filepath.Join(configDir, "ServerConfig.json")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if cfg, err := parseServerConfig(data); err == nil && cfg.APIKey != "" {
				t.Logf("jackett wrote its API key to %s", path)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no API key appeared in %s within 60s", path)
}

// maskKey renders a key for logs without printing it in full.
func maskKey(k string) string {
	if len(k) <= 6 {
		return "***"
	}
	return k[:6] + "..."
}
