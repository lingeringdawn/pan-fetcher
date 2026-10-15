// Update support for the managed Jackett install.
//
// Jackett keeps its own auto-update, but that is disabled for a managed install
// (pan-fetcher passes --NoUpdates): letting a self-updating process rewrite the
// tree we manage would race with our own install and start logic. The update is
// therefore driven from here, where it can be sequenced and reported.
package jackett

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// UpdateStatus reports the installed version against the latest release.
type UpdateStatus struct {
	// Installed is the version currently on disk, empty when unknown.
	Installed string `json:"installed"`
	// Latest is the newest release tag, empty when it could not be fetched.
	Latest string `json:"latest"`
	// HasUpdate is true when Latest is newer than Installed.
	HasUpdate bool `json:"has_update"`
	// Asset is the release asset that would be downloaded.
	Asset string `json:"asset,omitempty"`
	// URL is the download URL for that asset.
	URL string `json:"url,omitempty"`
	// Dir is the install directory inspected.
	Dir string `json:"dir"`
	// InstalledAt is the mtime of the launcher, i.e. when it was installed.
	InstalledAt time.Time `json:"installed_at,omitempty"`
	// Error carries a non-fatal reason the check could not complete.
	Error string `json:"error,omitempty"`
}

// versionRe matches a semantic version inside a release tag.
var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// parseVersionNumbers extracts the numeric components of a version string.
func parseVersionNumbers(s string) []int {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	out := make([]int, 0, 3)
	for _, part := range m[1:] {
		n := 0
		for _, c := range part {
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

// compareVersions returns positive when a is newer than b.
func compareVersions(a, b string) int {
	va, vb := parseVersionNumbers(a), parseVersionNumbers(b)
	if len(va) == 0 || len(vb) == 0 {
		// Unparsable input: fall back to inequality so we never claim an
		// update exists based on a guess.
		if a == b {
			return 0
		}
		return 0
	}
	for i := 0; i < len(va) && i < len(vb); i++ {
		if va[i] != vb[i] {
			return va[i] - vb[i]
		}
	}
	return 0
}

// InstalledVersion reports the version of the Jackett in dir.
//
// Jackett has no --version flag, so the release tag is recovered from the
// files on disk. The install writes a marker file recording what was installed;
// that is authoritative. When it is absent (an install made by other means) the
// version is derived from the launcher's mtime, which is only a lower bound and
// is reported as empty rather than guessed.
func InstalledVersion(dir string) string {
	marker := filepath.Join(dir, installedVersionFile)
	if data, err := os.ReadFile(marker); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v
		}
	}
	return ""
}

// installedVersionFile records the release tag a managed install came from.
const installedVersionFile = ".pan-fetcher-jackett-version"

// writeInstalledVersion records the installed tag. Best-effort: a failure only
// costs us the version display, so it is not treated as an install failure.
func writeInstalledVersion(dir, tag string) error {
	return os.WriteFile(filepath.Join(dir, installedVersionFile), []byte(tag+"\n"), 0o644)
}

// CheckUpdate compares the installed Jackett against the latest release.
//
// A failure to reach GitHub is reported in Error rather than as an error: the
// caller usually wants to render the rest of the panel regardless.
func CheckUpdate(ctx context.Context, dir string) UpdateStatus {
	st := UpdateStatus{Dir: dir, Installed: InstalledVersion(dir)}

	if launcher := filepath.Join(dir, executableName()); launcher != "" {
		if fi, err := os.Stat(launcher); err == nil {
			st.InstalledAt = fi.ModTime()
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 0} // bounded by ctx
	tag, url, err := fetchLatestRelease(ctx, client)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Latest = tag
	st.URL = url

	asset, aerr := assetNameForPlatform(platformArch())
	if aerr == nil {
		st.Asset = asset
	}

	// An install with no recorded version cannot be compared. Treat a missing
	// marker as "unknown" and report an update as available so the user can
	// refresh to a known state, which is harmless and self-correcting.
	if st.Installed == "" {
		st.HasUpdate = true
		return st
	}
	st.HasUpdate = compareVersions(st.Latest, st.Installed) > 0
	return st
}

// Update replaces the managed install with the latest release.
//
// It reuses Install with force=true, which downloads to a temporary file and
// only then swaps the tree, so a failed download leaves the working install
// untouched. The caller is responsible for stopping the running instance first.
func Update(ctx context.Context, dir string, logf func(string, ...interface{})) (InstallResult, error) {
	// Refuse to touch a directory that does not look like our install: the
	// caller may have been pointed somewhere unintended.
	if _, err := os.Stat(filepath.Join(dir, executableName())); err != nil {
		return InstallResult{}, fmt.Errorf("未在该目录找到 Jackett 安装: %s", dir)
	}
	return Install(ctx, dir, true, logf)
}

// fetchReleaseByTag returns the download URL of a specific release's asset.
func fetchReleaseByTag(ctx context.Context, client *http.Client, tag string) (string, error) {
	u := "https://api.github.com/repos/Jackett/Jackett/releases/tags/" + tag
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("查询 Jackett %s: %w", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("查询 Jackett %s: HTTP %d %s", tag, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rel releaseAsset
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("解析 Jackett %s 发布信息失败: %w", tag, err)
	}
	want, err := assetNameForPlatform(platformArch())
	if err != nil {
		return "", err
	}
	for _, a := range rel.Assets {
		if a.Name == want {
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("Jackett %s 未提供适配当前平台的安装包: %s", tag, want)
}
