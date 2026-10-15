// Environment detection for the managed Jackett install.
//
// Jackett ships a self-contained .NET build, but it is not dependency-free: the
// official Linux archive needs the ICU libraries at runtime. When they are
// missing the process aborts during CultureInfo initialisation, which surfaces
// as an unexplained immediate exit (see crashHint). Rather than let the user hit
// that wall, the environment is inspected up front and the missing pieces are
// reported with the exact install command for the detected distribution.
package jackett

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Distro identifies the Linux distribution family, which determines package
// names and the package manager used for the install hints.
type Distro string

const (
	DistroDebian  Distro = "debian"  // Debian, Ubuntu, Raspberry Pi OS
	DistroRedHat  Distro = "redhat"  // RHEL, CentOS, Fedora, Rocky, Alma
	DistroAlpine  Distro = "alpine"  // Alpine (musl)
	DistroArch    Distro = "arch"    // Arch, Manjaro
	DistroSUSE    Distro = "suse"    // openSUSE, SLES
	DistroUnknown Distro = "unknown" // anything else
)

// EnvReport describes what the host provides for running Jackett.
type EnvReport struct {
	// OS is runtime.GOOS.
	OS string `json:"os"`
	// Arch is runtime.GOARCH.
	Arch string `json:"arch"`
	// Distro is the detected distribution family.
	Distro Distro `json:"distro"`
	// DistroName is the human-readable name from /etc/os-release, e.g.
	// "Debian GNU/Linux 13 (trixie)".
	DistroName string `json:"distro_name"`
	// Musl reports a musl libc (Alpine), which selects a different Jackett
	// build and a different set of package names.
	Musl bool `json:"musl"`
	// GlibcVersion is the detected glibc version, empty when not glibc.
	GlibcVersion string `json:"glibc_version"`
	// HasICU reports whether the ICU libraries .NET needs are present.
	HasICU bool `json:"has_icu"`
	// ICUPackage is the package that would provide ICU here, for the hint.
	ICUPackage string `json:"icu_package"`
	// PackageManager is the detected manager (apt-get, dnf, apk, ...).
	PackageManager string `json:"package_manager"`
	// CanInstall reports whether we can run the install ourselves: the manager
	// exists and the process has the privileges to use it.
	CanInstall bool `json:"can_install"`
	// IsRoot reports euid 0.
	IsRoot bool `json:"is_root"`
	// InstallDir is where a managed Jackett would live.
	InstallDir string `json:"install_dir"`
	// InstallDirWritable reports whether that path can be created and written.
	InstallDirWritable bool `json:"install_dir_writable"`
	// DiskFreeBytes is the free space on the filesystem holding InstallDir.
	DiskFreeBytes uint64 `json:"disk_free_bytes"`
	// DiskRequiredBytes is what an install needs (archive plus extracted tree).
	DiskRequiredBytes uint64 `json:"disk_required_bytes"`
	// Problems lists blocking issues in plain language.
	Problems []string `json:"problems"`
	// Warnings lists non-blocking notes.
	Warnings []string `json:"warnings"`
}

// Supported reports whether a managed install can run on this host at all.
func (e EnvReport) Supported() bool {
	return len(e.Problems) == 0
}

// diskRequiredBytes is the space an install needs: the ~50 MB archive plus the
// ~200 MB extracted tree, with headroom.
const diskRequiredBytes = 400 << 20

// DetectEnv inspects the host and reports what is needed to run Jackett.
//
// installDir is where a managed install would go; it is used to check
// writability and free space. Pass "" to skip those checks.
func DetectEnv(installDir string) EnvReport {
	rep := EnvReport{
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		InstallDir:        installDir,
		DiskRequiredBytes: diskRequiredBytes,
	}

	if runtime.GOOS != "linux" {
		rep.Problems = append(rep.Problems,
			fmt.Sprintf("仅支持 Linux，当前系统为 %s", runtime.GOOS))
		return rep
	}

	rep.IsRoot = os.Geteuid() == 0
	rep.Musl = detectMusl()
	rep.Distro, rep.DistroName = detectDistro()
	if !rep.Musl {
		rep.GlibcVersion = detectGlibc()
	}
	rep.HasICU = detectICU()
	rep.PackageManager = detectPackageManager(rep.Distro)
	rep.CanInstall = rep.PackageManager != "" && rep.IsRoot
	rep.ICUPackage = icuPackageFor(rep)

	if installDir != "" {
		rep.InstallDirWritable = checkWritable(installDir)
		rep.DiskFreeBytes = freeSpace(installDir)
	}

	// Blocking issues.
	if !rep.HasICU {
		hint := installHint(rep)
		rep.Problems = append(rep.Problems,
			fmt.Sprintf("缺少 ICU 库，Jackett（.NET）启动时会立即退出。%s", hint))
	}
	if installDir != "" && !rep.InstallDirWritable {
		rep.Problems = append(rep.Problems,
			fmt.Sprintf("安装目录不可写：%s。请以 root 运行，或调整该目录权限。", installDir))
	}
	if installDir != "" && rep.DiskFreeBytes > 0 && rep.DiskFreeBytes < diskRequiredBytes {
		rep.Problems = append(rep.Problems,
			fmt.Sprintf("磁盘空间不足：可用 %.0f MB，需要约 %.0f MB。",
				float64(rep.DiskFreeBytes)/(1<<20), float64(diskRequiredBytes)/(1<<20)))
	}

	// Non-blocking notes.
	if rep.Arch != "amd64" && rep.Arch != "arm64" && rep.Arch != "arm" {
		rep.Warnings = append(rep.Warnings,
			fmt.Sprintf("架构 %s 可能没有对应的 Jackett 发行包。", rep.Arch))
	}
	if rep.PackageManager != "" && !rep.IsRoot {
		rep.Warnings = append(rep.Warnings,
			"当前进程不是 root，无法自动安装系统依赖；如需安装请手动执行上面的命令。")
	}
	if rep.Distro == DistroUnknown {
		rep.Warnings = append(rep.Warnings,
			"未能识别发行版，依赖包名请按实际情况调整。")
	}

	return rep
}

// readOSRelease parses /etc/os-release into a key/value map.
func readOSRelease() map[string]string {
	out := map[string]string{}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// detectDistro maps /etc/os-release onto a distribution family.
func detectDistro() (Distro, string) {
	rel := readOSRelease()
	name := rel["PRETTY_NAME"]
	if name == "" {
		name = rel["NAME"]
	}

	// ID_LIKE covers derivatives; ID is authoritative when present.
	keys := strings.ToLower(rel["ID"] + " " + rel["ID_LIKE"])

	switch {
	case strings.Contains(keys, "alpine"):
		return DistroAlpine, name
	case strings.Contains(keys, "debian") || strings.Contains(keys, "ubuntu"):
		return DistroDebian, name
	case strings.Contains(keys, "rhel") || strings.Contains(keys, "fedora") ||
		strings.Contains(keys, "centos") || strings.Contains(keys, "suse"):
		if strings.Contains(keys, "suse") {
			return DistroSUSE, name
		}
		return DistroRedHat, name
	case strings.Contains(keys, "arch"):
		return DistroArch, name
	}

	// Fall back to the package manager actually present.
	switch {
	case hasExecutable("apt-get"):
		return DistroDebian, name
	case hasExecutable("dnf") || hasExecutable("yum"):
		return DistroRedHat, name
	case hasExecutable("apk"):
		return DistroAlpine, name
	case hasExecutable("pacman"):
		return DistroArch, name
	case hasExecutable("zypper"):
		return DistroSUSE, name
	}
	return DistroUnknown, name
}

// detectMusl reports whether the C library is musl rather than glibc.
//
// The check is by file: `ldd --version` prints "musl libc" on Alpine, but ldd
// itself may be absent, so the loader path is the more reliable signal.
func detectMusl() bool {
	for _, p := range []string{
		"/lib/ld-musl-x86_64.so.1",
		"/lib/ld-musl-aarch64.so.1",
		"/lib/ld-musl-armhf.so.1",
		"/lib/libc.musl-x86_64.so.1",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	out, err := exec.Command("ldd", "--version").CombinedOutput()
	if err == nil && strings.Contains(strings.ToLower(string(out)), "musl") {
		return true
	}
	return false
}

// detectGlibc reports the glibc version, or "" when it cannot be determined.
func detectGlibc() string {
	// ldd prints "ldd (Debian GLIBC 2.41-12+deb13u4) 2.41".
	out, err := exec.Command("ldd", "--version").Output()
	if err == nil {
		line := strings.SplitN(string(out), "\n", 2)[0]
		if i := strings.LastIndex(line, ") "); i >= 0 {
			if v := strings.TrimSpace(line[i+2:]); v != "" {
				return v
			}
		}
	}

	// Fall back to the shared object itself, which is versioned by name.
	matches, _ := filepath.Glob("/lib/*/libc.so.6")
	matches2, _ := filepath.Glob("/lib/libc.so.6")
	matches = append(matches, matches2...)
	if len(matches) > 0 {
		if out, err := exec.Command(matches[0]).Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "GNU C Library"); ok {
					// "(stable release version 2.41." style output.
					if i := strings.Index(v, "version "); i >= 0 {
						rest := v[i+len("version "):]
						rest = strings.TrimRight(rest, ".")
						return strings.TrimSpace(rest)
					}
				}
			}
		}
	}
	return ""
}

// detectICU reports whether the ICU libraries .NET needs are loadable.
//
// It checks the dynamic linker cache first (fast, and covers any install path),
// then falls back to looking for the shared objects directly, because a minimal
// container may have the libraries without ldconfig having been run.
func detectICU() bool {
	if hasExecutable("ldconfig") {
		out, err := exec.Command("ldconfig", "-p").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "libicuuc.so") {
					return true
				}
			}
		}
	}
	patterns := []string{
		"/usr/lib/*/libicuuc.so.*",
		"/usr/lib/libicuuc.so.*",
		"/usr/local/lib/libicuuc.so.*",
		"/lib/*/libicuuc.so.*",
	}
	for _, p := range patterns {
		if m, _ := filepath.Glob(p); len(m) > 0 {
			return true
		}
	}
	return false
}

// detectPackageManager returns the install command name for the distribution,
// or "" when none is available.
func detectPackageManager(d Distro) string {
	var candidates []string
	switch d {
	case DistroDebian:
		candidates = []string{"apt-get"}
	case DistroRedHat:
		candidates = []string{"dnf", "yum"}
	case DistroAlpine:
		candidates = []string{"apk"}
	case DistroArch:
		candidates = []string{"pacman"}
	case DistroSUSE:
		candidates = []string{"zypper"}
	default:
		candidates = []string{"apt-get", "dnf", "yum", "apk", "pacman", "zypper"}
	}
	for _, c := range candidates {
		if hasExecutable(c) {
			return c
		}
	}
	return ""
}

// icuPackageFor names the ICU package on this distribution.
//
// The name is versioned on Debian (libicu72, libicu76, ...) so the candidate is
// resolved through the package manager rather than hard-coded; the literal name
// is only a fallback for the hint text.
func icuPackageFor(rep EnvReport) string {
	switch rep.Distro {
	case DistroDebian:
		if p := resolveDebianICU(); p != "" {
			return p
		}
		return "libicu-dev"
	case DistroRedHat:
		return "libicu"
	case DistroAlpine:
		return "icu-libs"
	case DistroArch:
		return "icu"
	case DistroSUSE:
		return "libicu"
	}
	return "libicu"
}

// resolveDebianICU asks apt for the concrete libicu package name, which carries
// a version suffix that changes between releases.
func resolveDebianICU() string {
	if !hasExecutable("apt-cache") {
		return ""
	}
	out, err := exec.Command("apt-cache", "search", "--names-only", "^libicu[0-9]+$").Output()
	if err != nil {
		return ""
	}
	best := ""
	for _, line := range strings.Split(string(out), "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || name == "" {
			continue
		}
		// Prefer the highest version number when several are listed.
		if best == "" || versionSuffix(name) > versionSuffix(best) {
			best = name
		}
	}
	return best
}

// versionSuffix extracts the trailing integer from a name like "libicu76".
func versionSuffix(name string) int {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	n, _ := strconv.Atoi(name[i:])
	return n
}

// InstallHint renders the command that installs the missing packages here.
func InstallHint(rep EnvReport) string {
	return installHint(rep)
}

// installHint renders the command that installs ICU here.
func installHint(rep EnvReport) string {
	pkg := rep.ICUPackage
	switch rep.PackageManager {
	case "apt-get":
		return fmt.Sprintf("安装：apt-get update && apt-get install -y %s", pkg)
	case "dnf":
		return fmt.Sprintf("安装：dnf install -y %s", pkg)
	case "yum":
		return fmt.Sprintf("安装：yum install -y %s", pkg)
	case "apk":
		return fmt.Sprintf("安装：apk add --no-cache %s", pkg)
	case "pacman":
		return fmt.Sprintf("安装：pacman -S --noconfirm %s", pkg)
	case "zypper":
		return fmt.Sprintf("安装：zypper install -y %s", pkg)
	}
	return fmt.Sprintf("请安装提供 libicuuc 的软件包（本机未能识别包管理器），包名通常为 %s。", pkg)
}

// InstallMissingDeps installs the packages Jackett needs, returning the trail of
// commands it ran.
//
// It only acts when the distribution, the package manager and root privileges
// all line up; otherwise it explains what to run by hand. Nothing is installed
// when the environment is already complete.
//
// The guards test HasICU directly rather than relying on Supported(): that
// helper only inspects Problems, so a report built by hand (or one whose
// Problems were not populated) would slip past them.
func InstallMissingDeps(rep EnvReport) (log []string, err error) {
	if rep.HasICU {
		return nil, nil // nothing to do
	}
	if rep.PackageManager == "" {
		return nil, fmt.Errorf("未找到可用的包管理器。%s", installHint(rep))
	}
	if !rep.IsRoot {
		return nil, fmt.Errorf("自动安装需要 root 权限。%s", installHint(rep))
	}

	step := func(format string, args ...interface{}) {
		log = append(log, fmt.Sprintf(format, args...))
	}

	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		out, err := cmd.CombinedOutput()
		if len(out) > 0 {
			step("%s", tailOf(strings.TrimSpace(string(out)), 800))
		}
		if err != nil {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return nil
	}

	switch rep.PackageManager {
	case "apt-get":
		step("刷新软件包索引")
		if err := run("apt-get", "update", "-qq"); err != nil {
			return log, err
		}
		step("安装 %s", rep.ICUPackage)
		if err := run("apt-get", "install", "-y", rep.ICUPackage); err != nil {
			return log, err
		}
	case "dnf", "yum":
		step("安装 %s", rep.ICUPackage)
		if err := run(rep.PackageManager, "install", "-y", rep.ICUPackage); err != nil {
			return log, err
		}
	case "apk":
		step("安装 %s", rep.ICUPackage)
		if err := run("apk", "add", "--no-cache", rep.ICUPackage); err != nil {
			return log, err
		}
	case "pacman":
		step("安装 %s", rep.ICUPackage)
		if err := run("pacman", "-Sy", "--noconfirm", rep.ICUPackage); err != nil {
			return log, err
		}
	case "zypper":
		step("安装 %s", rep.ICUPackage)
		if err := run("zypper", "--non-interactive", "install", rep.ICUPackage); err != nil {
			return log, err
		}
	default:
		return log, fmt.Errorf("不支持的包管理器 %s。%s", rep.PackageManager, installHint(rep))
	}

	step("依赖安装完成")
	return log, nil
}

// hasExecutable reports whether name resolves on PATH.
func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// checkWritable reports whether dir can be created and written to.
func checkWritable(dir string) bool {
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

// freeSpace returns the free bytes on the filesystem holding path.
//
// The path may not exist yet, so it walks up to the nearest existing parent.
func freeSpace(path string) uint64 {
	dir := path
	for dir != "" && dir != "/" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			break
		}
		dir = filepath.Dir(dir)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

// tailOf returns the last n bytes of s, prefixed with an ellipsis when
// truncated. Package output can be long, and only the end carries the error.
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
