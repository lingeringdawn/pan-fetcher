package jackett

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectEnvOnLinux pins the shape of the report on the platform the project
// supports. The individual values depend on the host, so only the invariants
// are asserted: a distro is identified, a package manager is found on a normal
// distribution, and the derived flags stay consistent with each other.
func TestDetectEnvOnLinux(t *testing.T) {
	dir := t.TempDir()
	rep := DetectEnv(dir)

	if rep.OS != "linux" {
		t.Fatalf("OS = %q, want linux (this project is Linux-only)", rep.OS)
	}
	if rep.Arch == "" {
		t.Error("Arch should be populated")
	}
	if rep.InstallDir != dir {
		t.Errorf("InstallDir = %q, want %q", rep.InstallDir, dir)
	}
	if rep.DiskRequiredBytes == 0 {
		t.Error("DiskRequiredBytes should be set")
	}

	// CanInstall must never claim more than the host can do.
	if rep.CanInstall && rep.PackageManager == "" {
		t.Error("CanInstall is true without a package manager")
	}
	if rep.CanInstall && !rep.IsRoot {
		t.Error("CanInstall is true without root")
	}

	// Supported must agree with Problems: that is the contract callers rely on.
	if rep.Supported() != (len(rep.Problems) == 0) {
		t.Errorf("Supported() disagrees with Problems (%d)", len(rep.Problems))
	}

	// ICU presence must be reflected in Problems, since a missing ICU is the
	// failure this whole check exists to prevent.
	if rep.HasICU {
		for _, p := range rep.Problems {
			if strings.Contains(p, "ICU") {
				t.Errorf("ICU reported present but Problems mentions it: %q", p)
			}
		}
	} else {
		found := false
		for _, p := range rep.Problems {
			if strings.Contains(p, "ICU") {
				found = true
			}
		}
		if !found {
			t.Error("ICU missing but not reported as a problem")
		}
	}
}

// TestDetectEnvWritableDir verifies the writability probe, which gates the
// install: a read-only target must be reported rather than failing mid-download.
func TestDetectEnvWritableDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "jackett")

	rep := DetectEnv(dir)
	if !rep.InstallDirWritable {
		t.Errorf("a fresh path under a writable parent should be writable: %+v", rep)
	}
	// The probe must not leave anything behind.
	if _, err := os.Stat(filepath.Join(dir, ".write-probe")); !os.IsNotExist(err) {
		t.Error("write probe left a file behind")
	}
}

// TestDetectEnvReadOnlyDir checks the negative case.
func TestDetectEnvReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "locked")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	rep := DetectEnv(dir)
	if rep.InstallDirWritable {
		t.Error("a read-only directory must not be reported writable")
	}
	found := false
	for _, p := range rep.Problems {
		if strings.Contains(p, "不可写") {
			found = true
		}
	}
	if !found {
		t.Errorf("read-only dir should be a reported problem, got %v", rep.Problems)
	}
}

// TestDetectEnvDiskSpace verifies free-space detection and that the requirement
// is compared against it rather than assumed.
func TestDetectEnvDiskSpace(t *testing.T) {
	rep := DetectEnv(t.TempDir())
	if rep.DiskFreeBytes == 0 {
		t.Fatal("free space not detected")
	}
	// Any sane test host has more than the required 400 MB; if it does not, the
	// problem list must say so rather than silently passing.
	if rep.DiskFreeBytes < rep.DiskRequiredBytes {
		found := false
		for _, p := range rep.Problems {
			if strings.Contains(p, "磁盘空间不足") {
				found = true
			}
		}
		if !found {
			t.Error("insufficient disk space not reported as a problem")
		}
	}
}

// TestInstallHintMatchesDistro checks that the hint names the package manager
// actually present, so the command can be pasted as-is.
func TestInstallHintMatchesDistro(t *testing.T) {
	cases := []struct {
		rep  EnvReport
		want string
	}{
		{EnvReport{Distro: DistroDebian, PackageManager: "apt-get", ICUPackage: "libicu76"}, "apt-get install -y libicu76"},
		{EnvReport{Distro: DistroRedHat, PackageManager: "dnf", ICUPackage: "libicu"}, "dnf install -y libicu"},
		{EnvReport{Distro: DistroAlpine, PackageManager: "apk", ICUPackage: "icu-libs"}, "apk add --no-cache icu-libs"},
		{EnvReport{Distro: DistroArch, PackageManager: "pacman", ICUPackage: "icu"}, "pacman -S --noconfirm icu"},
		{EnvReport{Distro: DistroSUSE, PackageManager: "zypper", ICUPackage: "libicu"}, "zypper install -y libicu"},
	}
	for _, c := range cases {
		got := InstallHint(c.rep)
		if !strings.Contains(got, c.want) {
			t.Errorf("hint for %s = %q, want it to contain %q", c.rep.Distro, got, c.want)
		}
	}
}

// TestInstallHintWithoutPackageManager still produces an actionable message.
func TestInstallHintWithoutPackageManager(t *testing.T) {
	got := InstallHint(EnvReport{Distro: DistroUnknown, ICUPackage: "libicu"})
	if !strings.Contains(got, "libicu") {
		t.Errorf("hint should name the package even without a manager: %q", got)
	}
}

// TestInstallMissingDepsRefusesWithoutRoot verifies the guard: the installer
// must not attempt to run a package manager it cannot use, and must explain why.
func TestInstallMissingDepsRefusesWithoutRoot(t *testing.T) {
	rep := EnvReport{
		OS:             "linux",
		HasICU:         false,
		PackageManager: "apt-get",
		IsRoot:         false,
		ICUPackage:     "libicu76",
	}
	if _, err := InstallMissingDeps(rep); err == nil {
		t.Fatal("expected a refusal when not root")
	} else if !strings.Contains(err.Error(), "root") {
		t.Errorf("error should mention root, got %q", err)
	}
}

// TestInstallMissingDepsRefusesWithoutPackageManager covers the other guard.
func TestInstallMissingDepsRefusesWithoutPackageManager(t *testing.T) {
	rep := EnvReport{OS: "linux", HasICU: false, IsRoot: true}
	if _, err := InstallMissingDeps(rep); err == nil {
		t.Fatal("expected a refusal without a package manager")
	}
}

// TestInstallMissingDepsNoopWhenSatisfied verifies nothing is run when the host
// is already fine: an unnecessary apt-get update is slow and noisy.
func TestInstallMissingDepsNoopWhenSatisfied(t *testing.T) {
	rep := EnvReport{OS: "linux", HasICU: true, PackageManager: "apt-get", IsRoot: true}
	log, err := InstallMissingDeps(rep)
	if err != nil {
		t.Fatalf("satisfied environment should not error: %v", err)
	}
	if len(log) != 0 {
		t.Errorf("satisfied environment should run nothing, got %v", log)
	}
}

// TestVersionComparison pins the ordering used to decide whether an update is
// available, including the unparsable case which must never claim an update.
func TestVersionComparison(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.24.2830", "v0.24.2829", 1},
		{"v0.24.2829", "v0.24.2830", -1},
		{"v0.24.2830", "v0.24.2830", 0},
		{"0.24.2830", "v0.24.2830", 0},
		{"v0.25.0", "v0.24.9999", 1},
		{"v1.0.0", "v0.99.99", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		sign := 0
		if got > 0 {
			sign = 1
		} else if got < 0 {
			sign = -1
		}
		if sign != c.want {
			t.Errorf("compareVersions(%q, %q) = %d (sign %d), want %d", c.a, c.b, got, sign, c.want)
		}
	}

	// Unparsable input must not be treated as newer.
	if compareVersions("garbage", "v1.0.0") > 0 {
		t.Error("unparsable version must not compare as newer")
	}
}

// TestInstalledVersionMarker verifies the version marker round-trip, which is
// the only reliable source of the installed version (Jackett has no --version).
func TestInstalledVersionMarker(t *testing.T) {
	dir := t.TempDir()

	if got := InstalledVersion(dir); got != "" {
		t.Errorf("no marker should read as empty, got %q", got)
	}
	if err := writeInstalledVersion(dir, "v0.24.2830"); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if got := InstalledVersion(dir); got != "v0.24.2830" {
		t.Errorf("InstalledVersion = %q, want v0.24.2830", got)
	}
	// Trailing whitespace must not leak into the comparison.
	if err := os.WriteFile(filepath.Join(dir, installedVersionFile), []byte("v0.24.2831\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := InstalledVersion(dir); got != "v0.24.2831" {
		t.Errorf("InstalledVersion should trim whitespace, got %q", got)
	}
}

// TestUpdateRefusesForeignDirectory verifies the safety check: Update must not
// rewrite a directory that does not hold our install.
func TestUpdateRefusesForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Update(nil, dir, nil); err == nil {
		t.Fatal("Update should refuse a directory with no Jackett install")
	} else if !strings.Contains(err.Error(), "未在该目录找到") {
		t.Errorf("error should explain the refusal, got %q", err)
	}
}

// TestAssetNameForPlatformMusl verifies that musl hosts get the musl build,
// which is a different asset name and would otherwise fail to run.
func TestAssetNameForPlatformMusl(t *testing.T) {
	t.Setenv("JACKETT_MUSL", "")
	glibc, err := assetNameForPlatform("amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(glibc, "LinuxAMDx64") {
		t.Errorf("glibc asset = %q, expected the non-musl build", glibc)
	}

	t.Setenv("JACKETT_MUSL", "1")
	musl, err := assetNameForPlatform("amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(musl, "Musl") {
		t.Errorf("musl asset = %q, expected the musl build", musl)
	}
	if musl == glibc {
		t.Error("musl and glibc must select different assets")
	}
}

// TestUninstallKeepsDataByDefault is the important safety property: uninstalling
// the program must not silently destroy the user's indexer configuration.
func TestUninstallKeepsDataByDefault(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, executableName())
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeInstalledVersion(dir, "v0.24.2850"); err != nil {
		t.Fatal(err)
	}

	log, err := Uninstall(dir, false)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("install directory should be gone")
	}
	if len(log) == 0 {
		t.Error("Uninstall should report what it did")
	}
	// The data directory is outside dir, so it is untouched either way; assert
	// the log says so, which is what the UI shows the user.
	joined := strings.Join(log, "\n")
	if !strings.Contains(joined, "保留") {
		t.Errorf("log should state the data was kept, got:\n%s", joined)
	}
}

// TestUninstallMissingDirectory verifies a clear error rather than a silent
// success when there is nothing to remove.
func TestUninstallMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	if _, err := Uninstall(dir, false); err == nil {
		t.Fatal("expected an error for a missing install directory")
	} else if !strings.Contains(err.Error(), "未找到") {
		t.Errorf("error should explain the missing directory, got %q", err)
	}
}

// TestUninstallPurgeReportsDataRemoval checks the log distinguishes the two
// modes, since the UI relies on it to tell the user what happened.
func TestUninstallPurgeReportsDataRemoval(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, executableName()), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Point the data directories at a throwaway location so the test never
	// touches the real ~/.config/Jackett.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	dataDir := filepath.Join(fakeHome, ".config", "Jackett")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ServerConfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	log, err := Uninstall(dir, true)
	if err != nil {
		t.Fatalf("Uninstall(purge): %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Error("purge should remove the data directory")
	}
	if !strings.Contains(strings.Join(log, "\n"), "已删除数据目录") {
		t.Errorf("log should record the data removal, got:\n%s", strings.Join(log, "\n"))
	}
}

// TestJackettDataDirsRespectsXDG verifies the uninstall targets the same
// locations the discovery reads, so a purge cannot miss the real config.
func TestJackettDataDirsRespectsXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	dirs := jackettDataDirs()
	if len(dirs) == 0 {
		t.Fatal("no data directories resolved")
	}
	var hasXDG bool
	for _, d := range dirs {
		if strings.HasPrefix(d, filepath.Join(home, "xdg")) {
			hasXDG = true
		}
	}
	if !hasXDG {
		t.Errorf("XDG_CONFIG_HOME location missing from %v", dirs)
	}
	// Duplicates would make the log confusing.
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("duplicate data directory %q", d)
		}
		seen[d] = true
	}
}
