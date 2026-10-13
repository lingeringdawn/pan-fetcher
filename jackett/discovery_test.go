package jackett

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAssetNameForPlatform covers the Linux-only asset mapping.
func TestAssetNameForPlatform(t *testing.T) {
	cases := []struct {
		goarch  string
		want    string
		wantErr bool
	}{
		{"amd64", "Jackett.Binaries.LinuxAMDx64.tar.gz", false},
		{"arm64", "Jackett.Binaries.LinuxARM64.tar.gz", false},
		{"arm", "Jackett.Binaries.LinuxARM32.tar.gz", false},
		{"mips", "", true},
		{"386", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := assetNameForPlatform(tc.goarch)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %q", tc.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.goarch, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.goarch, got, tc.want)
		}
	}
}

// TestAssetNameMusl verifies the musl opt-in selects the musl builds, which is
// what Alpine-based containers need.
func TestAssetNameMusl(t *testing.T) {
	t.Setenv("JACKETT_MUSL", "1")
	got, err := assetNameForPlatform("amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "Jackett.Binaries.LinuxMuslAMDx64.tar.gz"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildBaseURL(t *testing.T) {
	cases := []struct {
		host string
		port int
		want string
	}{
		{"127.0.0.1", 9117, "http://127.0.0.1:9117"},
		{"0.0.0.0", 9117, "http://127.0.0.1:9117"},   // wildcard is not diallable
		{"", 9117, "http://127.0.0.1:9117"},          // empty falls back to loopback
		{"*", 9117, "http://127.0.0.1:9117"},         // bind-all wildcard
		{"localhost", 0, "http://localhost:9117"},    // zero port uses the default
		{"http://1.2.3.4", 8080, "http://1.2.3.4:8080"}, // scheme is stripped
	}
	for _, tc := range cases {
		if got := buildBaseURL(tc.host, tc.port); got != tc.want {
			t.Errorf("buildBaseURL(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestPortFromURL(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"http://127.0.0.1:9117", 9117},
		{"http://127.0.0.1:9117/", 9117},
		{"http://localhost:8080/ui", 8080},
		{"not a url", 0},
		{"http://noport.example.com", 0},
	}
	for _, tc := range cases {
		if got := portFromURL(tc.raw); got != tc.want {
			t.Errorf("portFromURL(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestParseServerConfig(t *testing.T) {
	// A realistic ServerConfig.json: Port is null until the user customises it.
	data := []byte(`{
  "Port": null,
  "LocalBindAddress": "0.0.0.0",
  "APIKey": "abc123def456",
  "AdminPassword": "secret"
}`)
	cfg, err := parseServerConfig(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.APIKey != "abc123def456" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.AdminPassword != "secret" {
		t.Errorf("AdminPassword = %q", cfg.AdminPassword)
	}
	if cfg.Port != nil {
		t.Errorf("Port should be nil, got %v", *cfg.Port)
	}
	if cfg.LocalBindAddress != "0.0.0.0" {
		t.Errorf("LocalBindAddress = %q", cfg.LocalBindAddress)
	}
}

func TestParseServerConfigExplicitPort(t *testing.T) {
	cfg, err := parseServerConfig([]byte(`{"Port": 8080, "APIKey": "k"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Port == nil || *cfg.Port != 8080 {
		t.Fatalf("Port = %v, want 8080", cfg.Port)
	}
}

// TestParseServerConfigBOM covers editors that write a UTF-8 BOM, which would
// otherwise make encoding/json reject the file.
func TestParseServerConfigBOM(t *testing.T) {
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"APIKey":"bomkey"}`)...)
	cfg, err := parseServerConfig(withBOM)
	if err != nil {
		t.Fatalf("parse with BOM: %v", err)
	}
	if cfg.APIKey != "bomkey" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "bomkey")
	}
}

func TestParseServerConfigInvalid(t *testing.T) {
	if _, err := parseServerConfig([]byte(`{not json`)); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestExtractVersion(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`<html>Jackett v0.24.2830</html>`, "0.24.2830"},
		{`Jackett Version: v0.21.1`, "0.21.1"},
		{`<html>Jackett</html>`, ""},
		{``, ""},
	}
	for _, tc := range cases {
		if got := extractVersion(tc.body); got != tc.want {
			t.Errorf("extractVersion(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

// TestExecutableName pins the launcher name inside the Linux archive. Jackett
// ships a lowercase "jackett" beside its managed DLLs — there is no capitalised
// binary, which is easy to get wrong.
func TestExecutableName(t *testing.T) {
	if got := executableName(); got != "jackett" {
		t.Errorf("executableName() = %q, want %q", got, "jackett")
	}
}

func TestInstallDir(t *testing.T) {
	got := InstallDir(filepath.Join("data", "db.sqlite"))
	want := filepath.Join("data", "db.sqlite", "jackett")
	if got != want {
		t.Errorf("InstallDir = %q, want %q", got, want)
	}
	// An empty path must still yield a usable relative directory.
	if got := InstallDir(""); got != filepath.Join(".", "jackett") {
		t.Errorf("InstallDir(\"\") = %q", got)
	}
}

// ---------- archive extraction ----------

// TestSafeJoinRejectsTraversal is the security-critical case: a malicious or
// corrupt archive must not be able to write outside the install directory.
func TestSafeJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()

	bad := []string{
		"../escape.txt",
		"../../escape.txt",
		"a/../../escape.txt",
		"/absolute/path.txt",
	}
	for _, name := range bad {
		if got, err := safeJoin(base, name); err == nil {
			t.Errorf("safeJoin(%q) allowed traversal, returned %q", name, got)
		}
	}

	good := []string{"file.txt", "sub/file.txt", "./file.txt", "a/b/c.txt"}
	for _, name := range good {
		got, err := safeJoin(base, name)
		if err != nil {
			t.Errorf("safeJoin(%q) unexpected error: %v", name, err)
			continue
		}
		rel, err := filepath.Rel(base, got)
		if err != nil || rel == ".." || filepath.IsAbs(rel) {
			t.Errorf("safeJoin(%q) escaped base: %q", name, got)
		}
	}
}

// TestFlattenSingleRoot covers Jackett's Linux archives, which nest everything
// under a top-level "Jackett" directory.
func TestFlattenSingleRoot(t *testing.T) {
	base := t.TempDir()
	inner := filepath.Join(base, "Jackett")
	if err := os.MkdirAll(filepath.Join(inner, "Content"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := executableName()
	if err := os.WriteFile(filepath.Join(inner, exe), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "Content", "x.dat"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := flattenSingleRoot(base, exe); err != nil {
		t.Fatalf("flattenSingleRoot: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, exe)); err != nil {
		t.Errorf("executable not lifted to top level: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "Content", "x.dat")); err != nil {
		t.Errorf("subdirectory not lifted: %v", err)
	}
}

// TestFlattenSingleRootNoopWhenExeAtTop verifies an already-flat archive is left
// alone rather than being reorganised.
func TestFlattenSingleRootNoopWhenExeAtTop(t *testing.T) {
	base := t.TempDir()
	exe := executableName()
	if err := os.WriteFile(filepath.Join(base, exe), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray sibling directory must not trigger a move.
	if err := os.MkdirAll(filepath.Join(base, "Content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "Content", "y.dat"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := flattenSingleRoot(base, exe); err != nil {
		t.Fatalf("flattenSingleRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "Content", "y.dat")); err != nil {
		t.Errorf("existing layout was disturbed: %v", err)
	}
}

// TestInstallReusesExisting covers the idempotency guarantee: Install with
// force=false must not re-download when the executable is already present.
func TestInstallReusesExisting(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, executableName())
	if err := os.WriteFile(exe, []byte("existing"), 0o755); err != nil {
		t.Fatal(err)
	}

	// No network access is available in tests; reaching the download step would
	// fail, so a successful return proves the early-exit path was taken.
	res, err := Install(t.Context(), dir, false, nil)
	if err != nil {
		t.Fatalf("Install should reuse existing binary: %v", err)
	}
	if !res.Installed {
		t.Error("expected Installed=true")
	}
	if res.Executable != exe {
		t.Errorf("Executable = %q, want %q", res.Executable, exe)
	}
}

// ---------- discovery ----------

// TestDiscoverEnvOverride verifies explicit environment variables win over
// on-disk configuration.
func TestDiscoverEnvOverride(t *testing.T) {
	t.Setenv("JACKETT_URL", "http://example.invalid:1234")
	t.Setenv("JACKETT_APIKEY", "envkey")

	d := Discover()
	if d.URL != "http://example.invalid:1234" {
		t.Errorf("URL = %q, want the env value", d.URL)
	}
	if d.APIKey != "envkey" {
		t.Errorf("APIKey = %q, want %q", d.APIKey, "envkey")
	}
	if !d.Found {
		t.Error("Found should be true when env vars are set")
	}
	if d.Source != "env" {
		t.Errorf("Source = %q, want %q", d.Source, "env")
	}
}

// TestDiscoverNoInstance makes sure a bare machine yields a clean negative
// rather than an error or a bogus URL.
func TestDiscoverNoInstance(t *testing.T) {
	// Point the config search at an empty directory by clearing the usual vars
	// and using a temp HOME.
	t.Setenv("JACKETT_URL", "")
	t.Setenv("JACKETT_APIKEY", "")
	t.Setenv("JACKETT_PORT", "")
	t.Setenv("ProgramData", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	d := Discover()
	// Nothing listens on the default port in the test environment, so a
	// discovered-and-running instance would be a genuine surprise.
	if d.Running {
		t.Errorf("unexpectedly found a running Jackett at %q", d.URL)
	}
}

// TestHealthCheckEmptyURL guards the validation entry point used by the UI.
func TestHealthCheckEmptyURL(t *testing.T) {
	if err := HealthCheck(""); err == nil {
		t.Error("expected error for empty URL")
	}
	if err := HealthCheck("   "); err == nil {
		t.Error("expected error for whitespace-only URL")
	}
}

// TestHealthCheckUnreachable ensures a dead address reports an error instead of
// hanging or claiming success.
func TestHealthCheckUnreachable(t *testing.T) {
	// Port 1 is reserved and never hosts Jackett.
	if err := HealthCheck("http://127.0.0.1:1"); err == nil {
		t.Error("expected error for unreachable address")
	}
}
