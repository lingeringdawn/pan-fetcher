package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lingeringdawn/pan-fetcher/indexer"
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

// renderIndexersPage executes the dashboard template for the indexers page.
// Execute errors are fatal here: the production handler discards the return
// value, so a mid-template failure leaves a truncated page with no trace.
func renderIndexersPage(t *testing.T, list []indexer.IndexerInfo) string {
	t.Helper()
	s := &Server{Port: 8115}
	data := s.pageData("", "")
	data.Page = "indexers"
	data.IndexerList = list

	var buf bytes.Buffer
	if err := dashboardTemplate.Execute(&buf, data); err != nil {
		t.Fatalf("dashboardTemplate.Execute: %v", err)
	}
	return buf.String()
}

// TestIndexersPageRendersWithActiveList is the regression test for the live
// deployment showing a truncated indexers page.
//
// The Jackett "remove" button lives inside {{range .IndexerList}}, where the
// dot is an IndexerInfo. It referenced {{index .T "remove"}}, but IndexerInfo
// has no T field, so rendering aborted mid-row and everything after the
// button's title attribute was lost — including the whole <script> block that
// defines apiPost, renderJKSetup and the rest. The page therefore stuck on
// "加载中…" and no indexers could be activated.
//
// The same class of bug affected updateBrowseModal, which is also defined
// inside the range.
func TestIndexersPageRendersWithActiveList(t *testing.T) {
	list := []indexer.IndexerInfo{
		{ID: "nyaasi", Name: "Nyaasi", Type: "public", Language: "en-US", SiteLink: "https://nyaa.si/", Enabled: true, Source: "jackett"},
		{ID: "dmhy", Name: "dmhy", Type: "public", Language: "zh-TW", SiteLink: "https://share.dmhy.org/", Enabled: true, Source: "jackett"},
		{ID: "mikan", Name: "Mikan Project", Type: "public", Language: "zh-CN", SiteLink: "https://mikanani.me/", Enabled: true, Source: "jackett"},
	}
	out := renderIndexersPage(t, list)

	// The page must reach its end, not stop part-way through a table row.
	if !strings.HasSuffix(strings.TrimSpace(out), "</html>") {
		t.Fatalf("page is truncated (%d bytes); it must end with </html>", len(out))
	}

	for _, mark := range []string{
		"apiPost",             // the script block after the table
		"renderJKSetup",       // setup panel renderer
		"Jackett 索引器库",      // the async library panel
		"showJKAddModal",      // add-to-library modal
		"jk-setup-body",       // setup panel target
		"updateBrowseModal",   // also defined inside the range
		"jkDeactivate('dmhy')", // the button that used to abort rendering
	} {
		if !strings.Contains(out, mark) {
			t.Errorf("rendered page is missing %q", mark)
		}
	}
}

// TestIndexersPageRendersEmptyList guards the other branch of the same block.
func TestIndexersPageRendersEmptyList(t *testing.T) {
	out := renderIndexersPage(t, nil)

	if !strings.HasSuffix(strings.TrimSpace(out), "</html>") {
		t.Fatalf("page is truncated (%d bytes)", len(out))
	}
	for _, mark := range []string{"apiPost", "renderJKSetup", "Jackett 索引器库"} {
		if !strings.Contains(out, mark) {
			t.Errorf("rendered page is missing %q", mark)
		}
	}
}

// TestPasswordLoadsAfterWorkingDirectoryChange is the regression test for the
// panel coming back with no password after a self-update.
//
// LoadPersistedPassword ran before the data directory was anchored, so it
// resolved web-settings.json against the process working directory. A
// self-update restart changes that directory to the binary's folder, the file
// was not found there, and the server started with an empty password — silently
// disabling authentication for the whole panel.
func TestPasswordLoadsAfterWorkingDirectoryChange(t *testing.T) {
	dataDir := t.TempDir()
	exeDir := t.TempDir()

	// The password lives beside the database, as it does in a real deployment.
	settings := `{"web_password":"s3cret"}`
	if err := os.WriteFile(filepath.Join(dataDir, "web-settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	// Simulate the post-restart working directory.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(exeDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Restore global state for other tests.
	savedDir, savedPw := dataDir, webPassword
	t.Cleanup(func() { dataDir = savedDir; webPassword = savedPw })

	webPassword = ""
	InitDataDir(filepath.Join(dataDir, "db.sqlite"))
	LoadPersistedPassword()

	if webPassword != "s3cret" {
		t.Fatalf("password not restored from the data dir: got %q", webPassword)
	}
}

// TestInitDataDirResolvesSidecars verifies InitDataDir re-anchors the
// package-level paths, so callers that run before New() still address the right
// files.
func TestInitDataDirResolvesSidecars(t *testing.T) {
	dir := t.TempDir()

	savedDir, savedDedup, savedRSS, savedSearch := dataDir, dedupCachePath, rssJsonPath, searchCacheFile
	t.Cleanup(func() {
		dataDir, dedupCachePath, rssJsonPath, searchCacheFile = savedDir, savedDedup, savedRSS, savedSearch
	})

	got := InitDataDir(filepath.Join(dir, "db.sqlite"))
	if got != dir {
		t.Errorf("InitDataDir returned %q, want %q", got, dir)
	}
	for name, path := range map[string]string{
		"dedupCachePath":  dedupCachePath,
		"rssJsonPath":     rssJsonPath,
		"searchCacheFile": searchCacheFile,
	} {
		if filepath.Dir(path) != dir {
			t.Errorf("%s = %q, want it under %q", name, path, dir)
		}
	}
}

// TestInitDataDirDefaults covers an unset database path, which happens when a
// bare `pan-fetcher server` runs with no config file.
func TestInitDataDirDefaults(t *testing.T) {
	savedDir := dataDir
	t.Cleanup(func() { dataDir = savedDir })

	if got := InitDataDir(""); got == "" {
		t.Error("InitDataDir(\"\") should fall back to a usable directory")
	}
}

// TestLogPollDoesNotDuplicateOrReorder covers the log panel's incremental
// refresh.
//
// The panel shows newest first, so the cursor is the FIRST rendered line. Two
// earlier bugs are pinned here:
//
//   - The cursor was seeded from the wrong end, so ?since= resolved near the
//     tail of the buffer and every poll re-added the whole history.
//   - The client appended each block, which put new lines at the bottom and
//     produced a jumbled order once the page was scrolled.
//
// The delta must contain exactly the new lines, newest first, so the client can
// prepend it verbatim.
func TestLogPollDoesNotDuplicateOrReorder(t *testing.T) {
	lb := newLogBuffer(500)
	for i := 1; i <= 10; i++ {
		lb.Write([]byte(fmt.Sprintf("2026/10/14 13:%02d:00 msg-%d", i, i)))
	}

	// What the page renders: newest first.
	rendered := lb.Lines()
	if rendered[0] != "2026/10/14 13:10:00 msg-10" {
		t.Fatalf("page should render newest-first, got %q", rendered[0])
	}

	// The cursor the client seeds from the rendered panel: first line = newest.
	cursor := rendered[0]

	// Two new lines arrive.
	lb.Write([]byte("2026/10/14 13:11:00 msg-11"))
	lb.Write([]byte("2026/10/14 13:12:00 msg-12"))

	// Recompute what /api/logs returns for that cursor.
	all := lb.Lines()
	idx := -1
	for i, l := range all {
		if l == cursor {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("cursor not found; the endpoint would signal a reset")
	}
	delta := all[:idx]

	if len(delta) != 2 {
		t.Fatalf("expected exactly the 2 new lines, got %d: %v", len(delta), delta)
	}
	// Newest first, matching the panel's order.
	if delta[0] != "2026/10/14 13:12:00 msg-12" || delta[1] != "2026/10/14 13:11:00 msg-11" {
		t.Errorf("delta not newest-first: %v", delta)
	}
}

// TestLogAPIReturnsNewestFirst pins the endpoint ordering the panel relies on.
func TestLogAPIReturnsNewestFirst(t *testing.T) {
	s := &Server{Port: 8115}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	logBuf = newLogBuffer(50)
	for i := 1; i <= 5; i++ {
		logBuf.Write([]byte(fmt.Sprintf("line-%d", i)))
	}

	req := httptest.NewRequest("GET", "/api/logs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(resp.Lines))
	}
	for i, want := range []string{"line-5", "line-4", "line-3", "line-2", "line-1"} {
		if resp.Lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, resp.Lines[i], want)
		}
	}
}

// TestLogAPIIncrementalAndReset covers both incremental and reset responses.
func TestLogAPIIncrementalAndReset(t *testing.T) {
	s := &Server{Port: 8115}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	logBuf = newLogBuffer(50)
	logBuf.Write([]byte("alpha"))
	logBuf.Write([]byte("beta"))

	// Cursor on the newest line: nothing new, and crucially no duplicate.
	req := httptest.NewRequest("GET", "/api/logs?since=beta", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp struct {
		Lines []string `json:"lines"`
		Reset bool     `json:"reset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Reset {
		t.Error("a found cursor must not request a reset")
	}
	if len(resp.Lines) != 0 {
		t.Errorf("expected no new lines, got %v", resp.Lines)
	}

	// A line arrives; the delta is just that line.
	logBuf.Write([]byte("gamma"))
	req = httptest.NewRequest("GET", "/api/logs?since=beta", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0] != "gamma" {
		t.Errorf("expected [gamma], got %v", resp.Lines)
	}

	// An unknown cursor (scrolled out of the ring buffer) asks for a reset, so
	// the client replaces its content instead of prepending a full copy.
	req = httptest.NewRequest("GET", "/api/logs?since=long-gone", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Reset {
		t.Error("an unknown cursor must set reset=true")
	}
	if len(resp.Lines) != 3 {
		t.Errorf("reset should carry the full log, got %v", resp.Lines)
	}
}
