package jackett

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// releaseAPI is the GitHub endpoint used to resolve the latest Jackett build.
const releaseAPI = "https://api.github.com/repos/Jackett/Jackett/releases/latest"

// installTimeout bounds the whole download+extract operation.
const installTimeout = 15 * time.Minute

// InstallResult reports what an install attempt produced.
type InstallResult struct {
	// Installed is true when a Jackett executable was extracted to disk.
	Installed bool
	// Started is true when the process was launched successfully.
	Started bool
	// Dir is the directory Jackett was installed into.
	Dir string
	// Executable is the full path of the launched binary.
	Executable string
	// Version is the release tag that was installed.
	Version string
	// Log is a human-readable trail of the steps performed.
	Log []string
}

// InstallDir returns the directory where a managed Jackett install lives.
//
// It is placed next to the pan-fetcher database so a single data volume carries
// both applications, and so the install survives container restarts.
func InstallDir(dataDir string) string {
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, "jackett")
}

// assetNameForPlatform maps GOOS/GOARCH onto Jackett's release asset names.
//
// Jackett publishes a single macOS build (universal), separate glibc and musl
// Linux builds, and one Windows zip.
func assetNameForPlatform(goos, goarch string) (string, error) {
	switch goos {
	case "windows":
		if goarch != "amd64" {
			return "", fmt.Errorf("Windows 仅支持 amd64（当前 %s）", goarch)
		}
		return "Jackett.Binaries.Windows.zip", nil
	case "darwin":
		if goarch == "arm64" {
			return "Jackett.Binaries.macOSARM64.tar.gz", nil
		}
		return "Jackett.Binaries.macOS.tar.gz", nil
	case "linux":
		// Default to the glibc build; musl can be requested explicitly.
		musl := strings.TrimSpace(os.Getenv("JACKETT_MUSL")) != ""
		switch goarch {
		case "amd64":
			if musl {
				return "Jackett.Binaries.LinuxMuslAMDx64.tar.gz", nil
			}
			return "Jackett.Binaries.LinuxAMDx64.tar.gz", nil
		case "arm64":
			if musl {
				return "Jackett.Binaries.LinuxMuslARM64.tar.gz", nil
			}
			return "Jackett.Binaries.LinuxARM64.tar.gz", nil
		case "arm":
			if musl {
				return "Jackett.Binaries.LinuxMuslARM32.tar.gz", nil
			}
			return "Jackett.Binaries.LinuxARM32.tar.gz", nil
		default:
			return "", fmt.Errorf("Linux 不支持该架构: %s", goarch)
		}
	default:
		return "", fmt.Errorf("不支持的操作系统: %s", goos)
	}
}

// executableName is the Jackett binary name for the current platform.
func executableName(goos string) string {
	if goos == "windows" {
		return "Jackett.exe"
	}
	return "Jackett"
}

// releaseAsset is the subset of the GitHub release payload that is needed.
type releaseAsset struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// fetchLatestRelease resolves the download URL for the current platform.
func fetchLatestRelease(ctx context.Context, client *http.Client) (tag, url string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pan-fetcher")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("查询 Jackett 版本失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("查询 Jackett 版本失败: HTTP %d", resp.StatusCode)
	}

	var rel releaseAsset
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", fmt.Errorf("解析 Jackett 版本信息失败: %w", err)
	}

	want, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", "", err
	}
	for _, a := range rel.Assets {
		if a.Name == want {
			return rel.TagName, a.URL, nil
		}
	}
	return "", "", fmt.Errorf("未找到适配当前平台的 Jackett 安装包: %s", want)
}

// Install downloads and extracts the latest Jackett release into dir.
//
// When force is false an existing installation is reused. The archive is
// downloaded to a temporary file first so a failed transfer cannot leave a
// half-extracted directory behind.
func Install(ctx context.Context, dir string, force bool, logf func(string, ...interface{})) (InstallResult, error) {
	res := InstallResult{Dir: dir}
	logStep := func(format string, args ...interface{}) {
		line := fmt.Sprintf(format, args...)
		res.Log = append(res.Log, line)
		if logf != nil {
			logf("[jackett-install] %s", line)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	exeName := executableName(runtime.GOOS)
	exePath := filepath.Join(dir, exeName)

	// Reuse an existing install unless the caller asked for a refresh.
	if !force {
		if st, err := os.Stat(exePath); err == nil && !st.IsDir() {
			res.Installed = true
			res.Executable = exePath
			logStep("已存在 Jackett 安装: %s", exePath)
			return res, nil
		}
	}

	client := &http.Client{Timeout: 0} // bounded by ctx
	tag, url, err := fetchLatestRelease(ctx, client)
	if err != nil {
		return res, err
	}
	logStep("准备安装 Jackett %s", tag)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("创建安装目录失败: %w", err)
	}

	// Download to a temp file next to the target so the rename is atomic.
	tmp, err := os.CreateTemp(dir, "jackett-download-*")
	if err != nil {
		return res, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		tmp.Close()
		return res, err
	}
	req.Header.Set("User-Agent", "pan-fetcher")

	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return res, fmt.Errorf("下载 Jackett 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return res, fmt.Errorf("下载 Jackett 失败: HTTP %d", resp.StatusCode)
	}

	written, err := io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return res, fmt.Errorf("下载 Jackett 中断: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return res, fmt.Errorf("写入安装包失败: %w", err)
	}
	logStep("已下载 %.1f MB", float64(written)/(1024*1024))

	// Extract by extension: Windows ships a zip, everything else a tar.gz.
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		if err := extractZip(tmpPath, dir); err != nil {
			return res, fmt.Errorf("解压 Jackett 失败: %w", err)
		}
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		if err := extractTarGz(tmpPath, dir); err != nil {
			return res, fmt.Errorf("解压 Jackett 失败: %w", err)
		}
	default:
		return res, fmt.Errorf("未知的安装包格式: %s", url)
	}
	logStep("已解压到 %s", dir)

	// Some archives nest everything under a single top-level directory; lift the
	// contents up so the executable sits directly in dir.
	if err := flattenSingleRoot(dir, exeName); err != nil {
		return res, err
	}

	if st, err := os.Stat(exePath); err != nil || st.IsDir() {
		return res, fmt.Errorf("解压完成但未找到可执行文件: %s", exePath)
	}
	// Ensure the binary is executable even if the archive lost its mode bits.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exePath, 0o755)
	}

	res.Installed = true
	res.Executable = exePath
	res.Version = tag
	logStep("安装完成: %s", exePath)
	return res, nil
}

// flattenSingleRoot moves the contents of a sole top-level subdirectory up into
// dir. Jackett's Linux archives extract into a "Jackett" folder, which would
// otherwise bury the executable one level deeper than expected.
func flattenSingleRoot(dir, exeName string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var dirs, files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "jackett-download-") {
			continue // our own temp file
		}
		if e.IsDir() {
			dirs = append(dirs, name)
		} else {
			files = append(files, name)
		}
	}
	// Nothing to do when the executable is already at the top level.
	for _, f := range files {
		if strings.EqualFold(f, exeName) {
			return nil
		}
	}
	if len(dirs) != 1 {
		return nil
	}

	inner := filepath.Join(dir, dirs[0])
	innerEntries, err := os.ReadDir(inner)
	if err != nil {
		return err
	}
	for _, e := range innerEntries {
		src := filepath.Join(inner, e.Name())
		dst := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // never clobber an existing file
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("整理安装目录失败: %w", err)
		}
	}
	_ = os.Remove(inner)
	return nil
}

// extractTarGz extracts a gzip-compressed tar archive into dir.
func extractTarGz(archivePath, dir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			// Archives occasionally carry relative symlinks; recreate them but
			// never let one escape the install directory.
			if filepath.IsAbs(hdr.Linkname) {
				continue
			}
			if _, err := safeJoin(filepath.Dir(target), hdr.Linkname); err != nil {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
}

// extractZip extracts a zip archive into dir.
func extractZip(archivePath, dir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		target, err := safeJoin(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

// safeJoin joins name onto base, rejecting entries that would escape base.
//
// Traversal is reported as an error rather than silently rewritten: an archive
// containing "../" or absolute paths is malformed or malicious, and failing the
// extraction surfaces that instead of writing files to surprising locations.
func safeJoin(base, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("安装包包含空路径")
	}
	// Normalise separators so Windows and Unix archives behave identically.
	slashed := filepath.ToSlash(name)
	if strings.HasPrefix(slashed, "/") || filepath.IsAbs(name) {
		return "", fmt.Errorf("安装包包含绝对路径: %s", name)
	}
	if len(slashed) >= 2 && slashed[1] == ':' {
		return "", fmt.Errorf("安装包包含盘符路径: %s", name) // e.g. C:\...
	}

	clean := filepath.Clean(filepath.FromSlash(slashed))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("安装包包含非法路径: %s", name)
	}

	target := filepath.Join(base, clean)
	// Belt and braces: confirm the result really is inside base.
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("安装包包含非法路径: %s", name)
	}
	return target, nil
}

// Start launches the Jackett process from dir and waits until it answers.
//
// The process is detached so it keeps running independently of pan-fetcher, and
// its output is appended to jackett.log for troubleshooting.
func Start(ctx context.Context, dir, url string, logf func(string, ...interface{})) error {
	logStep := func(format string, args ...interface{}) {
		if logf != nil {
			logf("[jackett-start] "+format, args...)
		}
	}

	exe := filepath.Join(dir, executableName(runtime.GOOS))
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("未找到 Jackett 可执行文件: %s", exe)
	}

	logPath := filepath.Join(dir, "jackett.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("无法写入日志文件: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(exe)
	cmd.Dir = dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Jackett 失败: %w", err)
	}
	pid := cmd.Process.Pid
	logStep("已启动 Jackett (pid=%d)", pid)
	// Record the PID so Stop can find the process later. Best-effort: failing to
	// persist it only disables the stop button.
	_ = os.WriteFile(filepath.Join(dir, "jackett.pid"), []byte(fmt.Sprintf("%d", pid)), 0o644)
	// Release the child so it is not killed when pan-fetcher exits.
	_ = cmd.Process.Release()

	// Wait for the HTTP endpoint to come up; Jackett needs a few seconds on
	// first run because it initialises its indexer definitions.
	if url == "" {
		url = buildBaseURL("127.0.0.1", DefaultPort)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, ok := probeJackett(url); ok {
			logStep("Jackett 已就绪: %s", url)
			return nil
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return fmt.Errorf("Jackett 已启动但未在 90 秒内就绪，请查看 %s", logPath)
}

// Stop terminates a Jackett process started from dir, when its PID is known.
func Stop(dir string) error {
	pidPath := filepath.Join(dir, "jackett.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("未找到 Jackett PID 记录")
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return fmt.Errorf("PID 记录无效")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil {
		return err
	}
	_ = os.Remove(pidPath)
	return nil
}
